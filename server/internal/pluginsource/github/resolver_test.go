package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func expectedAssetName() string {
	return testSource.AssetName(testVersion)
}

func expectedTag() string {
	return testSource.ReleaseTag(testVersion)
}

func releasePath() string {
	return "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag()
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

func newTestServer(t *testing.T, opts ...serverOption) *httptest.Server {
	t.Helper()
	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag() + "/" + expectedAssetName()
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
					Name:               expectedAssetName(),
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

func TestResolveSuccess(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	defer srv.Close()

	browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag() + "/" + expectedAssetName()

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
	if want := "does not contain asset " + expectedAssetName(); !strings.Contains(errMsg, want) {
		t.Errorf("error should mention expected asset name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "wrong-asset.tar.gz") || !strings.Contains(errMsg, "also-wrong.zip") {
		t.Errorf("error should list available assets, got: %s", errMsg)
	}
}

func TestResolveAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	var log requestLog
	srv := newTestServer(t, withAuthLog(&log))
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
	srv := newTestServer(t, withAuthLog(&log))
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

	srv := newTestServer(t, withAssetStatus(http.StatusInternalServerError))
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
