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

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	secretsplugin "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/plugin"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	testOwner   = "testowner"
	testRepo    = "testrepo"
	testPlugin  = "testplugin"
	testVersion = "1.0.0"
	testSource  = "github.com/" + testOwner + "/" + testRepo + "/plugins/" + testPlugin
	testBinary  = "fake-binary-content"
)

type fakeResolver struct {
	archivePath string
	resolvedURL string
	sha256      string
	calls       int
	lastSrc     pluginsource.Source
	lastVersion string
}

type fakeResolverResult struct {
	archivePath string
	resolvedURL string
	sha256      string
}

type fakeResolverCall struct {
	src     pluginsource.Source
	version string
}

type fakeMultiResolver struct {
	results map[string]fakeResolverResult
	calls   []fakeResolverCall
}

func (f *fakeResolver) Resolve(_ context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	f.calls++
	f.lastSrc = src
	f.lastVersion = version
	return &pluginsource.ResolvedPackage{
		LocalPath:     f.archivePath,
		Cleanup:       func() {},
		ArchiveSHA256: f.sha256,
		ResolvedURL:   f.resolvedURL,
	}, nil
}

func (f *fakeMultiResolver) Resolve(_ context.Context, src pluginsource.Source, version string) (*pluginsource.ResolvedPackage, error) {
	f.calls = append(f.calls, fakeResolverCall{src: src, version: version})

	result, ok := f.results[src.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected source %q", src.String())
	}
	return &pluginsource.ResolvedPackage{
		LocalPath:     result.archivePath,
		Cleanup:       func() {},
		ArchiveSHA256: result.sha256,
		ResolvedURL:   result.resolvedURL,
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
	return buildV2ArchiveForArtifact(t, dir, source, version, artPath, "", binaryContent)
}

func buildV2ArchiveForArtifact(t *testing.T, dir, source, version, artifactPath, libc, binaryContent string) string {
	t.Helper()

	safeName := strings.NewReplacer("/", "-", ".", "_").Replace(artifactPath + "-" + libc + "-" + binaryContent)
	srcDir := filepath.Join(dir, safeName+"-src")
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactPath))), 0755); err != nil {
		t.Fatalf("create provider src dir: %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    pluginmanifestv1.KindPlugin, Spec: &pluginmanifestv1.Spec{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				LibC:   libc,
				Path:   artifactPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoint: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactPath)), []byte(binaryContent), 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, safeName+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir plugin: %v", err)
	}

	return archivePath
}

func buildExecutableArchive(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryContent string) string {
	t.Helper()

	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, []byte(binaryContent))
}

func buildExecutableArchiveFromBinaryPath(t *testing.T, dir, srcDirName, source, version, kind, binaryName, binaryPath string) string {
	t.Helper()

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary %s: %v", binaryPath, err)
	}
	return buildExecutableArchiveData(t, dir, srcDirName, source, version, kind, binaryName, data)
}

func buildExecutableArchiveData(t *testing.T, dir, srcDirName, source, version, kind, binaryName string, binaryData []byte) string {
	t.Helper()

	artPath := artifactRelPath(binaryName)
	srcDir := filepath.Join(dir, srcDirName)
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artPath))), 0755); err != nil {
		t.Fatalf("create plugin src dir: %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: artPath,
				SHA256: func() string {
					sum := sha256.Sum256(binaryData)
					return hex.EncodeToString(sum[:])
				}(),
			},
		},
	}
	manifest.Kind = kind
	manifest.Spec = &pluginmanifestv1.Spec{}
	manifest.Entrypoint = &pluginmanifestv1.Entrypoint{ArtifactPath: artPath}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write provider manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("write provider catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artPath)), binaryData, 0755); err != nil {
		t.Fatalf("write provider artifact: %v", err)
	}

	archivePath := filepath.Join(dir, srcDirName+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir plugin: %v", err)
	}

	return archivePath
}

