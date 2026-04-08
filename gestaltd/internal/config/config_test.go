package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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
auth:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
  config:
    client_id: client-1
    client_secret: secret-1
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
  config:
    path: /tmp/gestalt.db
server:
  encryption_key: server-key
  public:
    host: 127.0.0.1
    port: 9090
  management:
    host: 127.0.0.1
    port: 9191
plugins:
  service-a:
    display_name: Service A
    provider:
      source:
        path: /tmp/plugin.yaml
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
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
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
  config:
    client_id: ${TEST_CLIENT_ID}
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
  config:
    path: /tmp/gestalt.db
server:
  encryption_key: ${TEST_ENCRYPTION}
plugins:
  service-a:
    provider:
      source:
        path: /tmp/plugin.yaml
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

	if cfg.Auth.Provider == nil {
		t.Fatal("Auth.Provider = nil")
	}
	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if authCfg["client_id"] != "client-from-env" {
		t.Fatalf("Auth.Config.client_id = %#v", authCfg["client_id"])
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
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
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
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "missing auth provider is allowed",
			yaml: `
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
`,
			wantErr: false,
		},
		{
			name: "missing datastore provider",
			yaml: `
auth:
  provider: none
server:
  encryption_key: server-key
`,
			wantErr: true,
		},
		{
			name: "missing encryption key",
			yaml: `
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "auth provider none is allowed",
			yaml: `
auth:
  provider: none
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
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
			if tc.name == "auth provider none is allowed" && cfg.Auth.Provider != nil {
				t.Fatalf("Auth.Provider = %#v, want nil", cfg.Auth.Provider)
			}
		})
	}
}

