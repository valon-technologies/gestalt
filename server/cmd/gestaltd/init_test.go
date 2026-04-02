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
				Manifest:    ".gestalt/plugins/integration_external/plugin.json",
				Executable:  ".gestalt/plugins/integration_external/artifacts/linux/amd64/provider",
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

	first, err := pluginFingerprint("external", plugin, ".")
	if err != nil {
		t.Fatalf("pluginFingerprint: %v", err)
	}
	second, err := pluginFingerprint("external", plugin, ".")
	if err != nil {
		t.Fatalf("pluginFingerprint second: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint changed between identical inputs: %q != %q", first, second)
	}

	plugin.Package = "./plugins/dummy.tar.gz"
	third, err := pluginFingerprint("external", plugin, ".")
	if err != nil {
		t.Fatalf("pluginFingerprint third: %v", err)
	}
	if third == first {
		t.Fatal("expected package change to affect fingerprint")
	}
}

func TestLoadConfigForExecutionAllowsManagedPluginConfigChangeAfterInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackageRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.1.0", "provider")
	cfgPath := writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"api_key": "one",
	})

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"api_key": "two",
	})

	_, cfg, err := loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution: %v", err)
	}

	pluginConfig, err := config.NodeToMap(cfg.Integrations["example"].Plugin.Config)
	if err != nil {
		t.Fatalf("NodeToMap: %v", err)
	}
	if got := pluginConfig["api_key"]; got != "two" {
		t.Fatalf("api_key = %v, want %q", got, "two")
	}
}

