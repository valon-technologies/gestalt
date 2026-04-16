package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
)

const (
	DefaultBaseURL      = "https://api.github.com"
	headerAccept        = "Accept"
	headerAuthorization = "Authorization"
	acceptOctetStream   = "application/octet-stream"
	authTokenPrefix     = "token "
	platformAssetPrefix = "gestalt-plugin-"
)

type platformAssetMatchKind int

const (
	noPlatformAssetMatch platformAssetMatchKind = iota
	pluginOnlyPlatformAssetMatch
	versionedPlatformAssetMatch
)

var (
	platformSeparators = []string{"_", "-", "."}
	platformOSAliases  = map[string][]string{
		"darwin":  {"darwin", "macos"},
		"linux":   {"linux"},
		"windows": {"windows", "win32"},
	}
	platformArchAliases = map[string][]string{
		"386":   {"386", "x86"},
		"amd64": {"amd64", "x86_64"},
		"arm":   {"arm", "armv7"},
		"arm64": {"arm64", "aarch64"},
	}
)

type releaseResponse struct {
	Assets []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type GitHubResolver struct {
	BaseURL    string
	HTTPClient *http.Client
}

func resolveGitHubToken(token string) string {
	for _, candidate := range []string{
		strings.TrimSpace(token),
		strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		strings.TrimSpace(os.Getenv("GH_TOKEN")),
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (r *GitHubResolver) Resolve(ctx context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	token := resolveGitHubToken(src.Token)

	tag := src.ReleaseTag(version)
	releaseURL := fmt.Sprintf("%s/repos/%s/releases/tags/%s", baseURL, src.RepoSlug(), url.PathEscape(tag))

	release, err := r.fetchRelease(ctx, client, releaseURL, token, tag, src.RepoSlug())
	if err != nil {
		return nil, err
	}

	asset, err := findAsset(release.Assets, src.PluginName(), version)
	if err != nil {
		return nil, err
	}

	dl, err := r.downloadAsset(ctx, client, asset.URL, token)
	if err != nil {
		return nil, err
	}

	return &pluginsource.ResolvedPackage{
		LocalPath:     dl.LocalPath,
		Cleanup:       dl.Cleanup,
		ArchiveSHA256: dl.SHA256Hex,
		ResolvedURL:   resolvedAssetURL(asset),
	}, nil
}

// ListPlatformArchives discovers all platform-specific archives in the release
// without downloading any of them. It implements pluginsource.PlatformEnumerator.
func (r *GitHubResolver) ListPlatformArchives(ctx context.Context, src pluginsource.Source, version string) ([]pluginsource.PlatformArchive, error) {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	token := resolveGitHubToken(src.Token)

	tag := src.ReleaseTag(version)
	releaseURL := fmt.Sprintf("%s/repos/%s/releases/tags/%s", baseURL, src.RepoSlug(), url.PathEscape(tag))

	release, err := r.fetchRelease(ctx, client, releaseURL, token, tag, src.RepoSlug())
	if err != nil {
		return nil, err
	}

	classified := classifyReleaseAssets(release.Assets, src.PluginName(), version)
	archives := make([]pluginsource.PlatformArchive, 0, len(classified))
	for platform, asset := range classified {
		archives = append(archives, pluginsource.PlatformArchive{
			Platform: platform,
			URL:      resolvedAssetURL(asset),
		})
	}
	return archives, nil
}

func resolvedAssetURL(asset releaseAsset) string {
	if strings.TrimSpace(asset.BrowserDownloadURL) != "" {
		return asset.BrowserDownloadURL
	}
	return asset.URL
}

func (r *GitHubResolver) fetchRelease(ctx context.Context, client *http.Client, url, token, tag, slug string) (*releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set(headerAccept, "application/json")
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release tag %s not found for %s", tag, slug)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching release %s for %s", resp.StatusCode, tag, slug)
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release response: %w", err)
	}
	return &release, nil
}

func findAsset(assets []releaseAsset, plugin, version string) (releaseAsset, error) {
	return findAssetForPlatform(assets, runtime.GOOS, runtime.GOARCH, plugin, version)
}

func findAssetForPlatform(assets []releaseAsset, goos, goarch, plugin, version string) (releaseAsset, error) {
	if plugin == "" {
		return releaseAsset{}, fmt.Errorf("plugin name is required")
	}
	for _, expectedName := range candidatePlatformAssetNamesFor(goos, goarch, plugin, version) {
		if asset, ok := findAssetByName(assets, expectedName); ok {
			return asset, nil
		}
	}

	versionedMatches := make([]releaseAsset, 0, 1)
	pluginOnlyMatches := make([]releaseAsset, 0, 1)
	for _, a := range assets {
		switch matchesPlatformAssetFor(a.Name, goos, goarch, plugin, version) {
		case versionedPlatformAssetMatch:
			versionedMatches = append(versionedMatches, a)
		case pluginOnlyPlatformAssetMatch:
			pluginOnlyMatches = append(pluginOnlyMatches, a)
		}
	}

	switch len(versionedMatches) {
	case 1:
		return versionedMatches[0], nil
	case 0:
	default:
		return releaseAsset{}, fmt.Errorf(
			"multiple %s/%s assets found for plugin %q version %q: %s",
			goos, goarch, plugin, version, joinAssetNames(versionedMatches),
		)
	}

	switch len(pluginOnlyMatches) {
	case 1:
		return pluginOnlyMatches[0], nil
	case 0:
	default:
		return releaseAsset{}, fmt.Errorf(
			"multiple %s/%s assets found for plugin %q version %q: %s",
			goos, goarch, plugin, version, joinAssetNames(pluginOnlyMatches),
		)
	}

	return releaseAsset{}, fmt.Errorf(
		"no %s/%s asset found for plugin %q in release v%s; available: %s",
		goos, goarch, plugin, version, joinAssetNames(assets),
	)
}

func platformAssetNameFor(goos, goarch, plugin, version string) string {
	return fmt.Sprintf("%s%s_v%s_%s.tar.gz", platformAssetPrefix, plugin, version, providerpkg.PlatformArchiveSuffix(goos, goarch))
}

func genericAssetName(plugin, version string) string {
	return fmt.Sprintf("%s%s_v%s.tar.gz", platformAssetPrefix, plugin, version)
}

func candidatePlatformAssetNamesFor(goos, goarch, plugin, version string) []string {
	names := []string{
		platformAssetNameFor(goos, goarch, plugin, version),
		genericAssetName(plugin, version),
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func findAssetByName(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return releaseAsset{}, false
}

func matchesPlatformAssetFor(name, goos, goarch, plugin, version string) platformAssetMatchKind {
	stem, ok := trimPlatformArchiveFor(name, goos, goarch)
	if !ok {
		return noPlatformAssetMatch
	}
	if !strings.HasPrefix(stem, platformAssetPrefix) {
		return noPlatformAssetMatch
	}

	rest := strings.TrimPrefix(stem, platformAssetPrefix)
	if rest == plugin {
		return pluginOnlyPlatformAssetMatch
	}

	for _, sep := range platformSeparators {
		for _, versionToken := range []string{"v" + version, version} {
			if rest == plugin+sep+versionToken || rest == versionToken+sep+plugin {
				return versionedPlatformAssetMatch
			}
		}
	}

	return noPlatformAssetMatch
}

func trimPlatformArchiveFor(name, goos, goarch string) (string, bool) {
	base, ok := trimPackageArchiveExtension(name)
	if !ok {
		return "", false
	}
	for _, suffix := range platformSuffixCandidatesFor(goos, goarch) {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix), true
		}
	}
	return "", false
}

func platformAliases(aliasMap map[string][]string, name string) []string {
	if aliases, ok := aliasMap[name]; ok {
		return aliases
	}
	return []string{name}
}

func platformSuffixCandidatesFor(goos, goarch string) []string {
	suffixes := make([]string, 0, 24)
	goosAliases := platformAliases(platformOSAliases, goos)
	goarchAliases := platformAliases(platformArchAliases, goarch)
	for _, osAlias := range goosAliases {
		for _, archAlias := range goarchAliases {
			for _, leadSep := range platformSeparators {
				for _, midSep := range platformSeparators {
					suffixes = append(suffixes, leadSep+osAlias+midSep+archAlias)
				}
			}
		}
	}
	return suffixes
}

// Remote plugin packages are tarball archives today; providerpkg only reads gzip-compressed tar packages.
func trimPackageArchiveExtension(name string) (string, bool) {
	for _, ext := range []string{".tar.gz", ".tgz"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext), true
		}
	}
	return "", false
}

