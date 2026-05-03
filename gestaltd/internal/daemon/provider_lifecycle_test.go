package daemon

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"gopkg.in/yaml.v3"
)

func TestE2EProviderAddDefaultsToPackageSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v5\nplugins:\n")
	indexURL := writeProviderLifecycleIndex(t, dir)

	runGestaltd(t, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	out := runGestaltd(t, "provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--no-lock")
	assertContains(t, out, "Added plugin alpha")
	assertContains(t, out, "Version: 1.2.3")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.APIVersion; got != config.ConfigAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", got, config.ConfigAPIVersion)
	}
	entry := cfg.Plugins["alpha"]
	if entry == nil {
		t.Fatal(`Plugins["alpha"] = nil`)
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
        kind: plugin
        runtime: executable
      1.3.0:
        metadata: %s
        kind: plugin
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
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v5\nplugins:\n")
	env := []string{"XDG_CONFIG_HOME=" + xdgConfigHome}

	out := runGestaltdWithEnv(t, env, "provider", "add", pkg, "--config", cfgPath, "--repo", "private", "--name", "alpha", "--version", "1.2.3", "--lockfile", lockPath)
	assertContains(t, out, "Added plugin alpha")
	assertContains(t, out, "Lockfile: "+lockPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if got := cfg.ProviderRepositories["private"].URL; got != server.URL+"/provider-index.yaml" {
		t.Fatalf("project repo URL = %q", got)
	}
	if got := cfg.Plugins["alpha"].Source.PackageRepo(); got != "private" {
		t.Fatalf("package repo = %q, want private", got)
	}
	assertProviderLifecycleLockEntry(t, lockPath, "1.2.3", server.URL+"/"+metadata123)

	out = runGestaltdWithEnv(t, env, "provider", "upgrade", "alpha", "--version", "1.3.0", "--config", cfgPath, "--lockfile", lockPath)
	assertContains(t, out, "Updated plugin alpha version constraint to 1.3.0")
	assertContains(t, out, "Lockfile: "+lockPath)
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after upgrade: %v", err)
	}
	if got := cfg.Plugins["alpha"].Source.PackageVersionConstraint(); got != "1.3.0" {
		t.Fatalf("version constraint = %q, want 1.3.0", got)
	}
	assertProviderLifecycleLockEntry(t, lockPath, "1.3.0", server.URL+"/"+metadata130)
	if got := authorizedIndexRequests.Load(); got < 3 {
		t.Fatalf("authorized index requests = %d, want at least 3", got)
	}
}

func TestE2EProviderAddExactSourceAndRejectsRepeatedConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	otherCfgPath := filepath.Join(dir, "other.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v5\nplugins:\n")
	writeProviderLifecycleTestFile(t, otherCfgPath, "apiVersion: gestaltd.config/v5\n")
	indexURL := writeProviderLifecycleIndex(t, dir)

	runGestaltd(t, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	out := runGestaltd(t, "provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--exact-source", "--no-lock")
	assertContains(t, out, "Added plugin alpha")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Plugins["alpha"].Source.MetadataURL(); got != "https://example.com/provider-release.yaml" {
		t.Fatalf("MetadataURL = %q", got)
	}

	out, err = runGestaltdResult("provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--config", otherCfgPath, "--repo", "local", "--name", "beta", "--no-lock")
	if err == nil {
		t.Fatalf("provider add with repeated --config succeeded: %s", out)
	}
	assertContains(t, out, "only one --config")
}

func TestE2EProviderAddRejectsExistingName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, "apiVersion: gestaltd.config/v5\nplugins:\n  alpha:\n    source: env\n")
	indexURL := writeProviderLifecycleIndex(t, dir)

	runGestaltd(t, "provider", "repo", "add", "local", indexURL, "--config", cfgPath)
	out, err := runGestaltdResult("provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--no-lock")
	if err == nil {
		t.Fatalf("provider add duplicate succeeded: %s", out)
	}
	assertContains(t, out, "already exists")
}

func TestE2EProviderListOfflineAndLockStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v5
providerRepositories:
  unreachable:
    url: http://127.0.0.1:1/provider-index.yaml
