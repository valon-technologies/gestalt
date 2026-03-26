package main

import (
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
	"testing"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func TestPrepareConfigWritesLockfileAndHiddenProviders(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	defer openAPIServer.Close()

	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	lockPath := filepath.Join(dir, initLockfileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stat lockfile: %v", err)
	}
	providerPath := filepath.Join(dir, filepath.FromSlash(preparedProvidersDir), "restapi.json")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("stat provider artifact: %v", err)
	}

	lock, err := readLockfile(lockPath)
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Providers["restapi"]
	if !ok {
		t.Fatalf("lockfile missing restapi entry: %+v", lock.Providers)
	}
	if entry.Provider != ".gestalt/providers/restapi.json" {
		t.Fatalf("entry.Provider = %q, want %q", entry.Provider, ".gestalt/providers/restapi.json")
	}
	if entry.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if lock.Version != lockVersion {
		t.Fatalf("lockfile version = %d, want %d", lock.Version, lockVersion)
	}
	if lock.Plugins == nil {
		t.Fatal("expected plugins map to be initialized")
	}
}

func TestLoadConfigForExecutionAutoPrepareThenServeOffline(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	_, _, preparedProviders, err := loadConfigForExecution(cfgPath, false)
	if err != nil {
		t.Fatalf("loadConfigForExecution auto: %v", err)
	}
	gotProvider := preparedProviders["restapi"]
	if gotProvider == "" {
		t.Fatal("expected prepared provider path to be injected")
	}

	openAPIServer.Close()

	_, _, preparedProviders, err = loadConfigForExecution(cfgPath, true)
	if err != nil {
		t.Fatalf("loadConfigForExecution require: %v", err)
	}
	if preparedProviders["restapi"] == "" {
		t.Fatal("expected prepared provider path in strict serve mode")
	}
}

func TestLoadConfigForExecutionRequirePreparedRejectsUnpreparedRemote(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	defer openAPIServer.Close()

	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	_, _, _, err := loadConfigForExecution(cfgPath, true)
	if err == nil {
		t.Fatal("expected strict serve to reject unprepared remote upstream")
	}
	if !strings.Contains(err.Error(), "gestaltd bundle") {
		t.Fatalf("expected init guidance, got: %v", err)
	}
}

func TestValidatePrefersPreparedProviders(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	openAPIServer.Close()

	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
}

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
  dev_mode: true
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

