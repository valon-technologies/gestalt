package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	secretsprovider "github.com/valon-technologies/gestalt/server/internal/drivers/secrets/provider"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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
	archivePath      string
	resolvedURL      string
	sha256           string
	platformArchives []pluginsource.PlatformArchive
}

type fakeResolverCall struct {
	src     pluginsource.Source
	version string
}

type fakeMultiResolver struct {
	results map[string]fakeResolverResult
	calls   []fakeResolverCall
}

type hostRewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
	hosts  map[string]struct{}
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

func (f *fakeMultiResolver) ListPlatformArchives(_ context.Context, src pluginsource.Source, _ string) ([]pluginsource.PlatformArchive, error) {
	result, ok := f.results[src.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected source %q", src.String())
	}
	return append([]pluginsource.PlatformArchive(nil), result.platformArchives...), nil
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
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kind:    providermanifestv1.KindPlugin, Spec: &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				LibC:   libc,
				Path:   artifactPath,
				SHA256: sha256hex(binaryContent),
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
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
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
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
	manifest := &providermanifestv1.Manifest{
		Source:  source,
		Version: version,
		Artifacts: []providermanifestv1.Artifact{
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
	manifest.Spec = &providermanifestv1.Spec{}
	manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artPath}

	manifestBytes, err := providerpkg.EncodeManifest(manifest)
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
	if err := providerpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir plugin: %v", err)
	}

	return archivePath
}