func joinAssetNames(assets []releaseAsset) string {
	if len(assets) == 0 {
		return "(none)"
	}
	names := make([]string, len(assets))
	for i, a := range assets {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

func DownloadResolvedAsset(ctx context.Context, client *http.Client, assetURL, token string) (*providerpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	token = resolveGitHubToken(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create asset download request: %w", err)
	}
	req.Header.Set(headerAccept, acceptOctetStream)
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}
	return providerpkg.DownloadRequest(client, req)
}

func (r *GitHubResolver) downloadAsset(ctx context.Context, client *http.Client, assetURL, token string) (*providerpkg.DownloadResult, error) {
	return DownloadResolvedAsset(ctx, client, assetURL, token)
}

// classifyReleaseAssets identifies all platform-specific plugin archives in a
// set of release assets, returning a map from platform key (e.g. "darwin/arm64")
// to the matching asset. Generic (platform-independent) archives are keyed as
// "generic". Assets that don't match the plugin name or version are skipped.
func classifyReleaseAssets(assets []releaseAsset, plugin, version string) map[string]releaseAsset {
	result := make(map[string]releaseAsset)
	genericName := genericAssetName(plugin, version)
	for _, a := range assets {
		if a.Name == genericName {
			result["generic"] = a
			continue
		}
		platform, ok := extractPlatformFromAssetName(a.Name, plugin, version)
		if !ok {
			continue
		}
		if existing, dup := result[platform]; dup {
			// When two assets map to the same platform (e.g. _linux_amd64
			// and _linux_amd64_musl from a transition-period release),
			// prefer the musl variant since it is statically linked.
			if strings.Contains(existing.Name, "_musl") {
				continue
			}
		}
		result[platform] = a
	}
	return result
}

// extractPlatformFromAssetName parses a canonical asset name and returns
// the platform string. It handles the standard format:
//
//	gestalt-plugin-{plugin}_v{version}_{goos}_{goarch}[_{libc}].tar.gz
//
// OS and arch aliases (e.g. macos->darwin, x86_64->amd64) are normalized.
// Trailing qualifiers like _musl are ignored.
func extractPlatformFromAssetName(name, plugin, version string) (string, bool) {
	stem, ok := trimPackageArchiveExtension(name)
	if !ok {
		return "", false
	}

	prefix := platformAssetPrefix + plugin + "_v" + version + "_"
	if !strings.HasPrefix(stem, prefix) {
		return "", false
	}
	suffix := stem[len(prefix):]

	goos, rest, ok := strings.Cut(suffix, "_")
	if !ok {
		return "", false
	}
	goos = normalizeOS(goos)
	if goos == "" {
		return "", false
	}
	goarch := matchArch(rest)
	if goarch == "" {
		return "", false
	}
	return providerpkg.PlatformString(goos, goarch), true
}

// matchArch resolves an arch string that may contain trailing qualifiers
// (e.g. "amd64_musl", "x86_64_musl") into a canonical GOARCH value.
// Known aliases are matched longest-first so "x86_64" takes priority over
// "x86". Unknown single-token values pass through for forward compatibility
// with future GOARCH values.
func matchArch(s string) string {
	lower := strings.ToLower(s)
	for _, entry := range sortedArchAliases {
		if lower == entry.alias || strings.HasPrefix(lower, entry.alias+"_") {
			return entry.canonical
		}
	}
	if i := strings.IndexByte(lower, '_'); i > 0 {
		return lower[:i]
	}
	return lower
}

type archAlias struct {
	alias     string
	canonical string
}

// sortedArchAliases is built from platformArchAliases at init time, sorted
// by alias length descending so longer aliases match first (x86_64 before x86).
var sortedArchAliases = func() []archAlias {
	var entries []archAlias
	for canonical, aliases := range platformArchAliases {
		for _, alias := range aliases {
			entries = append(entries, archAlias{alias: alias, canonical: canonical})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].alias) > len(entries[j].alias)
	})
	return entries
}()

// normalizeOS maps OS aliases to canonical Go GOOS values. Unknown values
// pass through for forward compatibility.
func normalizeOS(s string) string {
	s = strings.ToLower(s)
	for canonical, aliases := range platformOSAliases {
		for _, alias := range aliases {
			if s == alias {
				return canonical
			}
		}
	}
	return s
}
