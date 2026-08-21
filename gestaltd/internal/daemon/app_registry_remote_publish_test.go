package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const remotePublishTestBuilderVersion = "0.0.1-test-builder"

type fakeRemoteGitRunner map[string]fakeRemoteGitResult

type fakeRemoteGitResult struct {
	Out string
	Err error
}

func (f fakeRemoteGitRunner) Run(name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if result, ok := f[key]; ok {
		return result.Out, result.Err
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func fakeGitNotRepositoryError() error {
	return &providerPublishCommandError{
		Name:   "git",
		Args:   []string{"-C", "/tmp", "rev-parse", "--show-toplevel"},
		Err:    fmt.Errorf("exit status 128"),
		Stderr: "fatal: not a git repository (or any of the parent directories): .git\n",
	}
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
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
		case strings.HasSuffix(r.URL.Path, "/publishes"):
			if state == appregistry.PublishStatePublished {
				writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, PublishedAt: "2026-08-17T12:00:00Z"})
				return
			}
			var uploads []appregistry.AdminPublishUpload
			if uploaded["linux/amd64"] == 0 {
				uploads = append(uploads, appregistry.AdminPublishUpload{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64?sha256=" + linuxDigest, Headers: linuxHeaders})
			}
			if uploaded["darwin/arm64"] == 0 {
				uploads = append(uploads, appregistry.AdminPublishUpload{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64?sha256=" + darwinDigest, Headers: darwinHeaders})
			}
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: publishID, App: "demo", Version: "0.3.0-dev.1", State: state, Uploads: uploads})
		}
	}))
	t.Cleanup(apiServer.Close)

	var out bytes.Buffer
	pub := &remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"}, Output: &out,
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

func TestRemoteRegistryPublishUploadsOverlapAndFinalizeAfterCompletion(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	_, linuxHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
	_, darwinHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))

	var active, maxActive, completed, finalized atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nowActive := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maxActive.Load()
			if nowActive <= previous || maxActive.CompareAndSwap(previous, nowActive) {
				break
			}
		}
		if nowActive == 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		completed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/finalize") {
			finalized.Store(completed.Load())
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
				PublishID: "pub_concurrent", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished,
			})
			return
		}
		writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
			PublishID: "pub_concurrent", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
			Uploads: []appregistry.AdminPublishUpload{
				{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/linux", Headers: linuxHeaders},
				{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/darwin", Headers: darwinHeaders},
			},
		})
	}))
	t.Cleanup(apiServer.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"},
		GitRunner:      base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(ctx)
	if err != nil {
		t.Fatalf("publish() err=%v", err)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("maximum concurrent uploads = %d, want at least 2", maxActive.Load())
	}
	if finalized.Load() != 2 {
		t.Fatalf("finalize observed %d completed uploads, want 2", finalized.Load())
	}
}

func TestRemoteRegistryPublishFailureCancelsSiblingAndSkipsFinalize(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	_, linuxHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
	_, darwinHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))

	siblingStarted := make(chan struct{})
	siblingCanceled := make(chan struct{})
	var startedOnce, canceledOnce sync.Once
	uploadClient := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/darwin":
			startedOnce.Do(func() { close(siblingStarted) })
			<-r.Context().Done()
			canceledOnce.Do(func() { close(siblingCanceled) })
			return nil, r.Context().Err()
		case "/linux":
			select {
			case <-siblingStarted:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader("controlled failure")),
			}, nil
		default:
			return nil, fmt.Errorf("unexpected upload path %q", r.URL.Path)
		}
	})}

	var finalizeCalls atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/finalize") {
			finalizeCalls.Add(1)
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
				PublishID: "pub_failed", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished,
			})
			return
		}
		writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
			PublishID: "pub_failed", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
			Uploads: []appregistry.AdminPublishUpload{
				{Platform: "linux/amd64", UploadURL: "http://upload.test/linux", Headers: linuxHeaders},
				{Platform: "darwin/arm64", UploadURL: "http://upload.test/darwin", Headers: darwinHeaders},
			},
		})
	}))
	t.Cleanup(apiServer.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"},
		Uploader:       &remoteRegistryUploader{HTTPClient: uploadClient},
		GitRunner:      base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(ctx)
	if err == nil || !strings.Contains(err.Error(), `upload platform "linux/amd64" returned 503`) {
		t.Fatalf("publish() err=%v, want controlled linux upload failure", err)
	}
	select {
	case <-siblingCanceled:
	default:
		t.Fatal("publish returned before the sibling upload observed cancellation")
	}
	if finalizeCalls.Load() != 0 {
		t.Fatalf("finalize calls = %d, want 0", finalizeCalls.Load())
	}
}

