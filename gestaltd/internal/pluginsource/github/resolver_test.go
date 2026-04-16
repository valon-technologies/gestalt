package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
)

const (
	testOwner   = "acme-corp"
	testRepo    = "tools"
	testPlugin  = "linter"
	testVersion = "1.2.3"
	testToken   = "ghp_test_token_abc123"
)

var testSource = pluginsource.Source{
	Host:  pluginsource.HostGitHub,
	Owner: testOwner,
	Repo:  testRepo,
	Path:  "plugins/" + testPlugin,
}

func currentPlatformAssetName() string {
	return testPlatformAssetName(testPlugin, testVersion)
}

func testPlatformAssetName(plugin, version string, extras ...string) string {
	base := platformAssetPrefix + plugin + "_v" + version
	for _, extra := range extras {
		if extra != "" {
			base += "_" + extra
		}
	}
	return base + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
}

func variantPlatformAssetName(stem, leadSep, midSep, ext string) string {
	return aliasedPlatformAssetName(stem, leadSep, midSep, ext, runtime.GOOS, runtime.GOARCH)
}

func aliasedPlatformAssetName(stem, leadSep, midSep, ext, goos, goarch string) string {
	return stem + leadSep + goos + midSep + goarch + ext
}

func secondaryPlatformName(aliases []string) string {
	if len(aliases) > 1 {
		return aliases[1]
	}
	return aliases[0]
}

func releaseJSON(t *testing.T, assets []releaseAsset) []byte {
	t.Helper()
	b, err := json.Marshal(releaseResponse{Assets: assets})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testAssetPayload() []byte {
	return []byte("fake-plugin-binary-data-here")
}

func testAssetSHA256() string {
	h := sha256.Sum256(testAssetPayload())
	return hex.EncodeToString(h[:])
}

func expectedTag() string {
	return testSource.ReleaseTag(testVersion)
}

func releasePath() string {
	return "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag()
}

func expectedBrowserDownloadURL(assetName string) string {
	return "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag() + "/" + assetName
}

type requestLog struct {
	mu      sync.Mutex
	headers []string
}

func (rl *requestLog) record(val string) {
	rl.mu.Lock()
	rl.headers = append(rl.headers, val)
	rl.mu.Unlock()
}

func (rl *requestLog) all() []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cp := make([]string, len(rl.headers))
	copy(cp, rl.headers)
	return cp
}

type serverOption func(w http.ResponseWriter, r *http.Request) bool

func withAuthLog(log *requestLog) serverOption {
	return func(_ http.ResponseWriter, r *http.Request) bool {
		log.record(r.Header.Get(headerAuthorization))
		return false
	}
}

func withAssetStatus(code int) serverOption {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/asset-dl" {
			w.WriteHeader(code)
			return true
		}
		return false
	}
}