func writeConfigYAML(t *testing.T, dir, source, version, artifactsDir string) string {
	t.Helper()

	lines := []string{
		"providers:",
		"  ui:",
		"    disabled: true",
		"  plugins:",
		"    alpha:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
		"server:",
		"  artifactsDir: " + artifactsDir,
	}

	yaml := strings.Join(lines, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func buildGoSourceSecretsBinary(t *testing.T) string {
	t.Helper()

	providerDir := filepath.Join(t.TempDir(), "go-secrets")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(providerDir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.mod"), []byte(testutil.GeneratedProviderModuleSource(t, "example.com/test-go-secrets")), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "go.sum"), testutil.GeneratedProviderModuleSum(t), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "secrets.go"), []byte(testutil.GeneratedSecretsPackageSource()), 0o644); err != nil {
		t.Fatalf("write secrets.go: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "secrets-provider")
	if err := pluginpkg.BuildGoComponentBinary(providerDir, outputPath, pluginmanifestv1.KindSecrets, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildGoComponentBinary(secrets): %v", err)
	}
	return outputPath
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
	if entry.Archives[pluginpkg.CurrentPlatformString()].URL != resolvedURL {
		t.Errorf("entry archive URL = %q, want %q", entry.Archives[pluginpkg.CurrentPlatformString()].URL, resolvedURL)
	}
	if entry.Archives[pluginpkg.CurrentPlatformString()].SHA256 != archiveSHA {
		t.Errorf("entry archive SHA256 = %q, want %q", entry.Archives[pluginpkg.CurrentPlatformString()].SHA256, archiveSHA)
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
	yaml := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  plugins:",
		"    gadget:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
		"server:",
		"  indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if cfg.Providers.Plugins["gadget"].Command != executablePath {
		t.Fatalf("plugin command = %q, want %q", cfg.Providers.Plugins["gadget"].Command, executablePath)
	}
}

func TestSourceAuthPluginLoadForExecution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	binaryContent := "fake-auth-binary"

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, pluginmanifestv1.KindAuth, "auth-plugin", binaryContent)
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
	configYAML := strings.Join([]string{
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    source: test-secrets",
		"  auth:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
		"      auth:",
		"        token: secret://source-token",
		"    config:",
		"      clientId: managed-auth-client",
		"server:",
		"  indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{"source-token": "ghp_inline_auth_source_token"},
		}, nil
	}

	lc := NewLifecycle(resolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if lock.Auth == nil {
		t.Fatal("lock auth entry not found")
	}
	if lock.Auth.Source != source {
		t.Fatalf("lock.Auth.Source = %q, want %q", lock.Auth.Source, source)
	}
	if lock.Auth.Executable == "" {
		t.Fatal("lock.Auth.Executable is empty")
	}
	if resolver.lastSrc.String() != source {
		t.Fatalf("resolver source = %q, want %q", resolver.lastSrc.String(), source)
	}
	if resolver.lastVersion != version {
		t.Fatalf("resolver version = %q, want %q", resolver.lastVersion, version)
	}
	if resolver.lastSrc.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolver source token = %q, want %q", resolver.lastSrc.Token, "ghp_inline_auth_source_token")
	}

	callsBefore := resolver.calls
	_, _, err = lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if resolver.calls != callsBefore {
		t.Errorf("resolver called during LoadForExecution (locked), want no additional calls")
	}

	authRoot := filepath.Join(artifactsDir, ".gestaltd", "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	downloadsBefore := downloadCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath after cache removal: %v", err)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during locked rehydration = %d, want 1", got)
	}

	if cfg.Providers.Auth == nil {
		t.Fatal("auth provider is nil after load")
	}
	executablePath := resolveLockPath(artifactsDir, lock.Auth.Executable)
	if cfg.Providers.Auth.Command != executablePath {
		t.Fatalf("auth provider command = %q, want %q", cfg.Providers.Auth.Command, executablePath)
	}
	authCfg, err := config.NodeToMap(cfg.Providers.Auth.Config)
	if err != nil {
		t.Fatalf("NodeToMap(auth config): %v", err)
	}
	if authCfg["command"] != executablePath {
		t.Fatalf("auth config command = %v, want %q", authCfg["command"], executablePath)
	}
	sourceCfg, ok := authCfg["source"].(map[string]any)
	if !ok {
		t.Fatalf("auth source config = %#v", authCfg["source"])
	}
	if _, ok := sourceCfg["auth"]; ok {
		t.Fatalf("auth source config leaked source.auth: %#v", sourceCfg)
	}
	nested, ok := authCfg["config"].(map[string]any)
	if !ok || nested["clientId"] != "managed-auth-client" {
		t.Fatalf("auth nested config = %#v", authCfg["config"])
	}
}

func TestSourceSecretsPluginBootstrapsManagedAuthSourceToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-src",
		secretsSource,
		secretsVersion,
		pluginmanifestv1.KindSecrets,
		"secrets-plugin",
		buildGoSourceSecretsBinary(t),
	)
	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-src",
		authSource,
		authVersion,
		pluginmanifestv1.KindAuth,
		"auth-plugin",
		"fake-auth-binary",
	)

	secretsArchiveData, err := os.ReadFile(secretsArchivePath)
	if err != nil {
		t.Fatalf("read secrets archive: %v", err)
	}
	secretsArchiveSum := sha256.Sum256(secretsArchiveData)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var secretsDownloads atomic.Int64
	var authDownloads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/secrets.tar.gz":
			secretsDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "" {
				http.Error(w, "unexpected auth header for secrets download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(secretsArchiveData)
		case "/auth.tar.gz":
			authDownloads.Add(1)
			if got := r.Header.Get("Authorization"); got != "token ghp_inline_auth_source_token" {
				http.Error(w, "bad auth header for auth download", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			secretsSource: {
				archivePath: secretsArchivePath,
				resolvedURL: srv.URL + "/secrets.tar.gz",
				sha256:      hex.EncodeToString(secretsArchiveSum[:]),
			},
			authSource: {
				archivePath: authArchivePath,
				resolvedURL: srv.URL + "/auth.tar.gz",
				sha256:      hex.EncodeToString(authArchiveSum[:]),
			},
		},
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    source:",
		"      ref: " + secretsSource,
		"      version: " + secretsVersion,
		"  auth:",
		"    source:",
		"      ref: " + authSource,
		"      version: " + authVersion,
		"      auth:",
		"        token: secret://source-token",
		"    config:",
		"      clientId: managed-auth-client",
		"server:",
		"  indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["plugin"] = secretsplugin.Factory

	lc := NewLifecycle(resolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if lock.Secrets == nil {
		t.Fatal("lock secrets entry not found")
	}
	if lock.Auth == nil {
		t.Fatal("lock auth entry not found")
	}
	if got := len(resolver.calls); got != 2 {
		t.Fatalf("resolver calls = %d, want 2", got)
	}
	if resolver.calls[0].src.String() != secretsSource {
		t.Fatalf("first resolver source = %q, want %q", resolver.calls[0].src.String(), secretsSource)
	}
	if resolver.calls[0].src.Token != "" {
		t.Fatalf("secrets resolver token = %q, want empty", resolver.calls[0].src.Token)
	}
	if resolver.calls[1].src.String() != authSource {
		t.Fatalf("second resolver source = %q, want %q", resolver.calls[1].src.String(), authSource)
	}
	if resolver.calls[1].src.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("auth resolver token = %q, want %q", resolver.calls[1].src.Token, "ghp_inline_auth_source_token")
	}

	secretsRoot := filepath.Join(artifactsDir, ".gestaltd", "secrets")
	if err := os.RemoveAll(secretsRoot); err != nil {
		t.Fatalf("RemoveAll secrets root: %v", err)
	}
	authRoot := filepath.Join(artifactsDir, ".gestaltd", "auth")
	if err := os.RemoveAll(authRoot); err != nil {
		t.Fatalf("RemoveAll auth root: %v", err)
	}

	callsBefore := len(resolver.calls)
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if got := len(resolver.calls); got != callsBefore {
		t.Fatalf("resolver calls during locked load = %d, want %d", got, callsBefore)
	}
	if got := secretsDownloads.Load(); got != 1 {
		t.Fatalf("secrets download count = %d, want 1", got)
	}
	if got := authDownloads.Load(); got != 1 {
		t.Fatalf("auth download count = %d, want 1", got)
	}
	if cfg.Providers.Auth == nil || cfg.Providers.Auth.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", cfg.Providers.Auth)
	}
	if cfg.Providers.Auth.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", cfg.Providers.Auth.Source.Auth.Token, "ghp_inline_auth_source_token")
	}

	secretsExecutablePath := resolveLockPath(artifactsDir, lock.Secrets.Executable)
	if cfg.Providers.Secrets == nil {
		t.Fatal("secrets provider is nil after load")
	}
	if cfg.Providers.Secrets.Command != secretsExecutablePath {
		t.Fatalf("secrets provider command = %q, want %q", cfg.Providers.Secrets.Command, secretsExecutablePath)
	}
	authExecutablePath := resolveLockPath(artifactsDir, lock.Auth.Executable)
	if cfg.Providers.Auth.Command != authExecutablePath {
		t.Fatalf("auth provider command = %q, want %q", cfg.Providers.Auth.Command, authExecutablePath)
	}
}

