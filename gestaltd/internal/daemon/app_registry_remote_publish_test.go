package daemon

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

type fakeRemoteGitRunner map[string]string

func (f fakeRemoteGitRunner) Run(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if out, ok := f[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestRemoteRegistryPublishFlowAndResume(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	linuxDigest, linuxHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
	darwinDigest, darwinHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))

	uploaded := map[string]int{}
	var uploadMu sync.Mutex
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range remoteSignedUploadHeaders {
			if r.Header.Get(name) == "" {
				http.Error(w, "missing signed header", http.StatusBadRequest)
				return
			}
		}
		uploadMu.Lock()
		uploaded[strings.TrimPrefix(r.URL.Path, "/upload/")]++
		uploadMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	state := appregistry.PublishStateUploading
	publishID := "pub_test123"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			state = appregistry.PublishStatePublished
			writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
		case strings.HasSuffix(r.URL.Path, "/publishes"):
			if state == appregistry.PublishStatePublished {
				writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
				return
			}
			var uploads []remoteRegistryUpload
			if uploaded["linux/amd64"] == 0 {
				uploads = append(uploads, remoteRegistryUpload{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64?sha256=" + linuxDigest, Headers: linuxHeaders})
			}
			if uploaded["darwin/arm64"] == 0 {
				uploads = append(uploads, remoteRegistryUpload{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64?sha256=" + darwinDigest, Headers: darwinHeaders})
			}
			writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, Uploads: uploads})
		}
	}))
	t.Cleanup(apiServer.Close)

	var out bytes.Buffer
	pub := &remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		Client: &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"}, Output: &out,
		GitRunner: base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}
	result, err := pub.publish(t.Context())
	if err != nil || result.State != appregistry.PublishStatePublished || uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("publish() = %#v uploads=%v err=%v", result, uploaded, err)
	}
	state = appregistry.PublishStatePublished
	if _, err := pub.publish(t.Context()); err != nil || uploaded["linux/amd64"] != 1 || uploaded["darwin/arm64"] != 1 {
		t.Fatalf("resume changed uploads: %#v err=%v", uploaded, err)
	}
}

func TestRemoteRegistryPublishAlreadyPublishedAndAuth(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	var authHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: "pub_done", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
	}))
	t.Cleanup(apiServer.Close)

	result, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "explicit-token",
		Client:    &remoteRegistryClient{BaseURL: apiServer.URL, Token: "explicit-token"},
		GitRunner: base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context())
	if err != nil || result.PublishID != "pub_done" || authHeader != "Bearer explicit-token" {
		t.Fatalf("publish() = %#v auth=%q err=%v", result, authHeader, err)
	}
}

func TestRemoteRegistryPublishInterruptedResume(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	_, darwinHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))
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
			writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
		case strings.HasSuffix(r.URL.Path, "/publishes"):
			if firstCreate {
				firstCreate = false
				_, linuxHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
				writeRemotePublishJSON(w, remoteRegistryResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []remoteRegistryUpload{
						{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64", Headers: linuxHeaders},
						{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders},
					},
				})
				return
			}
			if !uploaded["darwin/arm64"] {
				writeRemotePublishJSON(w, remoteRegistryResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []remoteRegistryUpload{{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders}},
				})
				return
			}
			writeRemotePublishJSON(w, remoteRegistryResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading})
		}
	}))
	t.Cleanup(apiServer.Close)

	uploaded["linux/amd64"] = true
	if _, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		Client:    &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"},
		GitRunner: base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context()); err != nil || !uploaded["darwin/arm64"] {
		t.Fatalf("resume upload = %#v err=%v", uploaded, err)
	}
}

func TestRemotePublishValidationAndProvenance(t *testing.T) {
	t.Parallel()
	runner := fakeRemoteGitRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": "/repo\n",
		"git -C /repo/apps/demo rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C /repo/apps/demo status --porcelain":        " M file\n?? scratch\n",
	}
	state := collectRemoteLocalSourceState("/repo/apps/demo/manifest.yaml", runner)
	if state == nil || !state.Dirty || !state.Untracked {
		t.Fatalf("state = %#v", state)
	}
	if collectRemoteLocalSourceState("/tmp/manifest.yaml", fakeRemoteGitRunner{}) != nil {
		t.Fatal("expected nil local source for non-git")
	}
	_, err := buildRemotePublishDeclaration("demo", "0.3.0-dev.1", "apps/demo/manifest.yaml", remoteTestManifest("demo", "0.3.0-dev.1"), remoteTestReleaseMetadata("demo", "0.3.0-dev.1"), []releaseArchive{
		{Target: "linux/amd64", Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64)},
	}, state, "1.2.3")
	if err == nil || !errors.Is(err, appregistry.ErrPublishRequiredPlatform) {
		t.Fatalf("buildRemotePublishDeclaration() = %v", err)
	}
}

