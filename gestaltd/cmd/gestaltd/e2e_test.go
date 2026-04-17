package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginstore"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestE2EValidateRejectsAuditConfigWhenProviderInheritsTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgText := strings.Replace(string(cfgBytes), "plugins:\n", `  audit:
    primary:
      config:
        format: json
plugins:
`, 1)
	cfgBytes = []byte(cfgText)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config audit: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
	}
	if !strings.Contains(string(out), "audit.config is not supported when audit.provider is") {
		t.Fatalf("expected inherit-provider audit config error, got: %s", out)
	}
}

func TestE2EValidateRejectsInvalidAuditSettings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		auditYAML string
		wantError string
	}{
		{
			name: "unknown audit provider",
			auditYAML: `  audit:
    primary:
      source: bogus
`,
			wantError: "unknown audit provider",
		},
		{
			name: "stdout audit requires mapping config",
			auditYAML: `  audit:
    primary:
      source: stdout
      config: nope
`,
			wantError: "stdout audit: parsing config",
		},
		{
			name: "otlp audit rejects non-otlp logs exporter",
			auditYAML: `  audit:
    primary:
      source: otlp
      config:
        logs:
          exporter: stdout
`,
			wantError: "otlp audit: logs.exporter must be",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pluginDir := setupPluginDir(t, dir)

			cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
			cfgBytes, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			cfgText := strings.Replace(string(cfgBytes), "plugins:\n", tc.auditYAML+"plugins:\n", 1)
			cfgBytes = []byte(cfgText)
			if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
				t.Fatalf("write config audit: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EValidateRejectsUnknownYAMLField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "bogus field",
			pluginYAML: `source:
  path: /tmp/manifest.yaml
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `source:
  path: /tmp/manifest.yaml
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `source:
  path: /tmp/manifest.yaml
params:
  tenant:
    required: true`,
			wantError: "params",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := "server:\n  encryptionKey: test-key\n" + authIndexedDBConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`plugins:
    example:
      %s
`, strings.ReplaceAll(tc.pluginYAML, "\n", "\n      "))
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unknown field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) || !strings.Contains(string(out), "parsing config YAML") {
				t.Fatalf("expected output to mention %q and YAML parsing, got: %s", tc.wantError, out)
			}
		})

	}
}

func TestE2EValidateRejectsUnknownYAMLFieldInOverriddenAwayBranch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		baseServerExtra string
		overrideCfg     string
	}{
		{
			name: "masked branch still fails",
			overrideCfg: `plugins:
  example: null
`,
		},
		{
			name:            "masked branch still fails when base has unrelated missing env fixed later",
			baseServerExtra: "  baseUrl: ${GESTALT_TEST_BASE_URL}\n",
			overrideCfg: `server:
  baseUrl: https://example.test
plugins:
  example: null
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			basePath := filepath.Join(dir, "base.yaml")
			overridePath := filepath.Join(dir, "override.yaml")
			baseCfg := "server:\n  encryptionKey: test-key\n" + tc.baseServerExtra + authIndexedDBConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + `plugins:
  example:
    source:
      path: /tmp/manifest.yaml
    dispalyName: Example
`
			if err := os.WriteFile(basePath, []byte(baseCfg), 0o644); err != nil {
				t.Fatalf("WriteFile base config: %v", err)
			}
			if err := os.WriteFile(overridePath, []byte(tc.overrideCfg), 0o644); err != nil {
				t.Fatalf("WriteFile override config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", basePath, "--config", overridePath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for masked unknown field, output: %s", out)
			}
			if !strings.Contains(string(out), "dispalyName") || !strings.Contains(string(out), "parsing config YAML") {
				t.Fatalf("expected output to mention dispalyName and YAML parsing, got: %s", out)
			}
		})
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`{{{invalid yaml`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for malformed YAML, output: %s", out)
	}
	if !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention YAML parsing failure, got: %s", out)
	}
}