func BenchmarkRemoteRegistryArtifactUploads(b *testing.B) {
	payload := bytes.Repeat([]byte("benchmark"), 128)
	path := filepath.Join(b.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		b.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	headers, err := appregistry.BuildSignedUploadHeaders(int64(len(payload)), digest)
	if err != nil {
		b.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(15 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(server.Close)

	inputs := make([]remoteRegistryUploadInput, remoteRegistryUploadParallelism)
	for i := range inputs {
		inputs[i] = remoteRegistryUploadInput{
			Platform: fmt.Sprintf("benchmark/%d", i), LocalPath: path, SHA256: digest,
			UploadURL: fmt.Sprintf("%s/%d", server.URL, i), Headers: headers,
		}
	}
	uploader := &remoteRegistryUploader{}
	b.Run("serial", func(b *testing.B) {
		for range b.N {
			for _, input := range inputs {
				if err := uploader.upload(context.Background(), input); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("bounded-concurrent", func(b *testing.B) {
		for range b.N {
			if err := uploadRemoteRegistryArtifacts(context.Background(), uploader, inputs, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestRemoteRegistryPublishProvenanceWarningsUseStderr(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, runner, base := setupRemotePublishFixture(t)
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(workDir, "apps", "demo")
	runner["git -C "+manifestDir+" status --porcelain"] = fakeRemoteGitResult{Out: " M apps/demo/manifest.yaml\n"}

	var stdout bytes.Buffer
	_, err = (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir},
		GestaltURL: "http://example.invalid", GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion, Output: &stdout,
		GitRunner: runner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest,
		buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context())
	if err == nil {
		t.Fatal("expected publish to fail before remote API call")
	}
	if strings.Contains(stdout.String(), "warning:") {
		t.Fatalf("stdout = %q, want provenance warnings off structured stdout", stdout.String())
	}
}

func TestRemoteRegistryPublishAlreadyPublishedAndAuth(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	var authHeader string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: "pub_done", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
	}))
	t.Cleanup(apiServer.Close)

	result, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "explicit-token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "explicit-token"},
		GitRunner:      base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context())
	if err != nil || result.PublishID != "pub_done" || authHeader != "Bearer explicit-token" {
		t.Fatalf("publish() = %#v auth=%q err=%v", result, authHeader, err)
	}
}

func TestRemoteRegistryPublishInterruptedResume(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)
	_, darwinHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "darwin-arm64.tar.gz"))
	var stateMu sync.Mutex
	uploaded := map[string]bool{}
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		uploaded[strings.TrimPrefix(r.URL.Path, "/upload/")] = true
		stateMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(uploadServer.Close)

	firstCreate := true
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStatePublished})
		case strings.HasSuffix(r.URL.Path, "/publishes"):
			stateMu.Lock()
			first := firstCreate
			if firstCreate {
				firstCreate = false
			}
			darwinDone := uploaded["darwin/arm64"]
			stateMu.Unlock()

			if first {
				_, linuxHeaders := remotePublishArtifactFixture(t, filepath.Join(distDir, "linux-amd64.tar.gz"))
				writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []appregistry.AdminPublishUpload{
						{Platform: "linux/amd64", UploadURL: uploadServer.URL + "/upload/linux/amd64", Headers: linuxHeaders},
						{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders},
					},
				})
				return
			}
			if !darwinDone {
				writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
					PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading,
					Uploads: []appregistry.AdminPublishUpload{{Platform: "darwin/arm64", UploadURL: uploadServer.URL + "/upload/darwin/arm64", Headers: darwinHeaders}},
				})
				return
			}
			writeRemotePublishJSON(w, appregistry.AdminPublishResponse{PublishID: "pub_resume", App: "demo", Version: "0.3.0-dev.1", State: appregistry.PublishStateUploading})
		}
	}))
	t.Cleanup(apiServer.Close)

	stateMu.Lock()
	uploaded["linux/amd64"] = true
	stateMu.Unlock()
	if _, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"},
		GitRunner:      base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context()); err != nil {
		t.Fatalf("resume upload err=%v", err)
	}
	stateMu.Lock()
	darwinDone := uploaded["darwin/arm64"]
	stateMu.Unlock()
	if !darwinDone {
		t.Fatalf("resume upload = %#v, want darwin/arm64 uploaded", uploaded)
	}
}

