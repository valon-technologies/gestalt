package appregistryremote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type fakeRunner map[string]string

func (f fakeRunner) Run(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if out, ok := f[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestRemotePublishFlowAndResume(t *testing.T) { //nolint:paralleltest // chdirs
	registerTestPublishHelpers(t)
	_, distDir, runner := setupTestPublishRepo(t)
	linuxPath := filepath.Join(distDir, "linux-amd64.tar.gz")
	darwinPath := filepath.Join(distDir, "darwin-arm64.tar.gz")
	linuxDigest, linuxHeaders := artifactFixture(t, linuxPath)
	darwinDigest, darwinHeaders := artifactFixture(t, darwinPath)

	uploaded := map[string]int{}
	var uploadMu sync.Mutex
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platform := strings.TrimPrefix(r.URL.Path, "/upload/")
		for _, name := range signedUploadHeaderOrder {
			if r.Header.Get(name) == "" {
				http.Error(w, "missing signed header", http.StatusBadRequest)
				return
			}
		}
		uploadMu.Lock()
		uploaded[platform]++
		uploadMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	state := appregistry.PublishStateUploading
	publishID := "pub_test123"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/finalize"):
			state = appregistry.PublishStatePublished
			writeTestJSON(w, PublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/publishes"):
			if state == appregistry.PublishStatePublished {
				writeTestJSON(w, PublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
				return
			}
			var uploads []PublishUpload
			if uploaded["linux/amd64"] == 0 {
				uploads = append(uploads, PublishUpload{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64?sha256=" + linuxDigest, Headers: linuxHeaders})
			}
			if uploaded["darwin/arm64"] == 0 {
				uploads = append(uploads, PublishUpload{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64?sha256=" + darwinDigest, Headers: darwinHeaders})
			}
			writeTestJSON(w, PublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, Uploads: uploads})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	var out bytes.Buffer
	input := PublishInput{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		Client: &Client{BaseURL: apiServer.URL, Token: "token"}, CommandRunner: runner, Output: &out,
	}
	result, err := Publish(t.Context(), input)
	if err != nil || result.State != appregistry.PublishStatePublished || uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("Publish() = %#v uploads=%v err=%v", result, uploaded, err)
	}
	state = appregistry.PublishStatePublished
	if _, err := Publish(t.Context(), input); err != nil || uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("resume changed uploads: %#v err=%v", uploaded, err)
	}
}

func TestRemotePublishAlreadyPublishedAndAuth(t *testing.T) { //nolint:paralleltest // chdirs
	registerTestPublishHelpers(t)
	_, distDir, runner := setupTestPublishRepo(t)
	var authHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		writeTestJSON(w, PublishResponse{PublishID: "pub_done", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
	}))
	t.Cleanup(apiServer.Close)

	result, err := Publish(t.Context(), PublishInput{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "explicit-token",
		Client: &Client{BaseURL: apiServer.URL, Token: "explicit-token"}, CommandRunner: runner,
	})
	if err != nil || result.PublishID != "pub_done" || authHeader != "Bearer explicit-token" {
		t.Fatalf("Publish() = %#v auth=%q err=%v", result, authHeader, err)
	}
}

func TestRemotePublishInterruptedResume(t *testing.T) { //nolint:paralleltest // chdirs
	registerTestPublishHelpers(t)
	_, distDir, runner := setupTestPublishRepo(t)
	_, darwinHeaders := artifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))
	uploaded := map[string]bool{}
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploaded[strings.TrimPrefix(r.URL.Path, "/upload/")] = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	firstCreate := true
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			writeTestJSON(w, PublishResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
		case strings.HasSuffix(r.URL.Path, "/publishes"):
			if firstCreate {
				firstCreate = false
				_, linuxHeaders := artifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
				writeTestJSON(w, PublishResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []PublishUpload{
						{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64", Headers: linuxHeaders},
						{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders},
					},
				})
				return
			}
			if !uploaded["darwin/arm64"] {
				writeTestJSON(w, PublishResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []PublishUpload{{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders}},
				})
				return
			}
			writeTestJSON(w, PublishResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading})
		}
	}))
	t.Cleanup(apiServer.Close)

	uploaded["linux/amd64"] = true
	if _, err := Publish(t.Context(), PublishInput{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		Client: &Client{BaseURL: apiServer.URL, Token: "token"}, CommandRunner: runner,
	}); err != nil || !uploaded["darwin/arm64"] {
		t.Fatalf("resume upload = %#v err=%v", uploaded, err)
	}
}

func TestPreNetworkValidationAndProvenance(t *testing.T) {
	t.Parallel()
	runner := fakeRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": "/repo\n",
		"git -C /repo/apps/demo rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C /repo/apps/demo status --porcelain":        " M file\n?? scratch\n",
	}
	state := collectLocalSourceState("/repo/apps/demo/manifest.yaml", runner)
	if state == nil || !state.Dirty || !state.Untracked {
		t.Fatalf("state = %#v", state)
	}
	if collectLocalSourceState("/tmp/manifest.yaml", fakeRunner{}) != nil {
		t.Fatal("expected nil local source for non-git")
	}
	_, err := buildPublishDeclaration("demo", "0.3.0-dev.1", "apps/demo/manifest.yaml", testManifest("demo", "0.3.0-dev.1"), testReleaseMetadata("demo", "0.3.0-dev.1"), []releaseArchive{
		{Target: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1},
	}, state, "1.2.3")
	if err == nil || !errors.Is(err, appregistry.ErrPublishRequiredPlatform) {
		t.Fatalf("buildPublishDeclaration() = %v, want required platform error", err)
	}
	decl, err := buildPublishDeclaration("demo", "0.3.0-dev.1", "apps/demo/manifest.yaml", testManifest("demo", "0.3.0-dev.1"), testReleaseMetadata("demo", "0.3.0-dev.1"), []releaseArchive{
		{Target: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1},
		{Target: "darwin/arm64", Filename: "darwin-arm64.tar.gz", SHA256: strings.Repeat("b", 64), Size: 1},
	}, state, "1.2.3")
	if err != nil || decl.SourceRef != "" || decl.BuilderVersion != "1.2.3" {
		t.Fatalf("declaration = %#v err=%v", decl, err)
	}
}