func TestSourcePluginGitHubResolverEndToEnd(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "wrong-env-token")

	dir := t.TempDir()

	src := pluginsource.Source{
		Host:  pluginsource.HostGitHub,
		Owner: testOwner,
		Repo:  testRepo,
		Path:  "plugins/" + testPlugin,
	}
	expectedAssetName := fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", src.PluginName(), testVersion, runtime.GOOS, runtime.GOARCH)
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
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  plugins:",
		"    alpha:",
		"      source:",
		"        ref: " + testSource,
		"        version: " + testVersion,
		"        auth:",
		"          token: test-token",
		"server:",
		"  indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if entry.Archives[pluginpkg.CurrentPlatformString()].URL == "" {
		t.Error("entry archive URL is empty")
	}
	if entry.Archives[pluginpkg.CurrentPlatformString()].URL != srv.URL+"/asset-dl" {
		t.Errorf("entry archive URL = %q, want %q", entry.Archives[pluginpkg.CurrentPlatformString()].URL, srv.URL+"/asset-dl")
	}
	if entry.Archives[pluginpkg.CurrentPlatformString()].SHA256 == "" {
		t.Error("entry archive SHA256 is empty")
	}
	wantSHA := sha256hex(string(archiveData))
	if entry.Archives[pluginpkg.CurrentPlatformString()].SHA256 != wantSHA {
		t.Errorf("entry archive SHA256 = %q, want %q", entry.Archives[pluginpkg.CurrentPlatformString()].SHA256, wantSHA)
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
	if cfg.Providers.Plugins["alpha"].Command != executablePath {
		t.Errorf("plugin command = %q, want %q", cfg.Providers.Plugins["alpha"].Command, executablePath)
	}
}