func TestE2EValidateRejectsLegacyConfigSecretSyntax(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `server:
  encryptionKey: secret://enc-key
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for legacy secret syntax, output: %s", out)
	}
	if !strings.Contains(string(out), "legacy secret:// syntax") {
		t.Fatalf("expected output to mention legacy secret syntax, got: %s", out)
	}
}

func TestE2EValidateRejectsMalformedStructuredSecretRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		cfg       string
		wantError string
	}{
		{
			name: "missing provider",
			cfg: `server:
  encryptionKey:
    secret:
      name: enc-key
`,
			wantError: "secret.provider is required",
		},
		{
			name: "extra key",
			cfg: `server:
  encryptionKey:
    secret:
      provider: env
      name: enc-key
      from: somewhere
`,
			wantError: "secret.from is not supported",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for malformed structured secret ref, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected output to mention %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EValidateAcceptsLayeredConfigs(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	baseDir := filepath.Join(rootDir, "base")
	overrideDir := filepath.Join(rootDir, "overrides")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("MkdirAll base: %v", err)
	}
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll overrides: %v", err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, rootDir))
	authManifest := componentProviderManifestPath(t, setupAuthProviderDir(t, rootDir, "local"))

	baseConfigPath := filepath.Join(baseDir, "base.yaml")
	baseConfig := fmt.Sprintf(`server:
  encryptionKey: test-key
  providers:
    indexeddb: sqlite
    auth: local
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
  auth:
    local:
      source:
        path: ./missing/manifest.yaml
`, indexedDBManifest, filepath.Join(rootDir, "gestalt.db"))
	if err := os.WriteFile(baseConfigPath, []byte(baseConfig), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	authRel, err := filepath.Rel(overrideDir, authManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(auth): %v", err)
	}
	overrideConfigPath := filepath.Join(overrideDir, "local.yaml")
	overrideConfig := fmt.Sprintf(`providers:
  auth:
    local:
      source:
        path: %s
`, filepath.ToSlash(authRel))
	if err := os.WriteFile(overrideConfigPath, []byte(overrideConfig), 0o644); err != nil {
		t.Fatalf("WriteFile override config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", baseConfigPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected base config validate to fail, output: %s", out)
	}
	if !strings.Contains(string(out), "missing/manifest.yaml") {
		t.Fatalf("expected base config failure to mention missing manifest, got: %s", out)
	}

	out, err = exec.Command(gestaltdBin, "validate", "--config", baseConfigPath, "--config", overrideConfigPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected layered config validate to succeed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected layered config output to mention success, got: %s", out)
	}
}

func setupPluginDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupPluginDirWithVersion(t, baseDir, "0.0.1-alpha.1")
}

func setupPluginDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "plugin-src")
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Spec:        &providermanifestv1.Spec{},
	}
	writeManifestFile(t, pluginDir, manifest)
	return pluginDir
}

func setupAuthProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "auth", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/auth/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "auth.go", []byte(authProviderSource(name)), 0o644)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth-provider"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, providermanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindAuth,
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupCacheProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "cache", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/cache/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "cache.go", []byte(testutil.GeneratedCachePackageSource()), 0o644)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "cache-provider"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, providermanifestv1.KindCache, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindCache,
		Source:      "github.com/test/providers/cache/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Cache " + name,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func authProviderSource(name string) string {
	source := testutil.GeneratedAuthPackageSource()
	displayName := name
	if name != "" {
		displayName = strings.ToUpper(name[:1]) + name[1:]
	}
	source = strings.Replace(source, `Name:        "generated-auth"`, fmt.Sprintf(`Name:        %q`, name), 1)
	source = strings.Replace(source, `DisplayName: "Generated Auth"`, fmt.Sprintf(`DisplayName: %q`, displayName), 1)
	return source
}

func componentProviderManifestPath(t *testing.T, providerDir string) string {
	t.Helper()

	manifestPath, err := providerpkg.FindManifestFile(providerDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", providerDir, err)
	}
	return manifestPath
}

func authIndexedDBConfigYAML(t *testing.T, dir, authName, datastoreName, dbPath string) string {
	t.Helper()

	authBlock := ""
	serverProvidersBlock := fmt.Sprintf(`  providers:
    indexeddb: %s
`, datastoreName)
	if authName != "" {
		authManifestPath := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, authName))
		serverProvidersBlock += fmt.Sprintf("    auth: %s\n", authName)
		authBlock = fmt.Sprintf(`  auth:
    %s:
      source:
        path: %s
`, authName, authManifestPath)
	}
	return fmt.Sprintf(`%s
providers:
%s  indexeddb:
    %s:
      source:
        ref: github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb
        version: 0.0.1-alpha.1
      config:
        dsn: %q
`, serverProvidersBlock, authBlock, datastoreName, "sqlite://"+dbPath)
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	data, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func reservePort(t *testing.T) (int, net.Listener) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	return l.Addr().(*net.TCPAddr).Port, l
}

func setupIndexedDBProviderDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "indexeddb-provider")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, filepath.Base(indexedDBBin))
	data, err := os.ReadFile(indexedDBBin)
	if err != nil {
		t.Fatalf("read indexeddb binary: %v", err)
	}
	if err := os.WriteFile(binDest, data, 0o755); err != nil {
		t.Fatalf("write indexeddb binary: %v", err)
	}

	artifactRel := filepath.Base(binDest)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      "github.com/test/providers/indexeddb-inmem",
		Version:     "0.0.1-alpha.1",
		DisplayName: "In-Memory IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupPrebuiltPluginDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "plugin-prebuilt")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, "gestalt-plugin-example")
	binData, err := os.ReadFile(pluginBin)
	if err != nil {
		t.Fatalf("read plugin binary: %v", err)
	}
	if err := os.WriteFile(binDest, binData, 0o755); err != nil {
		t.Fatalf("write plugin binary: %v", err)
	}

	srcDir := testutil.MustExampleProviderPluginPath()
	catalogData, err := os.ReadFile(filepath.Join(srcDir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("read catalog.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "catalog.yaml"), catalogData, 0o644); err != nil {
		t.Fatalf("write catalog.yaml: %v", err)
	}

	_, srcManifest, err := providerpkg.ReadSourceManifestFile(filepath.Join(srcDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	artifactRel := filepath.Base(binDest)
	sum := sha256.Sum256(binData)
	srcManifest.Source = "github.com/test/plugins/provider"
	srcManifest.Version = "0.0.1-alpha.1"
	srcManifest.Artifacts = []providermanifestv1.Artifact{
		{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel, SHA256: hex.EncodeToString(sum[:])},
	}
	srcManifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactRel}
	writeManifestFile(t, providerDir, srcManifest)
	return providerDir
}

type mountedUITestConfig struct {
	Name         string
	Path         string
	ManifestPath string
}

func setupMountedWebUIDir(t *testing.T, baseDir string) *mountedUITestConfig {
	t.Helper()

	uiDir := filepath.Join(baseDir, "mounted-webui")
	distDir := filepath.Join(uiDir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", assetsDir, err)
	}

	writeTestFile(t, uiDir, filepath.Join("dist", "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Roadmap Review UI</title>
  </head>
  <body>
    <div id="app">Roadmap Review UI</div>
    <script type="module" src="assets/app.js"></script>
  </body>
</html>
`), 0o644)
	writeTestFile(t, uiDir, filepath.Join("dist", "assets", "app.js"), []byte(`window.__ROADMAP_REVIEW_UI__ = "ready";
`), 0o644)
	writeManifestFile(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      "github.com/test/webui/roadmap-review",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap Review UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	})

	return &mountedUITestConfig{
		Name:         "roadmap_review",
		Path:         "/create-customer-roadmap-review",
		ManifestPath: filepath.Join(uiDir, "manifest.yaml"),
	}
}