func writeConfigYAML(t *testing.T, dir, source, version, artifactsDir string) string {
	t.Helper()

	lines := []string{
		"plugins:",
		"  alpha:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
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
	if err := providerpkg.BuildGoComponentBinary(providerDir, outputPath, providermanifestv1.KindSecrets, runtime.GOOS, runtime.GOARCH); err != nil {
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
	if entry.Archives[providerpkg.CurrentPlatformString()].URL != resolvedURL {
		t.Errorf("entry archive URL = %q, want %q", entry.Archives[providerpkg.CurrentPlatformString()].URL, resolvedURL)
	}
	if entry.Archives[providerpkg.CurrentPlatformString()].SHA256 != archiveSHA {
		t.Errorf("entry archive SHA256 = %q, want %q", entry.Archives[providerpkg.CurrentPlatformString()].SHA256, archiveSHA)
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
	var written providerLockfile
	if err := json.Unmarshal(lockData, &written); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	if written.Schema != providerLockSchemaName {
		t.Errorf("written lockfile schema = %q, want %q", written.Schema, providerLockSchemaName)
	}
	if written.SchemaVersion != providerLockSchemaVersion {
		t.Errorf("written lockfile schemaVersion = %d, want %d", written.SchemaVersion, providerLockSchemaVersion)
	}
	if _, ok := written.Providers.Plugin["alpha"]; !ok {
		t.Fatalf(`written.Providers.Plugin["alpha"] not found`)
	}
	writtenEntry := written.Providers.Plugin["alpha"]
	if writtenEntry.InputDigest == "" {
		t.Fatal("written plugin inputDigest is empty")
	}
	if writtenEntry.Package != source {
		t.Fatalf("written plugin package = %q, want %q", writtenEntry.Package, source)
	}
	if writtenEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("written plugin kind = %q, want %q", writtenEntry.Kind, providermanifestv1.KindPlugin)
	}
	if writtenEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("written plugin runtime = %q, want %q", writtenEntry.Runtime, providerLockRuntimeExecutable)
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

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if _, ok := t.hosts[strings.ToLower(req.URL.Hostname())]; ok {
		clone.URL.Scheme = t.target.Scheme
		clone.URL.Host = t.target.Host
	}
	return t.base.RoundTrip(clone)
}

func newGitHubRewriteClient(t *testing.T, target string) *http.Client {
	t.Helper()
	targetURL, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse rewrite target URL: %v", err)
	}
	return &http.Client{
		Transport: &hostRewriteTransport{
			base:   http.DefaultTransport,
			target: targetURL,
			hosts: map[string]struct{}{
				"github.com":     {},
				"api.github.com": {},
			},
		},
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

func TestSourcePluginMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/alpha"
	version := "1.2.3"
	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-url-plugin-binary")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	extraPlatform := struct {
		goos   string
		goarch string
	}{
		goos:   "linux",
		goarch: "amd64",
	}
	for _, candidate := range []struct {
		goos   string
		goarch string
	}{
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "darwin", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	} {
		if candidate.goos != runtime.GOOS || candidate.goarch != runtime.GOARCH {
			extraPlatform = candidate
			break
		}
	}
	extraPlatformKey := providerpkg.PlatformString(extraPlatform.goos, extraPlatform.goarch)
	extraArchiveData := []byte("metadata-extra-platform-archive")
	extraArchiveSHA := sha256.Sum256(extraArchiveData)

	var metadataCount atomic.Int64
	var currentArchiveCount atomic.Int64
	var extraArchiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}
	metadataPath := "/providers/alpha/provider-release.yaml"
	currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
	extraArchivePathURL := "/providers/alpha/alpha-extra.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(currentArchivePathURL),
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
					extraPlatformKey: {
						Path:   filepath.Base(extraArchivePathURL),
						SHA256: hex.EncodeToString(extraArchiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case currentArchivePathURL:
			currentArchiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("current archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		case extraArchivePathURL:
			extraArchiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("extra archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(extraArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.APIVersionV3,
		"plugins:",
		"  alpha:",
		"    source: " + srv.URL + metadataPath + "?download=1",
		"    auth:",
		"      token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPathWithPlatforms(configPath, "", []struct{ GOOS, GOARCH, LibC string }{
		{GOOS: extraPlatform.goos, GOARCH: extraPlatform.goarch},
	})
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPathWithPlatforms: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] not found`)
	}
	if entry.Source != srv.URL+metadataPath+"?download=1" {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, srv.URL+metadataPath+"?download=1")
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindPlugin)
	}
	if entry.Runtime != providerReleaseRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != srv.URL+currentArchivePathURL {
		t.Fatalf("current archive URL = %q, want %q", got, srv.URL+currentArchivePathURL)
	}
	if got := entry.Archives[extraPlatformKey].SHA256; got != hex.EncodeToString(extraArchiveSHA[:]) {
		t.Fatalf("extra platform SHA256 = %q, want %q", got, hex.EncodeToString(extraArchiveSHA[:]))
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := currentArchiveCount.Load(); got != 1 {
		t.Fatalf("current archive request count = %d, want 1", got)
	}
	if got := extraArchiveCount.Load(); got != 0 {
		t.Fatalf("extra archive request count = %d, want 0", got)
	}

	lockData, err := os.ReadFile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	var diskLock providerLockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	diskEntry, ok := diskLock.Providers.Plugin["alpha"]
	if !ok {
		t.Fatal(`disk lock providers.plugin["alpha"] not found`)
	}
	if diskEntry.Package != packageSource {
		t.Fatalf("disk lock package = %q, want %q", diskEntry.Package, packageSource)
	}
	if diskEntry.Source != srv.URL+metadataPath+"?download=1" {
		t.Fatalf("disk lock source = %q, want %q", diskEntry.Source, srv.URL+metadataPath+"?download=1")
	}
	if diskEntry.Runtime != providerReleaseRuntimeExecutable {
		t.Fatalf("disk lock runtime = %q, want %q", diskEntry.Runtime, providerReleaseRuntimeExecutable)
	}
	if diskEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("disk lock kind = %q, want %q", diskEntry.Kind, providermanifestv1.KindPlugin)
	}
	if got := diskEntry.Archives[extraPlatformKey].URL; got != srv.URL+extraArchivePathURL {
		t.Fatalf("disk lock extra archive URL = %q, want %q", got, srv.URL+extraArchivePathURL)
	}

	pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "alpha")
	if err := os.RemoveAll(pluginRoot); err != nil {
		t.Fatalf("RemoveAll plugin root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	currentBefore := currentArchiveCount.Load()
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
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := currentArchiveCount.Load() - currentBefore; got != 1 {
		t.Fatalf("current archive request count during locked load = %d, want 1", got)
	}
	if got := extraArchiveCount.Load(); got != 0 {
		t.Fatalf("extra archive request count after locked load = %d, want 0", got)
	}
	if cfg.Plugins["alpha"] == nil {
		t.Fatal(`cfg.Plugins["alpha"] = nil`)
	}
	if cfg.Plugins["alpha"].ResolvedManifest == nil {
		t.Fatal(`cfg.Plugins["alpha"].ResolvedManifest = nil`)
	}
	if cfg.Plugins["alpha"].ResolvedManifest.Source != packageSource {
		t.Fatalf("ResolvedManifest.Source = %q, want %q", cfg.Plugins["alpha"].ResolvedManifest.Source, packageSource)
	}
	executablePath := resolveLockPath(artifactsDir, entry.Executable)
	if cfg.Plugins["alpha"].Command != executablePath {
		t.Fatalf("plugin command = %q, want %q", cfg.Plugins["alpha"].Command, executablePath)
	}
}

func TestSourceWorkflowMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/workflow-runner"
	version := "2.3.4"
	archivePath := buildExecutableArchive(t, dir, "workflow-metadata-src", packageSource, version, providermanifestv1.KindWorkflow, "workflow-runner", "metadata-workflow-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read workflow archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/workflow/provider-release.yaml"
	archivePathURL := "/providers/workflow/workflow-runner.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindWorkflow,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
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
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.APIVersionV3 + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  workflow:",
		"    runner:",
		"      source: " + srv.URL + metadataPath,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Workflows["runner"]
	if !ok {
		t.Fatal(`lock.Workflows["runner"] not found`)
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindWorkflow {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindWorkflow)
	}
	if entry.Runtime != providerReleaseRuntimeExecutable {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeExecutable)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	workflowRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedWorkflowDir), "runner")
	if err := os.RemoveAll(workflowRoot); err != nil {
		t.Fatalf("RemoveAll workflow root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	workflow := cfg.Providers.Workflow["runner"]
	if workflow == nil || workflow.ResolvedManifest == nil {
		t.Fatalf("workflow resolved manifest = %+v", workflow)
	}
	if got := workflow.ResolvedManifest.Kind; got != providermanifestv1.KindWorkflow {
		t.Fatalf("workflow manifest kind = %q, want %q", got, providermanifestv1.KindWorkflow)
	}
	if got := workflow.Command; got != resolveLockPath(artifactsDir, entry.Executable) {
		t.Fatalf("workflow command = %q, want %q", got, resolveLockPath(artifactsDir, entry.Executable))
	}
}

func TestSourceWebUIMetadataURLInitAndLockedLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packageSource := "github.com/acme/tools/roadmap-ui"
	version := "0.9.1"
	archivePath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      packageSource,
		Version:     version,
		DisplayName: "Roadmap UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}, map[string]string{
		"dist/index.html": "<html>roadmap</html>",
	}, false)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read webui archive: %v", err)
	}
	archiveSHA := sha256.Sum256(archiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/roadmap/provider-release.yaml"
	archivePathURL := "/providers/roadmap/roadmap-ui.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindWebUI,
				Version:       version,
				Runtime:       providerReleaseRuntimeWebUI,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
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
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := "apiVersion: " + config.APIVersionV3 + "\n" + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"  ui:",
		"    roadmap:",
		"      source: " + srv.URL + metadataPath + "?download=1",
		"      auth:",
		"        token: test-token",
		"      path: /roadmap",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.UIs["roadmap"]
	if !ok {
		t.Fatal(`lock.UIs["roadmap"] not found`)
	}
	if entry.Source != srv.URL+metadataPath+"?download=1" {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, srv.URL+metadataPath+"?download=1")
	}
	if entry.Package != packageSource {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packageSource)
	}
	if entry.Kind != providermanifestv1.KindWebUI {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, providermanifestv1.KindWebUI)
	}
	if entry.Runtime != providerReleaseRuntimeWebUI {
		t.Fatalf("entry.Runtime = %q, want %q", entry.Runtime, providerReleaseRuntimeWebUI)
	}
	if entry.Version != version {
		t.Fatalf("entry.Version = %q, want %q", entry.Version, version)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	uiRoot := filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir), "roadmap")
	if err := os.RemoveAll(uiRoot); err != nil {
		t.Fatalf("RemoveAll ui root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	ui := cfg.Providers.UI["roadmap"]
	if ui == nil || ui.ResolvedManifest == nil {
		t.Fatalf("ui resolved manifest = %+v", ui)
	}
	if got := ui.ResolvedManifest.Kind; got != providermanifestv1.KindWebUI {
		t.Fatalf("ui manifest kind = %q, want %q", got, providermanifestv1.KindWebUI)
	}
	if got := ui.ResolvedManifest.Source; got != packageSource {
		t.Fatalf("ui manifest source = %q, want %q", got, packageSource)
	}
	if got := ui.ResolvedAssetRoot; got != resolveLockPath(artifactsDir, entry.AssetRoot) {
		t.Fatalf("ui asset root = %q, want %q", got, resolveLockPath(artifactsDir, entry.AssetRoot))
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
		"plugins:",
		"  gadget:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
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
	install, err := inspectPreparedInstall(pluginRoot)
	if err != nil {
		t.Fatalf("inspectPreparedInstall(%s): %v", pluginRoot, err)
	}
	if cfg.Plugins["gadget"].Command != install.executablePath {
		t.Fatalf("plugin command = %q, want %q", cfg.Plugins["gadget"].Command, install.executablePath)
	}
}

func TestSourcePluginInitRejectsRefSourceManifestMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/gadget"
	version := "2.0.0"

	archivePath := buildV2Archive(t, dir, "github.com/acme/tools/other-gadget", version, "fake-gadget-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	resolver := &fakeResolver{
		archivePath: archivePath,
		resolvedURL: "https://example.com/plugin.tar.gz",
		sha256:      archiveSHA,
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	yaml := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"plugins:",
		"  gadget:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	_, err = lc.InitAtPath(configPath)
	if err == nil {
		t.Fatal("InitAtPath unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), `manifest source "github.com/acme/tools/other-gadget" does not match expected package "github.com/acme/tools/gadget"`) {
		t.Fatalf("InitAtPath error = %v, want manifest source mismatch", err)
	}
}

func TestSourcePluginMetadataURLUsesGitHubAssetTransport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const version = testVersion

	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-github-asset-plugin-binary")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/alpha/provider-release.yaml"
	githubArchiveURL := "https://api.github.com/repos/" + testOwner + "/" + testRepo + "/releases/assets/123"
	githubArchivePath := "/repos/" + testOwner + "/" + testRepo + "/releases/assets/123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   githubArchiveURL,
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case githubArchivePath:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.APIVersionV3,
		"plugins:",
		"  alpha:",
		"    source: " + srv.URL + metadataPath,
		"    auth:",
		"      token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil).WithHTTPClient(newGitHubRewriteClient(t, srv.URL))
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] not found`)
	}
	if got := entry.Archives[providerpkg.CurrentPlatformString()].URL; got != githubArchiveURL {
		t.Fatalf("current archive URL = %q, want %q", got, githubArchiveURL)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	pluginRoot := filepath.Join(artifactsDir, ".gestaltd", "providers", "alpha")
	if err := os.RemoveAll(pluginRoot); err != nil {
		t.Fatalf("RemoveAll plugin root: %v", err)
	}

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != metadataBefore {
		t.Fatalf("metadata request count during locked load = %d, want %d", got, metadataBefore)
	}
	if got := archiveCount.Load() - archiveBefore; got != 1 {
		t.Fatalf("archive request count during locked load = %d, want 1", got)
	}
	if cfg.Plugins["alpha"] == nil || cfg.Plugins["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata plugin manifest missing after locked load")
	}
}

