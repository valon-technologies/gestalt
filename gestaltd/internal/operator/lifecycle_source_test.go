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
		Plugin:  &pluginmanifestv1.Plugin{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				LibC:   libc,
				Path:   artifactPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactPath},
		},
	}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "provider.json"), manifestBytes, 0644); err != nil {
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
	switch kind {
	case pluginmanifestv1.KindPlugin:
		manifest.Plugin = &pluginmanifestv1.Plugin{}
		manifest.Entrypoints.Provider = &pluginmanifestv1.Entrypoint{ArtifactPath: artPath}
	case pluginmanifestv1.KindAuth:
		manifest.Auth = &pluginmanifestv1.AuthMetadata{}
		manifest.Entrypoints.Auth = &pluginmanifestv1.Entrypoint{ArtifactPath: artPath}
	case pluginmanifestv1.KindDatastore:
		manifest.Datastore = &pluginmanifestv1.DatastoreMetadata{}
		manifest.Entrypoints.Datastore = &pluginmanifestv1.Entrypoint{ArtifactPath: artPath}
	case pluginmanifestv1.KindSecrets:
		manifest.Secrets = &pluginmanifestv1.SecretsMetadata{}
		manifest.Entrypoints.Secrets = &pluginmanifestv1.Entrypoint{ArtifactPath: artPath}
	default:
		t.Fatalf("unsupported kind %q", kind)
	}

	manifestBytes, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "provider.json"), manifestBytes, 0644); err != nil {
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
		"ui:",
		"  provider: none",
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"plugins:",
		"  alpha:",
		"    provider:",
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
	yaml := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"plugins:",
		"  gadget:",
		"    provider:",
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
	if cfg.Integrations["gadget"].Plugin.Command != executablePath {
		t.Fatalf("plugin command = %q, want %q", cfg.Integrations["gadget"].Plugin.Command, executablePath)
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
		requiredDatastoreConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"secrets:",
		"  provider: test-secrets",
		"auth:",
		"  provider:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
		"      auth:",
		"        token: secret://source-token",
		"  config:",
		"    client_id: managed-auth-client",
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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

	if cfg.Auth.Provider == nil {
		t.Fatal("auth provider is nil after load")
	}
	executablePath := resolveLockPath(artifactsDir, lock.Auth.Executable)
	if cfg.Auth.Provider.Command != executablePath {
		t.Fatalf("auth provider command = %q, want %q", cfg.Auth.Provider.Command, executablePath)
	}
	authCfg, err := config.NodeToMap(cfg.Auth.Config)
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
	if !ok || nested["client_id"] != "managed-auth-client" {
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
		requiredDatastoreConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"secrets:",
		"  provider:",
		"    source:",
		"      ref: " + secretsSource,
		"      version: " + secretsVersion,
		"auth:",
		"  provider:",
		"    source:",
		"      ref: " + authSource,
		"      version: " + authVersion,
		"      auth:",
		"        token: secret://source-token",
		"  config:",
		"    client_id: managed-auth-client",
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if cfg.Auth.Provider == nil || cfg.Auth.Provider.Source == nil || cfg.Auth.Provider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", cfg.Auth.Provider)
	}
	if cfg.Auth.Provider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", cfg.Auth.Provider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}

	secretsExecutablePath := resolveLockPath(artifactsDir, lock.Secrets.Executable)
	if cfg.Secrets.Provider == nil {
		t.Fatal("secrets provider is nil after load")
	}
	if cfg.Secrets.Provider.Command != secretsExecutablePath {
		t.Fatalf("secrets provider command = %q, want %q", cfg.Secrets.Provider.Command, secretsExecutablePath)
	}
	authExecutablePath := resolveLockPath(artifactsDir, lock.Auth.Executable)
	if cfg.Auth.Provider.Command != authExecutablePath {
		t.Fatalf("auth provider command = %q, want %q", cfg.Auth.Provider.Command, authExecutablePath)
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
		"server:",
		"  artifacts_dir: " + artifactsDir,
		"  encryption_key: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"plugins:",
		"  alpha:",
		"    provider:",
		"      source:",
		"        ref: " + testSource,
		"        version: " + testVersion,
		"        auth:",
		"          token: test-token",
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
	if cfg.Integrations["alpha"].Plugin.Command != executablePath {
		t.Errorf("plugin command = %q, want %q", cfg.Integrations["alpha"].Plugin.Command, executablePath)
	}
}

func TestSourcePluginGitHubResolverPrefersCurrentLinuxLibcAsset(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("linux libc-specific asset selection only applies on linux hosts")
	}
	currentLibC := pluginpkg.CurrentRuntimeLibC()
	if currentLibC == "" {
		t.Skip("linux libc detection unavailable on this host")
	}

	src := pluginsource.Source{
		Host:  pluginsource.HostGitHub,
		Owner: testOwner,
		Repo:  testRepo,
		Path:  "plugins/" + testPlugin,
	}
	dir := t.TempDir()
	exactPath := artifactRelPath("provider-" + currentLibC)
	exactArchivePath := buildV2ArchiveForArtifact(t, dir, testSource, testVersion, exactPath, currentLibC, "exact-libc-binary")
	genericArchivePath := buildV2ArchiveForArtifact(t, dir, testSource, testVersion, artifactRelPath("provider-generic"), "", "generic-binary")
	exactArchiveData, err := os.ReadFile(exactArchivePath)
	if err != nil {
		t.Fatalf("read exact archive: %v", err)
	}
	genericArchiveData, err := os.ReadFile(genericArchivePath)
	if err != nil {
		t.Fatalf("read generic archive: %v", err)
	}

	exactAssetName := "gestalt-plugin-" + testPlugin + "_v" + testVersion + "_" + pluginpkg.PlatformArchiveSuffix(runtime.GOOS, runtime.GOARCH, currentLibC) + ".tar.gz"
	genericAssetName := "gestalt-plugin-" + testPlugin + "_v" + testVersion + "_" + pluginpkg.PlatformArchiveSuffix(runtime.GOOS, runtime.GOARCH, "") + ".tar.gz"
	releasePath := "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + src.ReleaseTag(testVersion)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"assets": []map[string]any{
					{
						"name":                 genericAssetName,
						"url":                  "http://" + r.Host + "/generic-dl",
						"browser_download_url": "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + src.ReleaseTag(testVersion) + "/" + genericAssetName,
					},
					{
						"name":                 exactAssetName,
						"url":                  "http://" + r.Host + "/exact-dl",
						"browser_download_url": "https://github.com/" + testOwner + "/" + testRepo + "/releases/download/" + src.ReleaseTag(testVersion) + "/" + exactAssetName,
					},
				},
			})
		case "/generic-dl":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(genericArchiveData)
		case "/exact-dl":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(exactArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := writeConfigYAML(t, dir, testSource, testVersion, artifactsDir)

	lc := NewLifecycle(&ghresolver.GitHubResolver{BaseURL: srv.URL})
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	entry := lock.Providers["alpha"]
	if entry.ResolvedURL != srv.URL+"/exact-dl" {
		t.Fatalf("ResolvedURL = %q, want %q", entry.ResolvedURL, srv.URL+"/exact-dl")
	}
	executablePath := resolveLockPath(artifactsDir, entry.Executable)
	data, err := os.ReadFile(executablePath)
	if err != nil {
		t.Fatalf("read executable: %v", err)
	}
	if string(data) != "exact-libc-binary" {
		t.Fatalf("executable content = %q, want %q", data, "exact-libc-binary")
	}
}