func writeServeConfig(t *testing.T, dir string, port int, mountedUI *mountedUITestConfig) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	pluginDir := setupPrebuiltPluginDir(t, dir)
	pluginManifest, err := providerpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}
	uiBlock := ""
	if mountedUI != nil {
		uiBlock = fmt.Sprintf(`  ui:
    %s:
      source:
        path: %q
      path: %s
`, mountedUI.Name, mountedUI.ManifestPath, mountedUI.Path)
	}

	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
%splugins:
  example:
    source:
      path: %s
`, port, indexedDBManifest, uiBlock, pluginManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeManagedSourceServeConfig(t *testing.T, dir string, port int) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
plugins:
  example:
    source:
      ref: github.com/test/plugins/provider
      version: 0.0.1-alpha.1
`, port, indexedDBManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func startGestaltdWithConfigs(t *testing.T, cfgPaths []string, locked bool) string {
	t.Helper()
	if len(cfgPaths) == 0 {
		t.Fatal("startGestaltdWithConfigs requires at least one config path")
	}

	port, holder := reservePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	primaryCfgPath := cfgPaths[0]
	cfgBytes, err := os.ReadFile(primaryCfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := strings.Replace(string(cfgBytes), "port: 0", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(primaryCfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_ = holder.Close()
	args := []string{"serve"}
	if locked {
		args = append(args, "--locked")
	}
	for _, cfgPath := range cfgPaths {
		args = append(args, "--config", cfgPath)
	}
	cmd := exec.Command(gestaltdBin, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(60 * time.Second)
	ready := false
	for !ready {
		select {
		case <-exited:
			t.Fatal("gestaltd exited before becoming ready")
		case <-timeout:
			t.Fatal("gestaltd did not become ready within 60 seconds")
		case <-tick.C:
			resp, err := client.Get(baseURL + "/ready")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					ready = true
				}
			}
		}
	}

	return baseURL
}

func startGestaltdWithConfig(t *testing.T, cfgPath string) string {
	t.Helper()
	return startGestaltdWithConfigs(t, []string{cfgPath}, false)
}

func startGestaltd(t *testing.T, dir string, mountedUI *mountedUITestConfig) string {
	t.Helper()
	return startGestaltdWithConfig(t, writeServeConfig(t, dir, 0, mountedUI))
}

func TestE2EServeAndHealthCheck(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E serve test in short mode")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir, nil)

	client := &http.Client{Timeout: 2 * time.Second}
	intResp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = intResp.Body.Close() }()
	body, _ := io.ReadAll(intResp.Body)
	if intResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", intResp.StatusCode, body)
	}

	var integrations []json.RawMessage
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decode integrations response: %v (body: %s)", err, body)
	}
	if len(integrations) == 0 {
		t.Fatal("expected at least one integration from the example plugin")
	}
}

