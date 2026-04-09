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
	return findAssetForLibC(assets, plugin, version, pluginpkg.CurrentRuntimeLibC())
}

func findAssetForLibC(assets []releaseAsset, plugin, version, libc string) (releaseAsset, error) {
	if plugin == "" {
		return releaseAsset{}, fmt.Errorf("plugin name is required")
	}
	for _, expectedName := range candidatePlatformAssetNames(plugin, version, libc) {
		if asset, ok := findAssetByName(assets, expectedName); ok {
			return asset, nil
		}
	}

	versionedMatches := make([]releaseAsset, 0, 1)
	pluginOnlyMatches := make([]releaseAsset, 0, 1)
	for _, a := range assets {
		switch matchesPlatformAsset(a.Name, plugin, version) {
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
			runtime.GOOS, runtime.GOARCH, plugin, version, joinAssetNames(versionedMatches),
		)
	}

	switch len(pluginOnlyMatches) {
	case 1:
		return pluginOnlyMatches[0], nil
	case 0:
	default:
		return releaseAsset{}, fmt.Errorf(
			"multiple %s/%s assets found for plugin %q version %q: %s",
			runtime.GOOS, runtime.GOARCH, plugin, version, joinAssetNames(pluginOnlyMatches),
		)
	}

	return releaseAsset{}, fmt.Errorf(
		"no %s/%s asset found for plugin %q in release v%s; available: %s",
		runtime.GOOS, runtime.GOARCH, plugin, version, joinAssetNames(assets),
	)
}

func platformAssetName(plugin, version, libc string) string {
	return fmt.Sprintf("%s%s_v%s_%s.tar.gz", platformAssetPrefix, plugin, version, pluginpkg.PlatformArchiveSuffix(runtime.GOOS, runtime.GOARCH, libc))
}

func genericAssetName(plugin, version string) string {
	return fmt.Sprintf("%s%s_v%s.tar.gz", platformAssetPrefix, plugin, version)
}

func candidatePlatformAssetNames(plugin, version, libc string) []string {
	names := make([]string, 0, 4)
	switch {
	case runtime.GOOS == "linux" && libc == "":
		// Minimal Linux images may not expose libc detection helpers like ldd.
		// Prefer musl when both libc-specific assets are present because it is the
		// safer portable choice for scratch-style runtimes.
		names = append(names,
			platformAssetName(plugin, version, pluginpkg.LinuxLibCMusl),
			platformAssetName(plugin, version, pluginpkg.LinuxLibCGLibC),
		)
	default:
		names = append(names, platformAssetName(plugin, version, libc))
	}
	if runtime.GOOS == "linux" && libc != "" {
		names = append(names, fmt.Sprintf("%s%s_v%s_%s.tar.gz", platformAssetPrefix, plugin, version, pluginpkg.PlatformArchiveSuffix(runtime.GOOS, runtime.GOARCH, "")))
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

func matchesPlatformAsset(name, plugin, version string) platformAssetMatchKind {
	stem, ok := trimPlatformArchive(name)
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

func trimPlatformArchive(name string) (string, bool) {
	base, ok := trimPackageArchiveExtension(name)
	if !ok {
		return "", false
	}
	for _, suffix := range platformSuffixCandidates() {
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

func platformSuffixCandidates() []string {
	suffixes := make([]string, 0, 24)
	goosAliases := platformAliases(platformOSAliases, runtime.GOOS)
	goarchAliases := platformAliases(platformArchAliases, runtime.GOARCH)
	for _, goos := range goosAliases {
		for _, goarch := range goarchAliases {
			for _, leadSep := range platformSeparators {
				for _, midSep := range platformSeparators {
					suffixes = append(suffixes, leadSep+goos+midSep+goarch)
					if runtime.GOOS == "linux" {
						for _, libc := range []string{pluginpkg.LinuxLibCGLibC, pluginpkg.LinuxLibCMusl} {
							for _, libSep := range platformSeparators {
								suffixes = append(suffixes, leadSep+goos+midSep+goarch+libSep+libc)
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