func TestSourcePluginMetadataURLUsesGitHubTokenFallbackForMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_TOKEN", "env-fallback-token")

	const packageSource = testSource
	const version = testVersion

	currentArchivePath := buildV2Archive(t, dir, packageSource, version, "metadata-github-fallback-plugin-binary")
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	currentArchiveSHA := sha256.Sum256(currentArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/" + testOwner + "/" + testRepo + "/releases/download/plugin/" + testPlugin + "/" + version + "/provider-release.yaml"
	metadataURL := "https://github.com" + metadataPath
	githubArchiveURL := "https://api.github.com/repos/" + testOwner + "/" + testRepo + "/releases/assets/456"
	githubArchivePath := "/repos/" + testOwner + "/" + testRepo + "/releases/assets/456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token env-fallback-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "token env-fallback-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   githubArchiveURL,
						SHA256: hex.EncodeToString(currentArchiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case githubArchivePath:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token env-fallback-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "token env-fallback-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("archive accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad archive accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.APIVersionV3,
		"plugins:",
		"  alpha:",
		"    source: " + metadataURL,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil).WithHTTPClient(newGitHubRewriteClient(t, srv.URL))
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}
	if got := lock.Providers["alpha"].Source; got != metadataURL {
		t.Fatalf("entry.Source = %q, want %q", got, metadataURL)
	}
}

func TestBuildArchivesMap_AllowsGenericDeclarativeTelemetryAndAuditPackages(t *testing.T) {
	t.Parallel()

	const source = "github.com/acme/providers/declarative"
	const version = "1.0.0"
	const archiveURL = "https://example.com/releases/download/provider/declarative.tar.gz"

	src, err := pluginsource.Parse(source)
	if err != nil {
		t.Fatalf("Parse source: %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  source,
		Version: version,
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{
						{Name: "ping", Method: "GET", Path: "/ping"},
					},
				},
			},
		},
	}
	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			src.String(): {
				platformArchives: []pluginsource.PlatformArchive{
					{Platform: platformKeyGeneric, URL: archiveURL},
				},
			},
		},
	}
	lc := NewLifecycle(resolver)

	for _, kind := range []string{providerLockKindTelemetry, providerLockKindAudit} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			archives, err := lc.buildArchivesMap(context.Background(), src, version, archiveURL, "", kind, kind+` "default"`, manifest)
			if err != nil {
				t.Fatalf("buildArchivesMap: %v", err)
			}
			if got := archives[platformKeyGeneric].URL; got != archiveURL {
				t.Fatalf("generic archive URL = %q, want %q", got, archiveURL)
			}
		})
	}
}

func TestMaterializeLockedComponent_AllowsGenericDeclarativeTelemetryAndAuditPackages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const source = "github.com/acme/providers/declarative"
	const version = "1.0.0"

	pkgPath := mustBuildManagedProviderPackage(t, dir, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  source,
		Version: version,
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{
						{Name: "ping", Method: "GET", Path: "/ping"},
					},
				},
			},
		},
	}, nil, false)
	pkgData, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	pkgSum := sha256.Sum256(pkgData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(pkgData)
	}))
	defer srv.Close()

	lc := NewLifecycle(nil)

	for _, kind := range []string{providerLockKindTelemetry, providerLockKindAudit} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(pkgData)
			}))
			defer srv.Close()

			entry := LockEntry{
				Source:  source,
				Version: version,
				Archives: map[string]LockArchive{
					platformKeyGeneric: {
						URL:    srv.URL,
						SHA256: hex.EncodeToString(pkgSum[:]),
					},
				},
			}
			providerEntry := &config.ProviderEntry{
				Source: config.ProviderSource{
					Ref:     source,
					Version: version,
				},
			}
			destDir := filepath.Join(dir, kind)
			if err := lc.materializeLockedComponent(context.Background(), initPaths{}, kind, "default", providerEntry, entry, destDir, true); err != nil {
				t.Fatalf("materializeLockedComponent: %v", err)
			}
			install, err := inspectPreparedInstall(destDir)
			if err != nil {
				t.Fatalf("inspectPreparedInstall: %v", err)
			}
			if install.manifest == nil || !install.manifest.IsDeclarativeOnlyProvider() {
				t.Fatalf("prepared manifest = %#v, want declarative manifest", install.manifest)
			}
		})
	}
}

func TestSourcePluginMetadataURLUnlockedLoadRefreshesMutableMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const packageSource = testSource
	const initialVersion = "1.0.0"
	const updatedVersion = "1.0.1"

	initialArchivePath := buildV2Archive(t, dir, packageSource, initialVersion, "metadata-mutable-plugin-v1")
	initialArchiveData, err := os.ReadFile(initialArchivePath)
	if err != nil {
		t.Fatalf("read initial archive: %v", err)
	}
	initialArchiveSHA := sha256.Sum256(initialArchiveData)

	updatedArchivePath := buildV2Archive(t, dir, packageSource, updatedVersion, "metadata-mutable-plugin-v2")
	updatedArchiveData, err := os.ReadFile(updatedArchivePath)
	if err != nil {
		t.Fatalf("read updated archive: %v", err)
	}
	updatedArchiveSHA := sha256.Sum256(updatedArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	var currentMu sync.RWMutex
	currentVersion := initialVersion
	currentArchiveData := initialArchiveData
	currentArchiveSHA := initialArchiveSHA

	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/alpha/provider-release.yaml"
	currentArchivePathURL := "/providers/alpha/alpha-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			version := currentVersion
			archiveSHA := currentArchiveSHA
			currentMu.RUnlock()
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       packageSource,
				Kind:          providermanifestv1.KindPlugin,
				Version:       version,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(currentArchivePathURL),
						SHA256: hex.EncodeToString(archiveSHA[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case currentArchivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer test-token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			currentMu.RLock()
			archiveData := currentArchiveData
			currentMu.RUnlock()
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configPath := filepath.Join(dir, "gestalt.yaml")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"apiVersion: " + config.APIVersionV3,
		"plugins:",
		"  alpha:",
		"    source: " + srv.URL + metadataPath,
		"    auth:",
		"      token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPath(configPath)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := lock.Providers["alpha"].Version; got != initialVersion {
		t.Fatalf("initial lock version = %q, want %q", got, initialVersion)
	}

	currentMu.Lock()
	currentVersion = updatedVersion
	currentArchiveData = updatedArchiveData
	currentArchiveSHA = updatedArchiveSHA
	currentMu.Unlock()

	metadataBefore := metadataCount.Load()
	archiveBefore := archiveCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err == nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
	}
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got <= metadataBefore {
		t.Fatalf("metadata request count after unlocked load = %d, want > %d", got, metadataBefore)
	}
	if got := archiveCount.Load(); got <= archiveBefore {
		t.Fatalf("archive request count after unlocked load = %d, want > %d", got, archiveBefore)
	}
	if cfg.Plugins["alpha"] == nil || cfg.Plugins["alpha"].ResolvedManifest == nil {
		t.Fatal("resolved metadata plugin manifest missing after unlocked refresh")
	}
	if got := cfg.Plugins["alpha"].ResolvedManifest.Version; got != updatedVersion {
		t.Fatalf("resolved manifest version after unlocked refresh = %q, want %q", got, updatedVersion)
	}

	updatedLock, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := updatedLock.Providers["alpha"].Version; got != updatedVersion {
		t.Fatalf("updated lock version = %q, want %q", got, updatedVersion)
	}
}

