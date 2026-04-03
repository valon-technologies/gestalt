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
	Host:   pluginsource.HostGitHub,
	Owner:  testOwner,
	Repo:   testRepo,
	Plugin: testPlugin,
}

func currentPlatformAssetName() string {
	return platformAssetNameFor(testPlugin, testVersion)
}

func platformAssetNameFor(plugin, version string, extras ...string) string {
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

func oldStyleAssetName() string {
	return testSource.AssetName(testVersion)
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

func legacyExpectedTag() string {
	return testSource.LegacyReleaseTag(testVersion)
}

func releasePath() string {
	return "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag()
}

func legacyReleasePath() string {
	return "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + legacyExpectedTag()
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
	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag() + "/" + assetName
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

func newLegacyTagServer(t *testing.T, assetName string, opts ...serverOption) *httptest.Server {
	t.Helper()
	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + legacyExpectedTag() + "/" + assetName
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, opt := range opts {
			if opt(w, r) {
				return
			}
		}
		switch r.URL.Path {
		case releasePath():
			w.WriteHeader(http.StatusNotFound)
		case legacyReleasePath():
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
		{Name: platformAssetNameFor(testPlugin, "1.2.2"), URL: "http://y", BrowserDownloadURL: "http://y"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != want {
		t.Errorf("findAsset() = %q, want %q", got.Name, want)
	}
}

func TestFindAssetBackwardCompat(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: oldStyleAssetName(), URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != oldStyleAssetName() {
		t.Errorf("findAsset() = %q, want %q", got.Name, oldStyleAssetName())
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
		{Name: platformAssetNameFor(testPlugin, "1.2.2"), URL: "http://x", BrowserDownloadURL: "http://x"},
	}
	_, err := findAsset(assets, testPlugin, testVersion)
	if err == nil {
		t.Fatal("expected error for wrong-version asset")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, testVersion) {
		t.Errorf("error should mention requested version, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, platformAssetNameFor(testPlugin, "1.2.2")) {
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

func TestFindAssetAmbiguousPatternFallsBackToLegacyName(t *testing.T) {
	t.Parallel()

	goos := secondaryPlatformName(platformAliases(platformOSAliases, runtime.GOOS))
	goarch := secondaryPlatformName(platformAliases(platformArchAliases, runtime.GOARCH))
	assets := []releaseAsset{
		{Name: aliasedPlatformAssetName(platformAssetPrefix+testPlugin+".v"+testVersion, ".", "-", ".tar.gz", goos, goarch), URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: aliasedPlatformAssetName(platformAssetPrefix+"v"+testVersion+"-"+testPlugin, "-", ".", ".tgz", goos, goarch), URL: "http://y", BrowserDownloadURL: "http://y"},
		{Name: oldStyleAssetName(), URL: "http://z", BrowserDownloadURL: "http://z"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != oldStyleAssetName() {
		t.Errorf("findAsset() = %q, want %q", got.Name, oldStyleAssetName())
	}
}

func TestFindAssetAmbiguousPluginOnlyPatternFallsBackToLegacyName(t *testing.T) {
	t.Parallel()

	goos := secondaryPlatformName(platformAliases(platformOSAliases, runtime.GOOS))
	goarch := secondaryPlatformName(platformAliases(platformArchAliases, runtime.GOARCH))
	assets := []releaseAsset{
		{Name: aliasedPlatformAssetName(platformAssetPrefix+testPlugin, ".", "-", ".tar.gz", goos, goarch), URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: aliasedPlatformAssetName(platformAssetPrefix+testPlugin, "-", ".", ".tgz", goos, goarch), URL: "http://y", BrowserDownloadURL: "http://y"},
		{Name: oldStyleAssetName(), URL: "http://z", BrowserDownloadURL: "http://z"},
	}
	got, err := findAsset(assets, testPlugin, testVersion)
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	if got.Name != oldStyleAssetName() {
		t.Errorf("findAsset() = %q, want %q", got.Name, oldStyleAssetName())
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

func TestFindAssetPrefersPatternMatchOverLegacyFallback(t *testing.T) {
	t.Parallel()

	want := variantPlatformAssetName(platformAssetPrefix+testPlugin+".v"+testVersion, ".", "-", ".tgz")
	assets := []releaseAsset{
		{Name: want, URL: "http://x", BrowserDownloadURL: "http://x"},
		{Name: oldStyleAssetName(), URL: "http://y", BrowserDownloadURL: "http://y"},
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

	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag() + "/" + assetName

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
	if pkg.ResolvedURL != browserURL {
		t.Errorf("ResolvedURL = %s, want %s", pkg.ResolvedURL, browserURL)
	}

	pkg.Cleanup()
	if _, err := os.Stat(pkg.LocalPath); !os.IsNotExist(err) {
		t.Error("temp file should be removed after Cleanup()")
	}
}

func TestResolveBackwardCompatAsset(t *testing.T) {
	t.Parallel()

	assetName := oldStyleAssetName()
	srv := newTestServer(t, assetName)
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	if pkg.ArchiveSHA256 != testAssetSHA256() {
		t.Errorf("SHA256 = %s, want %s", pkg.ArchiveSHA256, testAssetSHA256())
	}
}

func TestResolveFallsBackToLegacyReleaseTag(t *testing.T) {
	t.Parallel()

	assetName := currentPlatformAssetName()
	srv := newLegacyTagServer(t, assetName)
	defer srv.Close()

	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + legacyExpectedTag() + "/" + assetName

	resolver := &GitHubResolver{BaseURL: srv.URL}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	if pkg.ResolvedURL != browserURL {
		t.Errorf("ResolvedURL = %s, want %s", pkg.ResolvedURL, browserURL)
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

	want := "github plugin release not found: release tag " + legacyExpectedTag() + " not found for " + testOwner + "/" + testRepo
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

	resolver := &GitHubResolver{BaseURL: srv.URL, Token: testToken}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
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

func TestResolveGitHubTokenEnvFallback(t *testing.T) {
	envToken := "ghp_env_fallback_token"
	t.Setenv(envGitHubToken, envToken)

	var log requestLog
	srv := newTestServer(t, currentPlatformAssetName(), withAuthLog(&log))
	defer srv.Close()

	resolver := &GitHubResolver{BaseURL: srv.URL}
	pkg, err := resolver.Resolve(context.Background(), testSource, testVersion)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	defer pkg.Cleanup()

	wantAuth := authTokenPrefix + envToken
	for i, got := range log.all() {
		if got != wantAuth {
			t.Errorf("request %d: Authorization = %q, want %q", i, got, wantAuth)
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
