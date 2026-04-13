package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func mustDecodeNode(t *testing.T, node yaml.Node) map[string]any {
	t.Helper()
	m := make(map[string]any)
	if err := node.Decode(&m); err != nil {
		t.Fatalf("decoding yaml.Node: %v", err)
	}
	return m
}

func mustWriteConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestLoadConfigGenericFixture(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  auth:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
    config:
      clientId: client-1
      clientSecret: secret-1
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
  plugins:
    service-a:
      displayName: Service A
      source:
        path: /tmp/manifest.yaml
server:
  indexeddb: sqlite
  encryptionKey: server-key
  public:
    host: 127.0.0.1
    port: 9090
  management:
    host: 127.0.0.1
    port: 9191
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Public.Port != 9090 {
		t.Fatalf("Server.Public.Port = %d", cfg.Server.Public.Port)
	}
	if cfg.Server.EncryptionKey != "server-key" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}
	if got := cfg.Providers.Plugins["service-a"].DisplayName; got != "Service A" {
		t.Fatalf("Integrations[service-a].DisplayName = %q", got)
	}
}

func TestLoadConfigDefaultsAndEnv(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		return map[string]string{
			"TEST_CLIENT_ID":  "client-from-env",
			"TEST_ENCRYPTION": "encryption-from-env",
		}[key]
	}

	path := mustWriteConfigFile(t, `
providers:
  auth:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
    config:
      clientId: ${TEST_CLIENT_ID}
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
  plugins:
    service-a:
      source:
        path: /tmp/manifest.yaml
server:
  indexeddb: sqlite
  encryptionKey: ${TEST_ENCRYPTION}
`)

	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		v := getenv(key)
		return v, v != ""
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}

	if cfg.Server.Public.Port != 8080 {
		t.Fatalf("Server.Public.Port = %d, want 8080", cfg.Server.Public.Port)
	}
	if cfg.Providers.Secrets.Source.Builtin != "env" {
		t.Fatalf("Secrets.Source.Builtin = %q, want env", cfg.Providers.Secrets.Source.Builtin)
	}
	if cfg.Server.EncryptionKey != "encryption-from-env" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}

	if cfg.Providers.Auth == nil {
		t.Fatal("Providers.Auth = nil")
	}
	authCfg := mustDecodeNode(t, cfg.Providers.Auth.Config)
	if authCfg["clientId"] != "client-from-env" {
		t.Fatalf("Auth.Config.clientId = %#v", authCfg["clientId"])
	}
}

func TestLoadConfigEnvFileFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secretPath := filepath.Join(dir, "encryption-key")
	if err := os.WriteFile(secretPath, []byte("file-based-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile secret: %v", err)
	}

	path := mustWriteConfigFile(t, `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: ${TEST_ENCRYPTION}
`)

	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		switch key {
		case "TEST_ENCRYPTION_FILE":
			return secretPath, true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}

	if cfg.Server.EncryptionKey != "file-based-secret" {
		t.Fatalf("Server.EncryptionKey = %q, want file-based-secret", cfg.Server.EncryptionKey)
	}
}

func TestLoadConfigMissingEnvVariableFails(t *testing.T) {
	t.Parallel()

	encryptionEnv := "GESTALT_TEST_ENCRYPTION_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	portEnv := encryptionEnv + "_PORT"
	path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: ${%s}
  public:
    port: ${%s}
`, encryptionEnv, portEnv))

	_, err := LoadWithLookup(path, func(string) (string, bool) {
		return "", false
	})
	if err == nil {
		t.Fatal("LoadWithLookup: expected error, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf(`environment variable %q not set`, encryptionEnv)) {
		t.Fatalf("expected missing env error, got: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("${%s:-}", encryptionEnv)) {
		t.Fatalf("expected empty-default hint, got: %v", err)
	}

	cfg, err := LoadAllowMissingEnv(path)
	if err != nil {
		t.Fatalf("LoadAllowMissingEnv: %v", err)
	}
	if cfg.Server.EncryptionKey != "" {
		t.Fatalf("Server.EncryptionKey = %q, want empty string", cfg.Server.EncryptionKey)
	}
	if cfg.Server.Public.Port != 8080 {
		t.Fatalf("Server.Public.Port = %d, want 8080", cfg.Server.Public.Port)
	}
}

func TestLoadConfigEmptyDefaultEnvSyntax(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: ${TEST_ENCRYPTION:-}
`)

	cfg, err := LoadWithLookup(path, func(string) (string, bool) {
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}
	if cfg.Server.EncryptionKey != "" {
		t.Fatalf("Server.EncryptionKey = %q, want empty string", cfg.Server.EncryptionKey)
	}
}

