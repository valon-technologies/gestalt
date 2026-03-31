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
integrations:
  service-a:
    display_name: Service A
    plugin:
      command: /usr/bin/provider
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
	if got := cfg.Integrations["service-a"].Plugin.Command; got != "/usr/bin/provider" {
		t.Fatalf("Plugin.Command = %q", got)
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
integrations:
  service-a:
    plugin:
      command: /usr/bin/provider
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

func TestLoadConfigEnvValueOverridesFile(t *testing.T) {
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
		case "TEST_ENCRYPTION":
			return "env-secret", true
		case "TEST_ENCRYPTION_FILE":
			return secretPath, true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}

	if cfg.Server.EncryptionKey != "env-secret" {
		t.Fatalf("Server.EncryptionKey = %q, want env-secret", cfg.Server.EncryptionKey)
	}
}

func TestLoadConfigEnvFileReadError(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: ${TEST_ENCRYPTION}
`)

	_, err := LoadWithLookup(path, func(key string) (string, bool) {
		switch key {
		case "TEST_ENCRYPTION_FILE":
			return "/does/not/exist", true
		default:
			return "", false
		}
	})
	if err == nil {
		t.Fatal("expected error for unreadable *_FILE path")
	}
	if !strings.Contains(err.Error(), "TEST_ENCRYPTION_FILE") {
		t.Fatalf("expected *_FILE context in error, got: %v", err)
	}
}

func TestLoadWithMappingEmptyValueDoesNotFallbackToFile(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
  base_url: ${TEST_BASE_URL}
`)

	cfg, err := LoadWithMapping(path, func(key string) string {
		switch key {
		case "TEST_BASE_URL":
			return ""
		case "TEST_BASE_URL_FILE":
			return "/tmp/should-not-be-used"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadWithMapping: %v", err)
	}
	if cfg.Server.BaseURL != "" {
		t.Fatalf("Server.BaseURL = %q, want empty string", cfg.Server.BaseURL)
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
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    icon_file: ../assets/service.svg
    plugin:
      command: ../bin/provider
  service-b:
    plugin:
      package: ../plugins/dummy.tar.gz
  service-c:
    plugin:
      package: https://example.com/dummy.tar.gz
  service-d:
    plugin:
      openapi: ../specs/service-d.json
runtimes:
  worker:
    plugin:
      command: ../bin/runtime
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
	if got := cfg.Integrations["service-d"].Plugin.OpenAPI; got != filepath.Join(dir, "specs", "service-d.json") {
		t.Fatalf("Plugin.OpenAPI = %q, want %q", got, filepath.Join(dir, "specs", "service-d.json"))
	}
	if got := cfg.Runtimes["worker"].Plugin.Command; got != filepath.Join(dir, "bin", "runtime") {
		t.Fatalf("runtime plugin command = %q", got)
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
			name: "missing encryption key",
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

func TestLoadSucceedsWithoutRuntimeFields(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
integrations:
  custom_tool:
    plugin:
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

func TestValidateRuntimeRequiresEncryptionKey(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: auth-provider
datastore:
  provider: data-store
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := ValidateRuntime(cfg); err == nil {
		t.Fatal("expected error for missing encryption_key, got nil")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "integration without plugin",
			yaml: `
integrations:
  service-a:
    display_name: Service A
`,
		},
		{
			name: "runtime requires type or plugin",
			yaml: `
runtimes:
  worker: {}
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
			name: "plugin only with command",
			yaml: `
integrations:
  custom_tool:
    plugin:
      command: /usr/bin/provider
`,
		},
		{
			name: "plugin only with package",
			yaml: `
integrations:
  custom_tool:
    plugin:
      package: https://example.com/custom-tool.tar.gz
`,
		},
		{
			name: "inline plugin with openapi",
			yaml: `
integrations:
  github:
    plugin:
      openapi: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
      auth:
        type: oauth2
        authorization_url: https://github.com/login/oauth/authorize
        token_url: https://github.com/login/oauth/access_token
`,
		},
		{
			name: "inline plugin with operations",
			yaml: `
integrations:
  weather:
    plugin:
      base_url: https://api.weather.test
      operations:
        - name: get_forecast
          description: Get the weather forecast
          method: GET
          path: /forecast
          parameters:
            - name: city
              type: string
              in: query
              required: true
`,
		},
		{
			name: "inline plugin with mcp_url",
			yaml: `
integrations:
  remote_mcp:
    plugin:
      mcp_url: https://example.test/mcp
`,
		},
		{
			name: "inline plugin with connections",
			yaml: `
integrations:
  multi:
    plugin:
      openapi: https://example.test/spec.json
      connections:
        api_conn:
          mode: user
          auth:
            type: oauth2
            authorization_url: https://example.test/auth
            token_url: https://example.test/token
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
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
`,
		},
		{
			name: "runtime plugin package is valid",
			yaml: `
runtimes:
  worker:
    plugin:
      package: https://example.com/dummy.tar.gz
`,
		},
		{
			name: "plugin package and command are mutually exclusive",
			yaml: `
integrations:
  external:
    plugin:
      command: /tmp/plugin
      package: ./plugins/dummy.tar.gz
`,
			wantErr: true,
		},
		{
			name: "plugin args require command not package",
			yaml: `
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
      args:
        - --verbose
`,
			wantErr: true,
		},
		{
			name: "plugin env with package is valid",
			yaml: `
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
      env:
        FOO: bar
`,
		},
		{
			name: "plugin config with package is valid",
			yaml: `
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
      config:
        base_url: https://example.com
`,
		},
		{
			name: "runtime plugin config must be sibling config block",
			yaml: `
runtimes:
  worker:
    plugin:
      command: /tmp/runtime
      config:
        poll_interval: 30s
`,
			wantErr: true,
		},
		{
			name: "runtime requires type or plugin",
			yaml: `
runtimes:
  worker: {}
`,
			wantErr: true,
		},
		{
			name: "runtime plugin cannot also define type",
			yaml: `
runtimes:
  worker:
    type: echo
    plugin:
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "integration requires plugin",
			yaml: `
integrations:
  external:
    display_name: Missing Plugin
`,
			wantErr: true,
		},
		{
			name: "plugin package with version is rejected",
			yaml: `
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "plugin command with version is rejected",
			yaml: `
integrations:
  external:
    plugin:
      command: /tmp/plugin
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "plugin source without version is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: example.com/org/repo/plugin
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
		{
			name: "egress credential auth_style bearer is valid",
			yaml: `
egress:
  credentials:
    - host: api.vendor.test
      secret_ref: vendor-key
      auth_style: bearer
`,
		},
		{
			name: "egress credential with no match criterion rejected",
			yaml: `
egress:
  credentials:
    - secret_ref: vendor-key
      auth_style: bearer
`,
			wantErr: true,
		},
		{
			name: "plugin source with version is valid",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
      version: 1.2.3
`,
		},
		{
			name: "plugin source with command is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
      version: 1.0.0
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "plugin source with package is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
      version: 1.0.0
      package: ./plugins/dummy.tar.gz
`,
			wantErr: true,
		},
		{
			name: "plugin source missing plugin segment is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "plugin source with uppercase is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/Acme-Corp/tools/widget
      version: 1.0.0
`,
			wantErr: true,
		},
		{
			name: "plugin source with leading v in version is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
      version: v1.0.0
`,
			wantErr: true,
		},
		{
			name: "external plugin with inline fields is rejected",
			yaml: `
integrations:
  external:
    plugin:
      command: /tmp/plugin
      openapi: https://example.test/spec.json
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
	if authCfg["allowed_domain"] != "example.test" {
		t.Fatalf("Auth.Config.allowed_domain = %q", authCfg["allowed_domain"])
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

	t.Run("zero day duration rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  api_token_ttl: "0d"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for zero api_token_ttl")
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
			wantErr: "requires a plugin definition",
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
			name: "command with version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Command: "/usr/bin/plugin", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.version is only valid with plugin.source",
		},
		{
			name: "args without command rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Package: "./some-dir", Args: []string{"--verbose"}}},
				},
			},
			wantErr: "plugin.args are only valid with plugin.command",
		},
		{
			name: "invalid source address rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{Source: "not-a-valid-source", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.source",
		},
		{
			name: "runtime plugin with type rejected",
			cfg: &Config{
				Runtimes: map[string]RuntimeDef{
					"worker": {Type: "grpc", Plugin: &PluginDef{Command: "/usr/bin/runtime"}},
				},
			},
			wantErr: "cannot set both plugin and type",
		},
		{
			name: "inline plugin with openapi is valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{OpenAPI: "https://example.test/spec.json"}},
				},
			},
		},
		{
			name: "inline plugin with operations is valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{
						BaseURL: "https://api.test",
						Operations: []InlineOperationDef{
							{Name: "get_item", Method: "GET", Path: "/items/{id}"},
						},
					}},
				},
			},
		},
		{
			name: "external plugin with inline fields is rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &PluginDef{
						Command: "/usr/bin/plugin",
						OpenAPI: "https://example.test/spec.json",
					}},
				},
			},
			wantErr: "cannot have both external reference",
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

func TestLoad_ResolvesRelativePluginPackagePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  sample:
    plugin:
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

func TestPluginIsInline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		plugin *PluginDef
		want   bool
	}{
		{name: "nil", plugin: nil, want: false},
		{name: "external command", plugin: &PluginDef{Command: "/bin/p"}, want: false},
		{name: "external package", plugin: &PluginDef{Package: "./pkg"}, want: false},
		{name: "external source", plugin: &PluginDef{Source: "github.com/x/y/z", Version: "1.0.0"}, want: false},
		{name: "openapi", plugin: &PluginDef{OpenAPI: "https://example.test/spec.json"}, want: true},
		{name: "graphql_url", plugin: &PluginDef{GraphQLURL: "https://example.test/graphql"}, want: true},
		{name: "mcp_url", plugin: &PluginDef{MCPURL: "https://example.test/mcp"}, want: true},
		{name: "base_url", plugin: &PluginDef{BaseURL: "https://api.test"}, want: true},
		{name: "operations", plugin: &PluginDef{Operations: []InlineOperationDef{{Name: "op"}}}, want: true},
		{name: "auth only", plugin: &PluginDef{Auth: &InlineAuthDef{Type: "oauth2"}}, want: true},
		{name: "connections", plugin: &PluginDef{Connections: map[string]*InlineConnectionDef{"c": {}}}, want: true},
		{name: "empty", plugin: &PluginDef{}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.plugin.IsInline(); got != tc.want {
				t.Fatalf("IsInline() = %v, want %v", got, tc.want)
			}
		})
	}
}
