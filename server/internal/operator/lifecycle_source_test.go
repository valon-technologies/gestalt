package operator

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

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

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestBytes = append(manifestBytes, '\n')

	archivePath := filepath.Join(dir, "plugin.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer func() { _ = f.Close() }()

	gzw := gzip.NewWriter(f)
	defer func() { _ = gzw.Close() }()
	tw := tar.NewWriter(gzw)
	defer func() { _ = tw.Close() }()

	writeEntry := func(name string, data []byte, mode int64) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data))}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write data %s: %v", name, err)
		}
	}

	writeEntry("plugin.json", manifestBytes, 0644)
	writeEntry(artPath, []byte(binaryContent), 0755)

	return archivePath
}

func writeConfigYAML(t *testing.T, dir, source, version string) string {
	t.Helper()

	yaml := strings.Join([]string{
		"integrations:",
		"  alpha:",
		"    plugin:",
		"      source: " + source,
		"      version: " + version,
	}, "\n") + "\n"

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

	configPath := writeConfigYAML(t, dir, source, version)

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	if lock.Version != LockVersion {
		t.Errorf("lock version = %d, want %d", lock.Version, LockVersion)
	}

	entry, ok := lock.Plugins[LockPluginKey("integration", "alpha")]
	if !ok {
		t.Fatal("lock entry for integration:alpha not found")
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
	if entry.Package != "" {
		t.Errorf("entry.Package should be empty for source plugins, got %q", entry.Package)
	}

	configDir := filepath.Dir(configPath)
	manifestPath := resolveLockPath(configDir, entry.Manifest)
	executablePath := resolveLockPath(configDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not found at %s: %v", manifestPath, err)
	}
	if _, err := os.Stat(executablePath); err != nil {
		t.Errorf("executable not found at %s: %v", executablePath, err)
	}

	wantPrefix := ".gestalt/plugins/integration_alpha/"
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
	readEntry := readBack.Plugins[LockPluginKey("integration", "alpha")]
	if readEntry.Source != source {
		t.Errorf("readback Source = %q, want %q", readEntry.Source, source)
	}
}

func TestReadLockfileV3Compat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	v3Lock := Lockfile{
		Version:   LockVersionCompat,
		Providers: map[string]LockProviderEntry{},
		Plugins: map[string]LockPluginEntry{
			"integration:beta": {
				Fingerprint: "abc123",
				Package:     "/tmp/beta.tar.gz",
				Manifest:    ".gestalt/plugins/integration_beta/plugin.json",
				Executable:  ".gestalt/plugins/integration_beta/artifacts/darwin/arm64/provider",
			},
		},
	}

	lockPath := filepath.Join(dir, InitLockfileName)
	data, err := json.MarshalIndent(v3Lock, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lock, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile v3: %v", err)
	}
	if lock.Version != LockVersionCompat {
		t.Errorf("version = %d, want %d", lock.Version, LockVersionCompat)
	}

	entry := lock.Plugins["integration:beta"]
	if entry.Fingerprint != "abc123" {
		t.Errorf("fingerprint = %q, want %q", entry.Fingerprint, "abc123")
	}
	if entry.Package != "/tmp/beta.tar.gz" {
		t.Errorf("package = %q, want %q", entry.Package, "/tmp/beta.tar.gz")
	}

	if entry.Source != "" {
		t.Errorf("v3 source should be empty, got %q", entry.Source)
	}
	if entry.Version != "" {
		t.Errorf("v3 version should be empty, got %q", entry.Version)
	}
	if entry.ResolvedURL != "" {
		t.Errorf("v3 resolved_url should be empty, got %q", entry.ResolvedURL)
	}
	if entry.ArchiveSHA256 != "" {
		t.Errorf("v3 archive_sha256 should be empty, got %q", entry.ArchiveSHA256)
	}
}

func TestReadLockfileRejectsV2(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	data := []byte(`{"version": 2, "providers": {}, "plugins": {}}`)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ReadLockfile(lockPath)
	if err == nil {
		t.Fatal("expected error for version 2 lockfile")
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version") {
		t.Errorf("error = %q, want to contain 'unsupported lockfile version'", err.Error())
	}
}

func TestSourcePluginNilResolver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/widget"
	version := "1.0.0"

	configPath := writeConfigYAML(t, dir, source, version)

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
	resolvedURL := "https://github.com/acme/tools/releases/download/v2.0.0/gestalt-plugin-gadget_v2.0.0.tar.gz"

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

	yaml := strings.Join([]string{
		"auth:",
		"  provider: noop",
		"datastore:",
		"  provider: sqlite",
		"  config:",
		"    path: " + filepath.Join(dir, "data.db"),
		"server:",
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"integrations:",
		"  gadget:",
		"    plugin:",
		"      source: " + source,
		"      version: " + version,
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
}

func TestSourcePluginGitHubResolverEndToEnd(t *testing.T) {
	t.Parallel()

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

	var requestCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		switch r.URL.Path {
		case releasePath:
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
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	configYAML := strings.Join([]string{
		"auth:",
		"  provider: noop",
		"datastore:",
		"  provider: sqlite",
		"  config:",
		"    path: " + filepath.Join(dir, "data.db"),
		"server:",
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"integrations:",
		"  alpha:",
		"    plugin:",
		"      source: " + testSource,
		"      version: " + testVersion,
	}, "\n") + "\n"
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolver := &ghresolver.GitHubResolver{BaseURL: srv.URL}
	lc := NewLifecycle(resolver)

	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	if lock.Version != LockVersion {
		t.Errorf("lock version = %d, want %d", lock.Version, LockVersion)
	}

	entry, ok := lock.Plugins[LockPluginKey("integration", "alpha")]
	if !ok {
		t.Fatal("lock entry for integration:alpha not found")
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
	if entry.ArchiveSHA256 == "" {
		t.Error("entry.ArchiveSHA256 is empty")
	}
	wantSHA := sha256hex(string(archiveData))
	if entry.ArchiveSHA256 != wantSHA {
		t.Errorf("entry.ArchiveSHA256 = %q, want %q", entry.ArchiveSHA256, wantSHA)
	}
	if entry.Package != "" {
		t.Errorf("entry.Package should be empty for source plugins, got %q", entry.Package)
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

	wantDirName := "integration_alpha"
	executablePath := resolveLockPath(configDir, entry.Executable)
	if !strings.Contains(executablePath, wantDirName) {
		t.Errorf("executable path %q does not contain expected dir %q", executablePath, wantDirName)
	}
	if _, err := os.Stat(executablePath); err != nil {
		t.Errorf("executable not found at %s: %v", executablePath, err)
	}
	manifestPath := resolveLockPath(configDir, entry.Manifest)
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

	requestsBefore := requestCount.Load()
	_, _, err = lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := requestCount.Load(); got != requestsBefore {
		t.Errorf("server received %d requests during locked load, want 0", got-requestsBefore)
	}
}