func newTestServer(t *testing.T, assetName string, opts ...serverOption) *httptest.Server {
	t.Helper()
	browserURL := expectedBrowserDownloadURL(assetName)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, opt := range opts {
			if opt(w, r) {
				return
			}
		}
		switch r.URL.Path {
		case releasePath():
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(releaseJSON(t, []releaseAsset{
				{
					Name:               assetName,
					URL:                "http://" + r.Host + "/asset-dl",
					BrowserDownloadURL: browserURL,
				},
			}))
		case "/asset-dl":
			_, _ = w.Write(testAssetPayload())
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFindAssetPlatformMatch(t *testing.T) {
	t.Parallel()

	want := aliasedPlatformAssetName(
		platformAssetPrefix+testPlugin+".v"+testVersion,
		".",
		"-",
		".tgz",
		secondaryPlatformName(platformAliases(platformOSAliases, runtime.GOOS)),
		secondaryPlatformName(platformAliases(platformArchAliases, runtime.GOARCH)),
	)
	assets := []releaseAsset{
		{Name: variantPlatformAssetName(platformAssetPrefix+"my-"+testPlugin, "_", "_", ".tar.gz"), URL: "http://z", BrowserDownloadURL: "http://z"},
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: testPlatformAssetName(testPlugin, "1.2.2"), URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Errorf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetNoMatch(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: "gestalt-plugin-linter_v1.2.3_fakeos_fakearch.tar.gz", URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: "unrelated.zip", URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for no matching asset")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("error should mention current platform, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "gestalt-plugin-linter_v1.2.3_fakeos_fakearch.tar.gz") || !strings.Contains(errMsg, "unrelated.zip") {
		t.Errorf("error should list available assets, got: %s", errMsg)
	}
}

func TestFindAssetRejectsWrongVersion(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: testPlatformAssetName(testPlugin, "1.2.2"), URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for wrong-version asset")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, testVersion) {
		t.Errorf("error should mention requested version, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, testPlatformAssetName(testPlugin, "1.2.2")) {
		t.Errorf("error should list available assets, got: %s", errMsg)
	}
}

func TestFindAssetRejectsAmbiguousMatches(t *testing.T) {
	t.Parallel()

	goos := secondaryPlatformName(platformAliases(platformOSAliases, runtime.GOOS))
	goarch := secondaryPlatformName(platformAliases(platformArchAliases, runtime.GOARCH))
	assets := []releaseAsset{
		{Name: aliasedPlatformAssetName(platformAssetPrefix+testPlugin+".v"+testVersion, ".", "-", ".tar.gz", goos, goarch), URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: aliasedPlatformAssetName(platformAssetPrefix+"v"+testVersion+"-"+testPlugin, "-", ".", ".tgz", goos, goarch), URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for ambiguous platform assets")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "multiple") {
		t.Errorf("error should mention multiple matches, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, aliasedPlatformAssetName(platformAssetPrefix+testPlugin+".v"+testVersion, ".", "-", ".tar.gz", goos, goarch)) || !strings.Contains(errMsg, aliasedPlatformAssetName(platformAssetPrefix+"v"+testVersion+"-"+testPlugin, "-", ".", ".tgz", goos, goarch)) {
		t.Errorf("error should list matching assets, got: %s", errMsg)
	}
}

func TestFindAssetMatchesGenericArchiveName(t *testing.T) {
	t.Parallel()

	want := genericAssetName(testPlugin, testVersion)
	assets := []releaseAsset{
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
	}

	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Fatalf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetVersionBeforePluginMatch(t *testing.T) {
	t.Parallel()

	want := variantPlatformAssetName(platformAssetPrefix+"v"+testVersion+"-"+testPlugin, ".", "-", ".tgz")
	assets := []releaseAsset{
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Errorf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetDoesNotMatchPluginSuffixOfDifferentPlugin(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: variantPlatformAssetName(platformAssetPrefix+"my-"+testPlugin+"_v"+testVersion, "_", "_", ".tar.gz"), URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: variantPlatformAssetName(platformAssetPrefix+"my_"+testPlugin+"_v"+testVersion, "_", "_", ".tar.gz"), URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for mismatched plugin suffix")
	}
	if !strings.Contains(err.Error(), "my-"+testPlugin) || !strings.Contains(err.Error(), "my_"+testPlugin) {
		t.Errorf("error should list mismatched asset, got: %s", err.Error())
	}
}

func TestFindAssetDoesNotMatchPluginOnlySuffixOfDifferentPlugin(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: variantPlatformAssetName(platformAssetPrefix+"my-"+testPlugin, "_", "_", ".tar.gz"), URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: variantPlatformAssetName(platformAssetPrefix+"my_"+testPlugin, "_", "_", ".tgz"), URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for mismatched plugin-only suffix")
	}
	if !strings.Contains(err.Error(), "my-"+testPlugin) || !strings.Contains(err.Error(), "my_"+testPlugin) {
		t.Errorf("error should list mismatched asset, got: %s", err.Error())
	}
}

func TestFindAssetMatchesOfficialPluginOnlyArchive(t *testing.T) {
	t.Parallel()

	want := aliasedPlatformAssetName(
		platformAssetPrefix+testPlugin,
		"-",
		"-",
		".tar.gz",
		secondaryPlatformName(platformAliases(platformOSAliases, runtime.GOOS)),
		secondaryPlatformName(platformAliases(platformArchAliases, runtime.GOARCH)),
	)
	assets := []releaseAsset{
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Errorf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetPrefersVersionedPatternOverPluginOnlyPattern(t *testing.T) {
	t.Parallel()

	want := variantPlatformAssetName(platformAssetPrefix+testPlugin+"-"+testVersion, "-", "-", ".tgz")
	assets := []releaseAsset{
		{Name: variantPlatformAssetName(platformAssetPrefix+testPlugin, "_", "_", ".tar.gz"), URL: "http://y", BrowserDownloadURL: "http://y"},
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Errorf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetRejectsUnofficialPlatformArchive(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: variantPlatformAssetName(testPlugin+".v"+testVersion, ".", "-", ".tgz"), URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for unofficial platform archive")
	}
	if !strings.Contains(err.Error(), testPlugin+".v"+testVersion) {
		t.Errorf("error should list unofficial asset, got: %s", err.Error())
	}
}

func TestFindAssetRejectsEmptyPlugin(t *testing.T) {
	t.Parallel()

	_, err := findAsset([]releaseAsset{{Name: currentPlatformAssetName(), URL: "http://x", BrowserDownloadURL: "http://x"}}, "", testVersion)
	if err == nil {
		t.Fatal("expected error for empty plugin name")
	}
	if !strings.Contains(err.Error(), "plugin name is required") {
		t.Errorf("error = %q, want mention of missing plugin name", err.Error())
	}
}

func TestResolveSuccess(t *testing.T) {
	t.Parallel()

	assetName := currentPlatformAssetName()
	srv := newTestServer(t, assetName)
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	if _, err := os.Stat(pkg.LocalPath); err != nil {
		t.Errorf("LocalPath does not exist: %v", err)
	}
	if pkg.ArchiveSHA256 != testAssetSHA256() {
		t.Errorf("SHA256 = %s, want %s", pkg.ArchiveSHA256, testAssetSHA256())
	}
	wantURL := expectedBrowserDownloadURL(assetName)
	if pkg.ResolvedURL != wantURL {
		t.Errorf("ResolvedURL = %s, want %s", pkg.ResolvedURL, wantURL)
	}

	pkg.Cleanup()
	if _, err := os.Stat(pkg.LocalPath); !os.IsNotExist(err) {
		t.Error("temp file should be removed after Cleanup()")
	}
}

func TestResolveReleaseNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	_, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err == nil {
		t.Fatal("expected error for 404 release")
	}

	want := "release tag " + expectedTag() + " not found for " + testOwner + "/" + testRepo
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestResolveAssetNameMismatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(releaseJSON(t, []releaseAsset{
			{Name: "wrong-asset.tar.gz", URL: "http://x", BrowserDownloadURL: "http://x"},
			{Name: "also-wrong.zip", URL: "http://y", BrowserDownloadURL: "http://y"},
		}))
	}))
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	_, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err == nil {
		t.Fatal("expected error for asset mismatch")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("error should mention current platform, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "wrong-asset.tar.gz") || !strings.Contains(errMsg, "also-wrong.zip") {
		t.Errorf("error should list available assets, got: %s", errMsg)
	}
}

func TestResolveAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	var log requestLog
	srv := newTestServer(t, currentPlatformAssetName(), withAuthLog(&log))
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	src := testSource
	src.Token = testToken
	pkg, err := resolver.Resolve(context.Background(), src, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	wantAuth := authTokenPrefix + testToken
	headers := log.all()
	if len(headers) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(headers))
	}
	for i, got := range headers {
		if got != wantAuth {
			t.Errorf("request %d: Authorization = %q, want %q", i, got, wantAuth)
		}
	}
}

func TestResolveUsesEnvironmentTokenFallback(t *testing.T) {
	var log requestLog
	srv := newTestServer(t, currentPlatformAssetName(), withAuthLog(&log))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "ghp_env_fallback_token")
	t.Setenv("GH_TOKEN", "ghp_gh_token")
	t.Setenv("HOMEBREW_GITHUB_API_TOKEN", "ghp_homebrew_token")

	resolver := &GitHubResolver{BaseURL: srv.URL}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	headers := log.all()
	if len(headers) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(headers))
	}
	for i, got := range headers {
		if got != authTokenPrefix+"ghp_env_fallback_token" {
			t.Errorf("request %d: Authorization = %q, want %q", i, got, authTokenPrefix+"ghp_env_fallback_token")
		}
	}
}

func TestResolveDownloadError(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, currentPlatformAssetName(), withAssetStatus(http.StatusInternalServerError))
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	_, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err == nil {
		t.Fatal("expected error for download failure")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("error = %q, want mention of status 500", err.Error())
	}
}