// buildWebUIArchive creates a tar.gz archive for a kind:webui plugin with
// a manifest and an asset root directory containing a stub index.html.
func buildWebUIArchive(t *testing.T, dir, srcDirName, source, version string) string {
	t.Helper()
	srcDir := filepath.Join(dir, srcDirName)
	assetDir := filepath.Join(srcDir, "out")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		t.Fatalf("create asset dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "index.html"), []byte("<html><body>plugin ui</body></html>"), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    pluginmanifestv1.KindWebUI,
		Spec:    &pluginmanifestv1.Spec{AssetRoot: "out"},
	}
	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	archivePath := filepath.Join(dir, srcDirName+".tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("create package: %v", err)
	}
	return archivePath
}

func writeConfigWithPluginWebUI(t *testing.T, dir, pluginSource, pluginVersion, webuiSource, webuiVersion, artifactsDir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "test.db")
	indexedDBManifest := writeStubIndexedDBManifest(t, dir)
	content := fmt.Sprintf(`providers:
  ui:
    disabled: true
  indexeddbs:
    sqlite:
      source:
        path: %s
      config:
        path: %q
  plugins:
    acme:
      source:
        ref: %s
        version: %s
      webui:
        source:
          ref: %s
          version: %s
server:
  indexeddb: sqlite
  encryptionKey: "0123456789abcdef0123456789abcdef"
  artifactsDir: %s
`, indexedDBManifest, dbPath, pluginSource, pluginVersion, webuiSource, webuiVersion, artifactsDir)
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestPluginWebUI_InitWritesLockfileEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginSource := "github.com/acme/tools/acme-plugin"
	pluginVersion := "1.0.0"
	webuiSource := "github.com/acme/tools/acme-webui"
	webuiVersion := "2.0.0"

	pluginArchive := buildV2Archive(t, dir, pluginSource, pluginVersion, "fake-plugin")
	webuiArchive := buildWebUIArchive(t, dir, "acme-webui-src", webuiSource, webuiVersion)

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			pluginSource: {archivePath: pluginArchive, resolvedURL: "https://example.com/plugin.tar.gz"},
			webuiSource:  {archivePath: webuiArchive, resolvedURL: "https://example.com/webui.tar.gz"},
		},
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := writeConfigWithPluginWebUI(t, dir, pluginSource, pluginVersion, webuiSource, webuiVersion, artifactsDir)

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	if lock.PluginWebUIs == nil {
		t.Fatal("lock.PluginWebUIs is nil")
	}
	entry, ok := lock.PluginWebUIs["acme"]
	if !ok {
		t.Fatal("lock.PluginWebUIs missing acme entry")
	}
	if entry.Source != webuiSource {
		t.Errorf("source = %q, want %q", entry.Source, webuiSource)
	}
	if entry.Version != webuiVersion {
		t.Errorf("version = %q, want %q", entry.Version, webuiVersion)
	}
	if entry.Fingerprint == "" {
		t.Error("fingerprint is empty")
	}
	if entry.Manifest == "" {
		t.Error("manifest path is empty")
	}
	if entry.AssetRoot == "" {
		t.Error("asset root path is empty")
	}

	assetRootAbs := resolveLockPath(artifactsDir, entry.AssetRoot)
	if _, err := os.Stat(assetRootAbs); err != nil {
		t.Errorf("asset root not found on disk: %v", err)
	}
	indexPath := filepath.Join(assetRootAbs, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("index.html not found in asset root: %v", err)
	}
}

