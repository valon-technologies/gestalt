package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestValidateRejectsPluginConfigForRuntimePlugins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
runtimes:
  worker:
    providers: []
    plugin:
      command: /tmp/runtime-plugin
      config:
        poll_interval: 30s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	err := validateConfig(cfgPath)
	if err == nil {
		t.Fatal("expected validateConfig to reject runtime plugin.config")
	}
	if !strings.Contains(err.Error(), "plugin.config") {
		t.Fatalf("expected plugin.config guidance, got: %v", err)
	}
}

func TestPreparedLockfileRoundTripPreservesPluginsSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, initLockfileName)
	want := &initLockfile{
		Version: lockVersion,
		Providers: map[string]lockProviderEntry{
			"restapi": {
				Fingerprint: "provider-fingerprint",
				Provider:    ".gestalt/providers/restapi.json",
			},
		},
		Plugins: map[string]lockPluginEntry{
			"integration:external": {
				Fingerprint: "plugin-fingerprint",
				Package:     "./plugins/dummy.tar.gz",
				Manifest:    ".gestalt/plugins/acme/provider/0.1.0/plugin.json",
				Executable:  ".gestalt/plugins/acme/provider/0.1.0/artifacts/linux/amd64/provider",
			},
		},
	}

	if err := writeLockfile(lockPath, want); err != nil {
		t.Fatalf("writeLockfile: %v", err)
	}

	got, err := readLockfile(lockPath)
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	if got.Version != lockVersion {
		t.Fatalf("lockfile version = %d, want %d", got.Version, lockVersion)
	}
	if got.Providers["restapi"].Fingerprint != want.Providers["restapi"].Fingerprint {
		t.Fatalf("provider fingerprint = %q, want %q", got.Providers["restapi"].Fingerprint, want.Providers["restapi"].Fingerprint)
	}
	key := "integration:external"
	if got.Plugins[key].Package != want.Plugins[key].Package {
		t.Fatalf("plugin package = %q, want %q", got.Plugins[key].Package, want.Plugins[key].Package)
	}
	if got.Plugins[key].Manifest != want.Plugins[key].Manifest {
		t.Fatalf("plugin manifest = %q, want %q", got.Plugins[key].Manifest, want.Plugins[key].Manifest)
	}
}

func TestPluginFingerprintStable(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Command: "/tmp/plugin",
		Args:    []string{"--verbose"},
		Env:     map[string]string{"API_KEY": "abc123"},
	}

	first, err := pluginFingerprint("external", plugin, nil)
	if err != nil {
		t.Fatalf("pluginFingerprint: %v", err)
	}
	second, err := pluginFingerprint("external", plugin, nil)
	if err != nil {
		t.Fatalf("pluginFingerprint second: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint changed between identical inputs: %q != %q", first, second)
	}

	plugin.Package = "./plugins/dummy.tar.gz"
	third, err := pluginFingerprint("external", plugin, nil)
	if err != nil {
		t.Fatalf("pluginFingerprint third: %v", err)
	}
	if third == first {
		t.Fatal("expected package change to affect fingerprint")
	}
}

func TestPluginFingerprintIncludesPreparedConfig(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{Package: "./plugins/dummy.tar.gz"}

	first, err := pluginFingerprint("external", plugin, map[string]any{"runtime_key": "one"})
	if err != nil {
		t.Fatalf("pluginFingerprint first: %v", err)
	}
	second, err := pluginFingerprint("external", plugin, map[string]any{"runtime_key": "two"})
	if err != nil {
		t.Fatalf("pluginFingerprint second: %v", err)
	}
	if first == second {
		t.Fatal("expected prepared config to affect fingerprint")
	}
}

func TestPrepareConfigResolvesPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackage(t, dir, "github.com/acme/plugins/provider", "0.1.0", "provider")
	cfgPath := writePreparedPluginPackageConfig(t, dir, packagePath)

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	lock, err := readLockfile(filepath.Join(dir, initLockfileName))
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Plugins[lockPluginKey("integration", "example")]
	if !ok {
		t.Fatalf("lockfile missing plugin entry: %+v", lock.Plugins)
	}
	if entry.Package != packagePath {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, packagePath)
	}

	_, cfg, err := loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution: %v", err)
	}
	plugin := cfg.Integrations["example"].Plugin
	if plugin.Command == "" {
		t.Fatal("expected plugin.Command to be set after apply")
	}
}