func TestExpandEnvVariablesPreservesMissingPlaceholder(t *testing.T) {
	t.Parallel()

	got, firstMissing, err := expandEnvVariables("value: ${MISSING}", func(string) (string, bool) {
		return "", false
	}, true)
	if err != nil {
		t.Fatalf("expandEnvVariables: %v", err)
	}
	if firstMissing != "MISSING" {
		t.Fatalf("expandEnvVariables firstMissing = %q, want MISSING", firstMissing)
	}
	if got != "value: ${MISSING}" {
		t.Fatalf("expandEnvVariables = %q, want value: ${MISSING}", got)
	}
}

func TestExpandEnvVariablesRejectsNonEmptyDefault(t *testing.T) {
	t.Parallel()

	_, _, err := expandEnvVariables("value: ${MISSING:-fallback}", func(string) (string, bool) {
		return "", false
	}, false)
	if err == nil {
		t.Fatal("expandEnvVariables: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "only ${MISSING:-} is supported for empty defaults") {
		t.Fatalf("expected unsupported default error, got: %v", err)
	}
}

func TestValidateRuntime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "missing auth provider is allowed",
			yaml: `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`,
			wantErr: false,
		},
		{
			name: "missing datastore",
			yaml: `
providers:
  auth:
    disabled: true
server:
  encryptionKey: server-key
`,
			wantErr: true,
		},
		{
			name: "missing encryption key",
			yaml: `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
`,
			wantErr: true,
		},
		{
			name: "auth disabled is allowed",
			yaml: `
providers:
  auth:
    disabled: true
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			err = ValidateRuntime(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ValidateRuntime: expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateRuntime: %v", err)
			}
			if tc.name == "auth disabled is allowed" && !cfg.Providers.Auth.Disabled {
				t.Fatal("Auth.Disabled = false, want true")
			}
		})
	}
}

func TestLoadSucceedsWithoutRuntimeFields(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  plugins:
    custom_tool:
      source:
        path: ./manifest.yaml
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Providers.Plugins["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "manifest.yaml") {
		t.Fatalf("unexpected plugin source path: %q", got)
	}
}

func TestLoadConfigUIProviderModes(t *testing.T) {
	t.Parallel()

	t.Run("omitted ui uses default provider", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.UI.Disabled {
			t.Fatal("UI.Disabled = true, want false")
		}
		if cfg.Providers.UI == nil {
			t.Fatalf("Providers.UI = nil, want default provider")
		}
		if got := cfg.Providers.UI.Source.Ref; got != DefaultWebUIProvider {
			t.Fatalf("UI.Source.Ref = %q, want %q", got, DefaultWebUIProvider)
		}
		if got := cfg.Providers.UI.Source.Version; got != DefaultWebUIVersion {
			t.Fatalf("UI.Source.Version = %q, want %q", got, DefaultWebUIVersion)
		}
	})

	t.Run("ui disabled true disables public ui", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    disabled: true
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Providers.UI.Disabled {
			t.Fatal("UI.Disabled = false, want true")
		}
	})

	t.Run("disabled field accepts all YAML boolean variants", func(t *testing.T) {
		t.Parallel()

		for _, variant := range []string{"true", "True", "TRUE"} {
			variant := variant
			t.Run(variant, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  ui:
    disabled: %s
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`, variant))

				cfg, err := Load(path)
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if !cfg.Providers.UI.Disabled {
					t.Fatalf("UI.Disabled = false with disabled: %s, want true", variant)
				}
			})
		}
	})

	t.Run("ui config is accepted when disabled", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    disabled: true
    config:
      brand_name: Acme
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("relative ui provider path resolves from config directory", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    source:
      path: ./web/default/provider.yaml
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Providers.UI == nil {
			t.Fatalf("Providers.UI = nil")
		}
		wantPath := filepath.Join(filepath.Dir(path), "web", "default", "provider.yaml")
		if got := cfg.Providers.UI.Source.Path; got != wantPath {
			t.Fatalf("UI.Source.Path = %q, want %q", got, wantPath)
		}
	})
}

func TestLoadRejectsUnknownProviderFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "provider field is rejected",
			yaml: `
providers:
  ui:
    provider: none
`,
			wantErr: `field provider not found`,
		},
		{
			name: "builtin field is rejected",
			yaml: `
providers:
  telemetry:
    builtin: stdout
`,
			wantErr: `field builtin not found`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadAcceptsNewComponentForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "builtin string",
			yaml: `
providers:
  telemetry:
    source: stdout
`,
		},
		{
			name: "disabled true",
			yaml: `
providers:
  ui:
    disabled: true
`,
		},
		{
			name: "external provider source",
			yaml: `
providers:
  auth:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
		})
	}
}

func TestLoadConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "provider with no source or surfaces",
			yaml: `
providers:
  plugins:
    service-a:
      displayName: Service A
`,
		},
		{
			name: "egress default action must be allow or deny",
			yaml: `
server:
  egress:
    defaultAction: block
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
		})
	}
}

func TestValidConfigurations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "managed source plugin only",
			yaml: `
providers:
  plugins:
    custom_tool:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
		},
		{
			name: "plugin with local source",
			yaml: `
providers:
  plugins:
    service:
      source:
        path: /usr/bin/manifest.yaml
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
		})
	}
}

func TestPluginValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "integration plugin source path is valid",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
`,
		},
		{
			name: "plugin source path and ref are mutually exclusive",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
			wantErr: "mutually exclusive",
		},
		{
			name: "plugin env with local source is valid",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
      env:
        FOO: bar
`,
		},
		{
			name: "plugin config with source is valid",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
      config:
        base_url: https://example.com
`,
		},
		{
			name: "plugin source is required for external",
			yaml: `
providers:
  plugins:
    external:
      {}
`,
			wantErr: "source.path or source.ref is required",
		},
		{
			name: "plugin source path with version is rejected",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
        version: 1.0.0
`,
			wantErr: "source.version is only valid with source.ref",
		},
		{
			name: "plugin source with version is valid",
			yaml: `
providers:
  plugins:
    external:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
		},
		{
			name: "plugin source with base_url override is rejected",
			yaml: `
providers:
  plugins:
    external:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
      base_url: https://api.example.com
`,
			wantErr: "field base_url not found",
		},
		{
			name: "plugin source without version is rejected",
			yaml: `
providers:
  plugins:
    external:
      source:
        ref: github.com/acme-corp/tools/widget
`,
			wantErr: "source.version is required when source.ref is set",
		},
		{
			name: "non-default connection params are accepted",
			yaml: `
providers:
  plugins:
    external:
      source:
        path: ./plugins/dummy/manifest.yaml
      connections:
        named:
          mode: user
          auth:
            type: none
          params:
            team:
              required: true
`,
		},
		{
			name: "egress default_action allow is valid",
			yaml: `
server:
  egress:
    defaultAction: allow
`,
		},
		{
			name: "egress default_action deny is valid",
			yaml: `
server:
  egress:
    defaultAction: deny
`,
		},
		{
			name: "egress default_action invalid",
			yaml: `
server:
  egress:
    defaultAction: block
`,
			wantErr: "default_action must be \"allow\" or \"deny\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("Load: expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
		})
	}
}

func TestValidateStructure_PluginValidationDirect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "local source valid",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {Source: ProviderSource{Path: "./some-dir/manifest.yaml"}},
					},
				},
			},
		},
		{
			name: "source valid",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
					},
				},
			},
		},
		{
			name: "source path and ref rejected",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {Source: ProviderSource{Path: "./manifest.yaml", Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
					},
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "nil plugin rejected",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {},
					},
				},
			},
			wantErr: "source.path or source.ref is required",
		},
		{
			name: "source without version rejected",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin"}},
					},
				},
			},
			wantErr: "source.version is required when source.ref is set",
		},
		{
			name: "source version without ref rejected",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {Source: ProviderSource{Version: "1.0.0"}},
					},
				},
			},
			wantErr: "source.path or source.ref is required",
		},
		{
			name: "auth provider valid",
			cfg: &Config{
				Providers: ProvidersConfig{
					Auth: &ProviderEntry{Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-auth", Version: "1.0.0"}},
				},
			},
		},
		{
			name: "auth provider none valid",
			cfg:  &Config{},
		},
		{
			name: "auth provider invalid when source missing",
			cfg: &Config{
				Providers: ProvidersConfig{
					Auth: &ProviderEntry{},
				},
			},
			wantErr: `source.path or source.ref is required`,
		},
		{
			name: "auth config accepted when auth disabled",
			cfg: &Config{
				Providers: ProvidersConfig{
					Auth: &ProviderEntry{Config: yaml.Node{Kind: yaml.MappingNode}, Disabled: true},
				},
			},
		},
		{
			name: "plugin auth rejects mcp oauth early",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {
							Source: ProviderSource{Path: "./manifest.yaml"},
							Auth:   &ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth},
						},
					},
				},
			},
			wantErr: `plugin auth type "mcp_oauth" requires an MCP surface`,
		},
		{
			name: "named connection rejects mcp oauth early",
			cfg: &Config{
				Providers: ProvidersConfig{
					Plugins: map[string]*ProviderEntry{
						"sample": {
							Source: ProviderSource{Path: "./manifest.yaml"},
							Connections: map[string]*ConnectionDef{
								"default": {Auth: ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth}},
							},
						},
					},
				},
			},
			wantErr: `connection "default" auth type "mcp_oauth" requires an MCP surface`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateStructure(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadConfigResolvesRelativePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	iconDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	iconPath := filepath.Join(iconDir, "service.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg/>`), 0o644); err != nil {
		t.Fatalf("WriteFile icon: %v", err)
	}

	cfgPath := filepath.Join(dir, "configs", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`
providers:
  auth:
    source:
      path: ../auth-plugin/provider.yaml
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
  plugins:
    service-a:
      iconFile: ../assets/service.svg
      source:
        path: ../bin/manifest.yaml
server:
  indexeddb: sqlite
`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Providers.Plugins["service-a"].IconFile; got != iconPath {
		t.Fatalf("IconFile = %q, want %q", got, iconPath)
	}
	if got := cfg.Providers.Auth.SourcePath(); got != filepath.Join(dir, "auth-plugin", "provider.yaml") {
		t.Fatalf("auth plugin source path = %q, want %q", got, filepath.Join(dir, "auth-plugin", "provider.yaml"))
	}
	if got := cfg.Providers.Plugins["service-a"].SourcePath(); got != filepath.Join(dir, "bin", "manifest.yaml") {
		t.Fatalf("integration plugin source path = %q, want %q", got, filepath.Join(dir, "bin", "manifest.yaml"))
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  auth:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
    config:
      clientId: client-1
      clientSecret: secret-1
      redirectUrl: https://example.test/callback
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Providers.Auth == nil {
		t.Fatal("Providers.Auth = nil")
	}
	authCfg := mustDecodeNode(t, cfg.Providers.Auth.Config)
	if len(authCfg) != 3 {
		t.Fatalf("Auth.Config length = %d, want 3", len(authCfg))
	}
	if authCfg["clientId"] != "client-1" {
		t.Fatalf("Auth.Config.clientId = %#v", authCfg["clientId"])
	}
	if authCfg["clientSecret"] != "secret-1" {
		t.Fatalf("Auth.Config.clientSecret = %#v", authCfg["clientSecret"])
	}
	if authCfg["redirectUrl"] != "https://example.test/callback" {
		t.Fatalf("Auth.Config.redirectUrl = %#v", authCfg["redirectUrl"])
	}
}

func TestLoadConfig_APITokenTTL(t *testing.T) {
	t.Parallel()

	t.Run("valid day duration", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  apiTokenTtl: "14d"
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Server.APITokenTTL != "14d" {
			t.Fatalf("APITokenTTL = %q, want %q", cfg.Server.APITokenTTL, "14d")
		}
	})

	t.Run("invalid duration rejected", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  apiTokenTtl: "not-a-duration"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid api_token_ttl")
		}
	})
}

