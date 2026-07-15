package e2e

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"gopkg.in/yaml.v3"
)

func TestE2EProviderAddDefaultsToPackageSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v8\napps:\n")
	indexURL := writeProviderLifecycleIndex(t, dir)

	runGestaltd(t, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	out := runGestaltd(t, "provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--no-lock")
	assertContains(t, out, "Added app alpha")
	assertContains(t, out, "version 1.2.3")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.APIVersion; got != config.ConfigAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", got, config.ConfigAPIVersion)
	}
	entry := cfg.Apps["alpha"]
	if entry == nil {
		t.Fatal(`Apps["alpha"] = nil`)
		return
	}
	if !entry.Source.IsPackage() {
		t.Fatal("Source.IsPackage = false, want true")
	}
	if got := entry.Source.PackageRepo(); got != "local" {
		t.Fatalf("Source.PackageRepo = %q, want local", got)
	}
	if got := entry.Source.PackageAddress(); got != "github.com/acme/providers/alpha" {
		t.Fatalf("Source.PackageAddress = %q", got)
	}
	if got := entry.Source.PackageVersionConstraint(); got != "" {
		t.Fatalf("Source.PackageVersionConstraint = %q, want empty", got)
	}
}

func TestE2EProviderCommandProgressAndRepositoryMutationOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v8\napps:\n")
	indexURL := writeProviderLifecycleIndex(t, dir)
	env := []string{
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "xdg-config"),
		"XDG_CACHE_HOME=" + filepath.Join(dir, "xdg-cache"),
	}

	stdout, stderr, err := runGestaltdStreamsWithEnv(env, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	if err != nil {
		t.Fatalf("provider repo add: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertContains(t, stderr, `Added provider repository "local"`)
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("provider repo add stdout = %q, want empty", stdout)
	}

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "search", "alpha", "--repo", "local", "--config", cfgPath)
	if err != nil {
		t.Fatalf("provider search: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertContains(t, stdout, "local\tgithub.com/acme/providers/alpha\tAlpha")
	if strings.Contains(stdout, "Fetching provider repository") {
		t.Fatalf("provider search progress leaked to stdout: %s", stdout)
	}
	assertContains(t, stderr, `Fetching provider repository "local"`)

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "info", "github.com/acme/providers/alpha", "--repo", "local", "--config", cfgPath)
	if err != nil {
		t.Fatalf("provider info: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertContains(t, stdout, "local\tgithub.com/acme/providers/alpha\tAlpha")
	assertContains(t, stderr, `Fetching provider repository "local"`)

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "repo", "remove", "local", "--config", cfgPath)
	if err != nil {
		t.Fatalf("provider repo remove: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertContains(t, stderr, `Removed provider repository "local"`)
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("provider repo remove stdout = %q, want empty", stdout)
	}

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "repo", "remove", "local", "--config", cfgPath)
	if err == nil {
		t.Fatalf("provider repo remove missing name succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(stderr, `Removed provider repository`) {
		t.Fatalf("missing repo remove reported success: stderr=%q", stderr)
	}
	if !strings.Contains(stdout+stderr, "not found") {
		t.Fatalf("missing repo remove error = stdout:%q stderr:%q", stdout, stderr)
	}

	updateConfigPath := filepath.Join(dir, "update.yaml")
	writeProviderLifecycleTestFile(t, updateConfigPath, fmt.Sprintf("apiVersion: gestaltd.config/v8\nproviderRepositories:\n  gestalt:\n    url: %s\n", indexURL))
	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "repo", "update", "--config", updateConfigPath)
	if err != nil {
		t.Fatalf("provider repo update: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertContains(t, stderr, `Updated provider repository "gestalt"`)
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("provider repo update stdout = %q, want empty", stdout)
	}
	assertContains(t, stderr, `Fetching provider repository "gestalt"`)
}

func TestE2EProviderAddAndUpgradeWriteLockWithTokenedRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const pkg = "github.com/acme/providers/alpha"
	metadata123 := writeProviderLifecycleRelease(t, dir, pkg, "1.2.3")
	metadata130 := writeProviderLifecycleRelease(t, dir, pkg, "1.3.0")
	indexPath := filepath.Join(dir, "provider-index.yaml")
	writeProviderLifecycleTestFile(t, indexPath, fmt.Sprintf(`schema: gestaltd-provider-index
schemaVersion: 1
packages:
  %s:
    displayName: Alpha
    versions:
      1.2.3:
        metadata: %s
        kind: app
        runtime: executable
      1.3.0:
        metadata: %s
        kind: app
        runtime: executable
`, pkg, metadata123, metadata130))

	var authorizedIndexRequests atomic.Int32
	fileServer := http.FileServer(http.Dir(dir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/provider-index.yaml" {
			if r.Header.Get("Authorization") != "Bearer secret-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			authorizedIndexRequests.Add(1)
			http.ServeFile(w, r, indexPath)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	xdgConfigHome := filepath.Join(dir, "xdg")
	writeProviderLifecycleTestFile(t, filepath.Join(xdgConfigHome, "gestalt", "provider-repositories.yaml"), fmt.Sprintf(`repositories:
  private:
    url: %s/provider-index.yaml
    token: secret-token
`, server.URL))

	cfgPath := filepath.Join(dir, "gestalt.yaml")
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v8\napps:\n")
	env := []string{"XDG_CONFIG_HOME=" + xdgConfigHome}

	stdout, stderr, err := runGestaltdStreamsWithEnv(env, "provider", "add", pkg, "--config", cfgPath, "--repo", "private", "--name", "alpha", "--version", "1.2.3", "--lockfile", lockPath)
	if err != nil {
		t.Fatalf("provider add: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertProviderMutationStatusStderr(t, stdout, stderr, "Added app alpha", "Lockfile: "+lockPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if got := cfg.ProviderRepositories["private"].URL; got != server.URL+"/provider-index.yaml" {
		t.Fatalf("project repo URL = %q", got)
	}
	if got := cfg.Apps["alpha"].Source.PackageRepo(); got != "private" {
		t.Fatalf("package repo = %q, want private", got)
	}
	assertProviderLifecycleLockEntry(t, lockPath, "1.2.3", server.URL+"/"+metadata123)

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "upgrade", "alpha", "--version", "1.3.0", "--config", cfgPath, "--lockfile", lockPath)
	if err != nil {
		t.Fatalf("provider upgrade: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	assertProviderMutationStatusStderr(t, stdout, stderr, "Updated app alpha version constraint to 1.3.0", "Lockfile: "+lockPath)
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after upgrade: %v", err)
	}
	if got := cfg.Apps["alpha"].Source.PackageVersionConstraint(); got != "1.3.0" {
		t.Fatalf("version constraint = %q, want 1.3.0", got)
	}
	assertProviderLifecycleLockEntry(t, lockPath, "1.3.0", server.URL+"/"+metadata130)
	if got := authorizedIndexRequests.Load(); got < 3 {
		t.Fatalf("authorized index requests = %d, want at least 3", got)
	}

	stdout, stderr, err = runGestaltdStreamsWithEnv(env, "provider", "remove", "alpha", "--kind", "app", "--config", cfgPath, "--lockfile", lockPath, "--quiet")
	if err != nil {
		t.Fatalf("quiet provider remove: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("quiet provider remove output = stdout:%q stderr:%q, want both empty", stdout, stderr)
	}
}

func TestE2EProviderAddExactSourceAndRejectsRepeatedConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	otherCfgPath := filepath.Join(dir, "other.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v8\napps:\n")
	writeProviderLifecycleTestFile(t, otherCfgPath, "apiVersion: gestaltd.config/v8\n")
	indexURL := writeProviderLifecycleIndex(t, dir)

	runGestaltd(t, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	out := runGestaltd(t, "provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--exact-source", "--no-lock")
	assertContains(t, out, "Added app alpha")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Apps["alpha"].Source.MetadataURL(); got != "https://example.com/provider-release.yaml" {
		t.Fatalf("MetadataURL = %q", got)
	}

	out, err = runGestaltdResult("provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--exact-source", "--no-lock")
	if err == nil {
		t.Fatalf("duplicate provider add succeeded: %s", out)
	}
	assertContains(t, out, "already exists")

	out, err = runGestaltdResult("provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--config", otherCfgPath, "--repo", "local", "--name", "beta", "--no-lock")
	if err == nil {
		t.Fatalf("provider add with repeated --config succeeded: %s", out)
	}
	assertContains(t, out, "only one --config")
}

func TestE2EProviderListOfflineAndLockStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v8
providerRepositories:
  unreachable:
    url: http://127.0.0.1:1/provider-index.yaml
apps:
  alpha:
    source:
      repo: unreachable
      package: github.com/acme/providers/alpha
      version: ">= 1.0.0"
  bravo:
    source: https://example.com/bravo/provider-release.yaml
providers:
  telemetry:
    traces:
      source:
        package: github.com/acme/providers/telemetry
        version: "1.0.0"
  audit:
    ledger:
      source: https://example.com/audit/provider-release.yaml
  secrets:
    env:
      source: env
`)
	out := runGestaltd(t, "provider", "list", "--config", cfgPath)
	assertContains(t, out, "KIND")
	assertContains(t, out, "alpha")
	if got := providerLifecycleListStatus(t, out, "app", "alpha"); got != "unlocked" {
		t.Fatalf("alpha status = %q, want unlocked\n%s", got, out)
	}
	assertContains(t, out, "builtin")

	cfg, err := config.LoadPartialAllowMissingEnvPaths([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadPartialAllowMissingEnvPaths: %v", err)
	}
	fingerprint, err := operator.ProviderFingerprint("alpha", cfg.Apps["alpha"], dir)
	if err != nil {
		t.Fatalf("ProviderFingerprint: %v", err)
	}
	bravoFingerprint, err := operator.ProviderFingerprint("bravo", cfg.Apps["bravo"], dir)
	if err != nil {
		t.Fatalf("ProviderFingerprint(bravo): %v", err)
	}
	telemetryFingerprint, err := operator.ProviderFingerprint("traces", cfg.Providers.Telemetry["traces"], dir)
	if err != nil {
		t.Fatalf("ProviderFingerprint(telemetry): %v", err)
	}
	auditFingerprint, err := operator.ProviderFingerprint("ledger", cfg.Providers.Audit["ledger"], dir)
	if err != nil {
		t.Fatalf("ProviderFingerprint(audit): %v", err)
	}
	lock := &operator.Lockfile{}
	lock.Providers.App = map[string]operator.LockEntry{
		"alpha": {
			InputDigest: fingerprint,
			Package:     "github.com/acme/providers/alpha",
			Kind:        "app",
			Runtime:     "executable",
			Version:     "1.2.3",
		},
		"bravo": {
			InputDigest: bravoFingerprint,
			Package:     "github.com/acme/providers/bravo",
			Kind:        "app",
			Runtime:     "executable",
			Source:      "https://example.com/bravo/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	lock.Providers.Telemetry = map[string]operator.LockEntry{
		"traces": {
			InputDigest: telemetryFingerprint,
			Package:     "github.com/acme/providers/telemetry",
			Kind:        "telemetry",
			Runtime:     "declarative",
			Source:      "https://example.com/telemetry/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	lock.Providers.Audit = map[string]operator.LockEntry{
		"ledger": {
			InputDigest: auditFingerprint,
			Package:     "github.com/acme/providers/audit",
			Kind:        "audit",
			Runtime:     "declarative",
			Source:      "https://example.com/audit/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	if err := operator.WriteLockfile(filepath.Join(dir, operator.LockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	out = runGestaltd(t, "provider", "list", "--config", cfgPath)
	assertContains(t, out, "1.2.3")
	if got := providerLifecycleListStatus(t, out, "app", "alpha"); got != "drifted" {
		t.Fatalf("alpha status = %q, want drifted when package lock source is missing\n%s", got, out)
	}

	lock = &operator.Lockfile{}
	lock.Providers.App = map[string]operator.LockEntry{
		"alpha": {
			InputDigest: fingerprint,
			Package:     "github.com/acme/providers/alpha",
			Kind:        "app",
			Runtime:     "executable",
			Source:      "https://example.com/provider-release.yaml",
			Version:     "1.2.3",
		},
		"bravo": {
			InputDigest: bravoFingerprint,
			Package:     "github.com/acme/providers/bravo",
			Kind:        "app",
			Runtime:     "executable",
			Source:      "https://example.com/bravo/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	lock.Providers.Telemetry = map[string]operator.LockEntry{
		"traces": {
			InputDigest: telemetryFingerprint,
			Package:     "github.com/acme/providers/telemetry",
			Kind:        "telemetry",
			Runtime:     "declarative",
			Source:      "https://example.com/telemetry/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	lock.Providers.Audit = map[string]operator.LockEntry{
		"ledger": {
			InputDigest: auditFingerprint,
			Package:     "github.com/acme/providers/audit",
			Kind:        "audit",
			Runtime:     "declarative",
			Source:      "https://example.com/audit/provider-release.yaml",
			Version:     "1.0.0",
		},
	}
	if err := operator.WriteLockfile(filepath.Join(dir, operator.LockfileName), lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	out = runGestaltd(t, "provider", "list", "--config", cfgPath)
	if got := providerLifecycleListStatus(t, out, "app", "alpha"); got != "unverified" {
		t.Fatalf("alpha status = %q, want unverified for unresolved package-source lock\n%s", got, out)
	}
	if got := providerLifecycleListStatus(t, out, "app", "bravo"); got != "locked" {
		t.Fatalf("bravo status = %q, want locked\n%s", got, out)
	}
	if got := providerLifecycleListStatus(t, out, "telemetry", "traces"); got != "unverified" {
		t.Fatalf("telemetry status = %q, want unverified\n%s", got, out)
	}
	if got := providerLifecycleListStatus(t, out, "audit", "ledger"); got != "locked" {
		t.Fatalf("audit status = %q, want locked\n%s", got, out)
	}

	writeProviderLifecycleTestFile(t, filepath.Join(dir, operator.LockfileName), "not json")
	out, err = runGestaltdResult("provider", "list", "--config", cfgPath)
	if err == nil {
		t.Fatalf("provider list with corrupt lockfile succeeded: %s", out)
	}
	assertContains(t, out, "parsing lockfile")
}

func TestE2EProviderRemoveUniqueAndAmbiguousKinds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v8
apps:
  alpha:
    source: env
providers:
  secrets:
    alpha:
      source: env
`)
	out, err := runGestaltdResult("provider", "remove", "alpha", "--config", cfgPath, "--no-lock")
	if err == nil {
		t.Fatalf("ambiguous provider remove succeeded: %s", out)
	}
	assertContains(t, out, "ambiguous")

	out = runGestaltd(t, "provider", "remove", "alpha", "--kind", "secrets", "--config", cfgPath, "--no-lock")
	assertContains(t, out, "Removed secrets alpha")
	cfg, err := config.LoadPartialAllowMissingEnvPaths([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadPartialAllowMissingEnvPaths: %v", err)
	}
	if _, ok := cfg.Providers.Secrets["alpha"]; ok {
		t.Fatal("secrets alpha still present after remove")
	}
	if _, ok := cfg.Apps["alpha"]; !ok {
		t.Fatal("apps alpha was removed unexpectedly")
	}
}

func TestE2EProviderUpgradeVersionRejectsRepeatedConfigAndAmbiguousKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	otherCfgPath := filepath.Join(dir, "other.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v8
apps:
  alpha:
    source:
      package: github.com/acme/providers/alpha
providers:
  secrets:
    alpha:
      source:
        package: github.com/acme/providers/alpha-secrets
`)
	writeProviderLifecycleTestFile(t, otherCfgPath, "apiVersion: gestaltd.config/v8\n")

	out, err := runGestaltdResult("provider", "upgrade", "alpha", "--version", "1.2.3", "--config", cfgPath, "--config", otherCfgPath)
	if err == nil {
		t.Fatalf("provider upgrade with repeated --config succeeded: %s", out)
	}
	assertContains(t, out, "only one --config")

	out, err = runGestaltdResult("provider", "upgrade", "alpha", "--version", "1.2.3", "--config", cfgPath)
	if err == nil {
		t.Fatalf("ambiguous provider upgrade succeeded: %s", out)
	}
	assertContains(t, out, "ambiguous")
}

func writeProviderLifecycleIndex(t *testing.T, dir string) string {
	t.Helper()
	indexPath := filepath.Join(dir, "provider-index.yaml")
	writeProviderLifecycleTestFile(t, indexPath, `schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/acme/providers/alpha:
    displayName: Alpha
    versions:
      1.2.3:
        metadata: https://example.com/provider-release.yaml
        kind: app
        runtime: executable
`)
	return (&url.URL{Scheme: "file", Path: indexPath}).String()
}

func writeProviderLifecycleRelease(t *testing.T, dir, pkg, version string) string {
	t.Helper()
	releaseDir := filepath.Join(dir, "releases", version)
	packageDir := filepath.Join(releaseDir, "package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(packageDir): %v", err)
	}
	executable := []byte("#!/bin/sh\nexit 0\n")
	executableName := "provider"
	executableSum := sha256.Sum256(executable)
	writeProviderLifecycleTestFile(t, filepath.Join(packageDir, executableName), string(executable))
	if err := os.Chmod(filepath.Join(packageDir, executableName), 0o755); err != nil {
		t.Fatalf("Chmod(provider): %v", err)
	}
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      pkg,
		Version:     version,
		DisplayName: "Alpha",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   executableName,
			SHA256: fmt.Sprintf("%x", executableSum[:]),
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: executableName},
	}
	manifestData, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeProviderLifecycleTestFile(t, filepath.Join(packageDir, "manifest.yaml"), string(manifestData))
	catalogData := []byte("name: alpha\noperations:\n  - id: echo\n    method: POST\n")
	writeProviderLifecycleTestFile(t, filepath.Join(packageDir, providerpkg.StaticCatalogFile), string(catalogData))

	archiveName := "alpha-" + version + ".tar.gz"
	archivePath := filepath.Join(releaseDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(packageDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	archiveDigest, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		t.Fatalf("ArchiveDigest: %v", err)
	}
	staticManifest, err := staticvalidation.ProjectManifest(manifest, "", true)
	if err != nil {
		t.Fatalf("ProjectManifest: %v", err)
	}
	var staticCatalog catalog.Catalog
	if err := yaml.Unmarshal(catalogData, &staticCatalog); err != nil {
		t.Fatalf("Unmarshal static catalog: %v", err)
	}
	metadata := providerrelease.Metadata{
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       pkg,
		Kind:          providermanifestv1.KindApp,
		Version:       version,
		Runtime:       providerrelease.RuntimeForManifest(providermanifestv1.KindApp, manifest),
		Artifacts: providerrelease.Artifacts{
			providerpkg.CurrentPlatformString(): {
				Path:   archiveName,
				SHA256: archiveDigest,
			},
		},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: staticManifest,
			Catalog:  &staticCatalog,
		},
	}
	if err := providerrelease.ValidateMetadata(&metadata); err != nil {
		t.Fatalf("ValidateMetadata: %v", err)
	}
	metadataData, err := yaml.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal release metadata: %v", err)
	}
	metadataRel := filepath.ToSlash(filepath.Join("releases", version, "provider-release.yaml"))
	metadataPath := filepath.Join(dir, filepath.FromSlash(metadataRel))
	writeProviderLifecycleTestFile(t, metadataPath, string(metadataData))
	return metadataRel
}

func writeProviderLifecycleTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runGestaltd(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runGestaltdResult(args...)
	if err != nil {
		t.Fatalf("gestaltd %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func runGestaltdResult(args ...string) (string, error) {
	cmd := gestaltdCommand(args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runGestaltdStreamsWithEnv(env []string, args ...string) (string, string, error) {
	cmd := gestaltdCommand(args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func assertProviderMutationStatusStderr(t *testing.T, stdout, stderr string, want ...string) {
	t.Helper()
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("provider mutation stdout = %q, want empty", stdout)
	}
	for _, part := range want {
		assertContains(t, stderr, part)
	}
	assertContains(t, stderr, "Running provider configuration and lock preflight")
	assertContains(t, stderr, "Provider configuration and lock preflight succeeded.")
	for _, leaked := range []string{"gestaltd-provider-preflight-", "gestaltd-lock-", "wrote lockfile"} {
		if strings.Contains(stderr, leaked) {
			t.Fatalf("provider mutation stderr leaked %q: %s", leaked, stderr)
		}
	}
}

func assertProviderLifecycleLockEntry(t *testing.T, lockPath, version, source string) {
	t.Helper()
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Providers.App["alpha"]
	if !ok {
		t.Fatal(`lock.Providers.App["alpha"] missing`)
	}
	if entry.Version != version {
		t.Fatalf("lock version = %q, want %q", entry.Version, version)
	}
	if entry.Source != source {
		t.Fatalf("lock source = %q, want %q", entry.Source, source)
	}
}

func providerLifecycleListStatus(t *testing.T, output, kind, name string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == kind && fields[1] == name {
			return fields[len(fields)-1]
		}
	}
	t.Fatalf("provider list row %s/%s not found in:\n%s", kind, name, output)
	return ""
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, got)
	}
}