func TestSourcePluginLoadForExecution_RehydratesWhenCachedManifestVersionMismatchesLock(t *testing.T) {
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
		"plugins:",
		"  gadget:",
		"    source:",
		"      ref: " + source,
		"      version: " + version,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	if _, err := lc.InitAtPath(configPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	lock, err := ReadLockfile(filepath.Join(filepath.Dir(configPath), InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if _, ok := lock.Providers["gadget"]; !ok {
		t.Fatal(`lock.Providers["gadget"] not found`)
	}
	install, err := inspectPreparedInstall(filepath.Join(artifactsDir, ".gestaltd", "providers", "gadget"))
	if err != nil {
		t.Fatalf("inspectPreparedInstall: %v", err)
	}
	manifestPath := install.manifestPath

	_, staleManifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s): %v", manifestPath, err)
	}
	staleManifest.Version = "1.9.9"
	staleBytes, err := providerpkg.EncodeManifest(staleManifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, staleBytes, 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", manifestPath, err)
	}

	callsBefore := resolver.calls
	downloadsBefore := downloadCount.Load()
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if resolver.calls != callsBefore {
		t.Fatalf("resolver called during locked rehydration: got %d, want %d", resolver.calls, callsBefore)
	}
	if got := downloadCount.Load() - downloadsBefore; got != 1 {
		t.Fatalf("download count during locked rehydration = %d, want 1", got)
	}

	gotManifest := cfg.Plugins["gadget"].ResolvedManifest
	if gotManifest == nil {
		t.Fatal("ResolvedManifest is nil")
	}
	if gotManifest.Version != version {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", gotManifest.Version, version)
	}

	_, readBack, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%s) after rehydrate: %v", manifestPath, err)
	}
	if readBack.Version != version {
		t.Fatalf("cached manifest version = %q, want %q", readBack.Version, version)
	}
}

func TestSourceAuthPluginLoadForExecution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	binaryContent := "fake-auth-binary"

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindAuth, "auth-plugin", binaryContent)
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
		"    secrets:",
		"      source: test-secrets",
		"  auth:",
		"    auth:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    auth: auth",
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
	authLockEntry := mustLockEntryByName(t, lock.Auth, "auth")
	if authLockEntry.Source != source {
		t.Fatalf("lock.Auth[auth].Source = %q, want %q", authLockEntry.Source, source)
	}
	if authLockEntry.Executable == "" {
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

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuth)
	if authProvider == nil {
		t.Fatal("auth provider is nil after load")
	}
	executablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != executablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, executablePath)
	}
	authCfg, err := config.NodeToMap(authProvider.Config)
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

func TestSourceAuthPluginInitAllowsMissingEnvPlaceholderInNonStringField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "github.com/acme/tools/auth-widget"
	version := "2.0.0"
	portEnv := "GESTALT_TEST_PORT_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))

	archivePath := buildExecutableArchive(t, dir, "auth-src", source, version, providermanifestv1.KindAuth, "auth-plugin", "fake-auth-binary")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	archiveSum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveSum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		"    secrets:",
		"      source: test-secrets",
		"  auth:",
		"    auth:",
		"      source:",
		"        ref: " + source,
		"        version: " + version,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    auth: auth",
		"  public:",
		"    port: ${" + portEnv + "}",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
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

	authLockEntry := mustLockEntryByName(t, lock.Auth, "auth")
	if authLockEntry.Source != source {
		t.Fatalf("lock.Auth[auth].Source = %q, want %q", authLockEntry.Source, source)
	}
	if authLockEntry.Executable == "" {
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
}

func TestManagedIndexedDBSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mainSource := "github.com/acme/providers/indexeddb-main"
	archiveSource := "github.com/acme/providers/indexeddb-archive"
	version := "1.0.0"

	mainArchivePath := buildExecutableArchive(t, dir, "indexeddb-main-src", mainSource, version, providermanifestv1.KindIndexedDB, "indexeddb-main", "main-indexeddb-binary")
	archiveArchivePath := buildExecutableArchive(t, dir, "indexeddb-archive-src", archiveSource, version, providermanifestv1.KindIndexedDB, "indexeddb-archive", "archive-indexeddb-binary")

	mainArchiveData, err := os.ReadFile(mainArchivePath)
	if err != nil {
		t.Fatalf("read main indexeddb archive: %v", err)
	}
	mainArchiveSum := sha256.Sum256(mainArchiveData)

	archiveArchiveData, err := os.ReadFile(archiveArchivePath)
	if err != nil {
		t.Fatalf("read archive indexeddb archive: %v", err)
	}
	archiveArchiveSum := sha256.Sum256(archiveArchiveData)

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			mainSource: {
				archivePath: mainArchivePath,
				resolvedURL: "https://example.com/indexeddb-main.tar.gz",
				sha256:      hex.EncodeToString(mainArchiveSum[:]),
			},
			archiveSource: {
				archivePath: archiveArchivePath,
				resolvedURL: "https://example.com/indexeddb-archive.tar.gz",
				sha256:      hex.EncodeToString(archiveArchiveSum[:]),
			},
		},
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"providers:",
		"  indexeddb:",
		"    main:",
		"      source:",
		"        ref: " + mainSource,
		"        version: " + version,
		"      config:",
		`        dsn: "sqlite://main.db"`,
		"    archive:",
		"      source:",
		"        ref: " + archiveSource,
		"        version: " + version,
		"      config:",
		`        dsn: "sqlite://archive.db"`,
		"server:",
		"  providers:",
		"    indexeddb: main",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if len(lock.IndexedDBs) != 2 {
		t.Fatalf("lock.IndexedDBs = %#v, want 2 entries", lock.IndexedDBs)
	}
	if _, ok := lock.IndexedDBs["main"]; !ok {
		t.Fatal(`lock.IndexedDBs["main"] not found`)
	}
	if _, ok := lock.IndexedDBs["archive"]; !ok {
		t.Fatal(`lock.IndexedDBs["archive"] not found`)
	}

	callsBefore := len(resolver.calls)
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if len(resolver.calls) != callsBefore {
		t.Fatalf("resolver called during locked load: got %d calls, want %d", len(resolver.calls), callsBefore)
	}

	for _, name := range []string{"main", "archive"} {
		entry := cfg.Providers.IndexedDB[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q] = nil", name)
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.IndexedDB[%q].ResolvedManifest = nil", name)
		}
		wantCommand := resolveLockPath(artifactsDir, lock.IndexedDBs[name].Executable)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.IndexedDB[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
	}
}

