package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func mustDecodeNode(t *testing.T, node yaml.Node) map[string]string {
	t.Helper()
	m := make(map[string]string)
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
auth:
  provider: auth-provider
  config:
    client_id: client-1
    client_secret: secret-1
datastore:
  provider: data-store
server:
  encryption_key: server-key
  port: 9090
providers:
  service-a:
    display_name: Service A
    from:
      command: /tmp/provider
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Auth.Provider != "auth-provider" {
		t.Fatalf("Auth.Provider = %q", cfg.Auth.Provider)
	}
	if cfg.Datastore.Provider != "data-store" {
		t.Fatalf("Datastore.Provider = %q", cfg.Datastore.Provider)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("Server.Port = %d", cfg.Server.Port)
	}
	if cfg.Server.EncryptionKey != "server-key" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}
	if got := cfg.Integrations["service-a"].DisplayName; got != "Service A" {
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
auth:
  provider: auth-provider
  config:
    client_id: ${TEST_CLIENT_ID}
datastore:
  provider: data-store
server:
  encryption_key: ${TEST_ENCRYPTION}
providers:
  service-a:
    from:
      command: /tmp/provider
`)

	cfg, err := LoadWithMapping(path, getenv)
	if err != nil {
		t.Fatalf("LoadWithMapping: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Fatalf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Secrets.Provider != "env" {
		t.Fatalf("Secrets.Provider = %q, want env", cfg.Secrets.Provider)
	}
	if cfg.Server.EncryptionKey != "encryption-from-env" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}

	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if authCfg["client_id"] != "client-from-env" {
		t.Fatalf("Auth.Config.client_id = %q", authCfg["client_id"])
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: ${TEST_ENCRYPTION}
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

func TestValidateRuntime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "missing auth provider",
			yaml: `
datastore:
  provider: data-store
server:
  encryption_key: server-key
`,
		},
		{
			name: "missing datastore provider",
			yaml: `
auth:
  provider: auth-provider
server:
  encryption_key: server-key
`,
		},
		{
			name: "missing encryption key with auth enabled",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
`,
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
			if err := ValidateRuntime(cfg); err == nil {
				t.Fatal("ValidateRuntime: expected error, got nil")
			}
		})
	}
}

func TestValidateRuntimeAllowsNoEncryptionKeyWithAuthNone(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: none
datastore:
  provider: data-store
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := ValidateRuntime(cfg); err != nil {
		t.Fatalf("ValidateRuntime: unexpected error: %v", err)
	}
}

