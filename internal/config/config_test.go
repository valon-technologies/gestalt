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
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
  config:
    client_id: my-client-id
    client_secret: my-secret
datastore:
  provider: sqlite
  config:
    path: ./data.db
integrations:
  alpha:
    client_id: key-1
  beta:
    upstreams:
      - type: rest
        url: https://example.com/spec.json
    client_id: key-2
server:
  port: 9090
  encryption_key: super-secret-key
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Auth.Provider != "google" {
		t.Errorf("Auth.Provider: got %q, want %q", cfg.Auth.Provider, "google")
	}
	if cfg.Datastore.Provider != "sqlite" {
		t.Errorf("Datastore.Provider: got %q, want %q", cfg.Datastore.Provider, "sqlite")
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port: got %d, want %d", cfg.Server.Port, 9090)
	}
	if cfg.Server.EncryptionKey != "super-secret-key" {
		t.Errorf("Server.EncryptionKey: got %q, want %q", cfg.Server.EncryptionKey, "super-secret-key")
	}
	if len(cfg.Integrations) != 2 {
		t.Fatalf("Integrations: got %d items, want 2", len(cfg.Integrations))
	}
	if cfg.Integrations["alpha"].ClientID != "key-1" {
		t.Errorf("Integrations[alpha].ClientID: got %q, want %q", cfg.Integrations["alpha"].ClientID, "key-1")
	}
	if len(cfg.Integrations["beta"].Upstreams) != 1 || cfg.Integrations["beta"].Upstreams[0].URL != "https://example.com/spec.json" {
		t.Errorf("Integrations[beta].Upstreams: got %+v", cfg.Integrations["beta"].Upstreams)
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port default: got %d, want 8080", cfg.Server.Port)
	}
}

func TestEnvVarResolution(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		return map[string]string{
			"GESTALT_TEST_CLIENT_ID": "env-client-id",
			"GESTALT_TEST_ENC_KEY":   "env-encryption-key",
		}[key]
	}

	yaml := `
auth:
  provider: google
  config:
    client_id: ${GESTALT_TEST_CLIENT_ID}
datastore:
  provider: sqlite
server:
  encryption_key: ${GESTALT_TEST_ENC_KEY}
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := LoadWithMapping(path, getenv)
	if err != nil {
		t.Fatalf("LoadWithMapping: unexpected error: %v", err)
	}

	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if authCfg["client_id"] != "env-client-id" {
		t.Errorf("Auth.Config.client_id: got %q, want %q", authCfg["client_id"], "env-client-id")
	}
	if cfg.Server.EncryptionKey != "env-encryption-key" {
		t.Errorf("Server.EncryptionKey: got %q, want %q", cfg.Server.EncryptionKey, "env-encryption-key")
	}
}

func TestEnvVarUnsetResolvesToEmpty(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "" }

	yaml := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  dev_mode: true
  encryption_key: ${GESTALT_TEST_NONEXISTENT}
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := LoadWithMapping(path, getenv)
	if err != nil {
		t.Fatalf("LoadWithMapping: unexpected error: %v", err)
	}

	if cfg.Server.EncryptionKey != "" {
		t.Errorf("Server.EncryptionKey: got %q, want empty string", cfg.Server.EncryptionKey)
	}
}

func TestLoadResolvesPathsRelativeToConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	iconDir := filepath.Join(dir, "icons")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	iconPath := filepath.Join(iconDir, "test.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg/>`), 0644); err != nil {
		t.Fatalf("WriteFile icon: %v", err)
	}

	cfgPath := filepath.Join(dir, "configs", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}
	content := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  myapi:
    icon_file: ../icons/test.svg
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if got := cfg.Integrations["myapi"].IconFile; got != iconPath {
		t.Fatalf("IconFile = %q, want %q", got, iconPath)
	}
}

func TestLoadResolvesPluginCommandPathsRelativeToConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configs", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}

	content := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  external:
    plugin:
      command: ../bin/provider