func TestManagedCacheSourcesLoadForExecutionWithMultipleBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionSource := "github.com/acme/providers/cache-session"
	rateLimitSource := "github.com/acme/providers/cache-rate-limit"
	version := "1.0.0"

	sessionArchivePath := buildExecutableArchive(t, dir, "cache-session-src", sessionSource, version, providermanifestv1.KindCache, "cache-session", "session-cache-binary")
	rateLimitArchivePath := buildExecutableArchive(t, dir, "cache-rate-limit-src", rateLimitSource, version, providermanifestv1.KindCache, "cache-rate-limit", "rate-limit-cache-binary")

	sessionArchiveData, err := os.ReadFile(sessionArchivePath)
	if err != nil {
		t.Fatalf("read session cache archive: %v", err)
	}
	sessionArchiveSum := sha256.Sum256(sessionArchiveData)

	rateLimitArchiveData, err := os.ReadFile(rateLimitArchivePath)
	if err != nil {
		t.Fatalf("read rate limit cache archive: %v", err)
	}
	rateLimitArchiveSum := sha256.Sum256(rateLimitArchiveData)

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			sessionSource: {
				archivePath: sessionArchivePath,
				resolvedURL: "https://example.com/cache-session.tar.gz",
				sha256:      hex.EncodeToString(sessionArchiveSum[:]),
			},
			rateLimitSource: {
				archivePath: rateLimitArchivePath,
				resolvedURL: "https://example.com/cache-rate-limit.tar.gz",
				sha256:      hex.EncodeToString(rateLimitArchiveSum[:]),
			},
		},
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	indexedDBManifestPath := writeStubIndexedDBManifest(t, dir)
	configYAML := strings.Join([]string{
		"providers:",
		"  secrets:",
		"    session:",
		"      source: session-secrets",
		"    rate_limit:",
		"      source: rate-limit-secrets",
		"  indexeddb:",
		"    main:",
		"      source:",
		"        path: " + indexedDBManifestPath,
		"      config:",
		`        path: "` + filepath.Join(dir, "gestalt.db") + `"`,
		"  cache:",
		"    session:",
		"      source:",
		"        ref: " + sessionSource,
		"        version: " + version,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: session",
		"            name: session-cache-password",
		"    rate_limit:",
		"      source:",
		"        ref: " + rateLimitSource,
		"        version: " + version,
		"      config:",
		"        password:",
		"          secret:",
		"            provider: rate_limit",
		"            name: rate-limit-cache-password",
		"server:",
		"  providers:",
		"    indexeddb: main",
		"    secrets: session",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["session-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{"session-cache-password": "resolved-session-cache-password"},
		}, nil
	}
	factories.Secrets["rate-limit-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{
			Secrets: map[string]string{"rate-limit-cache-password": "resolved-rate-limit-cache-password"},
		}, nil
	}

	lc := NewLifecycle(resolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})
	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	if len(lock.Caches) != 2 {
		t.Fatalf("lock.Caches = %#v, want 2 entries", lock.Caches)
	}
	if _, ok := lock.Caches["session"]; !ok {
		t.Fatal(`lock.Caches["session"] not found`)
	}
	if _, ok := lock.Caches["rate_limit"]; !ok {
		t.Fatal(`lock.Caches["rate_limit"] not found`)
	}
	lockPath := filepath.Join(dir, InitLockfileName)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	var diskLock providerLockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	if _, ok := diskLock.Providers.Cache["session"]; !ok {
		t.Fatal(`disk lock cache["session"] not found`)
	}
	if _, ok := diskLock.Providers.Cache["rate_limit"]; !ok {
		t.Fatal(`disk lock cache["rate_limit"] not found`)
	}

	callsBefore := len(resolver.calls)
	cfg, _, err := lc.LoadForExecutionAtPath(configPath, true)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
	}
	if len(resolver.calls) != callsBefore {
		t.Fatalf("resolver called during locked load: got %d calls, want %d", len(resolver.calls), callsBefore)
	}

	wantPasswords := map[string]string{
		"session":    "resolved-session-cache-password",
		"rate_limit": "resolved-rate-limit-cache-password",
	}
	for _, name := range []string{"session", "rate_limit"} {
		entry := cfg.Providers.Cache[name]
		if entry == nil {
			t.Fatalf("cfg.Providers.Cache[%q] = nil", name)
		}
		if entry.ResolvedManifest == nil {
			t.Fatalf("cfg.Providers.Cache[%q].ResolvedManifest = nil", name)
		}
		wantCommand := resolveLockPath(artifactsDir, lock.Caches[name].Executable)
		if entry.Command != wantCommand {
			t.Fatalf("cfg.Providers.Cache[%q].Command = %q, want %q", name, entry.Command, wantCommand)
		}
		runtimeCfg, err := config.NodeToMap(entry.Config)
		if err != nil {
			t.Fatalf("NodeToMap(cache %q config): %v", name, err)
		}
		configMap, ok := runtimeCfg["config"].(map[string]any)
		if !ok {
			t.Fatalf("cache %q runtime config = %#v", name, runtimeCfg["config"])
		}
		if got := configMap["password"]; got != wantPasswords[name] {
			t.Fatalf("cache %q password = %#v, want %q", name, got, wantPasswords[name])
		}
	}
}