func TestLoadSucceedsWithoutRuntimeFields(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  custom_tool:
    from:
      package: https://example.com/custom-tool.tar.gz
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Integrations["custom_tool"].Plugin.Package != "https://example.com/custom-tool.tar.gz" {
		t.Fatalf("unexpected plugin package: %q", cfg.Integrations["custom_tool"].Plugin.Package)
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
  service-a:
    display_name: Service A
`,
		},
		{
			name: "egress default action must be allow or deny",
			yaml: `
egress:
  default_action: block
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
			name: "plugin only",
			yaml: `
providers:
  custom_tool:
    from:
      package: https://example.com/custom-tool.tar.gz
`,
		},
		{
			name: "plugin with command",
			yaml: `
providers:
  service:
    from:
      command: /usr/bin/provider
`,
		},
		{
			name: "inline plugin with openapi",
			yaml: `
providers:
  service:
    connections:
      default:
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    surfaces:
      openapi:
        document: https://example.test/spec.json
        base_url: https://api.example.test
`,
		},
		{
			name: "inline plugin with operations",
			yaml: `
providers:
  service:
    surfaces:
      rest:
        base_url: https://api.example.test
        operations:
          - name: list_users
            method: GET
            path: /users
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
		wantErr bool
	}{
		{
			name: "integration plugin package is valid",
			yaml: `
providers:
  external:
    from:
      package: ./plugins/dummy.tar.gz
`,
		},
		{
			name: "plugin package and command are mutually exclusive",
			yaml: `
providers:
  external:
    from:
      command: /tmp/plugin
      package: ./plugins/dummy.tar.gz
`,
			wantErr: true,
		},
		{
			name: "plugin args require command not package",
			yaml: `
providers:
  external:
    from:
      package: ./plugins/dummy.tar.gz
      args:
        - --verbose
`,
			wantErr: true,
		},
		{
			name: "plugin env with package is valid",
			yaml: `
providers:
  external:
    from:
      package: ./plugins/dummy.tar.gz
      env:
        FOO: bar
`,
		},
		{
			name: "plugin config with package is valid",
			yaml: `
providers:
  external:
    from:
      package: ./plugins/dummy.tar.gz
    config:
      base_url: https://example.com
`,
		},
		{
			name: "plugin command or package is required for external",
			yaml: `
providers:
  external:
    {}
`,
			wantErr: true,
		},
		{
			name: "plugin package with version is rejected",
			yaml: `
providers:
  external:
    from:
      package: ./plugins/dummy.tar.gz
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "plugin source with version is valid",
			yaml: `
providers:
  external:
    from:
      source: github.com/acme-corp/tools/widget
      version: 1.2.3
`,
		},
		{
			name: "plugin source without version is rejected",
			yaml: `
providers:
  external:
    from:
      source: github.com/acme-corp/tools/widget
`,
			wantErr: true,
		},
		{
			name: "egress default_action allow is valid",
			yaml: `
egress:
  default_action: allow
`,
		},
		{
			name: "egress default_action deny is valid",
			yaml: `
egress:
  default_action: deny
`,
		},
		{
			name: "egress default_action invalid",
			yaml: `
egress:
  default_action: block
`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("Load: expected error, got nil")
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
			name: "package valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "./some-dir"}},
				},
			},
		},
		{
			name: "source valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
		},
		{
			name: "both package and source rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "./some-dir", Source: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "nil plugin rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {},
				},
			},
			wantErr: "requires a plugin",
		},
		{
			name: "source without version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: "github.com/test-org/test-repo/test-plugin"}},
				},
			},
			wantErr: "plugin.version is required",
		},
		{
			name: "package with version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "./some-dir", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.version is only valid with plugin.source",
		},
		{
			name: "http package rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "http://evil.com/pkg"}},
				},
			},
			wantErr: "HTTPS",
		},
		{
			name: "https package accepted",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "https://releases.example.com/pkg.tar.gz"}},
				},
			},
		},
		{
			name: "inline plugin with openapi accepted",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{OpenAPI: "https://example.com/spec.json"}},
				},
			},
		},
		{
			name: "inline plugin with auth and openapi accepted",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{
						OpenAPI: "https://example.com/spec.json",
						Auth:    &ConnectionAuthDef{Type: "oauth2"},
					}},
				},
			},
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
  service-a:
    icon_file: ../assets/service.svg
    from:
      command: ../bin/provider
  service-b:
    from:
      package: ../plugins/dummy.tar.gz
  service-c:
    from:
      package: https://example.com/dummy.tar.gz
