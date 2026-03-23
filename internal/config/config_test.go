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

func warningsContain(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
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
	if cfg.Datastore.Provider != "sqlite" {
		t.Errorf("Datastore.Provider default: got %q, want %q", cfg.Datastore.Provider, "sqlite")
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

func TestDatastoreWarnings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		yaml          string
		env           map[string]string
		wantSubstring string
		wantWarnings  bool
	}{
		{
			name: "default sqlite path warns for local filesystem",
			yaml: `
auth:
  provider: google
server:
  encryption_key: key123
`,
			wantSubstring: `local filesystem storage`,
			wantWarnings:  true,
		},
		{
			name: "temp sqlite path warns for restart loss",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
  config:
    path: /tmp/gestalt.db
server:
  encryption_key: key123
`,
			wantSubstring: `temporary storage`,
			wantWarnings:  true,
		},
		{
			name: "cloud run warns for non durable sqlite",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
  config:
    path: /data/gestalt.db
server:
  encryption_key: key123
`,
			env: map[string]string{
				"K_SERVICE": "gestalt",
			},
			wantSubstring: `not durable on Cloud Run`,
			wantWarnings:  true,
		},
		{
			name: "kubernetes warns for single instance sqlite",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
  config:
    path: /data/gestalt.db
server:
  encryption_key: key123
`,
			env: map[string]string{
				"KUBERNETES_SERVICE_HOST": "10.0.0.1",
			},
			wantSubstring: `single-instance only`,
			wantWarnings:  true,
		},
		{
			name: "postgres has no datastore warnings",
			yaml: `
auth:
  provider: google
datastore:
  provider: postgres
  config:
    dsn: postgres://user:pass@localhost:5432/gestalt
server:
  encryption_key: key123
`,
		},
		{
			name: "mounted absolute sqlite path without platform signals stays quiet",
			yaml: `
auth:
  provider: google
datastore:
  provider: sqlite
  config:
    path: /data/gestalt.db
server:
  encryption_key: key123
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := mustWriteConfigFile(t, tc.yaml)
			getenv := func(key string) string {
				return tc.env[key]
			}

			cfg, err := LoadWithMapping(path, getenv)
			if err != nil {
				t.Fatalf("LoadWithMapping: unexpected error: %v", err)
			}

			warnings := DatastoreWarnings(cfg, getenv)
			if tc.wantWarnings && len(warnings) == 0 {
				t.Fatalf("DatastoreWarnings: got none, want at least one")
			}
			if !tc.wantWarnings && len(warnings) != 0 {
				t.Fatalf("DatastoreWarnings: got %v, want none", warnings)
			}
			if tc.wantSubstring != "" && !warningsContain(warnings, tc.wantSubstring) {
				t.Fatalf("DatastoreWarnings: got %v, want substring %q", warnings, tc.wantSubstring)
			}
		})
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
			yaml:    "server:\n  encryption_key: key123\n",
			wantErr: true,
		},
		{
			name:    "missing encryption key",
			yaml:    "auth:\n  provider: google\nserver:\n  port: 8080\n",
			wantErr: true,
		},
		{
			name:    "dev mode skips encryption key",
			yaml:    "auth:\n  provider: google\nserver:\n  dev_mode: true\n",
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

func TestPluginConfigLoads(t *testing.T) {
	t.Parallel()

	yaml := `
auth:
  provider: google
server:
  encryption_key: key123
integrations:
  custom_plugin:
    plugin:
      command:
        - node
        - ./plugins/custom-plugin/index.js
      cwd: ./plugins/custom-plugin
      env:
        API_BASE_URL: https://example.com
      config:
        project: ENG
        retries: 2
      allowed_operations:
        run_task: Run the task
      startup_timeout: 5s
      request_timeout: 30s
`
	path := mustWriteConfigFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	intg := cfg.Integrations["custom_plugin"]
	if intg.Plugin == nil {
		t.Fatal("Integrations[custom_plugin].Plugin: got nil, want non-nil")
	}
	if len(intg.Plugin.Command) != 2 || intg.Plugin.Command[0] != "node" {
		t.Fatalf("Plugin.Command: got %v", intg.Plugin.Command)
	}
	if intg.Plugin.Cwd != "./plugins/custom-plugin" {
		t.Errorf("Plugin.Cwd: got %q, want %q", intg.Plugin.Cwd, "./plugins/custom-plugin")
	}
	if intg.Plugin.Env["API_BASE_URL"] != "https://example.com" {
		t.Errorf("Plugin.Env[API_BASE_URL]: got %q, want %q", intg.Plugin.Env["API_BASE_URL"], "https://example.com")
	}
	if intg.Plugin.AllowedOperations["run_task"] != "Run the task" {
		t.Errorf("Plugin.AllowedOperations[run_task]: got %q, want %q", intg.Plugin.AllowedOperations["run_task"], "Run the task")
	}
	if intg.Plugin.StartupTimeout != "5s" {
		t.Errorf("Plugin.StartupTimeout: got %q, want %q", intg.Plugin.StartupTimeout, "5s")
	}
	if intg.Plugin.RequestTimeout != "30s" {
		t.Errorf("Plugin.RequestTimeout: got %q, want %q", intg.Plugin.RequestTimeout, "30s")
	}

	var pluginCfg map[string]any
	if err := intg.Plugin.Config.Decode(&pluginCfg); err != nil {
		t.Fatalf("Plugin.Config.Decode: %v", err)
	}
	if pluginCfg["project"] != "ENG" {
		t.Errorf("Plugin.Config.project: got %v, want ENG", pluginCfg["project"])
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
