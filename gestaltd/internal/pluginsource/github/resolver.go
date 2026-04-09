package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
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

func (r *GitHubResolver) Resolve(ctx context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	token := strings.TrimSpace(src.Token)

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
		ResolvedURL:   asset.URL,
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
	token := strings.TrimSpace(src.Token)

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
			URL:      asset.URL,
		})
	}
	return archives, nil
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
	return findAssetForPlatform(assets, runtime.GOOS, runtime.GOARCH, plugin, version, pluginpkg.CurrentRuntimeLibC())
}

func findAssetForPlatform(assets []releaseAsset, goos, goarch, plugin, version, libc string) (releaseAsset, error) {
	if plugin == "" {
		return releaseAsset{}, fmt.Errorf("plugin name is required")
	}
	for _, expectedName := range candidatePlatformAssetNamesFor(goos, goarch, plugin, version, libc) {
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

func platformAssetNameFor(goos, goarch, plugin, version, libc string) string {
	return fmt.Sprintf("%s%s_v%s_%s.tar.gz", platformAssetPrefix, plugin, version, pluginpkg.PlatformArchiveSuffix(goos, goarch, libc))
}

func genericAssetName(plugin, version string) string {
	return fmt.Sprintf("%s%s_v%s.tar.gz", platformAssetPrefix, plugin, version)
}

func candidatePlatformAssetNamesFor(goos, goarch, plugin, version, libc string) []string {
	names := make([]string, 0, 4)
	switch {
	case goos == "linux" && libc == "":
		names = append(names,
			platformAssetNameFor(goos, goarch, plugin, version, pluginpkg.LinuxLibCMusl),
			platformAssetNameFor(goos, goarch, plugin, version, pluginpkg.LinuxLibCGLibC),
		)
	default:
		names = append(names, platformAssetNameFor(goos, goarch, plugin, version, libc))
	}
	if goos == "linux" && libc != "" {
		names = append(names, fmt.Sprintf("%s%s_v%s_%s.tar.gz", platformAssetPrefix, plugin, version, pluginpkg.PlatformArchiveSuffix(goos, goarch, "")))
	}
	names = append(names, genericAssetName(plugin, version))
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
					if goos == "linux" {
						for _, libc := range []string{pluginpkg.LinuxLibCGLibC, pluginpkg.LinuxLibCMusl} {
							for _, libSep := range platformSeparators {
								suffixes = append(suffixes, leadSep+osAlias+midSep+archAlias+libSep+libc)
							}
						}
					}
				}
			}
		}
	}
	return suffixes
}

// Remote plugin packages are tarball archives today; pluginpkg only reads gzip-compressed tar packages.
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

func DownloadResolvedAsset(ctx context.Context, client *http.Client, assetURL, token string) (*pluginpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	token = strings.TrimSpace(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create asset download request: %w", err)
	}
	req.Header.Set(headerAccept, acceptOctetStream)
	if token != "" {
		req.Header.Set(headerAuthorization, authTokenPrefix+token)
	}
	return pluginpkg.DownloadRequest(client, req)
}

func (r *GitHubResolver) downloadAsset(ctx context.Context, client *http.Client, assetURL, token string) (*pluginpkg.DownloadResult, error) {
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
		if platform, ok := extractPlatformFromAssetName(a.Name, plugin, version); ok {
			result[platform] = a
		}
	}
	return result
}

// extractPlatformFromAssetName parses a canonical asset name and returns
// the platform string. It handles the standard format:
//
//	gestalt-plugin-{plugin}_v{version}_{goos}_{goarch}[_{libc}].tar.gz
//
// OS and arch aliases (e.g. macos→darwin, x86_64→amd64) are normalized.
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

	// Try parsing with 2, 3, or 4 underscore-separated parts to handle
	// aliases like x86_64 that contain underscores.
	parts := strings.Split(suffix, "_")
	for _, split := range possiblePlatformSplits(parts) {
		goos := normalizeOS(split.os)
		goarch := normalizeArch(split.arch)
		if goos == "" || goarch == "" {
			continue
		}
		if split.libc != "" {
			libc := normalizeLibC(split.libc)
			if libc == "" {
				continue
			}
			return pluginpkg.PlatformString(goos, goarch, libc), true
		}
		return pluginpkg.PlatformString(goos, goarch, ""), true
	}
	return "", false
}

type platformSplit struct {
	os, arch, libc string
}

// possiblePlatformSplits returns candidate (os, arch, libc) interpretations
// for underscore-separated parts, handling aliases like x86_64 that contain
// an underscore.
func possiblePlatformSplits(parts []string) []platformSplit {
	var splits []platformSplit
	switch len(parts) {
	case 2:
		// os_arch
		splits = append(splits, platformSplit{parts[0], parts[1], ""})
	case 3:
		// os_arch_libc  OR  os_x86_64 (arch alias with underscore)
		splits = append(splits,
			platformSplit{parts[0], parts[1], parts[2]},
			platformSplit{parts[0], parts[1] + "_" + parts[2], ""},
		)
	case 4:
		// os_x86_64_libc
		splits = append(splits, platformSplit{parts[0], parts[1] + "_" + parts[2], parts[3]})
	}
	return splits
}

// normalizeOS maps OS aliases to canonical Go GOOS values.
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

// normalizeArch maps arch aliases to canonical Go GOARCH values.
func normalizeArch(s string) string {
	s = strings.ToLower(s)
	for canonical, aliases := range platformArchAliases {
		for _, alias := range aliases {
			if s == alias {
				return canonical
			}
		}
	}
	return s
}

func normalizeLibC(s string) string {
	s = strings.ToLower(s)
	switch s {
	case pluginpkg.LinuxLibCGLibC, pluginpkg.LinuxLibCMusl:
		return s
	default:
		return ""
	}
}
