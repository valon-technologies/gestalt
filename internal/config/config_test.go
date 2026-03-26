package config

import (
	"os"
	"path/filepath"
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
    client_id: integration-client
    upstreams:
      - type: rest
        url: https://example.test/spec.json
        allowed_operations:
          list_records: ""
          get_record: Get a record
      - type: mcp
        url: https://example.test/mcp
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
	if got := cfg.Integrations["service-a"].Upstreams[0].AllowedOperations["list_records"]; got != "" {
		t.Fatalf("AllowedOperations[list_records] = %q", got)
	}
	if got := cfg.Integrations["service-a"].Upstreams[0].AllowedOperations["get_record"]; got != "Get a record" {
		t.Fatalf("AllowedOperations[get_record] = %q", got)
	}
}

func TestLoadConfigDefaultsAndEnv(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		return map[string]string{
			"TEST_CLIENT_ID":    "client-from-env",
			"TEST_ENCRYPTION":   "encryption-from-env",
			"TEST_REDIRECT_URL": "https://app.example.test/callback",
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
auth_profiles:
  shared:
    client_id: profile-client
    client_secret: profile-secret
    redirect_url: ${TEST_REDIRECT_URL}
integrations:
  service-a:
    auth_profile: shared
    upstreams:
      - type: rest
        url: https://example.test/spec.json
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
	if got := cfg.Integrations["service-a"].ClientSecret; got != "profile-secret" {
		t.Fatalf("Integrations[service-a].ClientSecret = %q", got)
	}
	if got := cfg.Integrations["service-a"].RedirectURL; got != "https://app.example.test/callback" {
		t.Fatalf("Integrations[service-a].RedirectURL = %q", got)
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
	if got := cfg.Runtimes["worker"].Plugin.Command; got != filepath.Join(dir, "bin", "runtime") {
		t.Fatalf("runtime plugin command = %q", got)
	}
}

func TestLoadConfigValidation(t *testing.T) {
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
		{
			name: "rest upstream requires url",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    upstreams:
      - type: rest
`,
		},
		{
			name: "multiple api upstreams rejected",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    upstreams:
      - type: rest
        url: https://example.test/openapi.json
      - type: graphql
        url: https://example.test/graphql
`,
		},
		{
			name: "replace plugin cannot also define upstreams",
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
    upstreams:
      - type: rest
        url: https://example.test/spec.json
`,
		},
		{
			name: "overlay plugin requires a base source",
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
      mode: overlay
      command: /tmp/provider
`,
		},
		{
			name: "runtime overlay mode rejected",
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
      mode: overlay
      command: /tmp/runtime
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

func TestAllowedOperationsForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want map[string]string
	}{
		{
			name: "list",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    upstreams:
      - type: rest
        url: https://example.test/spec.json
        allowed_operations:
          - list_records
          - get_record
`,
			want: map[string]string{"list_records": "", "get_record": ""},
		},
		{
			name: "map",
			yaml: `
auth:
  provider: auth-provider
datastore:
  provider: data-store
server:
  encryption_key: server-key
integrations:
  service-a:
    upstreams:
      - type: rest
        url: https://example.test/spec.json
        allowed_operations:
          list_records: List all records
          get_record: ""
`,
			want: map[string]string{"list_records": "List all records", "get_record": ""},
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

			got := cfg.Integrations["service-a"].Upstreams[0].AllowedOperations
			if len(got) != len(tc.want) {
				t.Fatalf("AllowedOperations length = %d, want %d", len(got), len(tc.want))
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Fatalf("AllowedOperations[%q] = %q, want %q", key, got[key], want)
				}
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
