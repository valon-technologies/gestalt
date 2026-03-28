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
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
          client_id: integration-client
          client_secret: integration-secret
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
    mcp:
      url: https://example.test/mcp
      connection: default
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
	if got := cfg.Integrations["service-a"].Connections["default"].Auth.ClientID; got != "integration-client" {
		t.Fatalf("Connections[default].Auth.ClientID = %q", got)
	}
	if got := cfg.Integrations["service-a"].API.Type; got != "rest" {
		t.Fatalf("API.Type = %q", got)
	}
	if got := cfg.Integrations["service-a"].MCP.URL; got != "https://example.test/mcp" {
		t.Fatalf("MCP.URL = %q", got)
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
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
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

func TestLoadConfigBaseURLResolvesRedirectURL(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
  base_url: https://app.example.test
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.Integrations["service-a"].Connections["default"].Auth.RedirectURL
	want := "https://app.example.test/api/v1/auth/callback"
	if got != want {
		t.Fatalf("RedirectURL = %q, want %q", got, want)
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
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: ../specs/service-d.json
      connection: default
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
	if got := cfg.Integrations["service-d"].API.OpenAPI; got != filepath.Join(dir, "specs", "service-d.json") {
		t.Fatalf("API.OpenAPI = %q, want %q", got, filepath.Join(dir, "specs", "service-d.json"))
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
			name: "declarative integration with no connections",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
`,
		},
		{
			name: "declarative integration with neither api nor mcp",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
`,
		},
		{
			name: "plugin with connections",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    plugin:
      command: /tmp/provider
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
`,
		},
		{
			name: "plugin with api",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    plugin:
      command: /tmp/provider
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
`,
		},
		{
			name: "api type rest without openapi",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      connection: default
`,
		},
		{
			name: "api type graphql without url",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: graphql
      connection: default
`,
		},
		{
			name: "mcp without url",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    mcp:
      connection: default
`,
		},
		{
			name: "surface references missing connection",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: missing
`,
		},
		{
			name: "mixed connection modes",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      api_conn:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
      mcp_conn:
        mode: identity
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: api_conn
    mcp:
      url: https://example.test/mcp
      connection: mcp_conn
`,
		},
		{
			name: "url template references undeclared param",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: graphql
      url: https://{subdomain}.example.test/graphql
      connection: default
`,
		},
		{
			name: "auth url template references undeclared param",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://{subdomain}.example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
`,
		},
		{
			name: "runtime requires type or plugin",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
runtimes:
  worker: {}
`,
		},
		{
			name: "egress default action must be allow or deny",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
			name: "rest integration",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  github:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://github.com/login/oauth/authorize
          token_url: https://github.com/login/oauth/access_token
          client_id: test-client
          client_secret: test-secret
    api:
      type: rest
      openapi: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
      connection: default
`,
		},
		{
			name: "graphql plus mcp with separate connections",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  shop:
    connections:
      api_conn:
        mode: user
        params:
          subdomain:
            required: true
        auth:
          type: oauth2
          authorization_url: https://accounts.shopify.com/oauth/authorize
          token_url: https://accounts.shopify.com/oauth/token
          client_id: test-client
          client_secret: test-secret
      mcp_conn:
        mode: user
        params:
          subdomain:
            required: true
        auth:
          type: oauth2
          authorization_url: https://mcp-auth.example.com/authorize
          token_url: https://mcp-auth.example.com/token
          client_id: mcp-client
          client_secret: mcp-secret
    api:
      type: graphql
      url: https://{subdomain}.myshopify.com/admin/api/graphql.json
      connection: api_conn
    mcp:
      url: https://tenant-{subdomain}.mcp.example.com
      connection: mcp_conn
`,
		},
		{
			name: "plugin only",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  custom_tool:
    plugin:
      package: https://example.com/custom-tool.tar.gz
`,
		},
		{
			name: "shared connection across api and mcp",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service:
    connections:
      shared:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
          client_id: test-client
          client_secret: test-secret
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: shared
    mcp:
      url: https://example.test/mcp
      connection: shared
`,
		},
		{
			name: "mcp only declarative",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    mcp:
      url: https://example.test/mcp
      connection: default
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  external:
    plugin:
      package: ./plugins/dummy.tar.gz
`,
		},
		{
			name: "runtime plugin package is valid",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
runtimes:
  worker:
    plugin:
      package: https://example.com/dummy.tar.gz
`,
		},
		{
			name: "plugin package and command are mutually exclusive",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
runtimes:
  worker: {}
`,
			wantErr: true,
		},
		{
			name: "runtime plugin cannot also define type",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
runtimes:
  worker:
    type: echo
    plugin:
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "plugin command or package is required",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  external:
    plugin: {}
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
egress:
  default_action: allow
`,
		},
		{
			name: "egress default_action deny is valid",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
egress:
  default_action: deny
`,
		},
		{
			name: "egress default_action invalid",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
egress:
  default_action: block
`,
			wantErr: true,
		},
		{
			name: "egress credential auth_style bearer is valid",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
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
			name: "plugin source without version is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
`,
			wantErr: true,
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
			name: "plugin source with args is rejected",
			yaml: `
integrations:
  external:
    plugin:
      source: github.com/acme-corp/tools/widget
      version: 1.0.0
      args:
        - --verbose
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
					"sample": {Plugin: &ExecutablePluginDef{Package: "./some-dir"}},
				},
			},
		},
		{
			name: "source valid",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Source: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
		},
		{
			name: "both package and source rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Package: "./some-dir", Source: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "neither package nor source nor command rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{}},
				},
			},
			wantErr: "plugin.command, plugin.package, or plugin.source is required",
		},
		{
			name: "source without version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Source: "github.com/test-org/test-repo/test-plugin"}},
				},
			},
			wantErr: "plugin.version is required",
		},
		{
			name: "package with version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Package: "./some-dir", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.version is only valid with plugin.source",
		},
		{
			name: "http package rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Package: "http://evil.com/pkg"}},
				},
			},
			wantErr: "HTTPS",
		},
		{
			name: "https package accepted",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Package: "https://releases.example.com/pkg.tar.gz"}},
				},
			},
		},
		{
			name: "command with version rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Command: "/usr/bin/plugin", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.version is only valid with plugin.source",
		},
		{
			name: "args without command rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Package: "./some-dir", Args: []string{"--verbose"}}},
				},
			},
			wantErr: "plugin.args are only valid with plugin.command",
		},
		{
			name: "invalid source address rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {Plugin: &ExecutablePluginDef{Source: "not-a-valid-source", Version: "1.0.0"}},
				},
			},
			wantErr: "plugin.source",
		},
		{
			name: "plugin with connections rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {
						Connections: map[string]ConnectionDef{"default": {Mode: "user"}},
						Plugin:     &ExecutablePluginDef{Command: "/usr/bin/plugin"},
					},
				},
			},
			wantErr: "cannot set both plugin and connections",
		},
		{
			name: "plugin with api rejected",
			cfg: &Config{
				Integrations: map[string]IntegrationDef{
					"sample": {
						Plugin: &ExecutablePluginDef{Command: "/usr/bin/plugin"},
						API:    &APIDef{Type: "rest"},
					},
				},
			},
			wantErr: "cannot set both plugin and api",
		},
		{
			name: "runtime plugin with type rejected",
			cfg: &Config{
				Runtimes: map[string]RuntimeDef{
					"worker": {Type: "grpc", Plugin: &ExecutablePluginDef{Command: "/usr/bin/runtime"}},
				},
			},
			wantErr: "cannot set both plugin and type",
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

func TestHasManagedArtifacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		plugin *ExecutablePluginDef
		want   bool
	}{
		{"nil plugin", nil, false},
		{"command only", &ExecutablePluginDef{Command: "/bin/x"}, false},
		{"package set", &ExecutablePluginDef{Package: "./dir"}, true},
		{"source set", &ExecutablePluginDef{Source: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.plugin.HasManagedArtifacts(); got != tc.want {
				t.Fatalf("HasManagedArtifacts() = %v, want %v", got, tc.want)
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

func TestAllowedOperations(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
integrations:
  service-a:
    connections:
      default:
        mode: user
        auth:
          type: oauth2
          authorization_url: https://example.test/auth
          token_url: https://example.test/token
    api:
      type: rest
      openapi: https://example.test/spec.json
      connection: default
      allowed_operations:
        list_records:
          alias: fetch_records
          description: Retrieve all records
        get_record:
        delete_record:
          alias: remove_record
    mcp:
      url: https://example.test/mcp
      connection: default
      allowed_operations:
        run_query:
          alias: execute_query
          description: Run a database query
        list_tables:
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	api := cfg.Integrations["service-a"].API
	if api.AllowedOperations == nil {
		t.Fatal("API.AllowedOperations is nil")
	}
	if len(api.AllowedOperations) != 3 {
		t.Fatalf("API.AllowedOperations length = %d, want 3", len(api.AllowedOperations))
	}

	listOp := api.AllowedOperations["list_records"]
	if listOp == nil {
		t.Fatal("list_records override is nil")
	}
	if listOp.Alias != "fetch_records" {
		t.Errorf("list_records alias = %q, want fetch_records", listOp.Alias)
	}
	if listOp.Description != "Retrieve all records" {
		t.Errorf("list_records description = %q", listOp.Description)
	}

	if api.AllowedOperations["get_record"] != nil {
		t.Error("get_record override should be nil for bare key")
	}

	deleteOp := api.AllowedOperations["delete_record"]
	if deleteOp == nil {
		t.Fatal("delete_record override is nil")
	}
	if deleteOp.Alias != "remove_record" {
		t.Errorf("delete_record alias = %q, want remove_record", deleteOp.Alias)
	}

	mcp := cfg.Integrations["service-a"].MCP
	if mcp.AllowedOperations == nil {
		t.Fatal("MCP.AllowedOperations is nil")
	}
	if len(mcp.AllowedOperations) != 2 {
		t.Fatalf("MCP.AllowedOperations length = %d, want 2", len(mcp.AllowedOperations))
	}

	runOp := mcp.AllowedOperations["run_query"]
	if runOp == nil {
		t.Fatal("run_query override is nil")
	}
	if runOp.Alias != "execute_query" {
		t.Errorf("run_query alias = %q, want execute_query", runOp.Alias)
	}
	if runOp.Description != "Run a database query" {
		t.Errorf("run_query description = %q", runOp.Description)
	}

	if mcp.AllowedOperations["list_tables"] != nil {
		t.Error("list_tables override should be nil for bare key")
	}
}
