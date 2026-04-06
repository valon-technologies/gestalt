package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const (
	testOwner   = "testowner"
	testRepo    = "testrepo"
	testPlugin  = "testplugin"
	testVersion = "1.0.0"
	testSource  = "github.com/" + testOwner + "/" + testRepo + "/" + testPlugin
	testBinary  = "fake-binary-content"
)

type fakeResolver struct {
	archivePath string
	resolvedURL string
	sha256      string
	calls       int
}

func (f *fakeResolver) Resolve(_ context.Context, _ pluginsource.Source, _ string) (*pluginsource.ResolvedPackage, error) {
	f.calls++
	return &pluginsource.ResolvedPackage{
		LocalPath:     f.archivePath,
		Cleanup:       func() {},
		ArchiveSHA256: f.sha256,
		ResolvedURL:   f.resolvedURL,
	}, nil
}

func sha256hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func artifactRelPath(binary string) string {
	return filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, binary))
}

func buildV2Archive(t *testing.T, dir, source, version, binaryContent string) string {
	t.Helper()

	artPath := artifactRelPath("provider")
	srcDir := filepath.Join(dir, "provider-src")
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artPath))), 0755); err != nil {
		t.Fatalf("create provider src dir: %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artPath},
		},
	}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artPath)), []byte(binaryContent), 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir provider: %v", err)
	}

	return archivePath
}