func TestRemoteRegistryPublishRejectsMissingBuilderVersion(t *testing.T) { //nolint:paralleltest // chdirs
	_, distDir, _, base := setupRemotePublishFixture(t)

	_, err := (&remoteRegistryPublisher{
		Version: "0.3.0-dev.1", DistDirs: []string{distDir},
		GitRunner: base.GitRunner, collectArchives: base.collectArchives, resolveManifest: base.resolveManifest, buildReleaseMetadata: base.buildReleaseMetadata,
	}).publish(t.Context())
	if err == nil || !errors.Is(err, appregistry.ErrPublishDeclarationInvalid) || !strings.Contains(err.Error(), "builderVersion is required") {
		t.Fatalf("publish() = %v, want missing builderVersion validation error", err)
	}
}

func TestRemotePublishValidationAndProvenance(t *testing.T) {
	t.Parallel()
	runner := fakeRemoteGitRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": {Out: "/repo\n"},
		"git -C /repo/apps/demo rev-parse HEAD":            {Out: "651a5c30feb995c9364c38f63d0d5c3880bc2055\n"},
		"git -C /repo/apps/demo status --porcelain":        {Out: " M file\n?? scratch\n"},
	}
	state, err := collectRemoteLocalSourceState("/repo/apps/demo/manifest.yaml", runner)
	if err != nil || state == nil || !state.Dirty || !state.Untracked {
		t.Fatalf("state = %#v err=%v", state, err)
	}
	nonGitRunner := fakeRemoteGitRunner{
		"git -C /tmp rev-parse --show-toplevel": {Err: fakeGitNotRepositoryError()},
	}
	state, err = collectRemoteLocalSourceState("/tmp/manifest.yaml", nonGitRunner)
	if err != nil || state != nil {
		t.Fatalf("non-git state = %#v err=%v", state, err)
	}
	gitFailureRunner := fakeRemoteGitRunner{
		"git -C /tmp rev-parse --show-toplevel": {Err: fmt.Errorf("git: permission denied")},
	}
	if _, err := collectRemoteLocalSourceState("/tmp/manifest.yaml", gitFailureRunner); err == nil {
		t.Fatal("expected git failure error")
	}
	statusFailureRunner := fakeRemoteGitRunner{
		"git -C /repo/apps/demo rev-parse --show-toplevel": {Out: "/repo\n"},
		"git -C /repo/apps/demo rev-parse HEAD":            {Out: "651a5c30feb995c9364c38f63d0d5c3880bc2055\n"},
		"git -C /repo/apps/demo status --porcelain":        {Err: fmt.Errorf("git status failed")},
	}
	if _, err := collectRemoteLocalSourceState("/repo/apps/demo/manifest.yaml", statusFailureRunner); err == nil {
		t.Fatal("expected git status failure")
	}
	archives := []releaseArchive{
		{Target: "linux/amd64", Path: filepath.Join(t.TempDir(), "linux-amd64.tar.gz"), SHA256: strings.Repeat("a", 64)},
	}
	if err := os.WriteFile(archives[0].Path, []byte("linux"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = buildRemotePublishDeclaration("demo", "0.3.0-dev.1", "apps/demo/manifest.yaml", remoteTestManifest("demo", "0.3.0-dev.1"), remoteTestReleaseMetadata("demo", "0.3.0-dev.1"), archives, state, "1.2.3")
	if err == nil || !errors.Is(err, appregistry.ErrPublishRequiredPlatform) {
		t.Fatalf("buildRemotePublishDeclaration() = %v", err)
	}
	_, err = buildRemotePublishDeclaration("demo", "0.3.0-dev.1", "apps/demo/manifest.yaml", remoteTestManifest("demo", "0.3.0-dev.1"), remoteTestReleaseMetadata("demo", "0.3.0-dev.1"), []releaseArchive{
		{Target: "linux/amd64", Path: filepath.Join(t.TempDir(), "missing.tgz"), SHA256: strings.Repeat("a", 64)},
		{Target: "darwin/arm64", Path: filepath.Join(t.TempDir(), "missing2.tgz"), SHA256: strings.Repeat("b", 64)},
	}, nil, "1.2.3")
	if err == nil || !strings.Contains(err.Error(), "stat archive") {
		t.Fatalf("buildRemotePublishDeclaration() stat = %v", err)
	}
}

func TestRemoteRegistryPublishUsesReleaseManifestMetadata(t *testing.T) { //nolint:paralleltest // chdirs
	const (
		checkoutVersion = "0.2.0"
		releaseVersion  = "0.3.0-dev.1"
	)
	root, distDir, runner := setupRemotePublishReleaseManifestFixture(t, checkoutVersion, releaseVersion, "Checkout Display", "Release Display")

	var capturedDeclaration *appregistry.PublishDeclaration
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/publishes") && r.Method == http.MethodPost {
			var payload struct {
				Declaration *appregistry.PublishDeclaration `json:"declaration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			capturedDeclaration = payload.Declaration
		}
		writeRemotePublishJSON(w, appregistry.AdminPublishResponse{
			PublishID: "pub_release_meta", App: "demo", Version: releaseVersion, State: appregistry.PublishStatePublished,
		})
	}))
	t.Cleanup(apiServer.Close)

	result, err := (&remoteRegistryPublisher{
		Version: releaseVersion, DistDirs: []string{distDir}, GestaltURL: apiServer.URL, GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		Client:         &remoteRegistryClient{BaseURL: apiServer.URL, Token: "token"},
		GitRunner:      runner,
		resolveManifest: func(appName string) (string, string, error) {
			return filepath.Join(root, "apps", appName, "manifest.yaml"), filepath.ToSlash(filepath.Join("apps", appName, "manifest.yaml")), nil
		},
	}).publish(t.Context())
	if err != nil {
		t.Fatalf("publish() err=%v", err)
	}
	if result.State != appregistry.PublishStatePublished {
		t.Fatalf("publish() state = %q, want published", result.State)
	}
	if capturedDeclaration == nil || capturedDeclaration.ReleaseMetadata == nil {
		t.Fatal("expected publish declaration with release metadata")
	}
	if capturedDeclaration.ReleaseMetadata.Version != releaseVersion {
		t.Fatalf("release metadata version = %q, want %q", capturedDeclaration.ReleaseMetadata.Version, releaseVersion)
	}
	if capturedDeclaration.ReleaseMetadata.StaticValidation == nil || capturedDeclaration.ReleaseMetadata.StaticValidation.Manifest == nil {
		t.Fatal("expected static validation manifest in release metadata")
	}
	if capturedDeclaration.ReleaseMetadata.StaticValidation.Manifest.Version != releaseVersion {
		t.Fatalf("static validation manifest version = %q, want release version %q", capturedDeclaration.ReleaseMetadata.StaticValidation.Manifest.Version, releaseVersion)
	}
	if capturedDeclaration.ReleaseMetadata.StaticValidation.Manifest.DisplayName != "Release Display" {
		t.Fatalf("static validation displayName = %q, want release manifest display name", capturedDeclaration.ReleaseMetadata.StaticValidation.Manifest.DisplayName)
	}
	if capturedDeclaration.Manifest == nil || capturedDeclaration.Manifest.Version != releaseVersion {
		t.Fatalf("declaration manifest version = %q, want requested version %q", capturedDeclaration.Manifest.Version, releaseVersion)
	}
}

func TestRemoteRegistryPublishRejectsReleaseManifestMismatch(t *testing.T) { //nolint:paralleltest // chdirs
	const releaseVersion = "0.3.0-dev.1"
	root, distDir, runner := setupRemotePublishReleaseManifestFixture(t, releaseVersion, releaseVersion, "Checkout Display", "Release Display")
	if err := os.WriteFile(filepath.Join(root, "apps", "demo", "manifest.yaml"), []byte("kind: ui\nsource: github.com/valon-technologies/valon-tools/apps/demo\nversion: "+releaseVersion+"\nspec: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (&remoteRegistryPublisher{
		Version: releaseVersion, DistDirs: []string{distDir}, GestaltURL: "http://example.test", GestaltToken: "token",
		BuilderVersion: remotePublishTestBuilderVersion,
		GitRunner:      runner,
		resolveManifest: func(appName string) (string, string, error) {
			return filepath.Join(root, "apps", appName, "manifest.yaml"), filepath.ToSlash(filepath.Join("apps", appName, "manifest.yaml")), nil
		},
	}).publish(t.Context())
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("publish() = %v, want release/source kind mismatch error", err)
	}
}

func TestRedactRemotePublishSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		input          string
		want           string
		mustNotContain []string
	}{
		{
			name:  "preserves trailing diagnostics after query token",
			input: "upload failed token=secret123; retry after 30s",
			want:  "upload failed token=[REDACTED]; retry after 30s",
		},
		{
			name:  "preserves trailing diagnostics after bearer",
			input: "Bearer secret-token request denied status=403",
			want:  "Bearer [REDACTED] request denied status=403",
		},
		{
			name:  "preserves trailing diagnostics after signature",
			input: "denied X-Goog-Signature=abc123 while validating upload status=403",
			want:  "denied X-Goog-Signature=[REDACTED] while validating upload status=403",
		},
		{
			name:  "redacts json token and keeps trailing fields",
			input: `{"error":"invalid","token":"secret-token","code":403}`,
			want:  `{"error":"invalid","token":"[REDACTED]","code":403}`,
		},
		{
			name:  "redacts json upload url and keeps trailing fields",
			input: `{"uploadUrl":"https://storage.googleapis.com/bucket/object?X-Goog-Signature=abc","status":"uploading"}`,
			want:  `{"uploadUrl":"[REDACTED]","status":"uploading"}`,
		},
		{
			name:  "redacts signed upload url and keeps trailing diagnostics",
			input: "transport failed for https://storage.googleapis.com/bucket/object?X-Goog-Signature=abc123 after timeout",
			want:  "transport failed for [REDACTED-URL] after timeout",
		},
		{
			name:           "mixed secrets",
			input:          "Bearer secret-token X-Goog-Signature=abc https://example/upload?token=xyz status=503",
			want:           "Bearer [REDACTED] X-Goog-Signature=[REDACTED] https://example/upload?token=[REDACTED] status=503",
			mustNotContain: []string{"secret-token", "abc", "xyz"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactRemotePublishSecrets(tc.input)
			if got != tc.want {
				t.Fatalf("redactRemotePublishSecrets() = %q, want %q", got, tc.want)
			}
			for _, secret := range tc.mustNotContain {
				if strings.Contains(got, secret) {
					t.Fatalf("redactRemotePublishSecrets() leaked %q in %q", secret, got)
				}
			}
		})
	}
}

func setupRemotePublishReleaseManifestFixture(t *testing.T, checkoutVersion, releaseVersion, checkoutDisplay, releaseDisplay string) (root, distDir string, runner fakeRemoteGitRunner) {
	t.Helper()
	remotePublishWorkingDirMu.Lock()
	t.Cleanup(remotePublishWorkingDirMu.Unlock)
	root = t.TempDir()
	manifestDir := filepath.Join(root, "apps", "demo")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checkoutManifest := demoReleaseManifestForTest(checkoutVersion, checkoutDisplay, "linux", "amd64")
	checkoutManifest.Artifacts = nil
	checkoutManifest.Entrypoint = nil
	checkoutData, err := encodeTestManifestFormat(checkoutManifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.yaml"), checkoutData, 0o644); err != nil {
		t.Fatal(err)
	}
	distDir = filepath.Join(root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	releaseManifest := demoReleaseManifestForTest(releaseVersion, releaseDisplay, "linux", "amd64")
	writeProviderReleaseArchiveForTest(t, distDir, "linux-amd64.tar.gz", releaseManifest)
	releaseManifest = demoReleaseManifestForTest(releaseVersion, releaseDisplay, "darwin", "arm64")
	writeProviderReleaseArchiveForTest(t, distDir, "darwin-arm64.tar.gz", releaseManifest)
	runner = fakeRemoteGitRunner{
		"git -C " + manifestDir + " rev-parse --show-toplevel": {Out: root + "\n"},
		"git -C " + manifestDir + " rev-parse HEAD":            {Out: "651a5c30feb995c9364c38f63d0d5c3880bc2055\n"},
		"git -C " + manifestDir + " status --porcelain":        {Out: ""},
	}
	return root, distDir, runner
}

func demoReleaseManifestForTest(version, displayName, goos, goarch string) *providermanifestv1.Manifest {
	artifactPath := filepath.ToSlash(filepath.Join("bin", "provider-"+goos+"-"+goarch))
	return &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/valon-technologies/valon-tools/apps/demo",
		Version:     version,
		DisplayName: displayName,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:   goos,
			Arch: goarch,
			Path: artifactPath,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
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
	payload := []byte("payload")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	validHeaders, err := appregistry.BuildSignedUploadHeaders(int64(len(payload)), digest)
	if err != nil {
		t.Fatal(err)
	}
	signedURL := "https://storage.googleapis.com/bucket/object?X-Goog-Algorithm=GOOG4&X-Goog-Signature=secret-signature-value"
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
	transportErr := (&remoteRegistryUploader{HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial tcp: connection refused to %s with X-Goog-Signature=secret-signature-value", signedURL)
	})}}).upload(t.Context(), remoteRegistryUploadInput{
		Platform: "linux/amd64", LocalPath: path, SHA256: digest, UploadURL: signedURL, Headers: validHeaders,
	})
	if transportErr == nil {
		t.Fatal("expected transport error")
	}
	transportMsg := transportErr.Error()
	if strings.Contains(transportMsg, signedURL) || strings.Contains(transportMsg, "secret-signature-value") {
		t.Fatalf("transport error leaked secrets: %q", transportMsg)
	}
	statusErr := (&remoteRegistryUploader{HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("denied for " + signedURL))}, nil
	})}}).upload(t.Context(), remoteRegistryUploadInput{
		Platform: "linux/amd64", LocalPath: path, SHA256: digest, UploadURL: signedURL, Headers: validHeaders,
	})
	if statusErr == nil {
		t.Fatal("expected status error")
	}
	statusMsg := statusErr.Error()
	if strings.Contains(statusMsg, signedURL) || strings.Contains(statusMsg, "secret-signature-value") {
		t.Fatalf("status error leaked secrets: %q", statusMsg)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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
		"git -C " + workDir + " rev-parse --show-toplevel":        {Out: workDir + "\n"},
		"git -C " + manifestDirAbs + " rev-parse --show-toplevel": {Out: workDir + "\n"},
		"git -C " + manifestDirAbs + " rev-parse HEAD":            {Out: "651a5c30feb995c9364c38f63d0d5c3880bc2055\n"},
		"git -C " + manifestDirAbs + " status --porcelain":        {Out: ""},
	}
	pub = &remoteRegistryPublisher{
		BuilderVersion: remotePublishTestBuilderVersion,
		GitRunner:      runner,
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
		buildReleaseMetadata: func(_ *providermanifestv1.Manifest, _ string, archives []releaseArchive, _ []byte) (*providerrelease.Metadata, error) {
			return remoteTestReleaseMetadataFromArchives("demo", "0.3.0-dev.1", archives), nil
		},
	}
	return workDir, distDir, runner, pub
}

func remoteTestManifest(appName, version string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{Kind: "app", Source: "github.com/valon-technologies/valon-tools/apps/" + appName, Version: version, Spec: &providermanifestv1.Spec{}}
}

func remoteTestReleaseMetadata(appName, version string) *providerrelease.Metadata {
	return remoteTestReleaseMetadataFromArchives(appName, version, nil)
}

func remoteTestReleaseMetadataFromArchives(appName, version string, archives []releaseArchive) *providerrelease.Metadata {
	artifacts := providerrelease.Artifacts{"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: strings.Repeat("a", 64)}}
	if len(archives) > 0 {
		artifacts = providerrelease.Artifacts{}
		for _, archive := range archives {
			artifacts[strings.TrimSpace(archive.Target)] = providerrelease.Artifact{
				Path:   filepath.Base(archive.Path),
				SHA256: strings.ToLower(strings.TrimSpace(archive.SHA256)),
			}
		}
	}
	return &providerrelease.Metadata{
		Schema: providerrelease.SchemaName, SchemaVersion: providerrelease.SchemaVersion,
		Package: "github.com/valon-technologies/valon-tools/apps/" + appName, Kind: "app", Version: version, Runtime: providerrelease.RuntimeExecutable,
		Artifacts: artifacts,
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: remoteTestManifest(appName, version),
			Catalog:  &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
}