func TestPluginWebUI_LoadResolvesAssetRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginSource := "github.com/acme/tools/acme-plugin"
	pluginVersion := "1.0.0"
	webuiSource := "github.com/acme/tools/acme-webui"
	webuiVersion := "2.0.0"

	pluginArchive := buildV2Archive(t, dir, pluginSource, pluginVersion, "fake-plugin")
	webuiArchive := buildWebUIArchive(t, dir, "acme-webui-src", webuiSource, webuiVersion)

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			pluginSource: {archivePath: pluginArchive, resolvedURL: "https://example.com/plugin.tar.gz"},
			webuiSource:  {archivePath: webuiArchive, resolvedURL: "https://example.com/webui.tar.gz"},
		},
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := writeConfigWithPluginWebUI(t, dir, pluginSource, pluginVersion, webuiSource, webuiVersion, artifactsDir)

	lc := NewLifecycle(resolver)
	if _, err := lc.InitAtPath(configPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	acmeEntry := cfg.Providers.Plugins["acme"]
	if acmeEntry == nil {
		t.Fatal("acme plugin entry is nil")
	}
	if acmeEntry.ResolvedAssetRoot == "" {
		t.Fatal("ResolvedAssetRoot is empty after load")
	}
	if _, err := os.Stat(acmeEntry.ResolvedAssetRoot); err != nil {
		t.Errorf("ResolvedAssetRoot path does not exist: %v", err)
	}
}