`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Integrations["service-a"].IconFile; got != iconPath {
		t.Fatalf("IconFile = %q, want %q", got, iconPath)
	}
	if got := cfg.Integrations["service-a"].Plugin.Command; got != filepath.Join(dir, "bin", "provider") {
		t.Fatalf("integration plugin command = %q", got)
	}
	if got := cfg.Integrations["service-b"].Plugin.Package; got != filepath.Join(dir, "plugins", "dummy.tar.gz") {
		t.Fatalf("integration plugin package = %q, want %q", got, filepath.Join(dir, "plugins", "dummy.tar.gz"))
	}
	if got := cfg.Integrations["service-c"].Plugin.Package; got != "https://example.com/dummy.tar.gz" {
		t.Fatalf("HTTPS plugin package should not be resolved = %q", got)
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: auth-provider
  config:
    client_id: client-1
    client_secret: secret-1
    allowed_domain: example.test
datastore:
  provider: data-store
server:
  encryption_key: server-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if len(authCfg) != 3 {
		t.Fatalf("Auth.Config length = %d, want 3", len(authCfg))
	}
}

func TestLoadConfig_APITokenTTL(t *testing.T) {
	t.Parallel()

	t.Run("valid day duration", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  api_token_ttl: "14d"
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
  api_token_ttl: "not-a-duration"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid api_token_ttl")
		}
	})
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

func TestLoad_ResolvesRelativePluginPackagePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `providers:
  sample:
    from:
      package: ./my-plugin
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	plugin := loaded.Integrations["sample"].Plugin
	if plugin == nil {
		t.Fatal("expected plugin to be loaded")
	}
	if !filepath.IsAbs(plugin.Package) {
		t.Fatalf("expected absolute path, got: %q", plugin.Package)
	}
	if plugin.Package != pluginDir {
		t.Fatalf("plugin.Package = %q, want %q", plugin.Package, pluginDir)
	}
}

func TestIsInline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		plugin *PluginDef
		want   bool
	}{
		{name: "nil plugin", plugin: nil, want: false},
		{name: "external command", plugin: &PluginDef{Command: "/bin/test"}, want: false},
		{name: "external package", plugin: &PluginDef{Package: "./pkg"}, want: false},
		{name: "external source", plugin: &PluginDef{Source: "github.com/a/b/c", Version: "1.0.0"}, want: false},
		{name: "inline openapi", plugin: &PluginDef{OpenAPI: "https://example.com/spec.json"}, want: true},
		{name: "inline graphql", plugin: &PluginDef{GraphQLURL: "https://example.com/graphql"}, want: true},
		{name: "inline mcp", plugin: &PluginDef{MCPURL: "https://example.com/mcp"}, want: true},
		{name: "base_url only", plugin: &PluginDef{BaseURL: "https://api.example.com"}, want: true},
		{name: "inline operations", plugin: &PluginDef{Operations: []InlineOperationDef{{Name: "op"}}}, want: true},
		{name: "auth only", plugin: &PluginDef{Auth: &ConnectionAuthDef{Type: "oauth2"}}, want: true},
		{name: "connections only", plugin: &PluginDef{Connections: map[string]*ConnectionDef{"default": {}}}, want: true},
		{name: "openapi with base_url", plugin: &PluginDef{OpenAPI: "https://example.com/spec.json", BaseURL: "https://api.example.com"}, want: true},
		{name: "operations with auth", plugin: &PluginDef{Operations: []InlineOperationDef{{Name: "op"}}, Auth: &ConnectionAuthDef{Type: "oauth2"}}, want: true},
		{name: "empty plugin", plugin: &PluginDef{}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.plugin.IsInline()
			if got != tc.want {
				t.Fatalf("IsInline() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExternalPluginRejectsInlineOperations(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Integrations: map[string]IntegrationDef{
			"bad": {
				Plugin: &PluginDef{
					Command: "echo",
					Operations: []InlineOperationDef{
						{Name: "op", Method: "GET", Path: "/op"},
					},
				},
			},
		},
	}
	err := ValidateStructure(cfg)
	if err == nil {
		t.Fatal("expected validation error for command + inline operations")
	}
	if !strings.Contains(err.Error(), "external plugin cannot use inline operations") {
		t.Fatalf("error = %q, want to contain 'external plugin cannot use inline operations'", err)
	}
}

func TestExternalPluginAllowsSpecURL(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Integrations: map[string]IntegrationDef{
			"ok": {
				Plugin: &PluginDef{
					Command: "echo",
					OpenAPI: "https://example.com/spec.json",
				},
			},
		},
	}
	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