func TestE2EAdminUI(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E admin UI test in short mode")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir, nil)
	client := &http.Client{Timeout: 2 * time.Second}

	t.Run("admin page serves embedded HTML", func(t *testing.T) {
		t.Parallel()

		resp, err := client.Get(baseURL + "/admin/")
		if err != nil {
			t.Fatalf("GET /admin/: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /admin/ 200, got %d", resp.StatusCode)
		}
		html := string(body)
		if !strings.Contains(html, "Prometheus metrics") {
			t.Fatal("expected admin page to contain 'Prometheus metrics'")
		}
		if !strings.Contains(html, "theme.css") {
			t.Fatal("expected admin page to reference theme.css")
		}
	})

	t.Run("admin theme CSS is served", func(t *testing.T) {
		t.Parallel()

		resp, err := client.Get(baseURL + "/admin/theme.css")
		if err != nil {
			t.Fatalf("GET /admin/theme.css: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /admin/theme.css 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "--background") {
			t.Fatal("expected theme.css to contain CSS custom properties")
		}
	})

	t.Run("metrics endpoint serves prometheus format", func(t *testing.T) {
		t.Parallel()

		resp, err := client.Get(baseURL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /metrics 200, got %d", resp.StatusCode)
		}
		content := string(body)
		if !strings.Contains(content, "# TYPE") {
			t.Fatal("expected /metrics to contain '# TYPE' marker")
		}
		if !strings.Contains(content, "# HELP") {
			t.Fatal("expected /metrics to contain '# HELP' marker")
		}
	})

	t.Run("admin redirect adds trailing slash", func(t *testing.T) {
		t.Parallel()

		noRedirect := &http.Client{
			Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := noRedirect.Get(baseURL + "/admin")
		if err != nil {
			t.Fatalf("GET /admin: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusPermanentRedirect {
			t.Fatalf("expected /admin to redirect, got %d", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasSuffix(loc, "/admin/") {
			t.Fatalf("expected redirect to /admin/, got Location: %s", loc)
		}
	})
}

func TestE2EServeStartsWithPluginBoundCacheProvider(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E cache serve test in short mode")
	}

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	cacheManifest := componentProviderManifestPath(t, setupCacheProviderDir(t, dir, "session"))
	pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))
	cfgPath := filepath.Join(dir, "config-cache.yaml")

	cfg := fmt.Sprintf(`server:
  public:
    port: 0
  encryptionKey: test-cache-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
  cache:
    session:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
    cache:
      - session
`, indexedDBManifest, cacheManifest, pluginManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	baseURL := startGestaltdWithConfig(t, cfgPath)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2EInitRejectsLocalPlugins(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E init test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("gestaltd init unexpectedly succeeded\noutput: %s", out)
	}
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	if !strings.Contains(string(out), "local plugin and UI sources are not supported during init") {
		t.Fatalf("gestaltd init output = %s, want local source rejection", out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func TestE2EValidateAllowsLocalPlugins(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping local plugin validate E2E test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("gestaltd validate output = %s, want config ok", out)
	}
}

func TestE2EInitAndServeLayeredConfigs(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping layered config E2E test in short mode")
	}

	dir := t.TempDir()
	basePath, overridePath, lockPath, _ := writeLayeredE2EConfigs(t, dir, 0)

	out, err := exec.Command(gestaltdBin, "init", "--config", basePath, "--config", overridePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init with layered configs failed: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file at %s: %v", lockPath, err)
	}

	baseURL := startGestaltdWithConfigs(t, []string{basePath, overridePath}, true)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected /api/v1/integrations 401 with layered auth override, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2EServeLockedRejectsGenericArchiveForExecutablePlugin(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping locked generic archive E2E test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeManagedSourceServeConfig(t, dir, 0)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", cfgPath, err)
	}

	fingerprint, err := operator.ProviderFingerprint("example", cfg.Plugins["example"], filepath.Dir(cfgPath))
	if err != nil {
		t.Fatalf("ProviderFingerprint(example): %v", err)
	}

	destDir := filepath.Join(dir, filepath.FromSlash(operator.PreparedProvidersDir), "example")
	installed, err := pluginstore.InstallFromDir(setupPrebuiltPluginDir(t, dir), destDir)
	if err != nil {
		t.Fatalf("Install(prebuilt provider): %v", err)
	}

	manifestRel, err := filepath.Rel(dir, installed.ManifestPath)
	if err != nil {
		t.Fatalf("filepath.Rel(manifest): %v", err)
	}
	executableRel, err := filepath.Rel(dir, installed.ExecutablePath)
	if err != nil {
		t.Fatalf("filepath.Rel(executable): %v", err)
	}

	lockPath := filepath.Join(dir, "gestalt.lock.json")
	lock := &operator.Lockfile{
		Providers: map[string]operator.LockProviderEntry{
			"example": {
				Fingerprint: fingerprint,
				Source:      cfg.Plugins["example"].SourceRef(),
				Version:     cfg.Plugins["example"].SourceVersion(),
				Archives: map[string]operator.LockArchive{
					"generic": {
						URL: "https://example.test/gestalt-plugin-provider_v0.0.1-alpha.1.tar.gz",
					},
				},
				Manifest:   filepath.ToSlash(manifestRel),
				Executable: filepath.ToSlash(executableRel),
			},
		},
	}
	if err := operator.WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile(%s): %v", lockPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, gestaltdBin, "serve", "--locked", "--config", cfgPath)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("gestaltd serve --locked unexpectedly stayed up after forcing a generic archive\noutput: %s", out)
	}
	if err == nil {
		t.Fatalf("expected gestaltd serve --locked to fail, output: %s", out)
	}
	if !strings.Contains(string(out), "generic release archives are not allowed") {
		t.Fatalf("expected generic-archive policy error, got: %s", out)
	}
}

func TestE2ECLIToServer(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E CLI-to-server test in short mode")
	}
	if gestaltCLIBin == "" {
		t.Skip("gestalt CLI binary not available (cargo not installed or build failed)")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir, nil)

	cliEnv := append(os.Environ(), "GESTALT_URL="+baseURL, "GESTALT_API_KEY=e2e-test-key")

	t.Run("integrations list", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command(gestaltCLIBin, "integrations", "list", "--format", "json", "--url", baseURL)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt integrations list failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "example") {
			t.Fatalf("expected 'example' integration in output, got: %s", out)
		}
	})

	t.Run("invoke echo operation", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command(gestaltCLIBin, "invoke", "example", "echo",
			"--format", "json",
			"--url", baseURL,
			"-p", "message=hello-e2e",
		)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt invoke failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "hello-e2e") {
			t.Fatalf("expected echo response to contain 'hello-e2e', got: %s", out)
		}
	})

	t.Run("describe operation", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command(gestaltCLIBin, "describe", "example", "echo",
			"--format", "json",
			"--url", baseURL,
		)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt describe failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "message") {
			t.Fatalf("expected 'message' parameter in describe output, got: %s", out)
		}
	})
}

