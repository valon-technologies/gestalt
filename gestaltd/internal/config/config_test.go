package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func mustSelectedProvider(t *testing.T, cfg *Config, kind HostProviderKind) (string, *ProviderEntry) {
	t.Helper()
	name, entry, err := cfg.SelectedHostProvider(kind)
	if err != nil {
		t.Fatalf("SelectedHostProvider(%s): %v", kind, err)
	}
	return name, entry
}

func singletonProviderEntry(entry *ProviderEntry) map[string]*ProviderEntry {
	if entry == nil {
		return nil
	}
	return map[string]*ProviderEntry{
		DefaultProviderInstance: entry,
	}
}

func TestLoadConfigGenericFixture(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  providers:
    auth: google
    indexeddb: sqlite
  encryptionKey: server-key
  public:
    host: 127.0.0.1
    port: 9090
  management:
    host: 127.0.0.1
    port: 9191
providers:
  auth:
    google:
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/google
        version: 1.0.0
      config:
        clientId: client-1
        clientSecret: secret-1
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
plugins:
  service-a:
    displayName: Service A
    source:
      path: /tmp/manifest.yaml
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
	if got := cfg.Plugins["service-a"].DisplayName; got != "Service A" {
		t.Fatalf("Integrations[service-a].DisplayName = %q", got)
	}
}

func TestLoadConfigParsesPluginMCPFlag(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
plugins:
  service-a:
    source:
      path: /tmp/manifest.yaml
    mcp: true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Plugins["service-a"].MCP {
		t.Fatal("expected plugins.service-a.mcp to be parsed")
	}
}

func TestLoadConfigSelectsDefaultProvidersFromNamedMaps(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
providers:
  auth:
    primary:
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/google
        version: 1.0.0
    backup:
      default: true
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/github
        version: 1.0.0
  indexeddb:
    main:
      source:
        path: ./providers/datastore/sqlite
    archive:
      default: true
      source:
        path: ./providers/datastore/archive
plugins:
  service-a:
    source:
      path: /tmp/manifest.yaml
    indexeddb: archive
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	authName, authEntry := mustSelectedProvider(t, cfg, HostProviderKindAuth)
	if authName != "backup" || authEntry == nil {
		t.Fatalf("SelectedAuthProvider = (%q, %#v), want backup", authName, authEntry)
	}
	indexedDBName, indexedDBEntry := mustSelectedProvider(t, cfg, HostProviderKindIndexedDB)
	if indexedDBName != "archive" || indexedDBEntry == nil {
		t.Fatalf("SelectedIndexedDBProvider = (%q, %#v), want archive", indexedDBName, indexedDBEntry)
	}
	wantIndexedDB := &PluginIndexedDBConfig{Provider: "archive"}
	if got := cfg.Plugins["service-a"].IndexedDB; !reflect.DeepEqual(got, wantIndexedDB) {
		t.Fatalf("Plugins[service-a].IndexedDB = %#v, want %#v", got, wantIndexedDB)
	}
}

func TestLoadConfigAcceptsLegacyAndCanonicalProviderFormsWhenKeysDoNotConflict(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  providers:
    indexeddb: main
  encryptionKey: server-key
providers:
  indexeddb:
    main:
      source:
        path: ./providers/datastore/sqlite
plugins:
  service-a:
    source:
      path: /tmp/manifest.yaml
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins["service-a"] == nil {
		t.Fatal("Plugins[service-a] = nil")
	}
	if _, indexeddb := mustSelectedProvider(t, cfg, HostProviderKindIndexedDB); indexeddb == nil {
		t.Fatal("SelectedIndexedDBProvider = nil")
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
server:
  providers:
    auth: local
    indexeddb: sqlite
  encryptionKey: ${TEST_ENCRYPTION}
providers:
  auth:
    local:
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/google
        version: 1.0.0
      config:
        clientId: ${TEST_CLIENT_ID}
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
plugins:
  service-a:
    source:
      path: /tmp/manifest.yaml
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
	_, secrets := mustSelectedProvider(t, cfg, HostProviderKindSecrets)
	if secrets == nil || secrets.Source.Builtin != "env" {
		if secrets == nil {
			t.Fatal("SelectedSecretsProvider = nil, want env builtin")
		}
		t.Fatalf("Secrets.Source.Builtin = %q, want env", secrets.Source.Builtin)
	}
	if cfg.Server.EncryptionKey != "encryption-from-env" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}

	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuth)
	if auth == nil {
		t.Fatal("SelectedAuthProvider = nil")
	}
	authCfg := mustDecodeNode(t, auth.Config)
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
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
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
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
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