func TestManagedCacheSourcesInitAtPathWithPlatformsHashesExtraPlatformArchives(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cacheSource := "github.com/acme/tools/cache-session"
	version := "1.0.0"

	cacheArchivePath := buildExecutableArchive(
		t,
		dir,
		"cache-src",
		cacheSource,
		version,
		providermanifestv1.KindCache,
		"cache-plugin",
		"fake-cache-binary",
	)
	cacheArchiveData, err := os.ReadFile(cacheArchivePath)
	if err != nil {
		t.Fatalf("read cache archive: %v", err)
	}
	cacheArchiveSum := sha256.Sum256(cacheArchiveData)

	extraPlatform := struct{ GOOS, GOARCH, LibC string }{GOOS: "linux", GOARCH: "amd64"}
	if runtime.GOOS == extraPlatform.GOOS && runtime.GOARCH == extraPlatform.GOARCH {
		extraPlatform = struct{ GOOS, GOARCH, LibC string }{GOOS: "darwin", GOARCH: "arm64"}
	}
	extraPlatformKey := providerpkg.PlatformString(extraPlatform.GOOS, extraPlatform.GOARCH)
	extraArchiveData := []byte("fake-cache-extra-platform-archive")
	extraArchiveSum := sha256.Sum256(extraArchiveData)

	var extraAssetCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cache-extra.tar.gz":
			extraAssetCount.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(extraArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	resolver := &fakeMultiResolver{
		results: map[string]fakeResolverResult{
			cacheSource: {
				archivePath: cacheArchivePath,
				resolvedURL: srv.URL + "/cache-current.tar.gz",
				sha256:      hex.EncodeToString(cacheArchiveSum[:]),
				platformArchives: []pluginsource.PlatformArchive{
					{Platform: extraPlatformKey, URL: srv.URL + "/cache-extra.tar.gz"},
				},
			},
		},
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "gestalt.db")) + strings.Join([]string{
		"  cache:",
		"    session:",
		"      source:",
		"        ref: " + cacheSource,
		"        version: " + version,
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPathWithPlatforms(configPath, "", []struct{ GOOS, GOARCH, LibC string }{extraPlatform})
	if err != nil {
		t.Fatalf("InitAtPathWithPlatforms: %v", err)
	}
	if got := extraAssetCount.Load(); got != 1 {
		t.Fatalf("extra asset request count = %d, want 1", got)
	}

	entry, ok := lock.Caches["session"]
	if !ok {
		t.Fatal(`lock.Caches["session"] not found`)
	}
	if got := entry.Archives[extraPlatformKey].SHA256; got != hex.EncodeToString(extraArchiveSum[:]) {
		t.Fatalf("lock extra-platform SHA256 = %q, want %q", got, hex.EncodeToString(extraArchiveSum[:]))
	}

	readBack, err := ReadLockfile(filepath.Join(dir, InitLockfileName))
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := readBack.Caches["session"].Archives[extraPlatformKey].SHA256; got != hex.EncodeToString(extraArchiveSum[:]) {
		t.Fatalf("readBack extra-platform SHA256 = %q, want %q", got, hex.EncodeToString(extraArchiveSum[:]))
	}
}

func TestSourceSecretsPluginBootstrapsManagedAuthSourceToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretsSourceToken := "ghp_inline_auth_source_token"
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	secretsSource := "github.com/acme/tools/secrets-widget"
	secretsVersion := "1.0.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	bootstrapManifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	bootstrapManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  bootstrapSource,
		Version: bootstrapVersion,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(bootstrapManifestPath, bootstrapManifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}

	secretsArchivePath := buildExecutableArchiveFromBinaryPath(
		t,
		dir,
		"secrets-src",
		secretsSource,
		secretsVersion,
		providermanifestv1.KindSecrets,
		"secrets-plugin",
		buildGoSourceSecretsBinary(t),
	)
	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-src",
		authSource,
		authVersion,
		providermanifestv1.KindAuth,
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
			if got := r.Header.Get("Authorization"); got != "token "+secretsSourceToken {
				http.Error(w, "bad auth header for secrets download", http.StatusUnauthorized)
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
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"    secrets:",
		"      source:",
		"        ref: " + secretsSource,
		"        version: " + secretsVersion,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"  auth:",
		"    auth:",
		"      source:",
		"        ref: " + authSource,
		"        version: " + authVersion,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: secrets",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: secrets",
		"    auth: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle(resolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	lock, err := lc.InitAtPath(configPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}
	secretsLockEntry := mustLockEntryByName(t, lock.Secrets, "secrets")
	authLockEntry := mustLockEntryByName(t, lock.Auth, "auth")
	if got := len(resolver.calls); got != 2 {
		t.Fatalf("resolver calls = %d, want 2", got)
	}
	if resolver.calls[0].src.String() != secretsSource {
		t.Fatalf("first resolver source = %q, want %q", resolver.calls[0].src.String(), secretsSource)
	}
	if resolver.calls[0].src.Token != secretsSourceToken {
		t.Fatalf("secrets resolver token = %q, want %q", resolver.calls[0].src.Token, secretsSourceToken)
	}
	if resolver.calls[1].src.String() != authSource {
		t.Fatalf("second resolver source = %q, want %q", resolver.calls[1].src.String(), authSource)
	}
	if resolver.calls[1].src.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("auth resolver token = %q, want %q", resolver.calls[1].src.Token, "ghp_inline_auth_source_token")
	}

	secretsRoot := filepath.Join(artifactsDir, ".gestaltd", "secrets", "secrets")
	if err := os.RemoveAll(secretsRoot); err != nil {
		t.Fatalf("RemoveAll secrets provider root: %v", err)
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
	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuth)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}

	secretsExecutablePath := resolveLockPath(artifactsDir, secretsLockEntry.Executable)
	secretsProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindSecrets)
	if secretsProvider == nil {
		t.Fatal("secrets provider is nil after load")
	}
	if secretsProvider.Source.Auth == nil {
		t.Fatalf("secrets provider source auth = %#v", secretsProvider)
	}
	if secretsProvider.Source.Auth.Token != secretsSourceToken {
		t.Fatalf("resolved secrets source token = %q, want %q", secretsProvider.Source.Auth.Token, secretsSourceToken)
	}
	if secretsProvider.Command != secretsExecutablePath {
		t.Fatalf("secrets provider command = %q, want %q", secretsProvider.Command, secretsExecutablePath)
	}
	authExecutablePath := resolveLockPath(artifactsDir, authLockEntry.Executable)
	if authProvider.Command != authExecutablePath {
		t.Fatalf("auth provider command = %q, want %q", authProvider.Command, authExecutablePath)
	}
}

func TestLoadForExecutionAtPath_UnlockedBootstrapInitPreparesOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	bootstrapManifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	bootstrapManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  bootstrapSource,
		Version: bootstrapVersion,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(bootstrapManifestPath, bootstrapManifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}

	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-src",
		authSource,
		authVersion,
		providermanifestv1.KindAuth,
		"auth-plugin",
		"fake-auth-binary",
	)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)
	resolver := &fakeResolver{
		archivePath: authArchivePath,
		resolvedURL: "https://example.test/auth.tar.gz",
		sha256:      hex.EncodeToString(authArchiveSum[:]),
	}

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"  auth:",
		"    auth:",
		"      source:",
		"        ref: " + authSource,
		"        version: " + authVersion,
		"        auth:",
		"          token:",
		"            secret:",
		"              provider: bootstrap",
		"              name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"    auth: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle(resolver).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if got := resolver.calls; got != 1 {
		t.Fatalf("resolver calls = %d, want 1", got)
	}
	if resolver.lastSrc.String() != authSource {
		t.Fatalf("resolver source = %q, want %q", resolver.lastSrc.String(), authSource)
	}
	if resolver.lastVersion != authVersion {
		t.Fatalf("resolver version = %q, want %q", resolver.lastVersion, authVersion)
	}
	if resolver.lastSrc.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolver token = %q, want %q", resolver.lastSrc.Token, "ghp_inline_auth_source_token")
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuth)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
	}
}