func TestLoadCallbackProxyConfig(t *testing.T) {
	path := mustWriteConfigFile(t, `
server:
  public:
    port: 8080
  baseUrl: ${PUBLIC_BASE_URL}
  encryptionKey: ${PROXY_ENCRYPTION_KEY}
providers:
  indexeddbs:
    main-db:
      source:
        path: ./providers/datastore/sqlite
`)
	t.Setenv("PUBLIC_BASE_URL", "https://preview.example.test/base/")
	t.Setenv("PROXY_ENCRYPTION_KEY", "secret-value")

	cfg, err := LoadCallbackProxy(path)
	if err != nil {
		t.Fatalf("LoadCallbackProxy: %v", err)
	}
	if got := cfg.Server.BaseURL; got != "https://preview.example.test/base" {
		t.Fatalf("Server.BaseURL = %q, want %q", got, "https://preview.example.test/base")
	}
	if got := cfg.Server.EncryptionKey; got != "secret-value" {
		t.Fatalf("Server.EncryptionKey = %q, want %q", got, "secret-value")
	}
}

func TestLoadCallbackProxyConfigRejectsSecretRef(t *testing.T) {
	path := mustWriteConfigFile(t, `
server:
  public:
    port: 8080
  encryptionKey: secret://gestalt-encryption-key
`)

	_, err := LoadCallbackProxy(path)
	if err == nil || !strings.Contains(err.Error(), "callback proxy requires server.encryptionKey to be resolved before startup") {
		t.Fatalf("LoadCallbackProxy error = %v", err)
	}
}