func TestLoadConfigForExecutionPreferRejectsUnpreparedPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writePreparedPluginPackageConfig(t, dir, "./nonexistent-plugin.tar.gz")

	_, _, err := loadConfigForExecution(cfgPath, false)
	if err == nil {
		t.Fatal("expected unprepared plugin package to fail")
	}
	if !strings.Contains(err.Error(), "gestaltd bundle") {
		t.Fatalf("expected init guidance, got: %v", err)
	}
}

func TestValidateConfigUsesPreparedManifestForPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackageWithSchema(t, dir, "github.com/acme/plugins/provider", "0.1.0", "not-an-executable", `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`)
	cfgPath := writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"api_key": "sk-test",
	})

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
}

func TestPrepareConfigRejectsPluginPackageSchemaViolation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackageWithSchema(t, dir, "github.com/acme/plugins/provider", "0.1.0", "not-an-executable", `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`)
	cfgPath := writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"wrong_key": "value",
	})

	if err := initConfig(cfgPath); err == nil {
		t.Fatal("expected schema validation failure during prepare")
	}
}

func TestValidateConfigUsesPreparedManifestForRuntimePluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestRuntimePackageWithSchema(t, dir, "github.com/acme/plugins/runtime", "0.1.0", "not-an-executable", `{
  "type": "object",
  "required": ["runtime_key"],
  "properties": {
    "runtime_key": { "type": "string" }
  }
}`)
	cfgPath := writePreparedRuntimePluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"runtime_key": "rk-test",
	})

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
}

func writePreparedPluginPackageConfig(t *testing.T, dir, packagePath string) string {
	t.Helper()
	return writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, nil)
}

func writePreparedPluginPackageConfigWithConfig(t *testing.T, dir, packagePath string, pluginConfig map[string]any) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	var configBlock string
	if pluginConfig != nil {
		configBlock += "\n      config:"
		payload, err := json.Marshal(pluginConfig)
		if err != nil {
			t.Fatalf("Marshal(pluginConfig): %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("Unmarshal(pluginConfig): %v", err)
		}
		for key, value := range decoded {
			configBlock += fmt.Sprintf("\n        %s: %q", key, value)
		}
	}
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
integrations:
  example:
    plugin:
      package: ` + packagePath + configBlock + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}

func writePreparedRuntimePluginPackageConfigWithConfig(t *testing.T, dir, packagePath string, runtimeConfig map[string]any) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	var configBlock string
	if runtimeConfig != nil {
		configBlock += "\n    config:"
		payload, err := json.Marshal(runtimeConfig)
		if err != nil {
			t.Fatalf("Marshal(runtimeConfig): %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("Unmarshal(runtimeConfig): %v", err)
		}
		for key, value := range decoded {
			configBlock += fmt.Sprintf("\n      %s: %q", key, value)
		}
	}
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  encryption_key: test-key
runtimes:
  example:
` + configBlock + `
    plugin:
      package: ` + packagePath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}

func buildPreparedTestPluginPackage(t *testing.T, dir, source, version, content string) string {
	t.Helper()
	return buildPreparedTestPluginPackageWithSchema(t, dir, source, version, content, "")
}

func buildPreparedTestPluginPackageWithSchema(t *testing.T, dir, source, version, content, schema string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.FromSlash(filepath.Dir(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	var schemaPath string
	if schema != "" {
		schemaPath = filepath.ToSlash(filepath.Join("schemas", "config.schema.json"))
		if err := os.MkdirAll(filepath.Join(srcDir, "schemas"), 0755); err != nil {
			t.Fatalf("MkdirAll schema dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(schemaPath)), []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol:         pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
			ConfigSchemaPath: schemaPath,
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: sha256HexForPrepareTest(content),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactRel,
			},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func buildPreparedTestRuntimePackageWithSchema(t *testing.T, dir, source, version, content, schema string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "runtime-plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "runtime"))
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.FromSlash(filepath.Dir(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	if schema != "" {
		schemaPath := filepath.Join(srcDir, "schemas", "config.schema.json")
		if err := os.MkdirAll(filepath.Dir(schemaPath), 0755); err != nil {
			t.Fatalf("MkdirAll schema dir: %v", err)
		}
		if err := os.WriteFile(schemaPath, []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kinds:   []string{pluginmanifestv1.KindRuntime},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: sha256HexForPrepareTest(content),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Runtime: &pluginmanifestv1.Entrypoint{
				ArtifactPath: artifactRel,
			},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, "runtime-plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func sha256HexForPrepareTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