func TestLoadForExecutionAtPath_UnlockedBootstrapMetadataInitPreparesOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bootstrapSource := "github.com/acme/tools/bootstrap-secrets"
	bootstrapVersion := "0.1.0"
	authSource := "github.com/acme/tools/auth-widget"
	authVersion := "2.0.0"
	bootstrapArtifact := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "bootstrap-secrets"))
	bootstrapManifestPath := filepath.Join(dir, "bootstrap-secrets-manifest.yaml")
	bootstrapManifest, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindSecrets,
		Source:  bootstrapSource,
		Version: bootstrapVersion,
		Spec:    &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: bootstrapArtifact},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: bootstrapArtifact},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat bootstrap: %v", err)
	}
	if err := os.WriteFile(bootstrapManifestPath, bootstrapManifest, 0o644); err != nil {
		t.Fatalf("write bootstrap manifest: %v", err)
	}
	bootstrapBinaryData, err := os.ReadFile(buildGoSourceSecretsBinary(t))
	if err != nil {
		t.Fatalf("read bootstrap binary: %v", err)
	}
	bootstrapBinaryPath := filepath.Join(dir, filepath.FromSlash(bootstrapArtifact))
	if err := os.MkdirAll(filepath.Dir(bootstrapBinaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap artifact: %v", err)
	}
	if err := os.WriteFile(bootstrapBinaryPath, bootstrapBinaryData, 0o755); err != nil {
		t.Fatalf("write bootstrap artifact: %v", err)
	}

	authArchivePath := buildExecutableArchive(
		t,
		dir,
		"auth-metadata-src",
		authSource,
		authVersion,
		providermanifestv1.KindAuth,
		"auth-plugin",
		"fake-auth-binary",
	)
	authArchiveData, err := os.ReadFile(authArchivePath)
	if err != nil {
		t.Fatalf("read auth archive: %v", err)
	}
	authArchiveSum := sha256.Sum256(authArchiveData)

	var metadataCount atomic.Int64
	var archiveCount atomic.Int64
	handlerErrs := make(chan error, 4)
	nextHandlerErr := func() error {
		t.Helper()
		select {
		case err := <-handlerErrs:
			return err
		default:
			return nil
		}
	}

	metadataPath := "/providers/auth/provider-release.yaml"
	archivePathURL := "/providers/auth/auth-current.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case metadataPath:
			metadataCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("metadata authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad metadata authorization", http.StatusBadRequest)
				return
			}
			metadata := providerReleaseMetadata{
				Schema:        providerReleaseSchemaName,
				SchemaVersion: providerReleaseSchemaVersion,
				Package:       authSource,
				Kind:          providermanifestv1.KindAuth,
				Version:       authVersion,
				Runtime:       providerReleaseRuntimeExecutable,
				Artifacts: map[string]providerReleaseArtifact{
					providerpkg.CurrentPlatformString(): {
						Path:   filepath.Base(archivePathURL),
						SHA256: hex.EncodeToString(authArchiveSum[:]),
					},
				},
			}
			data, err := yaml.Marshal(metadata)
			if err != nil {
				handlerErrs <- fmt.Errorf("marshal metadata: %v", err)
				http.Error(w, "metadata marshal failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(data)
		case archivePathURL:
			archiveCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer ghp_inline_auth_source_token" {
				handlerErrs <- fmt.Errorf("archive authorization = %q, want %q", got, "Bearer ghp_inline_auth_source_token")
				http.Error(w, "bad archive authorization", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(authArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := strings.Join([]string{
		"apiVersion: " + config.APIVersionV3,
		requiredIndexedDBConfigYAML(t, dir, filepath.Join(dir, "data.db")),
		"  secrets:",
		"    bootstrap:",
		"      source:",
		"        path: ./bootstrap-secrets-manifest.yaml",
		"  auth:",
		"    auth:",
		"      source: " + srv.URL + metadataPath + "?download=1",
		"      auth:",
		"        token:",
		"          secret:",
		"            provider: bootstrap",
		"            name: source-token",
		"      config:",
		"        clientId: managed-auth-client",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"    secrets: bootstrap",
		"    auth: auth",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"

	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	factories := bootstrap.NewFactoryRegistry()
	factories.Secrets["provider"] = secretsprovider.Factory

	lc := NewLifecycle(nil).WithConfigSecretResolver(func(ctx context.Context, cfg *config.Config) error {
		return bootstrap.ResolveConfigSecrets(ctx, cfg, factories)
	})

	cfg, _, err := lc.LoadForExecutionAtPath(configPath, false)
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("LoadForExecutionAtPath(locked=false): %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := metadataCount.Load(); got != 1 {
		t.Fatalf("metadata request count = %d, want 1", got)
	}
	if got := archiveCount.Load(); got != 1 {
		t.Fatalf("archive request count = %d, want 1", got)
	}

	authProvider := mustSelectedHostProviderEntry(t, cfg, config.HostProviderKindAuth)
	if authProvider == nil || authProvider.Source.Auth == nil {
		t.Fatalf("auth provider source auth = %#v", authProvider)
	}
	if authProvider.Source.Auth.Token != "ghp_inline_auth_source_token" {
		t.Fatalf("resolved auth source token = %q, want %q", authProvider.Source.Auth.Token, "ghp_inline_auth_source_token")
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
			browserURL := "http://" + r.Host + "/browser-dl"
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
		case "/browser-dl":
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
		"plugins:",
		"  alpha:",
		"    source:",
		"      ref: " + testSource,
		"      version: " + testVersion,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
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
	if entry.Archives[providerpkg.CurrentPlatformString()].URL == "" {
		t.Error("entry archive URL is empty")
	}
	browserURL := srv.URL + "/browser-dl"
	if entry.Archives[providerpkg.CurrentPlatformString()].URL != browserURL {
		t.Errorf("entry archive URL = %q, want %q", entry.Archives[providerpkg.CurrentPlatformString()].URL, browserURL)
	}
	if entry.Archives[providerpkg.CurrentPlatformString()].SHA256 == "" {
		t.Error("entry archive SHA256 is empty")
	}
	wantSHA := sha256hex(string(archiveData))
	if entry.Archives[providerpkg.CurrentPlatformString()].SHA256 != wantSHA {
		t.Errorf("entry archive SHA256 = %q, want %q", entry.Archives[providerpkg.CurrentPlatformString()].SHA256, wantSHA)
	}

	configDir := filepath.Dir(configPath)
	lockPath := filepath.Join(configDir, InitLockfileName)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}
	if !strings.Contains(string(lockData), `"schema": "gestaltd-provider-lock"`) {
		t.Fatalf("lockfile = %s, want provider lock schema", lockData)
	}
	if strings.Contains(string(lockData), `"version": 7`) {
		t.Fatalf("lockfile = %s, want schema-based versioning", lockData)
	}
	if strings.Contains(string(lockData), `"manifest":`) || strings.Contains(string(lockData), `"executable":`) {
		t.Fatalf("lockfile = %s, want portable entries only", lockData)
	}
	var diskLock providerLockfile
	if err := json.Unmarshal(lockData, &diskLock); err != nil {
		t.Fatalf("Unmarshal lockfile: %v", err)
	}
	diskEntry, ok := diskLock.Providers.Plugin["alpha"]
	if !ok {
		t.Fatal(`disk lock providers.plugin["alpha"] not found`)
	}
	if diskEntry.InputDigest == "" {
		t.Fatal("disk lock plugin inputDigest is empty")
	}
	if diskEntry.Package != testSource {
		t.Fatalf("disk lock plugin package = %q, want %q", diskEntry.Package, testSource)
	}
	if diskEntry.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("disk lock plugin kind = %q, want %q", diskEntry.Kind, providermanifestv1.KindPlugin)
	}
	if diskEntry.Runtime != providerLockRuntimeExecutable {
		t.Fatalf("disk lock plugin runtime = %q, want %q", diskEntry.Runtime, providerLockRuntimeExecutable)
	}
	if diskEntry.Source != "" {
		t.Fatalf("disk lock plugin source = %q, want omitted portable source", diskEntry.Source)
	}
	if diskEntry.Version != testVersion {
		t.Fatalf("disk lock plugin entry = %#v", diskEntry)
	}
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
	if cfg.Plugins["alpha"].Command != executablePath {
		t.Errorf("plugin command = %q, want %q", cfg.Plugins["alpha"].Command, executablePath)
	}
}

func TestSourcePluginInitWithPlatformsPersistsExtraPlatformHash(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "wrong-env-token")

	dir := t.TempDir()

	src := pluginsource.Source{
		Host:  pluginsource.HostGitHub,
		Owner: testOwner,
		Repo:  testRepo,
		Path:  "plugins/" + testPlugin,
	}
	expectedTag := src.ReleaseTag(testVersion)
	releasePath := "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag

	currentAssetName := fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", src.PluginName(), testVersion, runtime.GOOS, runtime.GOARCH)
	extraPlatform := struct {
		goos   string
		goarch string
	}{
		goos:   "linux",
		goarch: "amd64",
	}
	for _, candidate := range []struct {
		goos   string
		goarch string
	}{
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "darwin", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	} {
		if candidate.goos != runtime.GOOS || candidate.goarch != runtime.GOARCH {
			extraPlatform = candidate
			break
		}
	}
	extraPlatformKey := providerpkg.PlatformString(extraPlatform.goos, extraPlatform.goarch)
	extraAssetName := fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", src.PluginName(), testVersion, extraPlatform.goos, extraPlatform.goarch)

	currentArchivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	extraArchiveData := []byte("extra-platform-archive")
	extraArchiveSHA := sha256.Sum256(extraArchiveData)

	var releaseCount atomic.Int64
	var currentAssetCount atomic.Int64
	var extraAssetCount atomic.Int64
	handlerErrs := make(chan error, 4)
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
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"assets": []map[string]any{
					{
						"name":                 currentAssetName,
						"url":                  "http://" + r.Host + "/asset-current",
						"browser_download_url": "http://" + r.Host + "/browser-current",
					},
					{
						"name":                 extraAssetName,
						"url":                  "http://" + r.Host + "/asset-extra",
						"browser_download_url": "http://" + r.Host + "/browser-extra",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/asset-current":
			currentAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("current asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("current asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		case "/browser-current":
			currentAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("current browser asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("current browser asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		case "/asset-extra":
			extraAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("extra asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("extra asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(extraArchiveData)
		case "/browser-extra":
			extraAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("extra browser asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("extra browser asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(extraArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"plugins:",
		"  alpha:",
		"    source:",
		"      ref: " + testSource,
		"      version: " + testVersion,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	lockPath := filepath.Join(dir, "state", "platforms", InitLockfileName)

	resolver := &ghresolver.GitHubResolver{BaseURL: srv.URL}
	lc := NewLifecycle(resolver)
	lock, err := lc.InitAtPathsWithPlatforms([]string{configPath}, StatePaths{LockfilePath: lockPath}, []struct{ GOOS, GOARCH, LibC string }{
		{GOOS: extraPlatform.goos, GOARCH: extraPlatform.goarch},
	})
	if err != nil {
		if handlerErr := nextHandlerErr(); handlerErr != nil {
			t.Fatal(handlerErr)
		}
		t.Fatalf("InitAtPathWithPlatforms: %v", err)
	}
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got := currentAssetCount.Load(); got != 1 {
		t.Fatalf("current asset request count = %d, want 1", got)
	}
	if got := extraAssetCount.Load(); got != 1 {
		t.Fatalf("extra asset request count = %d, want 1", got)
	}
	if got := releaseCount.Load(); got < 2 {
		t.Fatalf("release request count = %d, want at least 2", got)
	}

	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] not found`)
	}
	if got := entry.Archives[extraPlatformKey].SHA256; got != hex.EncodeToString(extraArchiveSHA[:]) {
		t.Fatalf("lock extra-platform SHA256 = %q, want %q", got, hex.EncodeToString(extraArchiveSHA[:]))
	}

	readBack, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := readBack.Providers["alpha"].Archives[extraPlatformKey].SHA256; got != hex.EncodeToString(extraArchiveSHA[:]) {
		t.Fatalf("readBack extra-platform SHA256 = %q, want %q", got, hex.EncodeToString(extraArchiveSHA[:]))
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestSourcePluginInitWithPlatformsRejectsGenericFallbackForExtraPlatform(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "wrong-env-token")

	dir := t.TempDir()

	src := pluginsource.Source{
		Host:  pluginsource.HostGitHub,
		Owner: testOwner,
		Repo:  testRepo,
		Path:  "plugins/" + testPlugin,
	}
	expectedTag := src.ReleaseTag(testVersion)
	releasePath := "/repos/" + testOwner + "/" + testRepo + "/releases/tags/" + expectedTag

	currentAssetName := fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", src.PluginName(), testVersion, runtime.GOOS, runtime.GOARCH)
	genericAssetName := src.AssetName(testVersion)
	extraPlatform := struct {
		goos   string
		goarch string
	}{
		goos:   "linux",
		goarch: "amd64",
	}
	for _, candidate := range []struct {
		goos   string
		goarch string
	}{
		{goos: "linux", goarch: "amd64"},
		{goos: "linux", goarch: "arm64"},
		{goos: "darwin", goarch: "amd64"},
		{goos: "darwin", goarch: "arm64"},
	} {
		if candidate.goos != runtime.GOOS || candidate.goarch != runtime.GOARCH {
			extraPlatform = candidate
			break
		}
	}

	currentArchivePath := buildV2Archive(t, dir, testSource, testVersion, testBinary)
	currentArchiveData, err := os.ReadFile(currentArchivePath)
	if err != nil {
		t.Fatalf("read current archive: %v", err)
	}
	genericArchiveData := []byte("generic-platform-archive")

	var releaseCount atomic.Int64
	var currentAssetCount atomic.Int64
	var genericAssetCount atomic.Int64
	handlerErrs := make(chan error, 4)
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
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"assets": []map[string]any{
					{
						"name":                 currentAssetName,
						"url":                  "http://" + r.Host + "/asset-current",
						"browser_download_url": "http://" + r.Host + "/browser-current",
					},
					{
						"name":                 genericAssetName,
						"url":                  "http://" + r.Host + "/asset-generic",
						"browser_download_url": "http://" + r.Host + "/browser-generic",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/asset-current":
			currentAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("current asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("current asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		case "/browser-current":
			currentAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("current browser asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("current browser asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(currentArchiveData)
		case "/asset-generic":
			genericAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("generic asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("generic asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(genericArchiveData)
		case "/browser-generic":
			genericAssetCount.Add(1)
			if got := r.Header.Get("Authorization"); got != "token test-token" {
				handlerErrs <- fmt.Errorf("generic browser asset authorization = %q, want %q", got, "token test-token")
				http.Error(w, "bad asset authorization", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				handlerErrs <- fmt.Errorf("generic browser asset accept = %q, want %q", got, "application/octet-stream")
				http.Error(w, "bad asset accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(genericArchiveData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	artifactsDir := filepath.Join(dir, "prepared-artifacts")
	configYAML := requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + strings.Join([]string{
		"plugins:",
		"  alpha:",
		"    source:",
		"      ref: " + testSource,
		"      version: " + testVersion,
		"      auth:",
		"        token: test-token",
		"server:",
		"  providers:",
		"    indexeddb: sqlite",
		"  artifactsDir: " + artifactsDir,
		"  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "\n") + "\n"
	configPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolver := &ghresolver.GitHubResolver{BaseURL: srv.URL}
	lc := NewLifecycle(resolver)
	_, err = lc.InitAtPathWithPlatforms(configPath, "", []struct{ GOOS, GOARCH, LibC string }{
		{GOOS: extraPlatform.goos, GOARCH: extraPlatform.goarch},
	})
	if handlerErr := nextHandlerErr(); handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if err == nil {
		t.Fatal("InitAtPathWithPlatforms unexpectedly succeeded with generic-only extra platform fallback")
	}
	if !strings.Contains(err.Error(), "generic release archives are not allowed") {
		t.Fatalf("InitAtPathWithPlatforms error = %v, want generic archive policy rejection", err)
	}
	if got := currentAssetCount.Load(); got != 1 {
		t.Fatalf("current asset request count = %d, want 1", got)
	}
	if got := genericAssetCount.Load(); got != 0 {
		t.Fatalf("generic asset request count = %d, want 0", got)
	}
	if got := releaseCount.Load(); got < 2 {
		t.Fatalf("release request count = %d, want at least 2", got)
	}
}