func TestLoadConfigStructuredSecretRefMissingEnvVariableFails(t *testing.T) {
	t.Parallel()

	providerEnv := "GESTALT_TEST_SECRET_PROVIDER_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
  secrets:
    default:
      source: env
server:
  providers:
    indexeddb: sqlite
  encryptionKey:
    secret:
      provider: ${%s}
      name: enc-key
`, providerEnv))

	_, err := LoadWithLookup(path, func(string) (string, bool) {
		return "", false
	})
	if err == nil {
		t.Fatal("LoadWithLookup: expected error, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf(`environment variable %q not set`, providerEnv)) {
		t.Fatalf("expected missing env error, got: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("${%s:-}", providerEnv)) {
		t.Fatalf("expected empty-default hint, got: %v", err)
	}
}

func TestLoadConfigEmptyDefaultEnvSyntax(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
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

func TestLoadConfigEnvValueWithDollarSignDoesNotReexpand(t *testing.T) {
	t.Parallel()

	secretEnv := "GESTALT_TEST_SECRET_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: ${%s}
`, secretEnv))

	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		if key == secretEnv {
			return "p$ssword", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}
	if cfg.Server.EncryptionKey != "p$ssword" {
		t.Fatalf("Server.EncryptionKey = %q, want p$ssword", cfg.Server.EncryptionKey)
	}
}

func TestLoadConfigEnvValueWithPlaceholderSyntaxDoesNotReexpand(t *testing.T) {
	t.Parallel()

	secretEnv := "GESTALT_TEST_SECRET_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: ${%s}
`, secretEnv))

	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		if key == secretEnv {
			return "abc${INNER}", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}
	if cfg.Server.EncryptionKey != "abc${INNER}" {
		t.Fatalf("Server.EncryptionKey = %q, want abc${INNER}", cfg.Server.EncryptionKey)
	}
}

func TestLoadConfigEnvValueWithSentinelLookingSubstringDoesNotCorruptValue(t *testing.T) {
	t.Parallel()

	secretEnv := "GESTALT_TEST_SECRET_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: ${%s}
`, secretEnv))

	want := "prefix__GESTALT_MISSING_ENV_SU5ORVI__suffix"
	cfg, err := LoadWithLookup(path, func(key string) (string, bool) {
		if key == secretEnv {
			return want, true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("LoadWithLookup: %v", err)
	}
	if cfg.Server.EncryptionKey != want {
		t.Fatalf("Server.EncryptionKey = %q, want %q", cfg.Server.EncryptionKey, want)
	}
}

func TestValidateStructureRejectsDuplicateAuthorizationPolicyMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		members []HumanPolicyMemberDef
		want    string
	}{
		{
			name: "duplicate subject id",
			members: []HumanPolicyMemberDef{
				{SubjectID: "user:123", Role: "viewer"},
				{SubjectID: "user:123", Role: "admin"},
			},
			want: "subjectID duplicates",
		},
		{
			name: "duplicate email after normalization",
			members: []HumanPolicyMemberDef{
				{Email: "Viewer@Test.local", Role: "viewer"},
				{Email: " viewer@test.local ", Role: "admin"},
			},
			want: "email duplicates",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{
				Authorization: AuthorizationConfig{
					Policies: map[string]HumanPolicyDef{
						"roadmap": {
							Default: "deny",
							Members: tc.members,
						},
					},
				},
			}

			err := ValidateStructure(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateStructure error = %v, want substring %q", err, tc.want)
			}
		})
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
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`,
			wantErr: false,
		},
		{
			name: "missing datastore",
			yaml: `
server:
  encryptionKey: server-key
`,
			wantErr: true,
		},
		{
			name: "missing encryption key",
			yaml: `
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
`,
			wantErr: true,
		},
		{
			name: "omitted auth is allowed",
			yaml: `
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
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
			if tc.name == "omitted auth is allowed" {
				_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuth)
				if auth != nil {
					t.Fatalf("SelectedAuthProvider = %#v, want nil", auth)
				}
			}
		})
	}
}

func TestLoadConfigAdminBaseURLValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid baseUrl is allowed when built-in admin auth is unset", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  baseUrl: not a url
  encryptionKey: server-key
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
`)

		if _, err := Load(path); err != nil {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("invalid management.baseUrl is allowed when built-in admin auth is unset", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
  management:
    host: 127.0.0.1
    port: 9090
    baseUrl: not a url
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
`)

		if _, err := Load(path); err != nil {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("invalid baseUrl is rejected for split built-in admin auth", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  baseUrl: not a url
  encryptionKey: server-key
  providers:
    auth: sample
    indexeddb: sqlite
  management:
    host: 127.0.0.1
    port: 9090
    baseUrl: https://gestalt.example.test:9090
  admin:
    authorizationPolicy: admin_policy
providers:
  auth:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
authorization:
  policies:
    admin_policy:
      default: deny
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.baseUrl must be an absolute URL") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid management.baseUrl is rejected for split built-in admin auth", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  baseUrl: https://gestalt.example.test
  encryptionKey: server-key
  providers:
    auth: sample
    indexeddb: sqlite
  management:
    host: 127.0.0.1
    port: 9090
    baseUrl: not a url
  admin:
    authorizationPolicy: admin_policy
providers:
  auth:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
authorization:
  policies:
    admin_policy:
      default: deny
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.management.baseUrl must be an absolute URL") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("management.baseUrl without management listener is rejected for built-in admin auth", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  baseUrl: https://gestalt.example.test
  encryptionKey: server-key
  providers:
    auth: sample
    indexeddb: sqlite
  management:
    baseUrl: https://gestalt.example.test:9090
  admin:
    authorizationPolicy: admin_policy
providers:
  auth:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
authorization:
  policies:
    admin_policy:
      default: deny
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.management.baseUrl requires server.management.host/server.management.port when server.admin.authorizationPolicy is set") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("blank admin allowedRoles entry is rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
  providers:
    auth: sample
    indexeddb: sqlite
  admin:
    authorizationPolicy: admin_policy
    allowedRoles:
      - ""
providers:
  auth:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
authorization:
  policies:
    admin_policy:
      default: deny
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.admin.allowedRoles[0] is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadSucceedsWithoutRuntimeFields(t *testing.T) {
	t.Parallel()

	t.Run("mapping local source path", func(t *testing.T) {
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
		if got := cfg.Plugins["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "manifest.yaml") {
			t.Fatalf("unexpected plugin source path: %q", got)
		}
	})

	t.Run("apiVersion classifies scalar local sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
plugins:
    custom_tool:
      source: ./manifest.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.APIVersion != APIVersionV3 {
			t.Fatalf("APIVersion = %q, want %q", cfg.APIVersion, APIVersionV3)
		}
		if got := cfg.Plugins["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "manifest.yaml") {
			t.Fatalf("unexpected plugin source path: %q", got)
		}
	})

	t.Run("apiVersion keeps colon-containing local sources as paths", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
plugins:
    custom_tool:
      source: demo:manifest.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Plugins["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "demo:manifest.yaml") {
			t.Fatalf("unexpected plugin source path: %q", got)
		}
	})

	t.Run("apiVersion classifies scalar workflow sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
  workflow:
    demo:
      source: ./providers/workflow/demo/manifest.yaml
plugins:
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.Workflow["demo"].SourcePath(); got != filepath.Join(filepath.Dir(path), "providers/workflow/demo/manifest.yaml") {
			t.Fatalf("unexpected workflow source path: %q", got)
		}
	})

	t.Run("apiVersion classifies scalar ui sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
  ui:
    dashboard:
      path: /dashboard
      source: ./providers/ui/dashboard/manifest.yaml
plugins:
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.UI["dashboard"].SourcePath(); got != filepath.Join(filepath.Dir(path), "providers/ui/dashboard/manifest.yaml") {
			t.Fatalf("unexpected ui source path: %q", got)
		}
	})

	t.Run("apiVersion moves sibling auth onto metadata URL sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
plugins:
    custom_tool:
      source: https://example.com/providers/custom_tool/provider-release.yaml?download=1
      auth:
        token: test-token
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Plugins["custom_tool"]
		if got := entry.SourceMetadataURL(); got != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("SourceMetadataURL = %q", got)
		}
		if entry.Source.Auth == nil || entry.Source.Auth.Token != "test-token" {
			t.Fatalf("Source.Auth = %#v", entry.Source.Auth)
		}
		if entry.InlineSourceAuth != nil {
			t.Fatalf("InlineSourceAuth = %#v, want nil", entry.InlineSourceAuth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		if got, ok := roundTripped["source"].(string); !ok || got != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		auth, ok := roundTripped["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("round-tripped auth = %#v", roundTripped["auth"])
		}

		marshaledConfig, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("yaml.Marshal config: %v", err)
		}
		var roundTrippedConfig map[string]any
		if err := yaml.Unmarshal(marshaledConfig, &roundTrippedConfig); err != nil {
			t.Fatalf("yaml.Unmarshal config: %v", err)
		}
		plugins, ok := roundTrippedConfig["plugins"].(map[string]any)
		if !ok {
			t.Fatalf("plugins = %#v", roundTrippedConfig["plugins"])
		}
		plugin, ok := plugins["custom_tool"].(map[string]any)
		if !ok {
			t.Fatalf("plugins.custom_tool = %#v", plugins["custom_tool"])
		}
		if got, ok := plugin["source"].(string); !ok || got != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("config round-tripped source = %#v", plugin["source"])
		}
		auth, ok = plugin["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("config round-tripped auth = %#v", plugin["auth"])
		}
	})

	t.Run("apiVersion preserves builtin scalar host provider sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v3
providers:
  secrets:
    default:
      source: file
      config:
        dir: /tmp/gestalt-secrets
  telemetry:
    default:
      source: otlp
      config:
        endpoint: otel-collector:4317
plugins:
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.Secrets["default"].Source.Builtin; got != "file" {
			t.Fatalf("secrets builtin = %q, want %q", got, "file")
		}
		if got := cfg.Providers.Telemetry["default"].Source.Builtin; got != "otlp" {
			t.Fatalf("telemetry builtin = %q, want %q", got, "otlp")
		}
		if cfg.Providers.Secrets["default"].Source.Path != "" {
			t.Fatalf("secrets path = %q, want empty", cfg.Providers.Secrets["default"].Source.Path)
		}
		if cfg.Providers.Telemetry["default"].Source.Path != "" {
			t.Fatalf("telemetry path = %q, want empty", cfg.Providers.Telemetry["default"].Source.Path)
		}
	})
}