func TestPrepareConfigGraphQLUpstream(t *testing.T) {
	t.Parallel()

	graphQLServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"__schema": map[string]any{
					"queryType":    map[string]any{"name": "Query"},
					"mutationType": nil,
					"types": []map[string]any{
						{
							"kind": "OBJECT", "name": "Query", "description": "",
							"fields": []map[string]any{
								{
									"name": "search", "description": "Search",
									"args": []map[string]any{},
									"type": map[string]any{"kind": "OBJECT", "name": "Result", "ofType": nil},
								},
							},
							"inputFields": nil, "enumValues": nil,
						},
						{"kind": "OBJECT", "name": "Result", "description": "", "fields": []map[string]any{}, "inputFields": nil, "enumValues": nil},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQLServer.Close()

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
  dev_mode: true
  encryption_key: test-key
integrations:
  graphapi:
    upstreams:
      - type: graphql
        url: ` + graphQLServer.URL + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if err := initConfig(cfgPath); err != nil {
		t.Fatalf("initConfig: %v", err)
	}

	lock, err := readLockfile(filepath.Join(dir, initLockfileName))
	if err != nil {
		t.Fatalf("readLockfile: %v", err)
	}
	entry, ok := lock.Providers["graphapi"]
	if !ok {
		t.Fatalf("lockfile missing graphapi entry: %+v", lock.Providers)
	}
	if entry.Provider != ".gestalt/providers/graphapi.json" {
		t.Fatalf("entry.Provider = %q, want %q", entry.Provider, ".gestalt/providers/graphapi.json")
	}

	providerPath := filepath.Join(dir, filepath.FromSlash(preparedProvidersDir), "graphapi.json")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("stat graphql provider artifact: %v", err)
	}
}

func TestValidateConfigRejectsOverlayWithoutSingleBaseSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		body        string
		wantContain string
	}{
		{
			name: "missing base source",
			body: `integrations:
  sample:
    plugin:
      mode: overlay
      command: /tmp/plugin
`,
			wantContain: "must declare a base source",
		},
		{
			name: "multiple base sources",
			body: `integrations:
  sample:
    plugin:
      mode: overlay
      base: sample-base
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantContain: "exactly one base source",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := `auth:
  provider: google
datastore:
  provider: sqlite
server:
  dev_mode: true
  encryption_key: test-key
` + tc.body
			if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			err := validateConfig(cfgPath)
			if err == nil {
				t.Fatal("expected validateConfig to fail")
			}
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantContain, err)
			}
		})
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

	plugin := &config.ExecutablePluginDef{
		Mode:    config.PluginModeReplace,
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

	plugin := &config.ExecutablePluginDef{Package: "./plugins/dummy.tar.gz"}

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
	packagePath := buildPreparedTestPluginPackage(t, dir, "acme/provider", "0.1.0", "provider")
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

	_, cfg, _, err := loadConfigForExecution(cfgPath, true)
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

	_, _, _, err := loadConfigForExecution(cfgPath, false)
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
	packagePath := buildPreparedTestPluginPackageWithSchema(t, dir, "acme/provider", "0.1.0", "not-an-executable", `{
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
	packagePath := buildPreparedTestPluginPackageWithSchema(t, dir, "acme/provider", "0.1.0", "not-an-executable", `{
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
	packagePath := buildPreparedTestRuntimePackageWithSchema(t, dir, "acme/runtime", "0.1.0", "not-an-executable", `{
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

func newPreparedTestOpenAPIServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]any{"title": "REST API"},
			"servers": []map[string]any{{"url": "https://api.example.com"}},
			"paths": map[string]any{
				"/items": map[string]any{
					"get": map[string]any{
						"operationId": "listItems",
						"summary":     "List items",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func writePreparedTestConfig(t *testing.T, dir, upstreamURL string) string {
	t.Helper()

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
  dev_mode: true
  encryption_key: test-key
integrations:
  restapi:
    display_name: REST API
    upstreams:
      - type: rest
        url: ` + upstreamURL + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
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
  dev_mode: true
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
  dev_mode: true
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

func buildPreparedTestPluginPackage(t *testing.T, dir, id, version, content string) string {
	t.Helper()
	return buildPreparedTestPluginPackageWithSchema(t, dir, id, version, content, "")
}

func buildPreparedTestPluginPackageWithSchema(t *testing.T, dir, id, version, content, schema string) string {
	t.Helper()

	source := filepath.Join(dir, "plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(source, filepath.FromSlash(filepath.Dir(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	var schemaPath string
	if schema != "" {
		schemaPath = filepath.ToSlash(filepath.Join("schemas", "config.schema.json"))
		if err := os.MkdirAll(filepath.Join(source, "schemas"), 0755); err != nil {
			t.Fatalf("MkdirAll schema dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(schemaPath)), []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindProvider},
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
	if err := os.WriteFile(filepath.Join(source, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(source, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func buildPreparedTestRuntimePackageWithSchema(t *testing.T, dir, id, version, content, schema string) string {
	t.Helper()

	source := filepath.Join(dir, "runtime-plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "runtime"))
	if err := os.MkdirAll(filepath.Join(source, filepath.FromSlash(filepath.Dir(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	if schema != "" {
		schemaPath := filepath.Join(source, "schemas", "config.schema.json")
		if err := os.MkdirAll(filepath.Dir(schemaPath), 0755); err != nil {
			t.Fatalf("MkdirAll schema dir: %v", err)
		}
		if err := os.WriteFile(schemaPath, []byte(schema), 0644); err != nil {
			t.Fatalf("WriteFile schema: %v", err)
		}
	}

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            id,
		Version:       version,
		Kinds:         []string{pluginmanifestv1.KindRuntime},
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
	if err := os.WriteFile(filepath.Join(source, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	archivePath := filepath.Join(dir, "runtime-plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(source, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func sha256HexForPrepareTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