func TestLoadRejectsLegacyTopLevelAuthDatastoreFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "legacy auth plugin field rejected",
			yaml: `
auth:
  plugin:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/oidc
      version: 1.0.0
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
`,
			wantErr: "field plugin not found in type config.AuthConfig",
		},
		{
			name: "legacy datastore plugin field rejected",
			yaml: `
auth:
  provider: none
datastore:
  plugin:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
`,
			wantErr: "field plugin not found in type config.DatastoreConfig",
		},
		{
			name: "auth config accepted",
			yaml: `
auth:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/oidc
      version: 1.0.0
  config:
    issuer_url: https://issuer.example.test
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
`,
			wantErr: "",
		},
		{
			name: "datastore config accepted",
			yaml: `
auth:
  provider: none
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
  config:
    path: /tmp/gestalt.db
server:
  encryption_key: server-key
`,
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadSucceedsWithoutRuntimeFields(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
plugins:
  custom_tool:
    provider:
      source:
        path: ./plugin.yaml
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Integrations["custom_tool"].Plugin.SourcePath(); got != filepath.Join(filepath.Dir(path), "plugin.yaml") {
		t.Fatalf("unexpected plugin source path: %q", got)
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
plugins:
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
			name: "managed source plugin only",
			yaml: `
plugins:
  custom_tool:
    provider:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
		},
		{
			name: "plugin with local source",
			yaml: `
plugins:
  service:
    provider:
      source:
        path: /usr/bin/provider.yaml
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
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
`,
		},
		{
			name: "plugin source path and ref are mutually exclusive",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
			wantErr: "mutually exclusive",
		},
		{
			name: "plugin env with local source is valid",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
      env:
        FOO: bar
`,
		},
		{
			name: "plugin config with source is valid",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
    config:
      base_url: https://example.com
`,
		},
		{
			name: "plugin source is required for external",
			yaml: `
plugins:
  external:
    {}
`,
			wantErr: "requires a plugin",
		},
		{
			name: "plugin source path with version is rejected",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
        version: 1.0.0
`,
			wantErr: "plugin.source.version is only valid",
		},
		{
			name: "plugin source with version is valid",
			yaml: `
plugins:
  external:
    provider:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
`,
		},
		{
			name: "plugin source with base_url override is rejected",
			yaml: `
plugins:
  external:
    provider:
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
plugins:
  external:
    provider:
      source:
        ref: github.com/acme-corp/tools/widget
`,
			wantErr: "plugin.source.version is required",
		},
		{
			name: "non-default connection params are rejected",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
    connections:
      named:
        mode: user
        auth:
          type: none
        params:
          team:
            required: true
`,
			wantErr: "connections.named.params are only supported on connections.default",
		},
		{
			name: "non-default connection discovery is rejected",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
    connections:
      named:
        mode: user
        auth:
          type: none
        discovery:
          url: https://example.com/connections
`,
			wantErr: "connections.named.discovery is only supported on connections.default",
		},
		{
			name: "mcp tool prefix requires mcp enabled",
			yaml: `
plugins:
  external:
    provider:
      source:
        path: ./plugins/dummy/plugin.yaml
    mcp:
      tool_prefix: external_
`,
			wantErr: "mcp.tool_prefix is only valid when mcp.enabled is true",
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
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: &PluginSourceDef{Path: "./some-dir/plugin.yaml"}}},
				},
			},
		},
		{
			name: "source valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: &PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}}},
				},
			},
		},
		{
			name: "source path and ref rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: &PluginSourceDef{Path: "./plugin.yaml", Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}}},
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
					"sample": {Plugin: &PluginDef{Source: &PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin"}}},
				},
			},
			wantErr: "plugin.source.version is required",
		},
		{
			name: "source version without ref rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: &PluginSourceDef{Version: "1.0.0"}}},
				},
			},
			wantErr: "plugin.source.path or plugin.source.ref is required",
		},
		{
			name: "auth provider valid",
			cfg: &Config{
				Auth: AuthConfig{
					Provider: &PluginDef{Source: &PluginSourceDef{Ref: "github.com/test-org/test-repo/test-auth", Version: "1.0.0"}},
				},
			},
		},
		{
			name: "auth provider none valid",
			cfg:  &Config{},
		},
		{
			name: "datastore provider valid",
			cfg: &Config{
				Datastore: DatastoreConfig{
					Provider: &PluginDef{Source: &PluginSourceDef{Path: "./some-dir/plugin.yaml"}},
				},
			},
		},
		{
			name: "auth provider invalid when source missing",
			cfg: &Config{
				Auth: AuthConfig{
					Provider: &PluginDef{},
				},
			},
			wantErr: `provider.source.path or provider.source.ref is required`,
		},
		{
			name: "auth config invalid when auth disabled",
			cfg: &Config{
				Auth: AuthConfig{
					Config: yaml.Node{Kind: yaml.MappingNode},
				},
			},
			wantErr: `auth.config is not supported when auth.provider is unset`,
		},
		{
			name: "plugin auth rejects mcp oauth early",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {
						Plugin: &PluginDef{
							Source: &PluginSourceDef{Path: "./plugin.yaml"},
							Auth:   &ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeMCPOAuth},
						},
					},
				},
			},
			wantErr: `plugin auth type "mcp_oauth" requires an MCP surface`,
		},
		{
			name: "named connection rejects mcp oauth early",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {
						Plugin: &PluginDef{
							Source: &PluginSourceDef{Path: "./plugin.yaml"},
							Connections: map[string]*ConnectionDef{
								"default": {Auth: ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeMCPOAuth}},
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
auth:
  provider:
    source:
      path: ../auth-plugin/plugin.yaml
datastore:
  provider:
    source:
      path: ../datastore-plugin/plugin.yaml
plugins:
  service-a:
    icon_file: ../assets/service.svg
    provider:
      source:
        path: ../bin/plugin.yaml
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
	if got := cfg.Auth.Provider.SourcePath(); got != filepath.Join(dir, "auth-plugin", "plugin.yaml") {
		t.Fatalf("auth plugin source path = %q, want %q", got, filepath.Join(dir, "auth-plugin", "plugin.yaml"))
	}
	if got := cfg.Datastore.Provider.SourcePath(); got != filepath.Join(dir, "datastore-plugin", "plugin.yaml") {
		t.Fatalf("datastore plugin source path = %q, want %q", got, filepath.Join(dir, "datastore-plugin", "plugin.yaml"))
	}
	if got := cfg.Integrations["service-a"].Plugin.SourcePath(); got != filepath.Join(dir, "bin", "plugin.yaml") {
		t.Fatalf("integration plugin source path = %q, want %q", got, filepath.Join(dir, "bin", "plugin.yaml"))
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/auth/google
      version: 1.0.0
  config:
    client_id: client-1
    client_secret: secret-1
    allowed_domain: example.test
datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 1.0.0
server:
  encryption_key: server-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Auth.Provider == nil {
		t.Fatal("Auth.Provider = nil")
	}
	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if len(authCfg) != 3 {
		t.Fatalf("Auth.Config length = %d, want 3", len(authCfg))
	}
	if authCfg["client_id"] != "client-1" {
		t.Fatalf("Auth.Config.client_id = %#v", authCfg["client_id"])
	}
	if authCfg["client_secret"] != "secret-1" {
		t.Fatalf("Auth.Config.client_secret = %#v", authCfg["client_secret"])
	}
	if authCfg["allowed_domain"] != "example.test" {
		t.Fatalf("Auth.Config.allowed_domain = %#v", authCfg["allowed_domain"])
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

func TestLoad_ResolvesRelativePluginSourcePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `plugins:
  sample:
    provider:
      source:
        path: ./my-plugin/plugin.yaml
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
	if !filepath.IsAbs(plugin.SourcePath()) {
		t.Fatalf("expected absolute path, got: %q", plugin.SourcePath())
	}
	wantPath := filepath.Join(pluginDir, "plugin.yaml")
	if plugin.SourcePath() != wantPath {
		t.Fatalf("plugin.SourcePath() = %q, want %q", plugin.SourcePath(), wantPath)
	}
}

func TestLoadRejectsLegacyInlinePluginFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "legacy openapi field rejected",
			yaml: `
plugins:
  example:
    provider:
      source:
        path: ./plugin.yaml
      openapi: https://example.com/spec.json
`,
			wantErr: "field openapi not found",
		},
		{
			name: "legacy operations field rejected",
			yaml: `
plugins:
  example:
    provider:
      source:
        path: ./plugin.yaml
      operations:
        - name: get_thing
          method: GET
          path: /things
`,
			wantErr: "field operations not found",
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
