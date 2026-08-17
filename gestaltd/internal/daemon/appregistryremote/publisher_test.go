package appregistryremote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestPublishUploadFinalizeAndResume(t *testing.T) {
	registerTestPublishHelpers(t)

	root, distDir, runner := setupTestPublishRepo(t)
	_ = root
	artifactPath := filepath.Join(distDir, "linux-amd64.tar.gz")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	darwinData, err := os.ReadFile(filepath.Join(distDir, "darwin-arm64.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	darwinSum := sha256.Sum256(darwinData)
	digest2 := hex.EncodeToString(darwinSum[:])

	uploaded := map[string]int{}
	var uploadMu sync.Mutex
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platform := strings.TrimPrefix(r.URL.Path, "/upload/")
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("x-goog-content-sha256"); got == "" {
			http.Error(w, "missing digest header", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != r.URL.Query().Get("sha256") {
			http.Error(w, "digest mismatch", http.StatusBadRequest)
			return
		}
		uploadMu.Lock()
		uploaded[platform]++
		uploadMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	state := "created"
	publishID := "pub_test123"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/publishes"):
			var req CreateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if req.Declaration == nil {
				http.Error(w, "declaration is required", http.StatusBadRequest)
				return
			}
			writeJSON(w, SessionResponse{
				PublishID: publishID,
				App:       "demo",
				Version:   "0.3.0-dev.1",
				State:     state,
				Uploads: []SessionUpload{
					{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64?sha256=" + digest},
					{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64?sha256=" + digest2},
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, publishID):
			missing := []string(nil)
			if uploaded["linux/amd64"] == 0 {
				missing = append(missing, "linux/amd64")
			}
			if uploaded["darwin/arm64"] == 0 {
				missing = append(missing, "darwin/arm64")
			}
			writeJSON(w, SessionResponse{
				PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state,
				MissingUploads: missing,
				Uploads: []SessionUpload{
					{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64?sha256=" + digest},
					{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64?sha256=" + digest2},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/finalize"):
			state = sessionStatePublished
			writeJSON(w, SessionResponse{
				PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state,
				PublishedAt: "2026-08-17T12:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	client := &Client{BaseURL: apiServer.URL, Token: "test-token"}
	input := PublishInput{
		Version:       "0.3.0-dev.1",
		DistDirs:      []string{distDir},
		GestaltURL:    apiServer.URL,
		GestaltToken:  "test-token",
		Client:        client,
		CommandRunner: runner,
	}

	result, err := Publish(t.Context(), input)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.State != sessionStatePublished || result.AdminURL == "" {
		t.Fatalf("Publish() = %#v", result)
	}
	if uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("upload counts = %#v", uploaded)
	}

	// Re-run should skip uploads and finalize idempotently.
	state = sessionStatePublished
	_, err = Publish(t.Context(), input)
	if err != nil {
		t.Fatalf("Publish() resume error = %v", err)
	}
	if uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("resume uploaded again: %#v", uploaded)
	}
}

func TestPublishReturnsCompletedSessionWithoutUpload(t *testing.T) {
	registerTestPublishHelpers(t)
	_, distDir, runner := setupTestPublishRepo(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, SessionResponse{
			PublishID: "pub_done", App: "demo", Version: "0.3.0-dev.1", State: sessionStatePublished,
			PublishedAt: "2026-08-17T12:00:00Z",
		})
	}))
	t.Cleanup(apiServer.Close)

	result, err := Publish(t.Context(), PublishInput{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir},
		GestaltURL: apiServer.URL, GestaltToken: "token",
		Client:        &Client{BaseURL: apiServer.URL, Token: "token"},
		CommandRunner: runner,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.PublishID != "pub_done" {
		t.Fatalf("Publish() = %#v", result)
	}
}

func TestPublishAuthPrecedenceUsesExplicitToken(t *testing.T) {
	registerTestPublishHelpers(t)
	t.Setenv("GESTALT_API_KEY", "env-token-should-not-be-used")
	_, distDir, runner := setupTestPublishRepo(t)

	var authHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		writeJSON(w, SessionResponse{PublishID: "pub_done", App: "demo", Version: "0.3.0-dev.1", State: sessionStatePublished})
	}))
	t.Cleanup(apiServer.Close)

	_, err := Publish(t.Context(), PublishInput{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir},
		GestaltURL: apiServer.URL, GestaltToken: "explicit-token",
		Client:        &Client{BaseURL: apiServer.URL, Token: "explicit-token"},
		CommandRunner: runner,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if authHeader != "Bearer explicit-token" {
		t.Fatalf("Authorization = %q", authHeader)
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func registerTestPublishHelpers(t *testing.T) {
	t.Helper()
	RegisterPublishHelpers(PublishHelpers{
		CollectReleaseArchivesFromDirs: func(distDirs []string, version string) (*providermanifestv1.Manifest, string, []DaemonReleaseArchive, error) {
			var archives []DaemonReleaseArchive
			for _, distDir := range distDirs {
				for _, name := range []string{"linux-amd64.tar.gz", "darwin-arm64.tar.gz"} {
					path := filepath.Join(distDir, name)
					data, err := os.ReadFile(path)
					if err != nil {
						return nil, "", nil, err
					}
					sum := sha256.Sum256(data)
					platform := strings.ReplaceAll(strings.TrimSuffix(name, ".tar.gz"), "-", "/")
					archives = append(archives, DaemonReleaseArchive{
						Path: path, SHA256: hex.EncodeToString(sum[:]), Target: platform, Size: int64(len(data)),
					})
				}
			}
			manifest := testManifest("demo", version)
			return manifest, version, archives, nil
		},
		BuildProviderReleaseMetadata: func(manifest *providermanifestv1.Manifest, version string, archives []DaemonReleaseArchive, raw []byte) (*providerrelease.Metadata, error) {
			return testReleaseMetadata("demo", version), nil
		},
		ValidateProviderPublishManifest: func(source, release *providermanifestv1.Manifest, releaseVersion, version string) error {
			if source.Source != release.Source || releaseVersion != version {
				return appregistry.ErrPublishDeclarationInvalid
			}
			return nil
		},
	})
}

func writeTestArchive(t *testing.T, name string, data []byte) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return path, hex.EncodeToString(sum[:])
}

func setupTestPublishRepo(t *testing.T) (root, distDir string, runner fakeRunner) {
	t.Helper()
	root = t.TempDir()
	manifestDir := filepath.Join(root, "apps", "demo")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(manifestDir, "manifest.yaml")
	content := []byte("kind: app\nsource: github.com/valon-technologies/valon-tools/apps/demo\nversion: 0.3.0-dev.1\nspec: {}\n")
	if err := os.WriteFile(manifestPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	distDir = filepath.Join(root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "linux-amd64.tar.gz"), []byte("linux"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "darwin-arm64.tar.gz"), []byte("darwin"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	runner = fakeRunner{
		"git -C " + root + " rev-parse --show-toplevel":        root + "\n",
		"git -C " + manifestDir + " rev-parse --show-toplevel": root + "\n",
		"git -C " + manifestDir + " rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C " + manifestDir + " status --porcelain":        "",
	}
	return root, distDir, runner
}

func writeTestDistDir(t *testing.T) string {
	_, distDir, _ := setupTestPublishRepo(t)
	return distDir
}

func testManifestPath(t *testing.T) string {
	root := t.TempDir()
	return filepath.Join(root, "apps", "demo", "manifest.yaml")
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrintPublishResult(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printPublishResult(&buf, PublishResult{
		PublishID: "pub_1", App: "demo", Version: "0.3.0-dev.1", State: sessionStatePublished,
		AdminURL: "https://valon.tools/apps/demo/admin/registry", PublishedAt: "2026-08-17T12:00:00Z",
	})
	out := buf.String()
	for _, want := range []string{"publishId: pub_1", "adminUrl: https://valon.tools/apps/demo/admin/registry", "publishedAt:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output = %q, missing %q", out, want)
		}
	}
}