func TestRemoteRegistryUploaderHeadersAndRedaction(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	headers, err := appregistry.BuildSignedUploadHeaders(4, digest)
	if err != nil {
		t.Fatal(err)
	}
	headers[appregistry.UploadHeaderContentLength] = "99"
	path := filepath.Join(t.TempDir(), "artifact.tgz")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	if err := (&remoteRegistryUploader{}).upload(t.Context(), remoteRegistryUploadInput{
		Platform: "linux/amd64", LocalPath: path, SHA256: digest, UploadURL: server.URL, Headers: headers,
	}); err == nil || requests.Load() != 0 {
		t.Fatalf("upload() = %v requests=%d", err, requests.Load())
	}
	got := redactRemotePublishSecrets("Bearer secret-token X-Goog-Signature=abc")
	if strings.Contains(got, "secret-token") || strings.Contains(got, "abc") {
		t.Fatalf("redactRemotePublishSecrets() = %q", got)
	}
}

func writeRemotePublishJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func remotePublishArtifactFixture(t *testing.T, path string) (digest string, headers map[string]string) {
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

var remotePublishWorkingDirMu sync.Mutex

func setupRemotePublishFixture(t *testing.T) (root, distDir string, runner fakeRemoteGitRunner, pub *remoteRegistryPublisher) {
	t.Helper()
	remotePublishWorkingDirMu.Lock()
	t.Cleanup(remotePublishWorkingDirMu.Unlock)
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
	runner = fakeRemoteGitRunner{
		"git -C " + workDir + " rev-parse --show-toplevel":        workDir + "\n",
		"git -C " + manifestDirAbs + " rev-parse --show-toplevel": workDir + "\n",
		"git -C " + manifestDirAbs + " rev-parse HEAD":            "651a5c30feb995c9364c38f63d0d5c3880bc2055\n",
		"git -C " + manifestDirAbs + " status --porcelain":        "",
	}
	pub = &remoteRegistryPublisher{
		GitRunner: runner,
		collectArchives: func(distDirs []string, version string) (*providermanifestv1.Manifest, string, []releaseArchive, error) {
			var archives []releaseArchive
			for _, dir := range distDirs {
				for _, name := range []string{"linux-amd64.tar.gz", "darwin-arm64.tar.gz"} {
					path := filepath.Join(dir, name)
					data, err := os.ReadFile(path)
					if err != nil {
						return nil, "", nil, err
					}
					sum := sha256.Sum256(data)
					platform := strings.ReplaceAll(strings.TrimSuffix(name, ".tar.gz"), "-", "/")
					archives = append(archives, releaseArchive{Path: path, SHA256: hex.EncodeToString(sum[:]), Target: platform})
				}
			}
			return remoteTestManifest("demo", version), version, archives, nil
		},
		resolveManifest: func(appName string) (string, string, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", "", err
			}
			path := filepath.Join(cwd, "apps", appName, "manifest.yaml")
			return path, filepath.ToSlash(filepath.Join("apps", appName, "manifest.yaml")), nil
		},
		buildReleaseMetadata: func(*providermanifestv1.Manifest, string, []releaseArchive, []byte) (*providerrelease.Metadata, error) {
			return remoteTestReleaseMetadata("demo", "0.3.0-dev.1"), nil
		},
	}
	return workDir, distDir, runner, pub
}

func remoteTestManifest(appName, version string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{Kind: "app", Source: "github.com/valon-technologies/valon-tools/apps/" + appName, Version: version, Spec: &providermanifestv1.Spec{}}
}

func remoteTestReleaseMetadata(appName, version string) *providerrelease.Metadata {
	return &providerrelease.Metadata{
		Schema: providerrelease.SchemaName, SchemaVersion: providerrelease.SchemaVersion,
		Package: "github.com/valon-technologies/valon-tools/apps/" + appName, Kind: "app", Version: version, Runtime: providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64)}},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: remoteTestManifest(appName, version),
			Catalog:  &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
}