func TestResolvedAssetURLFallsBackToAPIAssetURL(t *testing.T) {
	t.Parallel()

	assetURL := "https://api.github.com/repos/acme-corp/tools/releases/assets/123"
	if got := resolvedAssetURL(releaseAsset{URL: assetURL}); got != assetURL {
		t.Fatalf("resolvedAssetURL() = %q, want %q", got, assetURL)
	}
}

func TestExtractPlatformFromAssetName_Canonical(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		asset    string
		wantPlat string
		wantOK   bool
	}{
		{"darwin_arm64", "gestalt-plugin-linter_v1.2.3_darwin_arm64.tar.gz", "darwin/arm64", true},
		{"linux_amd64", "gestalt-plugin-linter_v1.2.3_linux_amd64.tar.gz", "linux/amd64", true},
		{"linux_amd64_musl", "gestalt-plugin-linter_v1.2.3_linux_amd64_musl.tar.gz", "linux/amd64", true},
		{"linux_arm64_musl", "gestalt-plugin-linter_v1.2.3_linux_arm64_musl.tar.gz", "linux/arm64", true},
		{"windows_amd64", "gestalt-plugin-linter_v1.2.3_windows_amd64.tar.gz", "windows/amd64", true},
		{"macos_alias", "gestalt-plugin-linter_v1.2.3_macos_aarch64.tar.gz", "darwin/arm64", true},
		{"x86_64_alias", "gestalt-plugin-linter_v1.2.3_linux_x86_64.tar.gz", "linux/amd64", true},
		{"generic", "gestalt-plugin-linter_v1.2.3.tar.gz", "", false},
		{"wrong_plugin", "gestalt-plugin-other_v1.2.3_darwin_arm64.tar.gz", "", false},
		{"wrong_version", "gestalt-plugin-linter_v9.9.9_darwin_arm64.tar.gz", "", false},
		{"not_plugin", "random-file.tar.gz", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extractPlatformFromAssetName(tt.asset, testPlugin, testVersion)
			if ok != tt.wantOK {
				t.Fatalf("extractPlatformFromAssetName(%q) ok = %v, want %v", tt.asset, ok, tt.wantOK)
			}
			if got != tt.wantPlat {
				t.Errorf("extractPlatformFromAssetName(%q) = %q, want %q", tt.asset, got, tt.wantPlat)
			}
		})
	}
}