runtimes:
  worker:
    plugin:
      command: ../bin/runtime
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if got := cfg.Integrations["external"].Plugin.Command; got != filepath.Join(dir, "bin", "provider") {
		t.Fatalf("integration plugin command = %q", got)
	}
	if got := cfg.Runtimes["worker"].Plugin.Command; got != filepath.Join(dir, "bin", "runtime") {
		t.Fatalf("runtime plugin command = %q", got)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "missing auth provider",
			yaml:    "datastore:\n  provider: sqlite\nserver:\n  encryption_key: key123\n",
			wantErr: true,
		},
		{
			name:    "missing datastore provider",
			yaml:    "auth:\n  provider: google\nserver:\n  encryption_key: key123\n",
			wantErr: true,
		},
		{
			name:    "missing encryption key",
			yaml:    "auth:\n  provider: google\ndatastore:\n  provider: sqlite\nserver:\n  port: 8080\n",
			wantErr: true,
		},
		{
			name:    "dev mode skips encryption key",
			yaml:    "auth:\n  provider: google\ndatastore:\n  provider: sqlite\nserver:\n  dev_mode: true\n",
			wantErr: false,
		},
		{
			name: "rest upstream requires url",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  api:
    upstreams:
      - type: rest
`,
			wantErr: true,
		},
		{
			name: "multiple api upstreams rejected",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  api:
    upstreams:
      - type: rest
        url: https://example.com/openapi.json
      - type: graphql
        url: https://example.com/graphql
`,
			wantErr: true,
		},
		{
			name: "integration plugin cannot also define upstreams",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  external:
    plugin:
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantErr: true,
		},
		{
			name: "overlay plugin with upstreams is valid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: overlay
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantErr: false,
		},
		{
			name: "overlay plugin without upstreams",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: overlay
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "replace plugin with upstreams rejected",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: replace
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantErr: true,
		},
		{
			name: "empty mode defaults to replace rejects upstreams",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantErr: true,
		},
		{
			name: "unknown plugin mode",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: turbo
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "runtime overlay mode rejected",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
runtimes:
  worker:
    plugin:
      mode: overlay
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "runtime requires type or plugin",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
runtimes:
  worker: {}
`,
			wantErr: true,
		},
		{
			name: "runtime plugin cannot also define type",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
runtimes:
  worker:
    type: echo
    plugin:
      command: /tmp/plugin
`,
			wantErr: true,
		},
		{
			name: "plugin command is required",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  external:
    plugin: {}
`,
			wantErr: true,
		},
		{
			name: "egress default_action allow is valid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress:
  default_action: allow
`,
			wantErr: false,
		},
		{
			name: "egress default_action deny is valid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress:
  default_action: deny
`,
			wantErr: false,
		},
		{
			name: "egress default_action invalid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress:
  default_action: block
`,
			wantErr: true,
		},
		{
			name: "egress policy rule valid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress:
  policies:
    - action: deny
      provider: restricted
`,
			wantErr: false,
		},
		{
			name: "egress policy rule invalid action",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress:
  policies:
    - action: block
      provider: restricted
`,
			wantErr: true,
		},
		{
			name: "egress empty is valid",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
egress: {}
`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if tc.wantErr && err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
		})
	}
}

func TestPluginModeValidationMessages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		yaml        string
		wantContain string
	}{
		{
			name: "overlay without upstreams",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: overlay
      command: /tmp/plugin
`,
			wantContain: "overlay plugin requires",
		},
		{
			name: "replace with upstreams",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: replace
      command: /tmp/plugin
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`,
			wantContain: "cannot set both",
		},
		{
			name: "unknown mode",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  gadget:
    plugin:
      mode: turbo
      command: /tmp/plugin
`,
			wantContain: "unknown plugin mode",
		},
		{
			name: "runtime overlay",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
runtimes:
  worker:
    plugin:
      mode: overlay
      command: /tmp/plugin
`,
			wantContain: "cannot be overlay",
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
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Fatalf("Load error = %q, want it to contain %q", err, tc.wantContain)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{
			name: "nonexistent file",
			path: "/tmp/this-file-does-not-exist-gestalt.yaml",
		},
		{
			name: "invalid YAML",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.path
			if path == "" {
				path = mustWriteConfigFile(t, `{{{invalid yaml`)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
		})
	}
}

func TestLoadEmptyConfigFallsThrough(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "# just a comment\n"} {
		path := mustWriteConfigFile(t, content)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected validation error, got nil")
		}
		if strings.Contains(err.Error(), "EOF") {
			t.Fatalf("Load: got confusing EOF error for empty config: %v", err)
		}
	}
}

func TestHelmValuesConfigLoads(t *testing.T) {
	t.Parallel()

	helmValues, err := os.ReadFile(filepath.Join("..", "..", "deploy", "helm", "gestalt", "values.yaml"))
	if err != nil {
		t.Fatalf("reading helm values.yaml: %v", err)
	}

	var values struct {
		Config yaml.Node `yaml:"config"`
	}
	if err := yaml.Unmarshal(helmValues, &values); err != nil {
		t.Fatalf("parsing helm values.yaml: %v", err)
	}

	configBytes, err := yaml.Marshal(&values.Config)
	if err != nil {
		t.Fatalf("re-marshaling config block: %v", err)
	}

	path := mustWriteConfigFile(t, string(configBytes))

	envMap := map[string]string{
		"GESTALT_BASE_URL":       "https://gestalt.example.com",
		"GESTALT_ENCRYPTION_KEY": "test-encryption-key-for-ci",
		"GOOGLE_CLIENT_ID":       "test-client-id",
		"GOOGLE_CLIENT_SECRET":   "test-client-secret",
	}
	getenv := func(key string) string { return envMap[key] }

	cfg, err := LoadWithMapping(path, getenv)
	if err != nil {
		t.Fatalf("LoadWithMapping on helm values config: %v", err)
	}

	if cfg.Auth.Provider != "google" {
		t.Errorf("Auth.Provider: got %q, want %q", cfg.Auth.Provider, "google")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port: got %d, want 8080", cfg.Server.Port)
	}
	if cfg.Datastore.Provider != "sqlite" {
		t.Errorf("Datastore.Provider: got %q, want %q", cfg.Datastore.Provider, "sqlite")
	}
}

func TestAllowedOperationsListForm(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  test_api:
    upstreams:
      - type: rest
        url: https://example.com/spec.json
        allowed_operations:
          - list_items
          - get_item
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	ops := cfg.Integrations["test_api"].Upstreams[0].AllowedOperations
	if len(ops) != 2 {
		t.Fatalf("AllowedOperations: got %d items, want 2", len(ops))
	}
	if ops["list_items"] != "" {
		t.Errorf("list_items description: got %q, want empty", ops["list_items"])
	}
	if ops["get_item"] != "" {
		t.Errorf("get_item description: got %q, want empty", ops["get_item"])
	}
}

func TestAllowedOperationsMapForm(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  test_api:
    upstreams:
      - type: rest
        url: https://example.com/spec.json
        allowed_operations:
          list_items: List all items
          get_item: ""
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	ops := cfg.Integrations["test_api"].Upstreams[0].AllowedOperations
	if len(ops) != 2 {
		t.Fatalf("AllowedOperations: got %d items, want 2", len(ops))
	}
	if ops["list_items"] != "List all items" {
		t.Errorf("list_items description: got %q, want %q", ops["list_items"], "List all items")
	}
	if ops["get_item"] != "" {
		t.Errorf("get_item description: got %q, want empty", ops["get_item"])
	}
}

func TestAllowedOperationsOmitted(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
datastore:
  provider: sqlite
server:
  encryption_key: key123
integrations:
  test_api:
    upstreams:
      - type: rest
        url: https://example.com/spec.json
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	ops := cfg.Integrations["test_api"].Upstreams[0].AllowedOperations
	if ops != nil {
		t.Fatalf("AllowedOperations: got %v, want nil", ops)
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
  config:
    client_id: cid
    client_secret: csec
    allowed_domain: example.com
datastore:
  provider: sqlite
server:
  encryption_key: key123
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	authCfg := mustDecodeNode(t, cfg.Auth.Config)
	if len(authCfg) != 3 {
		t.Fatalf("Auth.Config: got %d entries, want 3", len(authCfg))
	}
	if authCfg["allowed_domain"] != "example.com" {
		t.Errorf("Auth.Config.allowed_domain: got %q, want %q", authCfg["allowed_domain"], "example.com")
	}
}