func writeConfigYAML(t *testing.T, dir, source, version, artifactsDir string) string {
	t.Helper()

	lines := []string{
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"providers:",
		"  alpha:",
		"    from:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
	}

	yaml := strings.Join(lines, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestSourcePluginEndToEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/widget"
	version := "1.0.0"
	binaryContent := "fake-binary-v1"
	resolvedURL := "https://github.com/acme/tools/releases/download/v1.0.0/gestalt-plugin-widget_v1.0.0.tar.gz"

	archivePath := buildV2Archive(t, dir, source, version, binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	resolver := &fakeResolver{
		archivePath: archivePath,
		resolvedURL: resolvedURL,
		sha256:      archiveSHA,
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := writeConfigYAML(t, dir, source, version, artifactsDir)

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	if lock.Version != LockVersion {
		t.Errorf("lock version = %d, want %d", lock.Version, LockVersion)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal("lock entry for provider alpha not found")
	}
	if entry.Source != source {
		t.Errorf("entry.Source = %q, want %q", entry.Source, source)
	}
	if entry.Version != version {
		t.Errorf("entry.Version = %q, want %q", entry.Version, version)
	}
	if entry.ResolvedURL != resolvedURL {
		t.Errorf("entry.ResolvedURL = %q, want %q", entry.ResolvedURL, resolvedURL)
	}
	if entry.ArchiveSHA256 != archiveSHA {
		t.Errorf("entry.ArchiveSHA256 = %q, want %q", entry.ArchiveSHA256, archiveSHA)
	}
	if entry.Manifest == "" {
		t.Error("entry.Manifest is empty")
	}
	if entry.Executable == "" {
		t.Error("entry.Executable is empty")
	}

	configDir := filepath.Dir(configPath)
	manifestPath := resolveLockPath(artifactsDir, entry.Manifest)
	executablePath := resolveLockPath(artifactsDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not found at %s: %v", manifestPath, err)
	}
	if _, err := os.Stat(executablePath); err != nil {
		t.Errorf("executable not found at %s: %v", executablePath, err)
	}

	wantPrefix := ".gestaltd/providers/alpha/"
	if !strings.HasPrefix(entry.Manifest, wantPrefix) {
		t.Errorf("manifest path %q does not start with %q", entry.Manifest, wantPrefix)
	}

	if resolver.calls != 1 {
		t.Errorf("resolver called %d times during init, want 1", resolver.calls)
	}

	lockPath := filepath.Join(configDir, InitLockfileName)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	var written Lockfile
	if err := json.Unmarshal(lockData, &written); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	if written.Version != LockVersion {
		t.Errorf("written lockfile version = %d, want %d", written.Version, LockVersion)
	}

	readBack, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	readEntry := readBack.Providers["alpha"]
	if readEntry.Source != source {
		t.Errorf("readback Source = %q, want %q", readEntry.Source, source)
	}
}

func TestSourcePluginNilResolver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/widget"
	version := "1.0.0"

	configPath := writeConfigYAML(t, dir, source, version, filepath.Join(dir, "prepared-artifacts"))

	lc := NewLifecycle(nil)
	_, err := lc.InitAtPath(configPath)
	if err == nil {
		t.Fatal("expected error with nil resolver")
	}
	if !strings.Contains(err.Error(), "source resolver") {
		t.Errorf("error = %q, want to contain 'source resolver'", err.Error())
	}
}

func TestSourcePluginLoadForExecution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/gadget"
	version := "2.0.0"
	binaryContent := "fake-gadget-binary"

	archivePath := buildV2Archive(t, dir, source, version, binaryContent)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	var downloadCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadCount.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	resolver := &fakeResolver{
		archivePath: archivePath,
		resolvedURL: srv.URL + "/plugin.tar.gz",
		sha256:      archiveSHA,
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	yaml := strings.Join([]string{
		"auth:",
		"  provider: noop",
		"datastore:",
		"  provider: sqlite",
		"  config:",
		"    path: " + filepath.Join(dir, "data.db"),
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"providers:",
		"  gadget:",
		"    from:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	_, err = lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	callsBefore := resolver.calls
	_, _, err = lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if resolver.calls != callsBefore {
		t.Errorf("resolver called during LoadForExecution (locked), want no additional calls")
	}

	lock, err := ReadLockfile(filepath.Join(filepath.Dir(configPath), InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry := lock.Providers["gadget"]
	pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "gadget")
	if err := os.RemoveAll(pluginRoot); err != nil {
		t.Fatalf("RemoveAll plugin root: %v", err)
	}

	callsBefore = resolver.calls
	downloadsBefore := downloadCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath after cache removal: %v", err)
	}
	if resolver.calls != callsBefore {
		t.Fatalf("resolver called during locked rehydration: got %d, want %d", resolver.calls, callsBefore)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during locked rehydration = %d, want 1", got)
	}

	manifestPath := resolveLockPath(artifactsDir, entry.Manifest)
	executablePath := resolveLockPath(artifactsDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest not rehydrated at %s: %v", manifestPath, err)
	}
	if _, err := os.Stat(executablePath); err != nil {
		t.Fatalf("executable not rehydrated at %s: %v", executablePath, err)
	}
	if cfg.Providers["gadget"].Plugin.Command != executablePath {
		t.Fatalf("plugin command = %q, want %q", cfg.Providers["gadget"].Plugin.Command, executablePath)
	}
}

func TestSourcePluginGitHubResolverEndToEnd(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	dir := t.TempDir()

	src := pluginsource.Source{
		Host:   pluginsource.HostGitHub,
		Owner:  testOwner,
		Repo:   testRepo,
		Plugin: testPlugin,
	}
	expectedAssetName := src.AssetName(testVersion)
	expectedTag := src.ReleaseTag(testVersion)
	releasePath := "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag
	archivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	var releaseCount atomic.Int64
	var assetCount atomic.Int64
	handlerErrs := make(chan error, 3)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			releaseCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("release authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad release authorization", http.StatusBadRequest)
				return
			}
			browserURL := "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + expectedTag + "/" + expectedAssetName
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"assets": []map[string]any{
					{
						"name":                 expectedAssetName,
						"url":                  "http://" + r.Host + "/asset-dl",
						"browser_download_url": browserURL,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/asset-dl":
			assetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"auth:",
		"  provider: noop",
		"datastore:",
		"  provider: sqlite",
		"  config:",
		"    path: " + filepath.Join(dir, "data.db"),
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"providers:",
		"  alpha:",
		"    from:",
		"      source:",
		"        ref: " + testSource,
		"        version: " + testVersion,
	}, "\n") + "\n"
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolver := &ghresolver.GitHubResolver{BaseURL: srv.URL}
	lc := NewLifecycle(resolver)

	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	if lock.Version != LockVersion {
		t.Errorf("lock version = %d, want %d", lock.Version, LockVersion)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal("lock entry for provider alpha not found")
	}
	if entry.Source != testSource {
		t.Errorf("entry.Source = %q, want %q", entry.Source, testSource)
	}
	if entry.Version != testVersion {
		t.Errorf("entry.Version = %q, want %q", entry.Version, testVersion)
	}
	if entry.ResolvedURL == "" {
		t.Error("entry.ResolvedURL is empty")
	}
	if entry.ResolvedURL != srv.URL+"/asset-dl" {
		t.Errorf("entry.ResolvedURL = %q, want %q", entry.ResolvedURL, srv.URL+"/asset-dl")
	}
	if entry.ArchiveSHA256 == "" {
		t.Error("entry.ArchiveSHA256 is empty")
	}
	wantSHA := sha256hex(string(archiveData))
	if entry.ArchiveSHA256 != wantSHA {
		t.Errorf("entry.ArchiveSHA256 = %q, want %q", entry.ArchiveSHA256, wantSHA)
	}

	configDir := filepath.Dir(configPath)
	lockPath := filepath.Join(configDir, InitLockfileName)
	readBack, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if readBack.Version != LockVersion {
		t.Errorf("lockfile on disk version = %d, want %d", readBack.Version, LockVersion)
	}

	wantDirName := string(filepath.Separator) + filepath.Join(".gestaltd", "providers", "alpha") + string(filepath.Separator)
	executablePath := resolveLockPath(artifactsDir, entry.Executable)
	if !strings.Contains(executablePath, wantDirName) {
		t.Errorf("executable path %q does not contain expected dir %q", executablePath, wantDirName)
	}
	if _, err := os.Stat(executablePath); err != nil {
		t.Errorf("executable not found at %s: %v", executablePath, err)
	}
	manifestPath := resolveLockPath(artifactsDir, entry.Manifest)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not found at %s: %v", manifestPath, err)
	}

	execData, err := os.ReadFile(executablePath)
	if err != nil {
		t.Fatalf("read executable: %v", err)
	}
	if string(execData) != testBinary {
		t.Errorf("executable content = %q, want %q", execData, testBinary)
	}

	pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "alpha")
	if err := os.RemoveAll(pluginRoot); err != nil {
		t.Fatalf("RemoveAll plugin root: %v", err)
	}

	releasesBefore := releaseCount.Load()
	assetsBefore := assetCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := releaseCount.Load(); got != releasesBefore {
		t.Errorf("release request count during locked load = %d, want %d", got, releasesBefore)
	}
	if got := assetCount.Load() - assetsBefore; got != 1 {
		t.Errorf("asset request count during locked load = %d, want 1", got)
	}
	if cfg.Providers["alpha"].Plugin.Command != executablePath {
		t.Errorf("plugin command = %q, want %q", cfg.Providers["alpha"].Plugin.Command, executablePath)
	}
}