func TestUploaderHeadersAndRedaction(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(4, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSignedUploadLeaseHeaders("linux/amd64", headers, 4, digest); err != nil {
		t.Fatalf("validate headers: %v", err)
	}
	headers[appregistry.UploadHeaderContentLength] = "99"
	if err := validateSignedUploadLeaseHeaders("linux/amd64", headers, 4, digest); err == nil {
		t.Fatal("expected content-length mismatch")
	}

	data := []byte("payload")
	path := t.TempDir() + "/artifact.tgz"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	uploader := &Uploader{}
	err = uploader.Upload(t.Context(), ArtifactUploadInput{
		Platform: "linux/amd64", LocalPath: path, SHA256: digest,
		UploadURL: server.URL, Headers: headers,
	})
	if err == nil || requests.Load() != 0 {
		t.Fatalf("Upload() = %v requests=%d", err, requests.Load())
	}

	got := redactSecrets("Bearer secret-token X-Goog-Signature=abc")
	if strings.Contains(got, "secret-token") || strings.Contains(got, "abc") {
		t.Fatalf("redactSecrets() = %q", got)
	}
	if _, err := (&Client{BaseURL: "https://example.com", Token: ""}).Begin(t.Context(), "demo", nil); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("Begin() = %v", err)
	}
}

func TestPrintPublishResult(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printPublishResult(&buf, PublishResult{PublishID: "pub_1", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished, AdminURL: "https://valon.tools/apps/demo/admin/registry"})
	if !strings.Contains(buf.String(), "publishId: pub_1") || !strings.Contains(buf.String(), "adminUrl:") {
		t.Fatalf("output = %q", buf.String())
	}
}

func artifactFixture(t *testing.T, path string) (digest string, headers map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest = hex.EncodeToString(sum[:])
	headers, err = appregistry.BuildSignedUploadHeaders(int64(len(data)), digest)
	if err != nil {
		t.Fatal(err)
	}
	return digest, headers
}

func writeTestJSON(w http.ResponseWriter, payload any) {
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
					archives = append(archives, DaemonReleaseArchive{Path: path, SHA256: hex.EncodeToString(sum[:]), Target: platform, Size: int64(len(data))})
				}
			}
			return testManifest("demo", version), version, archives, nil
		},
		BuildProviderReleaseMetadata: func(*providermanifestv1.Manifest, string, []DaemonReleaseArchive, []byte) (*providerrelease.Metadata, error) {
			return testReleaseMetadata("demo", "0.3.0-dev.1"), nil
		},
		ValidateProviderPublishManifest: func(source, release *providermanifestv1.Manifest, releaseVersion, version string) error {
			if source.Source != release.Source || releaseVersion != version {
				return appregistry.ErrPublishDeclarationInvalid
			}
			return nil
		},
		ResolvePublishManifest: func(appName string) (string, string, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", "", err
			}
			path := filepath.Join(cwd, "apps", appName, "manifest.yaml")
			return path, filepath.ToSlash(filepath.Join("apps", appName, "manifest.yaml")), nil
		},
	})
}

var testWorkingDirMu sync.Mutex

func setupTestPublishRepo(t *testing.T) (root, distDir string, runner fakeRunner) {
	t.Helper()
	testWorkingDirMu.Lock()
	t.Cleanup(testWorkingDirMu.Unlock)
	root = t.TempDir()
	manifestDir := filepath.Join(root, "apps", "demo")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.yaml"), []byte("kind: app\nsource: github.com/valon-technologies/valon-tools/apps/demo\nversion: 0.3.0-dev.1\nspec: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	distDir = filepath.Join(root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"linux-amd64.tar.gz": "linux", "darwin-arm64.tar.gz": "darwin"} {
		if err := os.WriteFile(filepath.Join(distDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	workDir, _ := os.Getwd()
	manifestDirAbs := filepath.Join(workDir, "apps", "demo")
	distDir = filepath.Join(workDir, "dist")
	runner = fakeRunner{
		"git -C " + workDir + " rev-parse --show-toplevel":        workDir + "\n",
		"git -C " + manifestDirAbs + " rev-parse --show-toplevel": workDir + "\n",
		"git -C " + manifestDirAbs + " rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C " + manifestDirAbs + " status --porcelain":        "",
	}
	return workDir, distDir, runner
}

func testManifest(appName, version string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{Kind: "app", Source: "github.com/valon-technologies/valon-tools/apps/" + appName, Version: version, Spec: &providermanifestv1.Spec{}}
}

func testReleaseMetadata(appName, version string) *providerrelease.Metadata {
	return &providerrelease.Metadata{
		Schema: providerrelease.SchemaName, SchemaVersion: providerrelease.SchemaVersion,
		Package: "github.com/valon-technologies/valon-tools/apps/" + appName, Kind: "app", Version: version, Runtime: providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64)}},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: testManifest(appName, version),
			Catalog:  &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
}