func TestPluginWebUI_ConfigChangeInvalidatesLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginSource := "github.com/acme/tools/acme-plugin"
	pluginVersion := "1.0.0"
	webuiSource := "github.com/acme/tools/acme-webui"
	webuiV1 := "1.0.0"
	webuiV2 := "2.0.0"

	pluginArchive := buildV2Archive(t, dir, pluginSource, pluginVersion, "fake-plugin")
	webuiArchive := buildWebUIArchive(t, dir, "acme-webui-src", webuiSource, webuiV1)

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			pluginSource: {archivePath: pluginArchive, resolvedURL: "https://example.com/plugin.tar.gz"},
			webuiSource:  {archivePath: webuiArchive, resolvedURL: "https://example.com/webui.tar.gz"},
		},
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := writeConfigWithPluginWebUI(t, dir, pluginSource, pluginVersion, webuiSource, webuiV1, artifactsDir)

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath v1: %v", err)
	}
	v1Fingerprint := lock.PluginWebUIs["acme"].Fingerprint

	// Rewrite config with v2 webui
	writeConfigWithPluginWebUI(t, dir, pluginSource, pluginVersion, webuiSource, webuiV2, artifactsDir)

	cfg, err := config.LoadAllowMissingEnv(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	paths := initPathsForConfigWithArtifactsDir(configPath, artifactsDir)
	if lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig should return false after webui version change")
	}

	// Re-init should produce a different fingerprint
	lock2, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath v2: %v", err)
	}
	if lock2.PluginWebUIs["acme"].Fingerprint == v1Fingerprint {
		t.Error("fingerprint should change after version update")
	}
}

func TestPluginWebUI_MissingLockEntryErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginSource := "github.com/acme/tools/acme-plugin"
	pluginVersion := "1.0.0"
	webuiSource := "github.com/acme/tools/acme-webui"
	webuiVersion := "1.0.0"

	pluginArchive := buildV2Archive(t, dir, pluginSource, pluginVersion, "fake-plugin")

	// Only resolve the plugin, not the webui -- simulates stale lock
	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			pluginSource: {archivePath: pluginArchive, resolvedURL: "https://example.com/plugin.tar.gz"},
			webuiSource:  {archivePath: pluginArchive, resolvedURL: "https://example.com/webui.tar.gz"}, // wrong archive, but init will write it
		},
	}

	artifactsDir := filepath.Join(dir, "artifacts")

	// First, write config WITHOUT webui and init
	dbPath := filepath.Join(dir, "test.db")
	indexedDBManifest := writeStubIndexedDBManifest(t, dir)
	noWebuiConfig := fmt.Sprintf(`providers:
  ui:
    disabled: true
  indexeddbs:
    sqlite:
      source:
        path: %s
      config:
        path: %q
  plugins:
    acme:
      source:
        ref: %s
        version: %s
server:
  indexeddb: sqlite
  encryptionKey: "0123456789abcdef0123456789abcdef"
  artifactsDir: %s
`, indexedDBManifest, dbPath, pluginSource, pluginVersion, artifactsDir)
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(noWebuiConfig), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	if _, err := lc.InitAtPath(configPath); err != nil {
		t.Fatalf("InitAtPath without webui: %v", err)
	}

	// Now rewrite config WITH webui but don't re-init
	writeConfigWithPluginWebUI(t, dir, pluginSource, pluginVersion, webuiSource, webuiVersion, artifactsDir)

	// Locked load should fail because lock has no PluginWebUIs entry
	_, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		t.Fatal("expected error for missing webui lock entry in locked mode")
	}
	if !strings.Contains(err.Error(), "gestaltd init") {
		t.Errorf("error should mention gestaltd init, got: %v", err)
	}
}