func TestLoadConfigUIEntries(t *testing.T) {
	t.Parallel()

	t.Run("omitted ui leaves mounted ui map empty", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.Providers.UI) != 0 {
			t.Fatalf("Providers.UI = %#v, want empty", cfg.Providers.UI)
		}
	})

	t.Run("ui entry rejects disabled field", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      disabled: true
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ui disabled field rejects YAML boolean variants", func(t *testing.T) {
		t.Parallel()

		for _, variant := range []string{"true", "True", "TRUE"} {
			variant := variant
			t.Run(variant, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, fmt.Sprintf(`
providers:
  ui:
    roadmap:
      disabled: %s
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`, variant))

				_, err := Load(path)
				if err == nil {
					t.Fatal("Load: expected error, got nil")
				}
				if !strings.Contains(err.Error(), `field disabled not found`) {
					t.Fatalf("disabled: %s unexpected error: %v", variant, err)
				}
			})
		}
	})

	t.Run("ui config is rejected when disabled field is present", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      disabled: true
      config:
        brand_name: Acme
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("relative ui provider path resolves from config directory", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      source:
        path: ./web/default/provider.yaml
      path: /create-customer-roadmap-review
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Providers.UI["roadmap"]
		if entry == nil {
			t.Fatal(`Providers.UI["roadmap"] = nil`)
		}
		wantPath := filepath.Join(filepath.Dir(path), "web", "default", "provider.yaml")
		if got := entry.Source.Path; got != wantPath {
			t.Fatalf(`Providers.UI["roadmap"].Source.Path = %q, want %q`, got, wantPath)
		}
	})

	t.Run("relative s3 provider path resolves from config directory", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  s3:
    minio:
      source:
        path: ./providers/s3/minio/manifest.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Providers.S3["minio"]
		if entry == nil {
			t.Fatal(`Providers.S3["minio"] = nil`)
		}
		wantPath := filepath.Join(filepath.Dir(path), "providers", "s3", "minio", "manifest.yaml")
		if got := entry.Source.Path; got != wantPath {
			t.Fatalf(`Providers.S3["minio"].Source.Path = %q, want %q`, got, wantPath)
		}
	})
}