func TestE2EMountedWebUI(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping mounted web UI E2E test in short mode")
	}

	dir := t.TempDir()
	mountedUI := setupMountedWebUIDir(t, dir)
	baseURL := startGestaltd(t, dir, mountedUI)

	noRedirect := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	client := &http.Client{Timeout: 2 * time.Second}

	t.Run("mounted UI path redirects to trailing slash", func(t *testing.T) {
		t.Parallel()

		resp, err := noRedirect.Get(baseURL + mountedUI.Path)
		if err != nil {
			t.Fatalf("GET %s: %v", mountedUI.Path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusPermanentRedirect {
			t.Fatalf("expected mounted UI redirect, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); got != mountedUI.Path+"/" {
			t.Fatalf("Location = %q, want %q", got, mountedUI.Path+"/")
		}
	})

	t.Run("mounted UI SPA shell serves under nested routes", func(t *testing.T) {
		t.Parallel()

		resp, err := client.Get(baseURL + mountedUI.Path + "/sync")
		if err != nil {
			t.Fatalf("GET %s/sync: %v", mountedUI.Path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected mounted UI shell 200, got %d", resp.StatusCode)
		}
		content := string(body)
		if !strings.Contains(content, "Roadmap Review UI") {
			t.Fatal("expected mounted UI shell marker in HTML")
		}
		if !strings.Contains(content, "assets/app.js") {
			t.Fatal("expected mounted UI shell to reference assets/app.js")
		}
	})

	t.Run("mounted UI assets are served from the prefixed path", func(t *testing.T) {
		t.Parallel()

		resp, err := client.Get(baseURL + mountedUI.Path + "/assets/app.js")
		if err != nil {
			t.Fatalf("GET %s/assets/app.js: %v", mountedUI.Path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected mounted UI asset 200, got %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "__ROADMAP_REVIEW_UI__") {
			t.Fatal("expected mounted UI asset marker in JS")
		}
	})
}

func writeE2EConfig(t *testing.T, dir, pluginDir string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, pluginDir, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeLayeredE2EConfigs(t *testing.T, dir string, port int) (string, string, string, string) {
	t.Helper()

	deployDir := filepath.Join(dir, "deploy")
	overrideDir := filepath.Join(deployDir, "overrides")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", overrideDir, err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	authManifest := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, "local"))

	indexedDBRel, err := filepath.Rel(deployDir, indexedDBManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(indexeddb): %v", err)
	}
	authRel, err := filepath.Rel(overrideDir, authManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(auth): %v", err)
	}

	basePath := filepath.Join(deployDir, "base.yaml")
	overridePath := filepath.Join(overrideDir, "local.yaml")
	baseCfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-layered-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
`, port, filepath.ToSlash(indexedDBRel))
	overrideCfg := fmt.Sprintf(`server:
  providers:
    auth: local
  artifactsDir: ../artifacts/local
providers:
  auth:
    local:
      source:
        path: %s
`, filepath.ToSlash(authRel))

	if err := os.WriteFile(basePath, []byte(baseCfg), 0o644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(overrideCfg), 0o644); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	return basePath, overridePath, filepath.Join(deployDir, "gestalt.lock.json"), filepath.Join(deployDir, "artifacts", "local")
}

func writeE2EConfigWithPaths(t *testing.T, dir, pluginDir, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}
	manifestPath, err := providerpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	serverBlock := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
`, port)
	if artifactsDir != "" {
		serverBlock += fmt.Sprintf("  artifactsDir: %s\n", artifactsDir)
	}
	cfg := serverBlock + authIndexedDBConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`plugins:
    example:
      source:
        path: %s
`, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