func TestLoadCallbackProxyConfigRejectsUnknownField(t *testing.T) {
	path := mustWriteConfigFile(t, `
server:
  public:
    port: 8080
  encryptionKey: secret-value
  encrpytionKey: typo-should-fail
`)

	_, err := LoadCallbackProxy(path)
	if err == nil || !strings.Contains(err.Error(), "field encrpytionKey not found") {
		t.Fatalf("LoadCallbackProxy error = %v", err)
	}
}

func TestLoadRejectsIntegrationCallbackBaseWithoutBaseURL(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  indexeddbs:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  indexeddb: sqlite
  encryptionKey: server-key
  integrationCallbackBaseUrl: https://auth-proxy.example.test
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "server.baseUrl is required when server.integrationCallbackBaseUrl is set") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := Load("/tmp/gestalt-config-does-not-exist.yaml")
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `{{{invalid yaml`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
	})
}

func TestLoad_ResolvesRelativePluginSourcePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `providers:
  plugins:
    sample:
      source:
        path: ./my-plugin/manifest.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry := loaded.Providers.Plugins["sample"]
	if entry == nil {
		t.Fatal("expected plugin to be loaded")
	}
	if !filepath.IsAbs(entry.SourcePath()) {
		t.Fatalf("expected absolute path, got: %q", entry.SourcePath())
	}
	wantPath := filepath.Join(pluginDir, "manifest.yaml")
	if entry.SourcePath() != wantPath {
		t.Fatalf("entry.SourcePath() = %q, want %q", entry.SourcePath(), wantPath)
	}
}