func TestLoadConfigMountedUIs(t *testing.T) {
	t.Parallel()

	t.Run("relative ui provider path resolves and mount path normalizes", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      source:
        path: ./web/roadmap/manifest.yaml
      path: /create-customer-roadmap-review/
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Providers.UI["roadmap"]
		if entry == nil {
			t.Fatal(`Providers.UI["roadmap"] = nil`)
		}
		wantSourcePath := filepath.Join(filepath.Dir(path), "web", "roadmap", "manifest.yaml")
		if got := entry.Source.Path; got != wantSourcePath {
			t.Fatalf(`Providers.UI["roadmap"].Source.Path = %q, want %q`, got, wantSourcePath)
		}
		if got := entry.Path; got != "/create-customer-roadmap-review" {
			t.Fatalf(`Providers.UI["roadmap"].Path = %q, want %q`, got, "/create-customer-roadmap-review")
		}
	})

	t.Run("plugin mount path binds an explicit ui entry", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      source:
        path: ./web/roadmap/manifest.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    ui: roadmap
    mountPath: /create-customer-roadmap-review/
    authorizationPolicy: roadmap_policy
authorization:
  policies:
    roadmap_policy:
      default: deny
      members:
        - email: viewer@example.test
          role: viewer
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Providers.UI["roadmap"]
		if entry == nil {
			t.Fatal(`Providers.UI["roadmap"] = nil`)
		}
		if got := entry.Path; got != "/create-customer-roadmap-review" {
			t.Fatalf(`Providers.UI["roadmap"].Path = %q, want %q`, got, "/create-customer-roadmap-review")
		}
		if got := entry.AuthorizationPolicy; got != "roadmap_policy" {
			t.Fatalf(`Providers.UI["roadmap"].AuthorizationPolicy = %q, want %q`, got, "roadmap_policy")
		}
		if got := entry.OwnerPlugin; got != "roadmap" {
			t.Fatalf(`Providers.UI["roadmap"].OwnerPlugin = %q, want %q`, got, "roadmap")
		}
	})

	t.Run("nested mounted ui provider paths are allowed", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    docs:
      source:
        path: ./web/docs/manifest.yaml
      path: /docs
    admin:
      source:
        path: ./web/docs-admin/manifest.yaml
      path: /docs/admin
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.UI["docs"].Path; got != "/docs" {
			t.Fatalf(`Providers.UI["docs"].Path = %q, want %q`, got, "/docs")
		}
		if got := cfg.Providers.UI["admin"].Path; got != "/docs/admin" {
			t.Fatalf(`Providers.UI["admin"].Path = %q, want %q`, got, "/docs/admin")
		}
	})

	t.Run("reserved path is rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    api:
      source:
        path: ./web/api/manifest.yaml
      path: /api
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.api.path "/api" conflicts with reserved path "/api"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("metrics namespace is rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    metrics:
      source:
        path: ./web/metrics/manifest.yaml
      path: /metrics/dashboard
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.metrics.path "/metrics/dashboard" conflicts with reserved path "/metrics"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("plugin-owned ui overlay still validates reserved mount paths", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    mountPath: /api
providers:
  ui:
    roadmap:
      source:
        path: ./web/roadmap/manifest.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugins.roadmap.mountPath "/api" conflicts with reserved path "/api"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("same-name plugin-owned ui overlay only suppresses duplicate mount checks", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    mountPath: /api
providers:
  ui:
    roadmap:
      source:
        path: ./web/roadmap/manifest.yaml
      path: /roadmap
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugins.roadmap.mountPath "/api" conflicts with reserved path "/api"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("plugin mount path prefix collision with mounted ui is rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    docs:
      source:
        path: ./web/docs/manifest.yaml
      path: /tools
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
plugins:
  admin:
    source:
      path: ./plugin/manifest.yaml
    mountPath: /tools/admin
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.docs.path "/tools" conflicts with plugins.admin.mountPath "/tools/admin"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("root path is accepted", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.UI["root"].Path; got != "/" {
			t.Fatalf("Providers.UI[root].Path = %q, want %q", got, "/")
		}
	})

	t.Run("builtin ui source is rejected", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      source: stdout
      path: /create-customer-roadmap-review
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui "roadmap" does not support builtin providers`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadConfigPluginIndexedDBBindings(t *testing.T) {
	t.Parallel()

	t.Run("plugin accepts indexeddb config object", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      provider: archive
      db: roadmap_review
      objectStores:
        - tasks
        - snapshots
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
    archive:
      source:
        path: ./providers/datastore/archive
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := &PluginIndexedDBConfig{
			Provider:     "archive",
			DB:           "roadmap_review",
			ObjectStores: []string{"tasks", "snapshots"},
		}
		got := cfg.Plugins["roadmap"].IndexedDB
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Plugins[roadmap].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("plugin accepts scalar indexeddb provider name", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		got := cfg.Plugins["roadmap"].IndexedDB
		want := &PluginIndexedDBConfig{
			Provider: "sqlite",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Plugins[roadmap].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("plugin rejects disabled indexeddb config", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      disabled: true
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects indexeddb bindings outside plugins", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /app
      indexeddb:
        provider: sqlite
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.root.indexeddb is only supported on plugins.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("loads plugin surface overrides", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  datadog:
    source:
      path: ./plugin/manifest.yaml
    surfaces:
      openapi:
        baseUrl: https://api.us5.datadoghq.com
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
  `)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Plugins["datadog"].Surfaces == nil || cfg.Plugins["datadog"].Surfaces.OpenAPI == nil {
			t.Fatal("Plugins[datadog].Surfaces.OpenAPI is nil")
		}
		if got := cfg.Plugins["datadog"].Surfaces.OpenAPI.BaseURL; got != "https://api.us5.datadoghq.com" {
			t.Fatalf("Plugins[datadog].Surfaces.OpenAPI.BaseURL = %q, want %q", got, "https://api.us5.datadoghq.com")
		}
	})

	t.Run("loads plugin indexeddb db override", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  datadog:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      db: plugin_data
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
  `)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := &PluginIndexedDBConfig{DB: "plugin_data"}
		if got := cfg.Plugins["datadog"].IndexedDB; !reflect.DeepEqual(got, want) {
			t.Fatalf("Plugins[datadog].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("rejects surface overrides outside plugins", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /app
      surfaces:
        mcp:
          url: https://mcp.example.test
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.root.surfaces is only supported on plugins.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects plugin mount fields outside plugins", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /app
      mountPath: /also-app
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.root.mountPath is only supported on plugins.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects legacy indexeddbSchema outside plugins", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /app
      indexeddbSchema: root_ui
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field indexeddbSchema not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown indexeddb provider names", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      provider: missing
providers:
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugins.roadmap.indexeddb.provider references unknown indexeddb "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects explicit indexeddb object without provider or inherited host indexeddb", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			body string
		}{
			{
				name: "db override",
				body: `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      db: roadmap_state
server:
  encryptionKey: server-key
`,
			},
			{
				name: "objectStores only",
				body: `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      objectStores:
        - tasks
server:
  encryptionKey: server-key
`,
			},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, tc.body)

				_, err := Load(path)
				if err == nil {
					t.Fatal("Load: expected error, got nil")
				}
				if !strings.Contains(err.Error(), `plugins.roadmap.indexeddb requires indexeddb.provider or an available selected/default host indexeddb`) {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("accepts empty indexeddb object without inherited host indexeddb", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name          string
			body          string
			wantIndexedDB bool
		}{
			{
				name: "empty object with no host indexeddb definitions",
				body: `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb: {}
server:
  encryptionKey: server-key
`,
				wantIndexedDB: true,
			},
			{
				name: "omitted indexeddb with no host indexeddb definitions",
				body: `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
server:
  encryptionKey: server-key
`,
				wantIndexedDB: false,
			},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, tc.body)

				cfg, err := Load(path)
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if got := cfg.Plugins["roadmap"].IndexedDB != nil; got != tc.wantIndexedDB {
					t.Fatalf("IndexedDB presence = %v, want %v", got, tc.wantIndexedDB)
				}
			})
		}
	})

	t.Run("rejects indexeddb disabled field", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      disabled: true
      provider: main-db
providers:
  indexeddb:
    main-db:
      source:
        path: ./providers/datastore/main-db
server:
  providers:
    indexeddb: main-db
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects duplicate indexeddb object stores", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      provider: main
      objectStores:
        - tasks
        - tasks
providers:
  indexeddb:
    main:
      source:
        path: ./providers/datastore/main
server:
  providers:
    indexeddb: main
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugins.roadmap.indexeddb.objectStores[1] duplicates "tasks"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects indexeddb sequences", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    indexeddb:
      - main
      - archive
providers:
  indexeddb:
    main:
      source:
        path: ./providers/datastore/main
    archive:
      source:
        path: ./providers/datastore/archive
server:
  providers:
    indexeddb: main
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugin indexeddb must be a mapping or scalar provider name`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("plugin accepts s3 bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    s3:
      - assets
providers:
  s3:
    assets:
      source:
        path: ./providers/s3/assets
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Plugins["roadmap"].S3; !reflect.DeepEqual(got, []string{"assets"}) {
			t.Fatalf("Plugins[roadmap].S3 = %#v, want %#v", got, []string{"assets"})
		}
	})

	t.Run("rejects s3 disabled field", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    s3:
      - assets
providers:
  s3:
    assets:
      disabled: true
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects cache disabled field", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    cache:
      - session
providers:
  cache:
    session:
      disabled: true
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `field disabled not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("plugin accepts workflow bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
        - backfill_items
      schedules:
        nightly:
          cron: "0 2 * * *"
          operation: nightly_sync
          input:
            source: yaml
      eventTriggers:
        task_updated:
          match:
            type: roadmap.task.updated
            source: roadmap
          operation: backfill_items
          paused: true
          input:
            source: event
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := &PluginWorkflowConfig{
			Provider:   "temporal",
			Operations: []string{"nightly_sync", "backfill_items"},
			Schedules: map[string]PluginWorkflowSchedule{
				"nightly": {
					Cron:      "0 2 * * *",
					Timezone:  "UTC",
					Operation: "nightly_sync",
					Input: map[string]any{
						"source": "yaml",
					},
				},
			},
			EventTriggers: map[string]PluginWorkflowEventTrigger{
				"task_updated": {
					Match: PluginWorkflowEventMatch{
						Type:   "roadmap.task.updated",
						Source: "roadmap",
					},
					Operation: "backfill_items",
					Paused:    true,
					Input: map[string]any{
						"source": "event",
					},
				},
			},
		}
		if got := cfg.Plugins["roadmap"].Workflow; !reflect.DeepEqual(got, want) {
			t.Fatalf("Plugins[roadmap].Workflow = %#v, want %#v", got, want)
		}
	})

	t.Run("plugin workflow binding can select an explicit provider when multiple workflow providers exist", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
    cleanup:
      source:
        path: ./providers/workflow/cleanup
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		effective, err := cfg.EffectivePluginWorkflow("roadmap", cfg.Plugins["roadmap"])
		if err != nil {
			t.Fatalf("EffectivePluginWorkflow: %v", err)
		}
		if effective.ProviderName != "temporal" {
			t.Fatalf("ProviderName = %q, want %q", effective.ProviderName, "temporal")
		}
	})

	t.Run("rejects workflow bindings outside plugins", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    root:
      source:
        path: ./web/root/manifest.yaml
      path: /app
      workflow:
        provider: temporal
        operations:
          - nightly_sync
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.root.workflow is only supported on plugins.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown workflow provider names", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: missing
      operations:
        - nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `plugins.roadmap.workflow.provider references unknown workflow "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects multiple workflow defaults even when plugins bind explicitly", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
providers:
  workflow:
    temporal:
      default: true
      source:
        path: ./providers/workflow/temporal
    cleanup:
      default: true
      source:
        path: ./providers/workflow/cleanup
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `providers.workflow declares multiple defaults: cleanup, temporal`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects workflow schedules with operations outside workflow bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
      schedules:
        invalid:
          cron: "*/5 * * * *"
          operation: backfill_items
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `workflow.schedules.invalid.operation "backfill_items" must be listed`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects workflow event triggers with operations outside workflow bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
      eventTriggers:
        invalid:
          match:
            type: roadmap.task.updated
          operation: backfill_items
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `workflow.eventTriggers.invalid.operation "backfill_items" must be listed`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects workflow event triggers without match type", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
      eventTriggers:
        invalid:
          match:
            source: roadmap
          operation: nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `workflow.eventTriggers.invalid.match.type is required`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid workflow schedule cron and timezone", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
plugins:
  roadmap:
    source:
      path: ./plugin/manifest.yaml
    workflow:
      provider: temporal
      operations:
        - nightly_sync
      schedules:
        invalid:
          cron: "0 0 0 * * *"
          timezone: Mars/Olympus
          operation: nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `workflow.schedules.invalid.cron`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workflow provider accepts indexeddb bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    basic:
      source:
        path: ./providers/workflow/indexeddb
      indexeddb:
        provider: workflow_state
        db: workflow
        objectStores:
          - workflow_schedules
          - workflow_runs
  indexeddb:
    workflow_state:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: workflow_state
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := &PluginIndexedDBConfig{
			Provider:     "workflow_state",
			DB:           "workflow",
			ObjectStores: []string{"workflow_schedules", "workflow_runs"},
		}
		if got := cfg.Providers.Workflow["basic"].IndexedDB; !reflect.DeepEqual(got, want) {
			t.Fatalf("Providers.Workflow[basic].IndexedDB = %#v, want %#v", got, want)
		}
		effective, err := cfg.EffectiveWorkflowIndexedDB("basic", cfg.Providers.Workflow["basic"])
		if err != nil {
			t.Fatalf("EffectiveWorkflowIndexedDB: %v", err)
		}
		if effective.ProviderName != "workflow_state" || effective.DB != "workflow" {
			t.Fatalf("effective = %#v", effective)
		}
	})

	t.Run("rejects unknown indexeddb bindings on workflow providers", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    basic:
      source:
        path: ./providers/workflow/indexeddb
      indexeddb:
        provider: missing
  indexeddb:
    workflow_state:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: workflow_state
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `providers.workflow.basic.indexeddb.provider references unknown indexeddb "missing"`) {
			t.Fatalf("unexpected error: %v", err)
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
  secrets:
    primary:
      provider: none
`,
			wantErr: `field provider not found`,
		},
		{
			name: "builtin field is rejected",
			yaml: `
providers:
  telemetry:
    primary:
      builtin: stdout
`,
			wantErr: `field builtin not found`,
		},
		{
			name: "disabled field wins over missing env",
			yaml: `
providers:
  ui:
    roadmap:
      disabled: true
      source:
        path: ${MISSING_UI_PATH}
      path: /roadmap
`,
			wantErr: `field disabled not found`,
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
		want string
	}{
		{
			name: "builtin string",
			yaml: `
providers:
  telemetry:
    primary:
      source: stdout
`,
		},
		{
			name: "external provider source",
			yaml: `
providers:
  auth:
    primary:
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/google
        version: 1.0.0
`,
		},
		{
			name: "apiVersion scalar local source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: ./plugins/dummy/manifest.yaml
`,
		},
		{
			name: "apiVersion metadata url with sibling auth",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        token: test-token
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
		want string
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
		{
			name: "multiple auth providers require selection or default",
			yaml: `
providers:
  auth:
    one:
      source:
        path: ./one/manifest.yaml
    two:
      source:
        path: ./two/manifest.yaml
`,
		},
		{
			name: "legacy server authorization is rejected",
			yaml: `
server:
  authorization:
    policies:
      sample_policy:
        default: deny
`,
			want: `field authorization not found in type config.ServerConfig`,
		},
		{
			name: "unsupported apiVersion is rejected",
			yaml: `
apiVersion: gestaltd.config/v99
providers:
plugins:
    external:
      source: ./plugins/dummy/manifest.yaml
`,
			want: `unsupported apiVersion "gestaltd.config/v99"`,
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
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsDuplicateYAMLKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "duplicate server providers mapping",
			yaml: `
server:
  providers:
    indexeddb: legacy
  providers:
    indexeddb: canonical
`,
			want: `mapping key "providers" already defined`,
		},
		{
			name: "duplicate indexeddb collection mapping",
			yaml: `
providers:
  indexeddb:
    legacy:
      source:
        path: ./legacy/manifest.yaml
  indexeddb:
    canonical:
      source:
        path: ./canonical/manifest.yaml
`,
			want: `mapping key "indexeddb" already defined`,
		},
		{
			name: "duplicate plugins mapping",
			yaml: `
plugins:
  legacy:
    source:
      path: ./legacy/manifest.yaml
plugins:
  canonical:
    source:
      path: ./canonical/manifest.yaml
`,
			want: `mapping key "plugins" already defined`,
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
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want substring %q", err, tc.want)
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
			name: "apiVersion metadata url with sibling auth is valid",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        token: test-token
`,
		},
		{
			name: "apiVersion local source rejects sibling auth",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: ./plugins/dummy/manifest.yaml
      auth:
        token: test-token
`,
			wantErr: "auth is only valid with metadata URL sources",
		},
		{
			name: "managed ref rejects sibling auth",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source:
        ref: github.com/acme-corp/tools/widget
        version: 1.2.3
        auth:
          token: nested-token
      auth:
        token: sibling-token
`,
			wantErr: "auth and source.auth are mutually exclusive",
		},
		{
			name: "apiVersion rejects non metadata http source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: https://example.com/providers/external/archive.tar.gz
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects git scalar source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: git+ssh://git@github.com/example/external.git
`,
			wantErr: "git+ sources are not supported in apiVersion v3 configs",
		},
		{
			name: "apiVersion rejects unsupported ssh scalar source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: ssh://github.com/example/external
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects unsupported file scalar source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: file:/tmp/provider-release.yaml
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects malformed hostless https metadata source",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
plugins:
    external:
      source: https:///provider-release.yaml
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects unsupported telemetry source before builtin defaulting",
			yaml: `
apiVersion: gestaltd.config/v3
providers:
  telemetry:
    default:
      source: https://example.com/providers/telemetry/archive.tar.gz
plugins:
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
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
				Plugins: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Path: "./some-dir/manifest.yaml"}},
				},
			},
		},
		{
			name: "source valid",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
		},
		{
			name: "source path and ref rejected",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Path: "./manifest.yaml", Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"}},
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "nil plugin rejected",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {},
				},
			},
			wantErr: "source.path or source.ref is required",
		},
		{
			name: "source without version rejected",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-plugin"}},
				},
			},
			wantErr: "source.version is required when source.ref is set",
		},
		{
			name: "source version without ref rejected",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Version: "1.0.0"}},
				},
			},
			wantErr: "source.path or source.ref is required",
		},
		{
			name: "auth provider valid",
			cfg: &Config{
				Providers: ProvidersConfig{
					Auth: singletonProviderEntry(&ProviderEntry{Source: ProviderSource{Ref: "github.com/test-org/test-repo/test-auth", Version: "1.0.0"}}),
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
					Auth: singletonProviderEntry(&ProviderEntry{}),
				},
			},
			wantErr: `source.path or source.ref is required`,
		},
		{
			name: "auth config requires source",
			cfg: &Config{
				Providers: ProvidersConfig{
					Auth: singletonProviderEntry(&ProviderEntry{Config: yaml.Node{Kind: yaml.MappingNode}}),
				},
			},
			wantErr: `source.path or source.ref is required`,
		},
		{
			name: "s3 entry validates source",
			cfg: &Config{
				Providers: ProvidersConfig{
					S3: map[string]*ProviderEntry{
						"assets": {
							Source: ProviderSource{
								Ref: "not-a-valid-ref",
							},
						},
					},
				},
			},
			wantErr: `source.ref`,
		},
		{
			name: "plugin auth rejects mcp oauth early",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {
						Source: ProviderSource{Path: "./manifest.yaml"},
						Auth:   &ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth},
					},
				},
			},
			wantErr: `plugin auth type "mcp_oauth" requires an MCP surface`,
		},
		{
			name: "named connection rejects mcp oauth early",
			cfg: &Config{
				Plugins: map[string]*ProviderEntry{
					"sample": {
						Source: ProviderSource{Path: "./manifest.yaml"},
						Connections: map[string]*ConnectionDef{
							"default": {Auth: ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth}},
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
    auth:
      source:
        path: ../auth-plugin/provider.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
plugins:
  service-a:
    iconFile: ../assets/service.svg
    source:
      path: ../bin/manifest.yaml
server:
  providers:
    indexeddb: sqlite
`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Plugins["service-a"].IconFile; got != iconPath {
		t.Fatalf("IconFile = %q, want %q", got, iconPath)
	}
	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuth)
	if auth == nil {
		t.Fatal("SelectedAuthProvider = nil")
	}
	if got := auth.SourcePath(); got != filepath.Join(dir, "auth-plugin", "provider.yaml") {
		t.Fatalf("auth plugin source path = %q, want %q", got, filepath.Join(dir, "auth-plugin", "provider.yaml"))
	}
	if got := cfg.Plugins["service-a"].SourcePath(); got != filepath.Join(dir, "bin", "manifest.yaml") {
		t.Fatalf("integration plugin source path = %q, want %q", got, filepath.Join(dir, "bin", "manifest.yaml"))
	}
}

func TestLoadPaths_ResolvesRelativeScalarSourcePathsPerFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(basePath), 0o755); err != nil {
		t.Fatalf("MkdirAll base: %v", err)
	}
	if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v3
providers:
plugins:
    sample:
      source: ../base-plugin/manifest.yaml
`), 0o644); err != nil {
		t.Fatalf("WriteFile base: %v", err)
	}

	overridePath := filepath.Join(dir, "overrides", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0o755); err != nil {
		t.Fatalf("MkdirAll override: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`
providers:
plugins:
    sample:
      source: ./override-plugin/manifest.yaml
`), 0o644); err != nil {
		t.Fatalf("WriteFile override: %v", err)
	}

	cfg, err := LoadPaths([]string{basePath, overridePath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}

	wantPath := filepath.Join(filepath.Dir(overridePath), "override-plugin", "manifest.yaml")
	if got := cfg.Plugins["sample"].SourcePath(); got != wantPath {
		t.Fatalf("SourcePath = %q, want %q", got, wantPath)
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  auth:
    auth:
      source:
        ref: github.com/valon-technologies/gestalt-providers/auth/google
        version: 1.0.0
      config:
        clientId: client-1
        clientSecret: secret-1
        redirectUrl: https://example.test/callback
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuth)
	if auth == nil {
		t.Fatal("SelectedAuthProvider = nil")
	}
	authCfg := mustDecodeNode(t, auth.Config)
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

	entry := loaded.Plugins["sample"]
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