func TestLoadConfigForExecutionResolvesLateBoundManagedPluginEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envVar := "TEST_API_KEY_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	portEnvVar := envVar + "_PORT"
	mcpEnvVar := envVar + "_MCP"
	packagePath := buildPreparedTestPluginPackageRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.1.0", "provider")
	cfgPath := writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"api_key": "${" + envVar + "}",
	})
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	updatedCfg := strings.Replace(string(cfgData), "server:\n  encryption_key: test-key\n", "server:\n  port: ${"+portEnvVar+"}\n  encryption_key: test-key\n", 1)
	if updatedCfg == string(cfgData) {
		t.Fatal("expected to rewrite server block in config")
	}
	updatedCfg += "    mcp:\n      enabled: ${" + mcpEnvVar + "}\n"
	if err := os.WriteFile(cfgPath, []byte(updatedCfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	savedEnv := map[string]*string{}
	for _, key := range []string{envVar, portEnvVar, mcpEnvVar} {
		if value, ok := os.LookupEnv(key); ok {
			value := value
			savedEnv[key] = &value
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range []string{envVar, portEnvVar, mcpEnvVar} {
			if value, ok := savedEnv[key]; ok && value != nil {
				_ = os.Setenv(key, *value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	})

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	if err := os.Setenv(envVar, "runtime-value"); err != nil {
		t.Fatalf("Setenv %s: %v", envVar, err)
	}

	_, cfg, err := loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution: %v", err)
	}

	pluginConfig, err := config.NodeToMap(cfg.Integrations["example"].Plugin.Config)
	if err != nil {
		t.Fatalf("NodeToMap: %v", err)
	}
	if got := pluginConfig["api_key"]; got != "runtime-value" {
		t.Fatalf("api_key = %v, want %q", got, "runtime-value")
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("server.port = %d, want %d", cfg.Server.Port, 8080)
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
	wantPackage := filepath.ToSlash(filepath.Base(packagePath))
	if entry.Package != wantPackage {
		t.Fatalf("entry.Package = %q, want %q (relative to config dir)", entry.Package, wantPackage)
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
	if !strings.Contains(err.Error(), "gestaltd init") {
		t.Fatalf("expected init guidance, got: %v", err)
	}
}

func TestValidateConfigUsesPreparedManifestForPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackageRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.1.0", "not-an-executable")
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
	packagePath := buildPreparedTestPluginPackageWithSchema(t, dir, "github.com/acme/plugins/provider", "0.1.0", "not-an-executable", "schemas/config.schema.yaml", `type: object
required:
  - api_key
properties:
  api_key:
    type: string
`)
	cfgPath := writePreparedPluginPackageConfigWithConfig(t, dir, packagePath, map[string]any{
		"wrong_key": "value",
	})

	if err := initConfig(cfgPath); err == nil {
		t.Fatal("expected schema validation failure during prepare")
	}
}

func TestPrepareConfigAcceptsPluginPackageSurfaceConnectionAliases(t *testing.T) {
	t.Parallel()

	const (
		testProviderSource      = "github.com/acme/plugins/provider"
		testProviderVersion     = "0.1.0"
		testArtifactContent     = "not-an-executable"
		testConnectionName      = "api"
		testOpenAPIURL          = "https://example.com/openapi.json"
		testGraphQLURL          = "https://example.com/graphql"
		testMCPURL              = "https://example.com/mcp"
		testAuthorizationURL    = "https://example.com/authorize"
		testTokenURL            = "https://example.com/token"
		testClientID            = "client-id"
		testClientSecret        = "client-secret"
		testAcceptHeader        = "application/json"
		testTokenMetadataTenant = "tenant_id"
		testTokenMetadataSite   = "site_id"
		testTokenParamAudience  = "audience"
		testTokenParamValue     = "api://gestalt"
		testRefreshParamPrompt  = "prompt"
		testRefreshParamValue   = "consent"
	)

	tokenParams := map[string]string{
		testTokenParamAudience: testTokenParamValue,
	}
	refreshParams := map[string]string{
		testRefreshParamPrompt: testRefreshParamValue,
	}
	tokenMetadata := []string{
		testTokenMetadataTenant,
		testTokenMetadataSite,
	}

	dir := t.TempDir()
	packagePath := buildPreparedTestPluginPackageWithManifestProvider(t, dir, testProviderSource, testProviderVersion, testArtifactContent, &pluginmanifestv1.Provider{
		OpenAPI:           testOpenAPIURL,
		GraphQLURL:        testGraphQLURL,
		MCPURL:            testMCPURL,
		OpenAPIConnection: testConnectionName,
		GraphQLConnection: testConnectionName,
		MCPConnection:     testConnectionName,
		Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
			testConnectionName: {
				Mode: "user",
				Auth: &pluginmanifestv1.ProviderAuth{
					Type:             pluginmanifestv1.AuthTypeOAuth2,
					AuthorizationURL: testAuthorizationURL,
					TokenURL:         testTokenURL,
					ClientID:         testClientID,
					ClientSecret:     testClientSecret,
					TokenParams:      tokenParams,
					RefreshParams:    refreshParams,
					AcceptHeader:     testAcceptHeader,
					TokenMetadata:    tokenMetadata,
				},
			},
		},
	}, "")
	cfgPath := writePreparedPluginPackageConfig(t, dir, packagePath)

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}

	_, cfg, err := loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution: %v", err)
	}
	plugin := cfg.Integrations["example"].Plugin
	if plugin.ResolvedManifest == nil {
		t.Fatal("expected ResolvedManifest to be set after prepare")
	}
}

func TestValidateConfigUsesPreparedManifestForSpecLoadedPluginPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedSpecLoadedPluginPackage(t, dir, "github.com/acme/plugins/spec-loaded", "0.1.0")
	cfgPath := writePreparedPluginPackageConfig(t, dir, packagePath)

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}

	_, cfg, err := loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution: %v", err)
	}
	plugin := cfg.Integrations["example"].Plugin
	if plugin.ResolvedManifest == nil || plugin.ResolvedManifest.Provider == nil {
		t.Fatal("expected resolved manifest provider to be set after prepare")
	}
	if !filepath.IsAbs(plugin.ResolvedManifest.Provider.OpenAPI) {
		t.Fatalf("resolved openapi path = %q, want absolute path", plugin.ResolvedManifest.Provider.OpenAPI)
	}
	if _, err := os.Stat(plugin.ResolvedManifest.Provider.OpenAPI); err != nil {
		t.Fatalf("resolved openapi path missing: %v", err)
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
		configBlock += "\n    config:"
		payload, err := json.Marshal(pluginConfig)
		if err != nil {
			t.Fatalf("Marshal(pluginConfig): %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("Unmarshal(pluginConfig): %v", err)
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
providers:
  example:
    from:
      package: ` + packagePath + configBlock + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}

func buildPreparedTestPluginPackage(t *testing.T, dir, source, version, content string) string {
	t.Helper()
	return buildPreparedTestPluginPackageWithSchema(t, dir, source, version, content, "", "")
}

func buildPreparedTestPluginPackageRequiringAPIKey(t *testing.T, dir, source, version, content string) string {
	t.Helper()
	return buildPreparedTestPluginPackageWithSchema(t, dir, source, version, content, "schemas/config.schema.json", `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`)
}

func buildPreparedTestPluginPackageWithSchema(t *testing.T, dir, source, version, content, schemaPath, schema string) string {
	t.Helper()
	provider := &pluginmanifestv1.Provider{}
	if schema != "" {
		provider.ConfigSchemaPath = filepath.ToSlash(schemaPath)
	}
	return buildPreparedTestPluginPackageWithManifestProvider(t, dir, source, version, content, provider, schema)
}

func buildPreparedTestPluginPackageWithManifestProvider(t *testing.T, dir, source, version, content string, provider *pluginmanifestv1.Provider, schema string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.FromSlash(filepath.Dir(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	if provider != nil && provider.ConfigSchemaPath != "" {
		if err := os.MkdirAll(filepath.Join(srcDir, "schemas"), 0755); err != nil {
			t.Fatalf("MkdirAll schema dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(provider.ConfigSchemaPath)), []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: provider,
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

func buildPreparedSpecLoadedPluginPackage(t *testing.T, dir, source, version string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "spec-plugin-src")
	if err := os.MkdirAll(filepath.Join(srcDir, "specs"), 0755); err != nil {
		t.Fatalf("MkdirAll specs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "specs", "openapi.yaml"), []byte("openapi: 3.0.0\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile spec: %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: filepath.ToSlash(filepath.Join("specs", "openapi.yaml")),
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, "spec-plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func sha256HexForPrepareTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