plugins:
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
	if got := providerLifecycleListStatus(t, out, "plugin", "alpha"); got != "unlocked" {
		t.Fatalf("alpha status = %q, want unlocked\n%s", got, out)
	}
	assertContains(t, out, "builtin")

	cfg, err := config.LoadPartialAllowMissingEnvPaths([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadPartialAllowMissingEnvPaths: %v", err)
	}
	fingerprint, err := operator.ProviderFingerprint("alpha", cfg.Plugins["alpha"], dir)
	if err != nil {
		t.Fatalf("ProviderFingerprint: %v", err)
	}
	bravoFingerprint, err := operator.ProviderFingerprint("bravo", cfg.Plugins["bravo"], dir)
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
	if err := operator.WriteLockfile(filepath.Join(dir, operator.LockfileName), &operator.Lockfile{
		Providers: map[string]operator.LockEntry{
			"alpha": {
				Fingerprint: fingerprint,
				Package:     "github.com/acme/providers/alpha",
				Kind:        "plugin",
				Runtime:     "executable",
				Version:     "1.2.3",
			},
			"bravo": {
				Fingerprint: bravoFingerprint,
				Package:     "github.com/acme/providers/bravo",
				Kind:        "plugin",
				Runtime:     "executable",
				Source:      "https://example.com/bravo/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
		Telemetry: map[string]operator.LockEntry{
			"traces": {
				Fingerprint: telemetryFingerprint,
				Package:     "github.com/acme/providers/telemetry",
				Kind:        "telemetry",
				Runtime:     "declarative",
				Source:      "https://example.com/telemetry/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
		Audit: map[string]operator.LockEntry{
			"ledger": {
				Fingerprint: auditFingerprint,
				Package:     "github.com/acme/providers/audit",
				Kind:        "audit",
				Runtime:     "declarative",
				Source:      "https://example.com/audit/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	out = runGestaltd(t, "provider", "list", "--config", cfgPath)
	assertContains(t, out, "1.2.3")
	if got := providerLifecycleListStatus(t, out, "plugin", "alpha"); got != "drifted" {
		t.Fatalf("alpha status = %q, want drifted when package lock source is missing\n%s", got, out)
	}

	if err := operator.WriteLockfile(filepath.Join(dir, operator.LockfileName), &operator.Lockfile{
		Providers: map[string]operator.LockEntry{
			"alpha": {
				Fingerprint: fingerprint,
				Package:     "github.com/acme/providers/alpha",
				Kind:        "plugin",
				Runtime:     "executable",
				Source:      "https://example.com/provider-release.yaml",
				Version:     "1.2.3",
			},
			"bravo": {
				Fingerprint: bravoFingerprint,
				Package:     "github.com/acme/providers/bravo",
				Kind:        "plugin",
				Runtime:     "executable",
				Source:      "https://example.com/bravo/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
		Telemetry: map[string]operator.LockEntry{
			"traces": {
				Fingerprint: telemetryFingerprint,
				Package:     "github.com/acme/providers/telemetry",
				Kind:        "telemetry",
				Runtime:     "declarative",
				Source:      "https://example.com/telemetry/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
		Audit: map[string]operator.LockEntry{
			"ledger": {
				Fingerprint: auditFingerprint,
				Package:     "github.com/acme/providers/audit",
				Kind:        "audit",
				Runtime:     "declarative",
				Source:      "https://example.com/audit/provider-release.yaml",
				Version:     "1.0.0",
			},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	out = runGestaltd(t, "provider", "list", "--config", cfgPath)
	if got := providerLifecycleListStatus(t, out, "plugin", "alpha"); got != "unverified" {
		t.Fatalf("alpha status = %q, want unverified for unresolved package-source lock\n%s", got, out)
	}
	if got := providerLifecycleListStatus(t, out, "plugin", "bravo"); got != "locked" {
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
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v5
plugins:
  alpha:
    source: env
providers:
  ui:
    alpha:
      source: ./ui-manifest.yaml
      path: /
  secrets:
    secretstore:
      source: env
`)
	out, err := runGestaltdResult("provider", "remove", "alpha", "--config", cfgPath, "--no-lock")
	if err == nil {
		t.Fatalf("ambiguous provider remove succeeded: %s", out)
	}
	assertContains(t, out, "ambiguous")

	out = runGestaltd(t, "provider", "remove", "secretstore", "--config", cfgPath, "--no-lock")
	assertContains(t, out, "Removed secrets secretstore")
	cfg, err := config.LoadPartialAllowMissingEnvPaths([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadPartialAllowMissingEnvPaths: %v", err)
	}
	if _, ok := cfg.Providers.Secrets["secretstore"]; ok {
		t.Fatal("secretstore still present after remove")
	}
}

func TestE2EProviderUpgradeVersionRejectsRepeatedConfigAndAmbiguousKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	otherCfgPath := filepath.Join(dir, "other.yaml")
	writeProviderLifecycleTestFile(t, cfgPath, `apiVersion: gestaltd.config/v5
plugins:
  alpha:
    source:
      package: github.com/acme/providers/alpha
providers:
  ui:
    alpha:
      source:
        package: github.com/acme/providers/alpha-ui
      path: /
`)
	writeProviderLifecycleTestFile(t, otherCfgPath, "apiVersion: gestaltd.config/v5\n")

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
        kind: plugin
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
		Kind:        providermanifestv1.KindPlugin,
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
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	writeProviderLifecycleTestFile(t, filepath.Join(packageDir, "manifest.yaml"), string(manifestData))
	writeProviderLifecycleTestFile(t, filepath.Join(packageDir, providerpkg.StaticCatalogFile), "name: alpha\noperations:\n  - id: echo\n    method: POST\n")

	archiveName := "alpha-" + version + ".tar.gz"
	archivePath := filepath.Join(releaseDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(packageDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	archiveDigest, err := providerpkg.ArchiveDigest(archivePath)
	if err != nil {
		t.Fatalf("ArchiveDigest: %v", err)
	}
	metadata := map[string]any{
		"schema":        "gestaltd-provider-release",
		"schemaVersion": 1,
		"package":       pkg,
		"kind":          providermanifestv1.KindPlugin,
		"version":       version,
		"runtime":       "executable",
		"artifacts": map[string]any{
			providerpkg.CurrentPlatformString(): map[string]any{
				"path":   archiveName,
				"sha256": archiveDigest,
			},
		},
	}
	metadataData, err := yaml.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal release metadata: %v", err)
	}
	metadataRel := filepath.ToSlash(filepath.Join("releases", version, "provider-release.yaml"))
	writeProviderLifecycleTestFile(t, filepath.Join(dir, filepath.FromSlash(metadataRel)), string(metadataData))
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

func runGestaltdWithEnv(t *testing.T, env []string, args ...string) string {
	t.Helper()
	out, err := runGestaltdResultWithEnv(env, args...)
	if err != nil {
		t.Fatalf("gestaltd %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func runGestaltdResult(args ...string) (string, error) {
	return runGestaltdResultWithEnv(nil, args...)
}

func runGestaltdResultWithEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command(gestaltdBin, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertProviderLifecycleLockEntry(t *testing.T, lockPath, version, source string) {
	t.Helper()
	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	entry, ok := lock.Providers["alpha"]
	if !ok {
		t.Fatal(`lock.Providers["alpha"] missing`)
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