func TestClassifyReleaseAssets(t *testing.T) {
	t.Parallel()
	assets := []releaseAsset{
		{Name: "gestalt-plugin-linter_v1.2.3_darwin_arm64.tar.gz", URL: "https://example.com/darwin-arm64"},
		{Name: "gestalt-plugin-linter_v1.2.3_linux_amd64.tar.gz", URL: "https://example.com/linux-amd64"},
		{Name: "gestalt-plugin-linter_v1.2.3_linux_amd64_musl.tar.gz", URL: "https://example.com/linux-amd64-musl"},
		{Name: "gestalt-plugin-linter_v1.2.3.tar.gz", URL: "https://example.com/generic"},
		{Name: "unrelated-file.zip", URL: "https://example.com/unrelated"},
		{Name: "checksums.txt", URL: "https://example.com/checksums"},
	}

	result := classifyReleaseAssets(assets, testPlugin, testVersion)

	expected := map[string]string{
		"darwin/arm64": "https://example.com/darwin-arm64",
		"linux/amd64":  "https://example.com/linux-amd64-musl",
		"generic":      "https://example.com/generic",
	}

	if len(result) != len(expected) {
		t.Fatalf("classifyReleaseAssets returned %d entries, want %d", len(result), len(expected))
	}
	for platform, wantURL := range expected {
		asset, ok := result[platform]
		if !ok {
			t.Errorf("missing platform %q", platform)
			continue
		}
		if asset.URL != wantURL {
			t.Errorf("platform %q URL = %q, want %q", platform, asset.URL, wantURL)
		}
	}
}

func TestClassifyReleaseAssets_PrefersMuslRegardlessOfOrder(t *testing.T) {
	t.Parallel()
	assets := []releaseAsset{
		{Name: "gestalt-plugin-linter_v1.2.3_linux_amd64_musl.tar.gz", URL: "https://example.com/musl"},
		{Name: "gestalt-plugin-linter_v1.2.3_linux_amd64.tar.gz", URL: "https://example.com/plain"},
	}

	result := classifyReleaseAssets(assets, testPlugin, testVersion)

	if asset, ok := result["linux/amd64"]; !ok {
		t.Fatal("missing linux/amd64")
	} else if asset.URL != "https://example.com/musl" {
		t.Fatalf("linux/amd64 URL = %q, want musl variant", asset.URL)
	}
}

func TestListPlatformArchives(t *testing.T) {
	t.Parallel()

	assetName := currentPlatformAssetName()
	srv := newTestServer(t, assetName)
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	archives, err := resolver.ListPlatformArchives(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("ListPlatformArchives: %v", err)
	}

	if len(archives) == 0 {
		t.Fatal("ListPlatformArchives returned no archives")
	}

	found := false
	for _, a := range archives {
		if a.Platform == runtime.GOOS+"/"+runtime.GOARCH || strings.HasPrefix(a.Platform, runtime.GOOS+"/"+runtime.GOARCH) {
			found = true
			wantURL := expectedBrowserDownloadURL(assetName)
			if a.URL != wantURL {
				t.Errorf("current platform archive URL = %q, want %q", a.URL, wantURL)
			}
		}
	}
	if !found {
		t.Errorf("current platform %s/%s not found in archives", runtime.GOOS, runtime.GOARCH)
	}
}
