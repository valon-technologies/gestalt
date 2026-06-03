package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
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
	return mustWriteRawConfigFile(t, withDefaultConfigAPIVersion(content))
}

func mustWriteRawConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func withDefaultConfigAPIVersion(content string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if strings.HasPrefix(trimmed, "apiVersion:") {
		return content
	}
	return "\napiVersion: " + ConfigAPIVersion + "\n" + strings.TrimLeft(content, "\r\n")
}

func TestValidateStructureRejectsPlatformOAuth2RefreshToken(t *testing.T) {
	t.Parallel()

	baseAuth := ConnectionAuthDef{
		Type:         providermanifestv1.AuthTypeOAuth2,
		GrantType:    "refresh_token",
		TokenURL:     "https://oauth2.example.test/token",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "refresh-token",
	}
	cases := []struct {
		name    string
		conn    ConnectionDef
		wantErr string
	}{
		{
			name:    "refresh_token grant rejected",
			conn:    ConnectionDef{Mode: providermanifestv1.ConnectionModeSubject, Auth: baseAuth},
			wantErr: "oauth2 refresh_token is not supported; use managed-subject credentials",
		},
		{
			name: "unsupported mode rejected before grant validation",
			conn: func() ConnectionDef {
				auth := baseAuth
				return ConnectionDef{Mode: providermanifestv1.ConnectionMode("unsupported-mode"), Auth: auth}
			}(),
			wantErr: `mode "unsupported-mode" is not supported`,
		},
		{
			name: "unsupported grant rejected",
			conn: func() ConnectionDef {
				auth := baseAuth
				auth.GrantType = "password"
				return ConnectionDef{Mode: providermanifestv1.ConnectionModeSubject, Auth: auth}
			}(),
			wantErr: "auth.grantType is only supported for oauth2 client_credentials or refresh_token",
		},
		{
			name: "refresh token without refresh_token grant rejected",
			conn: func() ConnectionDef {
				auth := baseAuth
				auth.GrantType = ""
				return ConnectionDef{Mode: providermanifestv1.ConnectionModeSubject, Auth: auth}
			}(),
			wantErr: "auth.refreshToken is only supported for oauth2 refresh_token",
		},
		{
			name: "refresh token with client credentials rejected",
			conn: func() ConnectionDef {
				auth := baseAuth
				auth.GrantType = "client_credentials"
				return ConnectionDef{Mode: providermanifestv1.ConnectionModeSubject, Auth: auth}
			}(),
			wantErr: "oauth2 client_credentials is not supported; use managed-subject credentials",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := tc.conn
			cfg := &Config{Connections: map[string]*ConnectionDef{"platform-mailbox": &conn}}
			err := ValidateStructure(cfg)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("ValidateStructure: expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateStructure: expected error, got nil")
			}
		})
	}
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
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: google
    indexeddb: sqlite
  encryptionKey: server-key
  public:
    host: 127.0.0.1
    port: 9090
  management:
    host: 127.0.0.1
    port: 9191
providers:
  authentication:
    google:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
      config:
        clientId: client-1
        clientSecret: secret-1
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
apps:
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
	if got := cfg.Apps["service-a"].DisplayName; got != "Service A" {
		t.Fatalf("Integrations[service-a].DisplayName = %q", got)
	}
}

func TestLoadConfigParsesAppMCPFlag(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
apps:
  service-a:
    source:
      path: /tmp/manifest.yaml
    mcp: true
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Apps["service-a"].MCP {
		t.Fatal("expected apps.service-a.mcp to be parsed")
	}
}

func TestLoadConfigParsesAppHTTPSecuritySchemesAndBindings(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
server:
  encryptionKey: server-key
apps:
  signed:
    source:
      path: /tmp/manifest.yaml
    securitySchemes:
      signed:
        type: hmac
        secret:
          env: REQUEST_SIGNING_SECRET
        signatureHeader: X-Request-Signature
        signaturePrefix: v0=
        payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
        timestampHeader: X-Request-Timestamp
        maxAgeSeconds: 300
    http:
      command:
        path: /command
        method: POST
        security: signed
        requestBody:
          required: true
          content:
            application/x-www-form-urlencoded: {}
        target: handle_command
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry := cfg.Apps["signed"]
	if entry == nil {
		t.Fatal("Apps[signed] = nil")
		return
	}
	scheme := entry.SecuritySchemes["signed"]
	if scheme == nil {
		t.Fatal("SecuritySchemes[signed] = nil")
		return
	}
	if scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		t.Fatalf("SecuritySchemes[signed] = %#v", entry.SecuritySchemes["signed"])
		return
	}
	if got, want := scheme.SignatureHeader, "X-Request-Signature"; got != want {
		t.Fatalf("SecuritySchemes[signed].SignatureHeader = %q, want %q", got, want)
	}
	if got, want := scheme.PayloadTemplate, "v0:{header:X-Request-Timestamp}:{raw_body}"; got != want {
		t.Fatalf("SecuritySchemes[signed].PayloadTemplate = %q, want %q", got, want)
	}
	if got, want := scheme.TimestampHeader, "X-Request-Timestamp"; got != want {
		t.Fatalf("SecuritySchemes[signed].TimestampHeader = %q, want %q", got, want)
	}
	if got, want := scheme.MaxAgeSeconds, 300; got != want {
		t.Fatalf("SecuritySchemes[signed].MaxAgeSeconds = %d, want %d", got, want)
	}
	if entry.HTTP["command"] == nil {
		t.Fatal("HTTP[command] = nil")
	}
	if got, want := entry.HTTP["command"].Path, "/command"; got != want {
		t.Fatalf("HTTP[command].Path = %q, want %q", got, want)
	}
	if got, want := entry.HTTP["command"].Target, "handle_command"; got != want {
		t.Fatalf("HTTP[command].Target = %q, want %q", got, want)
	}
}

func TestProviderEntryEffectiveHTTPSecuritySchemes_MergesHMACFields(t *testing.T) {
	t.Parallel()

	entry := &ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Spec: &providermanifestv1.Spec{
				SecuritySchemes: map[string]*providermanifestv1.HTTPSecurityScheme{
					"signed": {
						Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
						SignatureHeader: "X-Old-Signature",
						SignaturePrefix: "v1=",
						PayloadTemplate: "{raw_body}",
						TimestampHeader: "X-Old-Timestamp",
						MaxAgeSeconds:   30,
						Secret:          &providermanifestv1.HTTPSecretRef{Env: "OLD_SIGNING_SECRET"},
					},
				},
			},
		},
		SecuritySchemes: map[string]*HTTPSecurityScheme{
			"signed": {
				SignatureHeader: "X-Request-Signature",
				SignaturePrefix: "v0=",
				PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
				TimestampHeader: "X-Request-Timestamp",
				MaxAgeSeconds:   300,
				Secret:          &providermanifestv1.HTTPSecretRef{Env: "REQUEST_SIGNING_SECRET"},
			},
		},
	}

	effective := entry.EffectiveHTTPSecuritySchemes()
	scheme := effective["signed"]
	if scheme == nil {
		t.Fatal("EffectiveHTTPSecuritySchemes()[signed] = nil")
		return
	}
	if got, want := scheme.SignatureHeader, "X-Request-Signature"; got != want {
		t.Fatalf("SignatureHeader = %q, want %q", got, want)
	}
	if got, want := scheme.SignaturePrefix, "v0="; got != want {
		t.Fatalf("SignaturePrefix = %q, want %q", got, want)
	}
	if got, want := scheme.PayloadTemplate, "v0:{header:X-Request-Timestamp}:{raw_body}"; got != want {
		t.Fatalf("PayloadTemplate = %q, want %q", got, want)
	}
	if got, want := scheme.TimestampHeader, "X-Request-Timestamp"; got != want {
		t.Fatalf("TimestampHeader = %q, want %q", got, want)
	}
	if got, want := scheme.MaxAgeSeconds, 300; got != want {
		t.Fatalf("MaxAgeSeconds = %d, want %d", got, want)
	}
	if scheme.Secret == nil || scheme.Secret.Env != "REQUEST_SIGNING_SECRET" {
		t.Fatalf("Secret = %#v, want REQUEST_SIGNING_SECRET", scheme.Secret)
	}
}

func TestLoadConfigSelectsDefaultProvidersFromNamedMaps(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
server:
  encryptionKey: server-key
  providers:
    authorization: indexeddb
providers:
  authentication:
    primary:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
    backup:
      default: true
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/github/v1.0.0/provider-release.yaml
  indexeddb:
    main:
      source:
        path: ./providers/datastore/sqlite
    archive:
      default: true
      source:
        path: ./providers/datastore/archive
  authorization:
    memory:
      source:
        path: ./providers/authorization/memory
    indexeddb:
      default: true
      source:
        path: ./providers/authorization/indexeddb
apps:
  service-a:
    source:
      path: /tmp/manifest.yaml
    indexeddb: archive
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	authName, authEntry := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
	if authName != "backup" || authEntry == nil {
		t.Fatalf("SelectedAuthenticationProvider = (%q, %#v), want backup", authName, authEntry)
	}
	indexedDBName, indexedDBEntry := mustSelectedProvider(t, cfg, HostProviderKindIndexedDB)
	if indexedDBName != "archive" || indexedDBEntry == nil {
		t.Fatalf("SelectedIndexedDBProvider = (%q, %#v), want archive", indexedDBName, indexedDBEntry)
	}
	authorizationName, authorizationEntry := mustSelectedProvider(t, cfg, HostProviderKindAuthorization)
	if authorizationName != "indexeddb" || authorizationEntry == nil {
		t.Fatalf("SelectedAuthorizationProvider = (%q, %#v), want indexeddb", authorizationName, authorizationEntry)
	}
	wantIndexedDB := &IndexedDBBindingConfig{Provider: "archive"}
	if got := cfg.Apps["service-a"].IndexedDB; !reflect.DeepEqual(got, wantIndexedDB) {
		t.Fatalf("Apps[service-a].IndexedDB = %#v, want %#v", got, wantIndexedDB)
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
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: local
    indexeddb: sqlite
  encryptionKey: ${TEST_ENCRYPTION}
providers:
  authentication:
    local:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
      config:
        clientId: ${TEST_CLIENT_ID}
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
apps:
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
	if secrets == nil {
		t.Fatal("SelectedSecretsProvider = nil, want env builtin")
		return
	}
	if secrets.Source.Builtin != "env" {
		t.Fatalf("Secrets.Source.Builtin = %q, want env", secrets.Source.Builtin)
	}
	if cfg.Server.EncryptionKey != "encryption-from-env" {
		t.Fatalf("Server.EncryptionKey = %q", cfg.Server.EncryptionKey)
	}

	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
	if auth == nil {
		t.Fatal("SelectedAuthenticationProvider = nil")
		return
	}
	authCfg := mustDecodeNode(t, auth.Config)
	if authCfg["clientId"] != "client-from-env" {
		t.Fatalf("Auth.Config.clientId = %#v", authCfg["clientId"])
	}
}

func TestLoadConfigAcceptsAuthenticationConfig(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: local
    indexeddb: sqlite
  encryptionKey: server-key
providers:
  authentication:
    local:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	authName, authEntry := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
	if authName != "local" || authEntry == nil {
		t.Fatalf("SelectedAuthenticationProvider = (%q, %#v), want local", authName, authEntry)
	}
	if cfg.Server.Providers.Authentication != "local" {
		t.Fatalf("Server.Providers.Authentication = %q, want local", cfg.Server.Providers.Authentication)
	}
	if cfg.Providers.Authentication["local"] == nil {
		t.Fatal("Providers.Authentication[local] = nil")
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

func TestLoadRejectsAuthorizationPolicies(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
authorization:
  policies:
    legacy_admins:
      default: deny
      members:
        - subjectID: user:legacy
          role: admin
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if !strings.Contains(err.Error(), `field policies not found`) {
		t.Fatalf("Load error = %v, want unknown policies field", err)
	}
}

func TestLoadAuthorizationModelFragments(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
authorization:
  models:
    default:
      resourceTypes:
        team:
          defaultAccessPolicy: deny
          relations:
            member:
              subjectTypes: [subject]
            admin:
              subjectTypes: [subject]
          actions:
            view:
              relations: [member, admin]
            manage:
              relations: [admin]
          dynamic:
            allowAdditionalRelationships: true
        app/github/repository:
          relations:
            maintainer:
              allowedTargets:
                - subjectType: subject
          actions:
            administer:
              relations: [maintainer]
  resourceTypes:
    team:
      dynamic:
        allowAdditionalRelationships: true
  relationships:
    - subject:
        type: subject
        id: user:alice
      relation: admin
      resource:
        type: team
        id: servicing
      source:
        layer: static_config
        id: authorization.relationships[0]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	team := cfg.Authorization.Models["default"].ResourceTypes["team"]
	if got := team.DefaultAccessPolicy; got != "deny" {
		t.Fatalf("team defaultAccessPolicy = %q, want deny", got)
	}
	if !team.Dynamic.AllowAdditionalRelationships {
		t.Fatal("team dynamic allowAdditionalRelationships = false, want true")
	}
	if got := team.Actions["view"].Relations; !reflect.DeepEqual(got, []string{"member", "admin"}) {
		t.Fatalf("team view relations = %#v, want member/admin", got)
	}
	repository := cfg.Authorization.Models["default"].ResourceTypes["app/github/repository"]
	if got := repository.Relations["maintainer"].AllowedTargets[0].SubjectType; got != "subject" {
		t.Fatalf("repository maintainer allowed target = %q, want subject", got)
	}
	relationship := cfg.Authorization.Relationships[0]
	if relationship.Subject.ID != "user:alice" || relationship.Resource.Type != "team" || relationship.Relation != "admin" {
		t.Fatalf("relationship parsed as %#v", relationship)
	}
}

func TestValidateAuthorizationModelFragments(t *testing.T) {
	t.Parallel()

	subjectRelation := func(subjectTypes ...string) AuthorizationRelationDef {
		return AuthorizationRelationDef{SubjectTypes: subjectTypes}
	}
	resourceType := func(relations map[string]AuthorizationRelationDef, actions map[string]AuthorizationActionDef) AuthorizationResourceTypeDef {
		return AuthorizationResourceTypeDef{Relations: relations, Actions: actions}
	}
	model := func(resourceTypes map[string]AuthorizationResourceTypeDef) AuthorizationModelDef {
		return AuthorizationModelDef{ResourceTypes: resourceTypes}
	}
	tests := []struct {
		name    string
		authz   AuthorizationConfig
		wantErr string
	}{
		{
			name: "duplicate resource type across models",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				"base": model(map[string]AuthorizationResourceTypeDef{
					"team": resourceType(map[string]AuthorizationRelationDef{"member": subjectRelation("subject")}, nil),
				}),
				"extra": model(map[string]AuthorizationResourceTypeDef{
					"team": resourceType(map[string]AuthorizationRelationDef{"admin": subjectRelation("subject")}, nil),
				}),
			}},
			wantErr: "duplicates resource type",
		},
		{
			name: "unknown action relation",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				"default": model(map[string]AuthorizationResourceTypeDef{
					"team": resourceType(
						map[string]AuthorizationRelationDef{"member": subjectRelation("subject")},
						map[string]AuthorizationActionDef{"manage": {Relations: []string{"admin"}}},
					),
				}),
			}},
			wantErr: `references unknown relation "admin"`,
		},
		{
			name: "invalid default access policy",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				"default": model(map[string]AuthorizationResourceTypeDef{
					"team": {
						DefaultAccessPolicy: "sometimes",
						Relations:           map[string]AuthorizationRelationDef{"member": subjectRelation("subject")},
					},
				}),
			}},
			wantErr: `authorization.models.default.resourceTypes.team.defaultAccessPolicy must be "allow" or "deny"`,
		},
		{
			name: "model key has surrounding whitespace",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				" default ": model(map[string]AuthorizationResourceTypeDef{
					"team": resourceType(map[string]AuthorizationRelationDef{"member": subjectRelation("subject")}, nil),
				}),
			}},
			wantErr: `authorization.models key " default " must not have surrounding whitespace`,
		},
		{
			name: "invalid allowed target resource type",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				"default": model(map[string]AuthorizationResourceTypeDef{
					"agent_session": resourceType(map[string]AuthorizationRelationDef{
						"parent": {
							AllowedTargets: []AuthorizationAllowedTargetDef{{ResourceType: "missing"}},
						},
					}, nil),
				}),
			}},
			wantErr: `allowedTargets[0].resourceType references unknown resource type "missing"`,
		},
		{
			name: "invalid allowed subject set relation",
			authz: AuthorizationConfig{Models: map[string]AuthorizationModelDef{
				"default": model(map[string]AuthorizationResourceTypeDef{
					"team": resourceType(map[string]AuthorizationRelationDef{
						"member": subjectRelation("subject"),
						"parent": {
							AllowedTargets: []AuthorizationAllowedTargetDef{{SubjectSet: &AuthorizationSubjectSetTargetDef{
								ResourceType: "team",
								Relation:     "missing",
							}}},
						},
					}, nil),
				}),
			}},
			wantErr: `allowedTargets[0].subjectSet.relation references unknown relation "missing"`,
		},
		{
			name: "unknown dynamic resource policy type",
			authz: AuthorizationConfig{
				Models: map[string]AuthorizationModelDef{
					"default": model(map[string]AuthorizationResourceTypeDef{
						"team": resourceType(map[string]AuthorizationRelationDef{"admin": subjectRelation("subject")}, nil),
					}),
				},
				ResourceTypes: map[string]AuthorizationResourcePolicyDef{
					"missing": {Dynamic: AuthorizationResourceDynamicPolicyDef{AllowAdditionalRelationships: true}},
				},
			},
			wantErr: `authorization.resourceTypes.missing references unknown resource type "missing"`,
		},
		{
			name: "malformed relationship resource",
			authz: AuthorizationConfig{
				Models: map[string]AuthorizationModelDef{
					"default": model(map[string]AuthorizationResourceTypeDef{
						"team": resourceType(map[string]AuthorizationRelationDef{"admin": subjectRelation("subject")}, nil),
					}),
				},
				Relationships: []AuthorizationRelationshipDef{{
					Subject:  AuthorizationSubjectDef{Type: "subject", ID: "user:alice"},
					Relation: "admin",
					Resource: AuthorizationResourceDef{Type: "missing", ID: "servicing"},
				}},
			},
			wantErr: `resource.type references unknown resource type "missing"`,
		},
		{
			name: "malformed relationship subject set target relation",
			authz: AuthorizationConfig{
				Models: map[string]AuthorizationModelDef{
					"default": model(map[string]AuthorizationResourceTypeDef{
						"team": resourceType(map[string]AuthorizationRelationDef{"member": subjectRelation("subject")}, nil),
					}),
				},
				Relationships: []AuthorizationRelationshipDef{{
					Subject:  AuthorizationSubjectDef{Type: "subject", ID: "user:alice"},
					Relation: "member",
					Resource: AuthorizationResourceDef{Type: "team", ID: "servicing"},
					Target: AuthorizationRelationshipTargetDef{SubjectSet: &AuthorizationSubjectSetDef{
						Resource: AuthorizationResourceDef{Type: "team", ID: "servicing"},
					}},
				}},
			},
			wantErr: `target.subjectSet.relation is required`,
		},
		{
			name: "relationship without model resource type",
			authz: AuthorizationConfig{
				Relationships: []AuthorizationRelationshipDef{{
					Subject:  AuthorizationSubjectDef{Type: "subject", ID: "user:alice"},
					Relation: "admin",
					Resource: AuthorizationResourceDef{Type: "team", ID: "servicing"},
				}},
			},
			wantErr: `resource.type references unknown resource type "team"`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateStructure(&Config{
				APIVersion:    ConfigAPIVersion,
				Authorization: tc.authz,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateStructure error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAuthorizationRelationshipAllowsExplicitSubjectSetTarget(t *testing.T) {
	t.Parallel()

	err := ValidateStructure(&Config{
		APIVersion: ConfigAPIVersion,
		Authorization: AuthorizationConfig{
			Models: map[string]AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]AuthorizationResourceTypeDef{
						"team": {
							Relations: map[string]AuthorizationRelationDef{
								"member": {SubjectTypes: []string{"subject"}},
							},
						},
						"AuthorizationProvider": {
							Relations: map[string]AuthorizationRelationDef{
								"admin": {
									AllowedTargets: []AuthorizationAllowedTargetDef{
										{SubjectType: "subject"},
										{SubjectSet: &AuthorizationSubjectSetTargetDef{
											ResourceType: "team",
											Relation:     "member",
										}},
									},
								},
							},
							Actions: map[string]AuthorizationActionDef{
								"SetAuthorizationState": {Relations: []string{"admin"}},
							},
						},
					},
				},
			},
			Relationships: []AuthorizationRelationshipDef{
				{
					Subject:  AuthorizationSubjectDef{Type: "subject", ID: "user:michael.wang@valon.com"},
					Relation: "member",
					Resource: AuthorizationResourceDef{Type: "team", ID: "gestalt_admins"},
				},
				{
					Target: AuthorizationRelationshipTargetDef{SubjectSet: &AuthorizationSubjectSetDef{
						Resource: AuthorizationResourceDef{Type: "team", ID: "gestalt_admins"},
						Relation: "member",
					}},
					Relation: "admin",
					Resource: AuthorizationResourceDef{Type: "AuthorizationProvider", ID: "authorization"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateStructure error = %v, want nil", err)
	}
}

func TestNormalizedAuthorizationRelationshipTargetDefCopiesSubjectSet(t *testing.T) {
	t.Parallel()

	subjectSet := &AuthorizationSubjectSetDef{
		Resource: AuthorizationResourceDef{
			Type:       " team ",
			ID:         " servicing ",
			Properties: map[string]string{" role ": " owner "},
		},
		Relation: " member ",
	}

	normalized := normalizedAuthorizationRelationshipTargetDef(AuthorizationRelationshipTargetDef{
		SubjectSet: subjectSet,
	})

	if normalized.SubjectSet == subjectSet {
		t.Fatal("normalized subject set reused original pointer")
	}
	if subjectSet.Resource.Type != " team " || subjectSet.Resource.ID != " servicing " || subjectSet.Relation != " member " {
		t.Fatalf("original subject set mutated: %#v", subjectSet)
	}
	if normalized.SubjectSet.Resource.Type != "team" ||
		normalized.SubjectSet.Resource.ID != "servicing" ||
		normalized.SubjectSet.Resource.Properties["role"] != "owner" ||
		normalized.SubjectSet.Relation != "member" {
		t.Fatalf("normalized subject set = %#v", normalized.SubjectSet)
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

func TestExpandEnvVariablesPreservesWorkflowExpressions(t *testing.T) {
	t.Parallel()

	got, firstMissing, err := expandEnvVariables(
		"value: ${{ signal.data.github }} env: ${NAME} short: $SHORT escaped: $${{ literal }} other: ${json.path}",
		func(key string) (string, bool) {
			if key == "NAME" {
				return "resolved", true
			}
			if key == "SHORT" {
				return "short-resolved", true
			}
			return "", false
		},
		false,
	)
	if err != nil {
		t.Fatalf("expandEnvVariables: %v", err)
	}
	if firstMissing != "" {
		t.Fatalf("expandEnvVariables firstMissing = %q, want empty", firstMissing)
	}
	want := "value: ${{ signal.data.github }} env: resolved short: short-resolved escaped: $${{ literal }} other: ${json.path}"
	if got != want {
		t.Fatalf("expandEnvVariables = %q, want %q", got, want)
	}

	got, firstMissing, err = expandEnvVariables("other: ${json.path}", func(string) (string, bool) {
		return "", false
	}, true)
	if err != nil {
		t.Fatalf("expandEnvVariables preserve missing: %v", err)
	}
	if firstMissing != "" {
		t.Fatalf("expandEnvVariables preserve missing firstMissing = %q, want empty", firstMissing)
	}
	if got != "other: ${json.path}" {
		t.Fatalf("expandEnvVariables preserve missing = %q, want other: ${json.path}", got)
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
			name: "missing authentication provider is allowed",
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
				_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
				if auth != nil {
					t.Fatalf("SelectedAuthenticationProvider = %#v, want nil", auth)
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
    authentication: sample
    indexeddb: sqlite
  management:
    host: 127.0.0.1
    port: 9090
    baseUrl: https://gestalt.example.test:9090
  admin:
    authorizationPolicy: admin_policy
providers:
  authentication:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
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
    authentication: sample
    indexeddb: sqlite
  management:
    host: 127.0.0.1
    port: 9090
    baseUrl: not a url
  admin:
    authorizationPolicy: admin_policy
providers:
  authentication:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
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
    authentication: sample
    indexeddb: sqlite
  management:
    baseUrl: https://gestalt.example.test:9090
  admin:
    authorizationPolicy: admin_policy
providers:
  authentication:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
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
    authentication: sample
    indexeddb: sqlite
  admin:
    authorizationPolicy: admin_policy
    allowedRoles:
      - ""
providers:
  authentication:
    sample:
      source:
        path: ./providers/auth/sample
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
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
apps:
    custom_tool:
      source:
        path: ./manifest.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Apps["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "manifest.yaml") {
			t.Fatalf("unexpected app source path: %q", got)
		}
	})

	t.Run("apiVersion classifies scalar local sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source: ./manifest.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.APIVersion != ConfigAPIVersion {
			t.Fatalf("APIVersion = %q, want %q", cfg.APIVersion, ConfigAPIVersion)
		}
		if got := cfg.Apps["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "manifest.yaml") {
			t.Fatalf("unexpected app source path: %q", got)
		}
	})

	t.Run("apiVersion keeps colon-containing local sources as paths", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source: demo:manifest.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Apps["custom_tool"].SourcePath(); got != filepath.Join(filepath.Dir(path), "demo:manifest.yaml") {
			t.Fatalf("unexpected app source path: %q", got)
		}
	})

	t.Run("apiVersion v5 classifies scalar local provider-release metadata sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source: ./dist/provider-release.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.APIVersion != ConfigAPIVersion {
			t.Fatalf("APIVersion = %q, want %q", cfg.APIVersion, ConfigAPIVersion)
		}
		if got := cfg.Apps["custom_tool"].SourceReleasePath(); got != filepath.Join(filepath.Dir(path), "dist", "provider-release.yaml") {
			t.Fatalf("unexpected app release metadata path: %q", got)
		}
		if got := cfg.Apps["custom_tool"].SourcePath(); got != "" {
			t.Fatalf("SourcePath() = %q, want empty for v5 local release metadata", got)
		}
	})

	t.Run("apiVersion classifies scalar workflow sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
  workflow:
    demo:
      source: ./providers/workflow/demo/manifest.yaml
apps:
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
apiVersion: gestaltd.config/v6
providers:
  ui:
    dashboard:
      path: /dashboard
      source: ./providers/ui/dashboard/manifest.yaml
apps:
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.UI["dashboard"].SourcePath(); got != filepath.Join(filepath.Dir(path), "providers/ui/dashboard/manifest.yaml") {
			t.Fatalf("unexpected ui source path: %q", got)
		}
	})

	t.Run("apiVersion preserves nested source auth on metadata URL sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source:
        url: https://example.com/providers/custom_tool/provider-release.yaml?download=1
        auth:
          token: test-token
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		if got := entry.SourceMetadataURL(); got != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("SourceMetadataURL = %q", got)
		}
		if entry.Source.Auth == nil || entry.Source.Auth.Token != "test-token" {
			t.Fatalf("Source.Auth = %#v", entry.Source.Auth)
		}
		if entry.RouteAuth != nil {
			t.Fatalf("RouteAuth = %#v, want nil", entry.RouteAuth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		source, ok := roundTripped["source"].(map[string]any)
		if !ok {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		if source["url"] != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("round-tripped source.url = %#v", source["url"])
		}
		auth, ok := source["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("round-tripped source.auth = %#v", source["auth"])
		}
		if _, ok := roundTripped["auth"]; ok {
			t.Fatalf("round-tripped auth = %#v, want absent", roundTripped["auth"])
		}

		marshaledConfig, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("yaml.Marshal config: %v", err)
		}
		var roundTrippedConfig map[string]any
		if err := yaml.Unmarshal(marshaledConfig, &roundTrippedConfig); err != nil {
			t.Fatalf("yaml.Unmarshal config: %v", err)
		}
		apps, ok := roundTrippedConfig["apps"].(map[string]any)
		if !ok {
			t.Fatalf("apps = %#v", roundTrippedConfig["apps"])
		}
		app, ok := apps["custom_tool"].(map[string]any)
		if !ok {
			t.Fatalf("apps.custom_tool = %#v", apps["custom_tool"])
		}
		source, ok = app["source"].(map[string]any)
		if !ok {
			t.Fatalf("config round-tripped source = %#v", app["source"])
		}
		if source["url"] != "https://example.com/providers/custom_tool/provider-release.yaml?download=1" {
			t.Fatalf("config round-tripped source.url = %#v", source["url"])
		}
		auth, ok = source["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("config round-tripped source.auth = %#v", source["auth"])
		}
		if _, ok := app["auth"]; ok {
			t.Fatalf("config round-tripped auth = %#v, want absent", app["auth"])
		}
	})

	t.Run("apiVersion preserves bare source.url mapping on round-trip", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: corporate
providers:
  authentication:
    corporate:
      source: https://example.com/providers/auth/corporate/provider-release.yaml
apps:
    custom_tool:
      source:
        url: https://example.com/providers/custom_tool/provider-release.yaml
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		if got := entry.SourceMetadataURL(); got != "https://example.com/providers/custom_tool/provider-release.yaml" {
			t.Fatalf("SourceMetadataURL = %q", got)
		}
		if entry.Source.Auth != nil {
			t.Fatalf("Source.Auth = %#v, want nil", entry.Source.Auth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		source, ok := roundTripped["source"].(map[string]any)
		if !ok {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		if source["url"] != "https://example.com/providers/custom_tool/provider-release.yaml" {
			t.Fatalf("round-tripped source.url = %#v", source["url"])
		}
		if _, ok := source["auth"]; ok {
			t.Fatalf("round-tripped source.auth = %#v, want absent", source["auth"])
		}

		marshaledConfig, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("yaml.Marshal config: %v", err)
		}
		var roundTrippedConfig map[string]any
		if err := yaml.Unmarshal(marshaledConfig, &roundTrippedConfig); err != nil {
			t.Fatalf("yaml.Unmarshal config: %v", err)
		}
		apps, ok := roundTrippedConfig["apps"].(map[string]any)
		if !ok {
			t.Fatalf("apps = %#v", roundTrippedConfig["apps"])
		}
		app, ok := apps["custom_tool"].(map[string]any)
		if !ok {
			t.Fatalf("apps.custom_tool = %#v", apps["custom_tool"])
		}
		source, ok = app["source"].(map[string]any)
		if !ok {
			t.Fatalf("config round-tripped source = %#v", app["source"])
		}
		if source["url"] != "https://example.com/providers/custom_tool/provider-release.yaml" {
			t.Fatalf("config round-tripped source.url = %#v", source["url"])
		}
		if _, ok := source["auth"]; ok {
			t.Fatalf("config round-tripped source.auth = %#v, want absent", source["auth"])
		}
		if _, ok := app["auth"]; ok {
			t.Fatalf("config round-tripped auth = %#v, want absent", app["auth"])
		}
	})

	t.Run("apiVersion preserves nested source auth with app route auth overrides", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: corporate
providers:
  authentication:
    corporate:
      source: https://example.com/providers/auth/corporate/provider-release.yaml
apps:
    custom_tool:
      source:
        url: https://example.com/providers/custom_tool/provider-release.yaml
        auth:
          token: source-token
      auth:
        provider: server
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		if got := entry.SourceMetadataURL(); got != "https://example.com/providers/custom_tool/provider-release.yaml" {
			t.Fatalf("SourceMetadataURL = %q", got)
		}
		if entry.Source.Auth == nil || entry.Source.Auth.Token != "source-token" {
			t.Fatalf("Source.Auth = %#v", entry.Source.Auth)
		}
		if entry.RouteAuth == nil || entry.RouteAuth.Provider != "server" {
			t.Fatalf("RouteAuth = %#v", entry.RouteAuth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		source, ok := roundTripped["source"].(map[string]any)
		if !ok {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		if source["url"] != "https://example.com/providers/custom_tool/provider-release.yaml" {
			t.Fatalf("round-tripped source.url = %#v", source["url"])
		}
		sourceAuth, ok := source["auth"].(map[string]any)
		if !ok || sourceAuth["token"] != "source-token" {
			t.Fatalf("round-tripped source.auth = %#v", source["auth"])
		}
		auth, ok := roundTripped["auth"].(map[string]any)
		if !ok || auth["provider"] != "server" {
			t.Fatalf("round-tripped auth = %#v", roundTripped["auth"])
		}
	})

	t.Run("apiVersion preserves github release metadata sources with nested auth", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source:
        githubRelease:
          repo: valon-technologies/toolshed
          tag: apps/custom-tool/v0.0.1-alpha.1
          asset: provider-release.yaml
        auth:
          token: test-token
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		wantLocation := "github-release://github.com/valon-technologies/toolshed?asset=provider-release.yaml&tag=apps%2Fcustom-tool%2Fv0.0.1-alpha.1"
		if got := entry.SourceRemoteLocation(); got != wantLocation {
			t.Fatalf("SourceRemoteLocation = %q, want %q", got, wantLocation)
		}
		release := entry.Source.GitHubReleaseSource()
		if release == nil || release.Repo != "valon-technologies/toolshed" || release.Tag != "apps/custom-tool/v0.0.1-alpha.1" || release.Asset != "provider-release.yaml" {
			t.Fatalf("Source.GitHubRelease = %#v", release)
		}
		if entry.Source.Auth == nil || entry.Source.Auth.Token != "test-token" {
			t.Fatalf("Source.Auth = %#v", entry.Source.Auth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		source, ok := roundTripped["source"].(map[string]any)
		if !ok {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		githubRelease, ok := source["githubRelease"].(map[string]any)
		if !ok || githubRelease["repo"] != "valon-technologies/toolshed" || githubRelease["tag"] != "apps/custom-tool/v0.0.1-alpha.1" || githubRelease["asset"] != "provider-release.yaml" {
			t.Fatalf("round-tripped githubRelease = %#v", source["githubRelease"])
		}
		auth, ok := source["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("round-tripped source.auth = %#v", source["auth"])
		}
		if _, ok := roundTripped["auth"]; ok {
			t.Fatalf("round-tripped auth = %#v, want absent", roundTripped["auth"])
		}
	})

	t.Run("apiVersion normalizes git source locations", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source:
        git:
          repo: HTTPS://GitHub.com/Valon-Technologies/Gestalt-Providers
          ref: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
          path: apps//custom_tool/../custom_tool/manifest.yaml
          materialization: source
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		wantLocation := "git+https://github.com/Valon-Technologies/Gestalt-Providers.git@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa#apps/custom_tool/manifest.yaml"
		if got := entry.SourceRemoteLocation(); got != wantLocation {
			t.Fatalf("SourceRemoteLocation = %q, want %q", got, wantLocation)
		}
		if got := entry.SourceReleaseLocation(); got != wantLocation {
			t.Fatalf("SourceReleaseLocation = %q, want %q", got, wantLocation)
		}
		gitSource := entry.Source.GitSource()
		if gitSource == nil {
			t.Fatal("Source.GitSource() = nil")
			return
		}
		repo, ref, manifestPath := gitSource.NormalizedLocationParts()
		if repo != "https://github.com/Valon-Technologies/Gestalt-Providers.git" ||
			ref != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
			manifestPath != "apps/custom_tool/manifest.yaml" {
			t.Fatalf("NormalizedLocationParts = (%q, %q, %q)", repo, ref, manifestPath)
		}
	})

	t.Run("apiVersion preserves nested source auth on local release metadata sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source:
        path: ./apps/custom_tool/dist/provider-release.yaml
        auth:
          token: test-token
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Apps["custom_tool"]
		wantPath := filepath.Join(filepath.Dir(path), "apps", "custom_tool", "dist", "provider-release.yaml")
		if got := entry.SourceReleasePath(); got != wantPath {
			t.Fatalf("SourceReleasePath = %q, want %q", got, wantPath)
		}
		if entry.Source.Auth == nil || entry.Source.Auth.Token != "test-token" {
			t.Fatalf("Source.Auth = %#v", entry.Source.Auth)
		}
		marshaled, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var roundTripped map[string]any
		if err := yaml.Unmarshal(marshaled, &roundTripped); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}
		source, ok := roundTripped["source"].(map[string]any)
		if !ok || source["path"] != wantPath {
			t.Fatalf("round-tripped source = %#v", roundTripped["source"])
		}
		auth, ok := source["auth"].(map[string]any)
		if !ok || auth["token"] != "test-token" {
			t.Fatalf("round-tripped source.auth = %#v", source["auth"])
		}
		if _, ok := roundTripped["auth"]; ok {
			t.Fatalf("round-tripped auth = %#v, want absent", roundTripped["auth"])
		}
	})

	t.Run("apiVersion preserves builtin scalar host provider sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
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
apps:
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

	t.Run("apiVersion preserves package host provider sources", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
server:
  encryptionKey: server-key
providers:
  secrets:
    default:
      source:
        repo: valon
        package: github.com/valon-technologies/gestalt-providers/secrets/google
        version: 0.0.1-alpha.2
      config:
        project: test-project
apps:
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		source := cfg.Providers.Secrets["default"].Source
		if source.Builtin != "" {
			t.Fatalf("secrets builtin = %q, want empty", source.Builtin)
		}
		if !source.IsPackage() {
			t.Fatalf("secrets source should remain package-backed: %#v", source)
		}
		if got := source.PackageAddress(); got != "github.com/valon-technologies/gestalt-providers/secrets/google" {
			t.Fatalf("secrets package = %q, want google secrets package", got)
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

	t.Run("relative ui provider path resolves from config directory", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  ui:
    roadmap:
      source:
        path: ./ui/default/provider.yaml
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
			return
		}
		wantPath := filepath.Join(filepath.Dir(path), "ui", "default", "provider.yaml")
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
			return
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
			return
		}
		wantSourcePath := filepath.Join(filepath.Dir(path), "web", "roadmap", "manifest.yaml")
		if got := entry.Source.Path; got != wantSourcePath {
			t.Fatalf(`Providers.UI["roadmap"].Source.Path = %q, want %q`, got, wantSourcePath)
		}
		if got := entry.Path; got != "/create-customer-roadmap-review" {
			t.Fatalf(`Providers.UI["roadmap"].Path = %q, want %q`, got, "/create-customer-roadmap-review")
		}
	})

	t.Run("app ui object binds an explicit ui entry", func(t *testing.T) {
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
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    ui:
      bundle: roadmap
      path: /create-customer-roadmap-review/
    authorizationPolicy: roadmap_policy
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
			return
		}
		if got := entry.Path; got != "/create-customer-roadmap-review" {
			t.Fatalf(`Providers.UI["roadmap"].Path = %q, want %q`, got, "/create-customer-roadmap-review")
		}
		if got := entry.AuthorizationPolicy; got != "roadmap_policy" {
			t.Fatalf(`Providers.UI["roadmap"].AuthorizationPolicy = %q, want %q`, got, "roadmap_policy")
		}
		if got := entry.OwnerApp; got != "roadmap" {
			t.Fatalf(`Providers.UI["roadmap"].OwnerApp = %q, want %q`, got, "roadmap")
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

	t.Run("app-owned ui overlay still validates reserved paths", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    ui:
      path: /api
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
		if !strings.Contains(err.Error(), `apps.roadmap.ui.path "/api" conflicts with reserved path "/api"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("same-name app-owned ui overlay only suppresses duplicate path checks", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    ui:
      path: /api
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
		if !strings.Contains(err.Error(), `apps.roadmap.ui.path "/api" conflicts with reserved path "/api"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("app ui path prefix collision with mounted ui is rejected", func(t *testing.T) {
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
apps:
  admin:
    source:
      path: ./app/manifest.yaml
    ui:
      path: /tools/admin
server:
  providers:
    indexeddb: sqlite
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `ui.docs.path "/tools" conflicts with apps.admin.ui.path "/tools/admin"`) {
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

	t.Run("ui scalar source is treated as local path", func(t *testing.T) {
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

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.UI["roadmap"].SourcePath(); got != filepath.Join(filepath.Dir(path), "stdout") {
			t.Fatalf("ui source path = %q, want local path", got)
		}
	})

	t.Run("external credentials scalar local source is a path", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
  externalCredentials:
    default:
      source: local
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: sqlite
    externalCredentials: default
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Providers.ExternalCredentials["default"].SourcePath(); got != filepath.Join(filepath.Dir(path), "local") {
			t.Fatalf("externalCredentials source path = %q, want local path", got)
		}
	})
}

func TestLoadConfigAppIndexedDBBindings(t *testing.T) {
	t.Parallel()

	t.Run("app accepts indexeddb config object", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		want := &IndexedDBBindingConfig{
			Provider:     "archive",
			DB:           "roadmap_review",
			ObjectStores: []string{"tasks", "snapshots"},
		}
		got := cfg.Apps["roadmap"].IndexedDB
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Apps[roadmap].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("app accepts scalar indexeddb provider name", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		got := cfg.Apps["roadmap"].IndexedDB
		want := &IndexedDBBindingConfig{
			Provider: "sqlite",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Apps[roadmap].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("rejects indexeddb bindings outside apps", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), `ui.root.indexeddb is only supported on apps.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("loads app surface overrides", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  datadog:
    source:
      path: ./app/manifest.yaml
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
		if cfg.Apps["datadog"].Surfaces == nil || cfg.Apps["datadog"].Surfaces.OpenAPI == nil {
			t.Fatal("Apps[datadog].Surfaces.OpenAPI is nil")
		}
		if got := cfg.Apps["datadog"].Surfaces.OpenAPI.BaseURL; got != "https://api.us5.datadoghq.com" {
			t.Fatalf("Apps[datadog].Surfaces.OpenAPI.BaseURL = %q, want %q", got, "https://api.us5.datadoghq.com")
		}
	})

	t.Run("loads app indexeddb db override", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  datadog:
    source:
      path: ./app/manifest.yaml
    indexeddb:
      db: app_data
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
		want := &IndexedDBBindingConfig{DB: "app_data"}
		if got := cfg.Apps["datadog"].IndexedDB; !reflect.DeepEqual(got, want) {
			t.Fatalf("Apps[datadog].IndexedDB = %#v, want %#v", got, want)
		}
	})

	t.Run("rejects surface overrides outside apps", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), `ui.root.surfaces is only supported on apps.*`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects app mount fields outside apps", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), `field mountPath not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown indexeddb provider names", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		if !strings.Contains(err.Error(), `apps.roadmap.idb.provider references unknown indexeddb "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects explicit indexeddb object without provider or inherited indexeddb provider", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			body string
		}{
			{
				name: "db override",
				body: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    indexeddb:
      db: roadmap_state
server:
  encryptionKey: server-key
`,
			},
			{
				name: "objectStores only",
				body: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
				if !strings.Contains(err.Error(), `apps.roadmap.indexeddb requires idb.provider or an available selected/default indexeddb provider`) {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("accepts empty indexeddb object without inherited indexeddb provider", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name          string
			body          string
			wantIndexedDB bool
		}{
			{
				name: "empty object with no indexeddb provider definitions",
				body: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    indexeddb: {}
server:
  encryptionKey: server-key
`,
				wantIndexedDB: true,
			},
			{
				name: "omitted indexeddb with no indexeddb provider definitions",
				body: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
				if got := cfg.Apps["roadmap"].IndexedDB != nil; got != tc.wantIndexedDB {
					t.Fatalf("IndexedDB presence = %v, want %v", got, tc.wantIndexedDB)
				}
			})
		}
	})

	t.Run("rejects duplicate indexeddb object stores", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		if !strings.Contains(err.Error(), `apps.roadmap.idb.objectStores[1] duplicates "tasks"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects indexeddb sequences", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		if !strings.Contains(err.Error(), `indexeddb must be a mapping or scalar provider name`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("app accepts s3 bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
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
		if got := cfg.Apps["roadmap"].S3; !reflect.DeepEqual(got, []string{"assets"}) {
			t.Fatalf("Apps[roadmap].S3 = %#v, want %#v", got, []string{"assets"})
		}
	})

	t.Run("top-level workflows config uses canonical targets", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
  slack:
    source:
      path: ./providers/slack/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
              credentialMode: none
              input:
                source: yaml
      on:
        schedule:
          schedule:
            cron: "0 2 * * *"
    task_updated:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-events
      steps:
        - id: main
          app:
              name: roadmap
              operation: backfill_items
              input:
                source: event
      on:
        event:
          event:
            type: roadmap.task.updated
            source: roadmap
          paused: true
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
		wantScheduleDefinition := WorkflowDefinitionConfig{
			Provider: "temporal",
			RunAs: &WorkflowRunAsConfig{
				Subject: &WorkflowRunAsSubjectConfig{
					ID: "service_account:roadmap-workflow",
				},
			},
			Steps: workflowTestAppStepConfig("roadmap", "nightly_sync", providermanifestv1.ConnectionModeNone, map[string]any{
				"source": "yaml",
			}),
			On: map[string]WorkflowActivationConfig{
				"schedule": {
					Schedule: &WorkflowScheduleActivationConfig{
						Cron:     "0 2 * * *",
						Timezone: "UTC",
					},
				},
			},
		}
		if got := cfg.Workflows.Definitions["nightly"]; !reflect.DeepEqual(got, wantScheduleDefinition) {
			t.Fatalf("Workflows.Definitions[nightly] = %#v, want %#v", got, wantScheduleDefinition)
		}
		wantEventDefinition := WorkflowDefinitionConfig{
			Provider: "temporal",
			RunAs: &WorkflowRunAsConfig{
				Subject: &WorkflowRunAsSubjectConfig{
					ID: "service_account:roadmap-events",
				},
			},
			Steps: workflowTestAppStepConfig("roadmap", "backfill_items", "", map[string]any{
				"source": "event",
			}),
			On: map[string]WorkflowActivationConfig{
				"event": {
					Event: &WorkflowEventActivationConfig{
						Type:   "roadmap.task.updated",
						Source: "roadmap",
					},
					Paused: true,
				},
			},
		}
		if got := cfg.Workflows.Definitions["task_updated"]; !reflect.DeepEqual(got, wantEventDefinition) {
			t.Fatalf("Workflows.Definitions[task_updated] = %#v, want %#v", got, wantEventDefinition)
		}
	})

	t.Run("workflow scalar expressions compile to value refs", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    review:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-events
      steps:
        - id: collect
          app:
              name: roadmap
              operation: collect
              input:
                repo: "${{ input.github.repository }}"
        - id: notify
          app:
              name: roadmap
              operation: notify
              input:
                previous: "${{ steps.collect.outputs }}"
                text: "repo=${{ input.github.repository }} status=${{ steps.collect.outputs.status }}"
      on:
        github_pr:
          event:
            type: github.pull_request
            source: github
          input:
            github: "${{ signal.data.github }}"
            raw: "${{ signal.data.raw }}"
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		definition := cfg.Workflows.Definitions["review"]
		activationInput := definition.On["github_pr"].Input.Object
		if got := activationInput["github"].Signal; got != "data.github" {
			t.Fatalf("activation github signal = %q, want data.github", got)
		}
		if got := activationInput["raw"].Signal; got != "data.raw" {
			t.Fatalf("activation raw signal = %q, want data.raw", got)
		}
		collectInput := definition.Steps[0].App.Input.Object
		if got := collectInput["repo"].Input; got != "github.repository" {
			t.Fatalf("collect repo input = %q, want github.repository", got)
		}
		notifyInput := definition.Steps[1].App.Input.Object
		previous := notifyInput["previous"].StepOutput
		if previous == nil || previous.StepID != "collect" || previous.Path != "" {
			t.Fatalf("notify previous step output = %#v, want collect whole output", previous)
		}
		text := notifyInput["text"].Template
		if text == nil || text.Template != "repo=${{ input.github.repository }} status=${{ steps.collect.outputs.status }}" {
			t.Fatalf("notify text template = %#v", text)
		}
	})

	t.Run("workflow target validation errors use canonical paths", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			yaml string
			want string
		}{
			{
				name: "unknown definition app",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: missing
              operation: nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].app.name references unknown app "missing"`,
			},
			{
				name: "definition rejects invokes",
				yaml: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
  slack:
    source:
      path: ./providers/slack/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
      invokes:
        - app: slack
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `field invokes not found`,
			},
			{
				name: "definition app rejects unsupported credential mode",
				yaml: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
              credentialMode: unsupported-mode
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].app.credentialMode "unsupported-mode" is not supported`,
			},
			{
				name: "agent step rejects sub-second negative timeout",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          timeout: -500ms
          agent:
              provider: simple
              prompt: "diagnose"
              output:
                text: {}
      on:
        nightly:
          schedule:
            cron: "0 2 * * *"
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].timeout must not be negative for agent steps`,
			},
			{
				name: "agent step when rejects self reference",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          timeout: 1s
          agent:
              provider: simple
              prompt: "diagnose"
              output:
                text: {}
          when:
              value:
                stepOutput:
                  stepId: diagnosis
                  path: agent.output.structured.value.actionable_for_pr
              equals: true
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].when.value.step_output.step_id "diagnosis" must reference an earlier step`,
			},
			{
				name: "step inputs reject future reference",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          timeout: 1s
          inputs:
              source:
                stepOutput:
                  stepId: pr_fix
                  path: agent.output.text.text
          agent:
              provider: simple
              prompt: "diagnose"
              output:
                text: {}
        - id: pr_fix
          timeout: 1s
          agent:
              provider: simple
              prompt: "fix"
              output:
                text: {}
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].inputs.source.step_output.step_id "pr_fix" must reference an earlier step`,
			},
			{
				name: "app input template rejects future reference",
				yaml: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          app:
              name: roadmap
              operation: diagnose
              input:
                message: "fix ${{ steps.pr_fix.outputs.text }}"
        - id: pr_fix
          app:
              name: roadmap
              operation: fix
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[0].app.input.message.template expression "steps.pr_fix.outputs.text": step "pr_fix" must reference an earlier step`,
			},
			{
				name: "agent step when requires equals",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          timeout: 1s
          agent:
              provider: simple
              prompt: "diagnose"
              output:
                text: {}
        - id: pr_fix
          timeout: 1s
          agent:
              provider: simple
              prompt: "fix"
              output:
                text: {}
          when:
              value:
                stepOutput:
                  stepId: diagnosis
                  path: agent.output.structured.value.actionable_for_pr
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[1].when.equals is required`,
			},
			{
				name: "agent step when requires value",
				yaml: `
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: diagnosis
          timeout: 1s
          agent:
              provider: simple
              prompt: "diagnose"
              output:
                text: {}
        - id: pr_fix
          timeout: 1s
          agent:
              provider: simple
              prompt: "fix"
              output:
                text: {}
          when:
              equals: null
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.steps[1].when.value is required`,
			},
			{
				name: "definition agent missing provider",
				yaml: `
workflows:
  definitions:
    task_updated:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-events
      steps:
        - id: run
          timeout: 1s
          agent:
              model: gpt-5.5
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.task_updated.steps[0].agent.provider is required`,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, tc.yaml)
				_, err := Load(path)
				if err == nil {
					t.Fatal("Load succeeded, want error")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Load error = %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("workflow definitions require runAs", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			yaml string
			want string
		}{
			{
				name: "scheduled definition",
				yaml: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.nightly.runAs is required`,
			},
			{
				name: "event definition",
				yaml: `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    task_updated:
      provider: temporal
      steps:
        - id: main
          app:
              name: roadmap
              operation: backfill_items
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
server:
  encryptionKey: server-key
`,
				want: `workflows.definitions.task_updated.runAs is required`,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				path := mustWriteConfigFile(t, tc.yaml)
				_, err := Load(path)
				if err == nil {
					t.Fatal("Load succeeded, want error")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Load error = %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("workflow binding can select an explicit provider when multiple workflow providers exist", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
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
		effective, _, err := cfg.EffectiveWorkflowProvider(cfg.Workflows.Definitions["nightly"].Provider)
		if err != nil {
			t.Fatalf("EffectiveWorkflowProvider: %v", err)
		}
		if effective != "temporal" {
			t.Fatalf("ProviderName = %q, want %q", effective, "temporal")
		}
	})

	t.Run("rejects workflow bindings outside apps", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), `field workflow not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown workflow provider names", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: missing
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
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
		if !strings.Contains(err.Error(), `workflows.definitions.nightly.provider`) || !strings.Contains(err.Error(), `unknown workflow "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects multiple workflow defaults even when apps bind explicitly", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    nightly:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
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

	t.Run("allows workflow definitions without provider operation allowlists", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    invalid:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
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
		if err != nil {
			t.Fatalf("Load: unexpected error: %v", err)
		}
	})

	t.Run("allows workflow event activations without provider operation allowlists", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    invalid:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-events
      steps:
        - id: main
          app:
              name: roadmap
              operation: backfill_items
      on:
        task_updated:
          event:
            type: roadmap.task.updated
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
		if err != nil {
			t.Fatalf("Load: unexpected error: %v", err)
		}
	})

	t.Run("rejects workflow event activations without event type", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    invalid:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-events
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
      on:
        task_updated:
          event:
            source: roadmap
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
		if !strings.Contains(err.Error(), `workflows.definitions.invalid.on.task_updated.event.type is required`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid workflow schedule activation cron and timezone", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
workflows:
  definitions:
    invalid:
      provider: temporal
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: main
          app:
              name: roadmap
              operation: nightly_sync
      on:
        nightly:
          schedule:
            cron: "0 0 0 * * *"
            timezone: Mars/Olympus
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
		if !strings.Contains(err.Error(), `workflows.definitions.invalid.on.nightly.schedule.cron`) {
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
		want := &IndexedDBBindingConfig{
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

	t.Run("agent provider accepts indexeddb bindings", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      indexeddb:
        provider: agent_state
        db: agent_simple
        objectStores:
          - runs
          - run_idempotency
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: agent_state
  encryptionKey: server-key
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := &IndexedDBBindingConfig{
			Provider:     "agent_state",
			DB:           "agent_simple",
			ObjectStores: []string{"runs", "run_idempotency"},
		}
		if got := cfg.Providers.Agent["simple"].IndexedDB; !reflect.DeepEqual(got, want) {
			t.Fatalf("Providers.Agent[simple].IndexedDB = %#v, want %#v", got, want)
		}
		effective, err := cfg.EffectiveAgentIndexedDB("simple", cfg.Providers.Agent["simple"])
		if err != nil {
			t.Fatalf("EffectiveAgentIndexedDB: %v", err)
		}
		if effective.ProviderName != "agent_state" || effective.DB != "agent_simple" {
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
		if !strings.Contains(err.Error(), `providers.workflow.basic.idb.provider references unknown indexeddb "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown indexeddb bindings on agent providers", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      indexeddb:
        provider: missing
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
server:
  providers:
    indexeddb: agent_state
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `providers.agent.simple.idb.provider references unknown indexeddb "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown hosted runtime on agent providers without indexeddb", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: missing
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  runtime:
    defaultProvider: hosted
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `providers.agent.simple.runtime.provider references unknown runtime "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects unknown top-level hosted runtime on agent providers", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
        provider: missing
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  runtime:
    defaultProvider: hosted
  encryptionKey: server-key
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), `providers.agent.simple.runtime.provider references unknown runtime "missing"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadConfigAgentRuntimeLifecycleFields(t *testing.T) {
	t.Parallel()

	base := `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      indexeddb: agent_state
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            maxReadyInstances: 2
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`
	t.Run("accepts required lifecycle fields under agent runtime pool", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, base)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		runtimeCfg := cfg.Providers.Agent["simple"].Runtime
		if runtimeCfg.Pool == nil {
			t.Fatal("runtime pool = nil")
		}
		if runtimeCfg.Pool.MinReadyInstances != 1 || runtimeCfg.Pool.MaxReadyInstances != 2 {
			t.Fatalf("runtime pool ready instances = %d/%d, want 1/2", runtimeCfg.Pool.MinReadyInstances, runtimeCfg.Pool.MaxReadyInstances)
		}
		if runtimeCfg.Pool.RestartPolicy != RuntimePlacementRestartPolicyAlways {
			t.Fatalf("restartPolicy = %q, want %q", runtimeCfg.Pool.RestartPolicy, RuntimePlacementRestartPolicyAlways)
		}
	})

	t.Run("accepts restart policy with host indexeddb only", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            maxReadyInstances: 2
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		entry := cfg.Providers.Agent["simple"]
		if entry.IndexedDB != nil {
			t.Fatalf("agent indexeddb = %#v, want host-level indexeddb only", entry.IndexedDB)
		}
		if entry.Runtime.Pool.RestartPolicy != RuntimePlacementRestartPolicyAlways {
			t.Fatalf("runtime restartPolicy = %q, want %q", entry.Runtime.Pool.RestartPolicy, RuntimePlacementRestartPolicyAlways)
		}
	})

	t.Run("accepts required lifecycle fields under agent runtime", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      indexeddb: agent_state
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            maxReadyInstances: 2
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		runtimeCfg := cfg.Providers.Agent["simple"].Runtime
		if runtimeCfg.Pool == nil {
			t.Fatal("runtime pool = nil")
		}
		if runtimeCfg.Pool.MinReadyInstances != 1 || runtimeCfg.Pool.MaxReadyInstances != 2 {
			t.Fatalf("runtime pool ready instances = %d/%d, want 1/2", runtimeCfg.Pool.MinReadyInstances, runtimeCfg.Pool.MaxReadyInstances)
		}
		if runtimeCfg.Pool.RestartPolicy != RuntimePlacementRestartPolicyAlways {
			t.Fatalf("runtime restartPolicy = %q, want %q", runtimeCfg.Pool.RestartPolicy, RuntimePlacementRestartPolicyAlways)
		}
	})

	t.Run("accepts required lifecycle fields under agent runtime", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      indexeddb: agent_state
      runtime:
        provider: hosted
        pool:
          minReadyInstances: 1
          maxReadyInstances: 2
          startupTimeout: 5m
          healthCheckInterval: 30s
          restartPolicy: always
          drainTimeout: 2m
  indexeddb:
    agent_state:
      source:
        path: ./providers/datastore/sqlite
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		runtimeCfg := cfg.Providers.Agent["simple"].Runtime
		if runtimeCfg == nil || runtimeCfg.Pool == nil {
			t.Fatalf("runtime pool = %#v", runtimeCfg)
		}
		if cfg.Providers.Agent["simple"].RuntimePlacementConfig() != runtimeCfg {
			t.Fatal("RuntimePlacementConfig did not return top-level runtime")
		}
		if runtimeCfg.Pool.MinReadyInstances != 1 || runtimeCfg.Pool.MaxReadyInstances != 2 {
			t.Fatalf("runtime pool ready instances = %d/%d, want 1/2", runtimeCfg.Pool.MinReadyInstances, runtimeCfg.Pool.MaxReadyInstances)
		}
	})

	t.Run("accepts Docker config JSON image pull auth under agent runtime", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          image: ghcr.io/example/simple-agent:latest
          imagePullAuth:
            dockerConfigJson: '{"auths":{"ghcr.io":{"username":"ghcr-user","password":"ghcr-token"}}}'
          pool:
            minReadyInstances: 1
            maxReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: never
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		auth := cfg.Providers.Agent["simple"].Runtime.ImagePullAuth
		if auth == nil {
			t.Fatal("imagePullAuth = nil")
			return
		}
		if auth.DockerConfigJSON != `{"auths":{"ghcr.io":{"username":"ghcr-user","password":"ghcr-token"}}}` {
			t.Fatalf("dockerConfigJson = %q", auth.DockerConfigJSON)
		}
	})

	t.Run("accepts secret ref Docker config JSON image pull auth under agent runtime", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  secrets:
    secrets:
      source: env
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          image: ghcr.io/example/simple-agent:latest
          imagePullAuth:
            dockerConfigJson:
              secret:
                provider: secrets
                name: ghcr-agent-runtime-dockerconfigjson
          pool:
            minReadyInstances: 1
            maxReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: never
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		auth := cfg.Providers.Agent["simple"].Runtime.ImagePullAuth
		if auth == nil {
			t.Fatal("imagePullAuth = nil")
			return
		}
		if _, isSecretRef, err := ParseSecretRefTransport(auth.DockerConfigJSON); err != nil || !isSecretRef {
			t.Fatalf("dockerConfigJson secret ref parse = %v, %v; want encoded secret ref", isSecretRef, err)
		}
	})

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "rejects invalid Docker config JSON image pull auth",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          image: ghcr.io/example/simple-agent:latest
          imagePullAuth:
            dockerConfigJson: '{}'
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: `providers.agent.simple.runtime.imagePullAuth.dockerConfigJson: must contain a non-empty "auths" object`,
		},
		{
			name: "rejects missing runtime pool",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.runtime.pool.minReadyInstances is required",
		},
		{
			name: "rejects missing lifecycle fields",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.runtime.pool.maxReadyInstances is required",
		},
		{
			name: "rejects removed execution config",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      execution:
        mode: hosted
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "provider execution has been removed; use runtime instead",
		},
		{
			name: "rejects missing lifecycle fields under runtime",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.runtime.pool.maxReadyInstances is required",
		},
		{
			name: "rejects max below min",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 2
            maxReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.runtime.pool.maxReadyInstances must be greater than or equal to minReadyInstances",
		},
		{
			name: "rejects unknown restart policy",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            maxReadyInstances: 2
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: sometimes
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.runtime.pool.restartPolicy must be one of",
		},
		{
			name: "rejects lifecycle fields on runtime",
			yaml: `
apps:
  service:
    source:
      path: ./apps/service/manifest.yaml
    runtime:
        provider: hosted
        pool:
          minReadyInstances: 1
          maxReadyInstances: 2
          startupTimeout: 5m
          healthCheckInterval: 30s
          restartPolicy: always
          drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "apps.service.runtime lifecycle fields are only supported on hosted agent and workflow providers",
		},
		{
			name: "rejects lifecycle fields on app top-level runtime",
			yaml: `
apps:
  service:
    source:
      path: ./apps/service/manifest.yaml
    runtime:
      provider: hosted
      pool:
        minReadyInstances: 1
        maxReadyInstances: 2
        startupTimeout: 5m
        healthCheckInterval: 30s
        restartPolicy: always
        drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "apps.service.runtime lifecycle fields are only supported on hosted agent and workflow providers",
		},
		{
			name: "rejects removed local execution mode",
			yaml: `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      runtime:
        provider: hosted
      execution:
        mode: local
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
server:
  encryptionKey: server-key
`,
			want: "provider execution has been removed; use runtime instead",
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
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadConfigWorkflowRuntimeLifecycleFields(t *testing.T) {
	t.Parallel()

	t.Run("accepts hosted workflow runtime pool without indexeddb", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
      runtime:
          provider: hosted
          template: workflow-workers
          metadata:
            workload: temporal-workers
          pool:
            minReadyInstances: 2
            maxReadyInstances: 2
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/gke
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		runtimeCfg := cfg.Providers.Workflow["temporal"].Runtime
		if runtimeCfg == nil || runtimeCfg.Pool == nil {
			t.Fatalf("workflow runtime pool = %#v", runtimeCfg)
		}
		if runtimeCfg.Pool.MinReadyInstances != 2 || runtimeCfg.Pool.MaxReadyInstances != 2 {
			t.Fatalf("workflow pool ready instances = %d/%d, want 2/2", runtimeCfg.Pool.MinReadyInstances, runtimeCfg.Pool.MaxReadyInstances)
		}
		if runtimeCfg.Pool.RestartPolicy != RuntimePlacementRestartPolicyAlways {
			t.Fatalf("workflow restartPolicy = %q, want %q", runtimeCfg.Pool.RestartPolicy, RuntimePlacementRestartPolicyAlways)
		}
	})

	t.Run("accepts top-level hosted workflow runtime pool without indexeddb", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
      runtime:
        provider: hosted
        template: workflow-workers
        metadata:
          workload: temporal-workers
        pool:
          minReadyInstances: 2
          maxReadyInstances: 2
          startupTimeout: 5m
          healthCheckInterval: 30s
          restartPolicy: always
          drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/gke
server:
  encryptionKey: server-key
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		runtimeCfg := cfg.Providers.Workflow["temporal"].Runtime
		if runtimeCfg == nil || runtimeCfg.Pool == nil {
			t.Fatalf("workflow runtime pool = %#v", runtimeCfg)
		}
		if cfg.Providers.Workflow["temporal"].RuntimePlacementConfig() != runtimeCfg {
			t.Fatal("RuntimePlacementConfig did not return top-level workflow runtime")
		}
		if runtimeCfg.Pool.MinReadyInstances != 2 || runtimeCfg.Pool.MaxReadyInstances != 2 {
			t.Fatalf("workflow pool ready instances = %d/%d, want 2/2", runtimeCfg.Pool.MinReadyInstances, runtimeCfg.Pool.MaxReadyInstances)
		}
		if runtimeCfg.Pool.RestartPolicy != RuntimePlacementRestartPolicyAlways {
			t.Fatalf("workflow restartPolicy = %q, want %q", runtimeCfg.Pool.RestartPolicy, RuntimePlacementRestartPolicyAlways)
		}
	})

	t.Run("rejects workflow runtime without pool", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
      runtime:
          provider: hosted
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/gke
server:
  encryptionKey: server-key
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "providers.workflow.temporal.runtime.pool is required for runtime-placed workflow providers") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects incomplete hosted workflow runtime pool", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 1
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/gke
server:
  encryptionKey: server-key
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "providers.workflow.temporal.runtime.pool.maxReadyInstances is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects autoscale-shaped hosted workflow runtime pool", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
providers:
  workflow:
    temporal:
      source:
        path: ./providers/workflow/temporal
      runtime:
          provider: hosted
          pool:
            minReadyInstances: 2
            maxReadyInstances: 3
            startupTimeout: 5m
            healthCheckInterval: 30s
            restartPolicy: always
            drainTimeout: 2m
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/gke
server:
  encryptionKey: server-key
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "providers.workflow.temporal.runtime.pool.maxReadyInstances must equal minReadyInstances") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadConfigAgentSessionStartLifecycle(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
providers:
  agent:
    simple:
      source:
        path: ./providers/agent/simple
      lifecycle:
        sessionStart:
          - id: load-memory
            command: ["bash", "-lc", "printf context"]
            cwd: ./hooks
            timeout: 5s
            env:
              MEMORY_ROOT: ./memory
            output:
              additionalContext: true
              metadata: true
server:
  encryptionKey: server-key
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hooks := cfg.Providers.Agent["simple"].Lifecycle.SessionStart
	if len(hooks) != 1 {
		t.Fatalf("sessionStart hooks = %d, want 1", len(hooks))
	}
	wantCWD := filepath.Join(filepath.Dir(path), "hooks")
	if hooks[0].Type != "command" || hooks[0].CWD != wantCWD || !hooks[0].Output.AdditionalContext || !hooks[0].Output.Metadata {
		t.Fatalf("sessionStart hook = %#v", hooks[0])
	}
}

func TestLoadConfigAgentSessionStartLifecycleValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "missing command",
			yaml: `
providers:
  agent:
    simple:
      source: ./providers/agent/simple
      lifecycle:
        sessionStart:
          - id: setup
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.lifecycle.sessionStart[0].command is required",
		},
		{
			name: "duplicate id",
			yaml: `
providers:
  agent:
    simple:
      source: ./providers/agent/simple
      lifecycle:
        sessionStart:
          - id: setup
            command: ["true"]
          - id: setup
            command: ["true"]
server:
  encryptionKey: server-key
`,
			want: `providers.agent.simple.lifecycle.sessionStart[1].id duplicates "setup"`,
		},
		{
			name: "unsupported type",
			yaml: `
providers:
  agent:
    simple:
      source: ./providers/agent/simple
      lifecycle:
        sessionStart:
          - id: setup
            type: native
            command: ["true"]
server:
  encryptionKey: server-key
`,
			want: `providers.agent.simple.lifecycle.sessionStart[0].type "native" is not supported`,
		},
		{
			name: "invalid hook id",
			yaml: `
providers:
  agent:
    simple:
      source: ./providers/agent/simple
      lifecycle:
        sessionStart:
          - id: setup.memory
            command: ["true"]
server:
  encryptionKey: server-key
`,
			want: "providers.agent.simple.lifecycle.sessionStart[0].id must contain only letters",
		},
		{
			name: "app lifecycle",
			yaml: `
apps:
  service:
    source: ./providers/app/service
    lifecycle:
      sessionStart:
        - id: setup
          command: ["true"]
server:
  encryptionKey: server-key
`,
			want: `apps.service.lifecycle is only supported on providers.agent.*`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := mustWriteConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadConfigRuntimeRelayBaseURL(t *testing.T) {
	t.Parallel()

	t.Run("accepts and trims relay base url", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  runtime:
    relayBaseUrl: http://valon-tools-gestaltd.gestalt-runtime.svc.cluster.local:8080/
`)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := cfg.Server.Runtime.RelayBaseURL; got != "http://valon-tools-gestaltd.gestalt-runtime.svc.cluster.local:8080" {
			t.Fatalf("server.runtime.relayBaseUrl = %q", got)
		}
	})

	t.Run("rejects relay base url with path", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  runtime:
    relayBaseUrl: https://gestalt.example.test/relay
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.runtime.relayBaseUrl must not include a path") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects removed default hosted provider", func(t *testing.T) {
		t.Parallel()

		path := mustWriteConfigFile(t, `
server:
  runtime:
    defaultHostedProvider: modal
`)

		_, err := Load(path)
		if err == nil {
			t.Fatal("Load: expected error, got nil")
		}
		if !strings.Contains(err.Error(), "server.runtime.defaultHostedProvider has been removed; use server.runtime.defaultProvider") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadPathsProviderRuntimeAndEgressOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	overridePath := filepath.Join(dir, "override.yaml")
	if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v6
server:
  encryptionKey: server-key
apps:
  service:
    source:
      path: ./apps/service/manifest.yaml
    runtime:
        provider: hosted
    egress:
      allowedHosts:
        - api.github.com
runtime:
  providers:
    hosted:
      source:
        path: ./providers/runtime/modal
`), 0o644); err != nil {
		t.Fatalf("writing base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`
apiVersion: gestaltd.config/v6
apps:
  service:
    runtime: null
    egress:
      allowedHosts: []
`), 0o644); err != nil {
		t.Fatalf("writing override config: %v", err)
	}

	cfg, err := LoadPaths([]string{basePath, overridePath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	entry := cfg.Apps["service"]
	if entry.UsesRuntimePlacement() {
		t.Fatal("UsesRuntimePlacement = true, want runtime override removed")
	}
	if got := entry.EffectiveAllowedHosts(); len(got) != 0 {
		t.Fatalf("EffectiveAllowedHosts = %#v, want empty after egress override", got)
	}
}

func TestLoadConfigProviderPackageSources(t *testing.T) {
	t.Parallel()

	path := mustWriteRawConfigFile(t, `
apiVersion: gestaltd.config/v6
providerRepositories:
  local:
    url: https://providers.example.test/index.yaml
apps:
  service:
    source:
      repo: local
      package: github.com/acme/providers/service
      version: ">= 1.2.0, < 2.0.0"
      auth:
        token: test-token
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.APIVersion; got != ConfigAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", got, ConfigAPIVersion)
	}
	if got := cfg.ProviderRepositories["local"].URL; got != "https://providers.example.test/index.yaml" {
		t.Fatalf("providerRepositories.local.url = %q", got)
	}
	entry := cfg.Apps["service"]
	if entry == nil {
		t.Fatal(`Apps["service"] = nil`)
		return
	}
	if !entry.Source.IsPackage() {
		t.Fatal("Source.IsPackage = false, want true")
	}
	if got := entry.Source.PackageRepo(); got != "local" {
		t.Fatalf("Source.PackageRepo = %q, want local", got)
	}
	if got := entry.Source.PackageAddress(); got != "github.com/acme/providers/service" {
		t.Fatalf("Source.PackageAddress = %q", got)
	}
	if got := entry.Source.PackageVersionConstraint(); got != ">= 1.2.0, < 2.0.0" {
		t.Fatalf("Source.PackageVersionConstraint = %q", got)
	}
	if entry.Source.Auth == nil || entry.Source.Auth.Token != "test-token" {
		t.Fatalf("Source.Auth = %#v, want token", entry.Source.Auth)
	}
}

func TestLoadConfigProviderPackageSourcesDoNotGetBuiltinDefaults(t *testing.T) {
	t.Parallel()

	path := mustWriteRawConfigFile(t, `
apiVersion: gestaltd.config/v6
providerRepositories:
  local:
    url: https://providers.example.test/index.yaml
providers:
  secrets:
    vault:
      source:
        repo: local
        package: github.com/acme/providers/secrets
        version: 1.0.0
  telemetry:
    otel:
      source:
        repo: local
        package: github.com/acme/providers/telemetry
        version: 1.0.0
  audit:
    auditlog:
      source:
        repo: local
        package: github.com/acme/providers/audit
        version: 1.0.0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for subject, entry := range map[string]*ProviderEntry{
		"secrets.vault":  cfg.Providers.Secrets["vault"],
		"telemetry.otel": cfg.Providers.Telemetry["otel"],
		"audit.auditlog": cfg.Providers.Audit["auditlog"],
	} {
		if entry == nil {
			t.Fatalf("%s entry = nil", subject)
			return
		}
		if got := entry.Source.Builtin; got != "" {
			t.Fatalf("%s Source.Builtin = %q, want empty for package source", subject, got)
		}
		if !entry.Source.IsPackage() {
			t.Fatalf("%s Source.IsPackage = false, want true", subject)
		}
	}
}

func TestLoadConfigProviderPackageSourceValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "package and url are mutually exclusive",
			yaml: `
apiVersion: gestaltd.config/v6
apps:
  service:
    source:
      url: https://example.com/provider-release.yaml
      package: github.com/acme/providers/service
`,
			want: `source.path and metadata URL sources are mutually exclusive`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := mustWriteRawConfigFile(t, tc.yaml)
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

func TestLoadPathsProviderPackageSourceLayering(t *testing.T) {
	t.Parallel()

	t.Run("overlay replaces metadata URL with package source", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		basePath := filepath.Join(dir, "base.yaml")
		overridePath := filepath.Join(dir, "override.yaml")
		if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v6
apps:
  service:
    source: https://example.com/service/provider-release.yaml
`), 0o644); err != nil {
			t.Fatalf("write base: %v", err)
		}
		if err := os.WriteFile(overridePath, []byte(`
apiVersion: gestaltd.config/v6
providerRepositories:
  local:
    url: https://providers.example.test/index.yaml
apps:
  service:
    source:
      repo: local
      package: github.com/acme/providers/service
`), 0o644); err != nil {
			t.Fatalf("write override: %v", err)
		}

		cfg, err := LoadPaths([]string{basePath, overridePath})
		if err != nil {
			t.Fatalf("LoadPaths: %v", err)
		}
		if got := cfg.APIVersion; got != ConfigAPIVersion {
			t.Fatalf("APIVersion = %q, want %q", got, ConfigAPIVersion)
		}
		if !cfg.Apps["service"].Source.IsPackage() {
			t.Fatal("merged source is not package source")
		}
	})
}

func TestLoadRejectsUnknownProviderFields(t *testing.T) {
	t.Parallel()

	legacyAgentHarnessKey := "local" + "Harness"
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
			name: "agent legacy harness field is rejected",
			yaml: fmt.Sprintf(`
providers:
  agent:
    primary:
      %s:
        command: /bin/sh
`, legacyAgentHarnessKey),
			wantErr: fmt.Sprintf(`field %s not found`, legacyAgentHarnessKey),
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
apiVersion: gestaltd.config/v6
providers:
  authentication:
    primary:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
`,
		},
		{
			name: "apiVersion scalar local source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: ./apps/dummy/manifest.yaml
`,
		},
		{
			name: "apiVersion metadata url with app route auth",
			yaml: `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: corporate
providers:
  authentication:
    corporate:
      source: https://example.com/providers/auth/corporate/provider-release.yaml
apps:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        provider: server
`,
		},
		{
			name: "apiVersion metadata url with nested source auth",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        url: https://example.com/providers/external/provider-release.yaml
        auth:
          token: test-token
`,
		},
		{
			name: "provider metadata url with nested source auth",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
  authentication:
    primary:
      source:
        url: https://example.com/providers/test-auth/provider-release.yaml
        auth:
          token: test-token
apps:
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
apps:
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
			name: "multiple authentication providers require selection or default",
			yaml: `
providers:
  authentication:
    one:
      source:
        path: ./one/manifest.yaml
    two:
      source:
        path: ./two/manifest.yaml
`,
		},
		{
			name: "unsupported apiVersion is rejected",
			yaml: `
apiVersion: gestaltd.config/v99
providers:
apps:
    external:
      source: ./apps/dummy/manifest.yaml
`,
			want: `unsupported apiVersion "gestaltd.config/v99"`,
		},
		{
			name: "provider auth override is rejected outside apps",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
  cache:
    shared:
      source: https://example.com/providers/cache/shared/provider-release.yaml
      auth:
        provider: server
apps:
`,
			want: `providers.cache.shared.auth is only supported on apps.*`,
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

func TestLoadConfigRequiresAPIVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "missing apiVersion is rejected",
			yaml: `
providers:
apps:
  external:
    source: ./apps/dummy/manifest.yaml
`,
		},
		{
			name: "empty apiVersion is rejected",
			yaml: `
apiVersion: ""
providers:
apps:
  external:
    source: ./apps/dummy/manifest.yaml
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := mustWriteRawConfigFile(t, tc.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), "apiVersion is required") {
				t.Fatalf("Load error = %v, want apiVersion required error", err)
			}
		})
	}
}

func TestLoadPathsRequiresAPIVersionInEveryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	overridePath := filepath.Join(dir, "override.yaml")
	if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v6
providers:
apps:
  external:
    source: ./apps/dummy/manifest.yaml
`), 0o644); err != nil {
		t.Fatalf("writing base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`
apps:
  external:
    displayName: External
`), 0o644); err != nil {
		t.Fatalf("writing override config: %v", err)
	}

	_, err := LoadPaths([]string{basePath, overridePath})
	if err == nil {
		t.Fatal("LoadPaths: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "apiVersion is required") {
		t.Fatalf("LoadPaths error = %v, want apiVersion required error", err)
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
    indexeddb: first
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
    first:
      source:
        path: ./first/manifest.yaml
  indexeddb:
    canonical:
      source:
        path: ./canonical/manifest.yaml
`,
			want: `mapping key "indexeddb" already defined`,
		},
		{
			name: "duplicate apps mapping",
			yaml: `
apps:
  first:
    source:
      path: ./first/manifest.yaml
apps:
  canonical:
    source:
      path: ./canonical/manifest.yaml
`,
			want: `mapping key "apps" already defined`,
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
			name: "metadata source app only",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    custom_tool:
      source: https://example.com/providers/custom_tool/provider-release.yaml
`,
		},
		{
			name: "app with local source",
			yaml: `
providers:
apps:
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

func TestAppValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "integration app source path is valid",
			yaml: `
providers:
apps:
    external:
      source:
        path: ./apps/dummy/manifest.yaml
`,
		},
		{
			name: "app env with local source is valid",
			yaml: `
providers:
apps:
    external:
      source:
        path: ./apps/dummy/manifest.yaml
      env:
        FOO: bar
`,
		},
		{
			name: "app config with source is valid",
			yaml: `
providers:
apps:
    external:
      source:
        path: ./apps/dummy/manifest.yaml
      config:
        base_url: https://example.com
`,
		},
		{
			name: "app source is required for external",
			yaml: `
providers:
apps:
    external:
      {}
`,
			wantErr: "source.path or provider-release metadata URL is required",
		},
		{
			name: "apiVersion route auth override is valid",
			yaml: `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: corporate
providers:
  authentication:
    corporate:
      source: https://example.com/providers/auth/corporate/provider-release.yaml
apps:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        provider: server
`,
		},
		{
			name: "apiVersion github release source with nested source auth is valid",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        githubRelease:
          repo: valon-technologies/toolshed
          tag: apps/external/v1.2.3
          asset: provider-release.yaml
        auth:
          token: test-token
`,
		},
		{
			name: "apiVersion github release source requires repo",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        githubRelease:
          tag: apps/external/v1.2.3
          asset: provider-release.yaml
`,
			wantErr: "source.githubRelease.repo is required",
		},
		{
			name: "apiVersion github release source requires owner slash name",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        githubRelease:
          repo: valon-technologies
          tag: apps/external/v1.2.3
          asset: provider-release.yaml
`,
			wantErr: "source.githubRelease.repo must be owner/name",
		},
		{
			name: "apiVersion nested source auth is valid",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        url: https://example.com/providers/external/provider-release.yaml
        auth:
          token: test-token
`,
		},
		{
			name: "app auth override is valid alongside nested source auth",
			yaml: `
apiVersion: gestaltd.config/v6
server:
  providers:
    authentication: corporate
providers:
  authentication:
    corporate:
      source: https://example.com/providers/auth/corporate/provider-release.yaml
apps:
    external:
      source:
        url: https://example.com/providers/external/provider-release.yaml
        auth:
          token: test-token
      auth:
        provider: server
`,
		},
		{
			name: "app auth override rejects source auth token mix",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        token: test-token
        provider: server
`,
			wantErr: "field token not found",
		},
		{
			name: "app auth override rejects unknown auth provider",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        provider: missing
`,
			wantErr: `apps.external.auth.provider references unknown authentication provider "missing"`,
		},
		{
			name: "app auth override rejects server alias without configured auth provider",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: https://example.com/providers/external/provider-release.yaml
      auth:
        provider: server
`,
			wantErr: `apps.external.auth.provider "server" requires a configured platform authentication provider`,
		},
		{
			name: "apiVersion local source rejects sibling auth",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: ./apps/dummy/manifest.yaml
      auth:
        token: test-token
`,
			wantErr: "field token not found",
		},
		{
			name: "apiVersion v5 local provider-release metadata is valid",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: ./apps/dummy/dist/provider-release.yaml
`,
		},
		{
			name: "apiVersion v5 local provider-release metadata allows current-directory file",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: provider-release.yaml
`,
		},
		{
			name: "apiVersion v5 local provider-release metadata accepts nested source auth",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source:
        path: ./apps/dummy/dist/provider-release.yaml
        auth:
          token: test-token
`,
		},
		{
			name: "apiVersion accepts local source manifests",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: ./apps/dummy/manifest.yaml
`,
		},
		{
			name: "apiVersion accepts absolute http metadata source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: https://example.com/providers/external/archive.tar.gz
`,
		},
		{
			name: "apiVersion rejects git scalar source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: git+ssh://git@github.com/example/external.git
`,
			wantErr: "git+ sources are not supported",
		},
		{
			name: "apiVersion rejects unsupported ssh scalar source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: ssh://github.com/example/external
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects unsupported file scalar source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: file:/tmp/provider-release.yaml
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion rejects malformed hostless https metadata source",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
apps:
    external:
      source: https:///provider-release.yaml
`,
			wantErr: "only provider-release.yaml metadata URLs are supported for remote sources",
		},
		{
			name: "apiVersion accepts absolute telemetry metadata source before builtin defaulting",
			yaml: `
apiVersion: gestaltd.config/v6
providers:
  telemetry:
    default:
      source: https://example.com/providers/telemetry/archive.tar.gz
apps:
`,
		},
		{
			name: "app source with base_url override is rejected",
			yaml: `
providers:
apps:
    external:
      source:
        path: ./apps/dummy/manifest.yaml
      base_url: https://api.example.com
`,
			wantErr: "field base_url not found",
		},
		{
			name: "non-default connection params are accepted",
			yaml: `
providers:
apps:
    external:
      source:
        path: ./apps/dummy/manifest.yaml
      connections:
        named:
          mode: subject
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

func TestValidateStructure_AppValidationDirect(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "local source valid",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Path: "./some-dir/manifest.yaml"}},
				},
			},
		},
		{
			name: "metadata source valid",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{metadataURL: "https://example.com/providers/sample/provider-release.yaml"}},
				},
			},
		},
		{
			name: "source path and metadata url rejected",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {Source: ProviderSource{Path: "./manifest.yaml", metadataURL: "https://example.com/providers/sample/provider-release.yaml"}},
				},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "nil app rejected",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {},
				},
			},
			wantErr: "source.path or provider-release metadata URL is required",
		},
		{
			name: "authentication provider valid",
			cfg: &Config{
				Providers: ProvidersConfig{
					Authentication: singletonProviderEntry(&ProviderEntry{Source: ProviderSource{metadataURL: "https://example.com/providers/test-auth/provider-release.yaml"}}),
				},
			},
		},
		{
			name: "authentication provider none valid",
			cfg:  &Config{},
		},
		{
			name: "authentication provider invalid when source missing",
			cfg: &Config{
				Providers: ProvidersConfig{
					Authentication: singletonProviderEntry(&ProviderEntry{}),
				},
			},
			wantErr: `source.path or provider-release metadata URL is required`,
		},
		{
			name: "authentication config requires source",
			cfg: &Config{
				Providers: ProvidersConfig{
					Authentication: singletonProviderEntry(&ProviderEntry{Config: yaml.Node{Kind: yaml.MappingNode}}),
				},
			},
			wantErr: `source.path or provider-release metadata URL is required`,
		},
		{
			name: "app auth rejects mcp oauth early",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {
						Source: ProviderSource{Path: "./manifest.yaml"},
						Auth:   &ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth},
					},
				},
			},
			wantErr: `app auth type "mcp_oauth" requires an MCP surface`,
		},
		{
			name: "named connection rejects mcp oauth early",
			cfg: &Config{
				Apps: map[string]*ProviderEntry{
					"sample": {
						Source: ProviderSource{Path: "./manifest.yaml"},
						Connections: map[string]*ConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeNone,
								Auth: ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth},
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
			if tc.cfg != nil && strings.TrimSpace(tc.cfg.APIVersion) == "" {
				tc.cfg.APIVersion = ConfigAPIVersion
			}
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

func TestValidateStructureCanonicalizesConnectionAliasBindings(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Connections: map[string]*ConnectionDef{
			"shared": {
				Mode: providermanifestv1.ConnectionModeNone,
				Auth: ConnectionAuthDef{Type: providermanifestv1.AuthTypeNone},
			},
		},
		Apps: map[string]*ProviderEntry{
			"sample": {
				Source: ProviderSource{Path: "./manifest.yaml"},
				Connections: map[string]*ConnectionDef{
					core.AppConnectionAlias: {
						Ref: "shared",
					},
				},
			},
		},
	}

	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
	connections := cfg.Apps["sample"].Connections
	if _, ok := connections[core.AppConnectionAlias]; ok {
		t.Fatalf("connections[%q] present, want alias removed after canonicalization", core.AppConnectionAlias)
	}
	canonical := connections[core.AppConnectionName]
	if canonical == nil {
		t.Fatalf("connections[%q] missing", core.AppConnectionName)
		return
	}
	if canonical.ConnectionID != "shared" || canonical.Ref != "shared" || !canonical.BindingResolved {
		t.Fatalf("canonical binding = %+v, want resolved shared connection", canonical)
	}
}

func TestValidateStructureConnectionRefPreservesCredentialRefreshOverride(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Connections: map[string]*ConnectionDef{
			"shared": {
				Mode: providermanifestv1.ConnectionModeSubject,
				Auth: ConnectionAuthDef{
					Type:     providermanifestv1.AuthTypeOAuth2,
					TokenURL: "https://oauth.example.test/token",
				},
				CredentialRefresh: &CredentialRefreshDef{
					RefreshInterval:     "30m",
					RefreshBeforeExpiry: "45m",
				},
			},
		},
		Apps: map[string]*ProviderEntry{
			"sample": {
				Source: ProviderSource{Path: "./manifest.yaml"},
				Connections: map[string]*ConnectionDef{
					"default": {
						Ref: "shared",
						CredentialRefresh: &CredentialRefreshDef{
							RefreshInterval:     "15m",
							RefreshBeforeExpiry: "30m",
						},
					},
				},
			},
		},
	}

	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
	canonical := cfg.Apps["sample"].Connections["default"]
	if canonical == nil || canonical.CredentialRefresh == nil {
		t.Fatalf("resolved credentialRefresh missing: %+v", canonical)
	}
	if canonical.CredentialRefresh.RefreshInterval != "15m" || canonical.CredentialRefresh.RefreshBeforeExpiry != "30m" {
		t.Fatalf("credentialRefresh = %+v, want binding override", canonical.CredentialRefresh)
	}
}

func TestValidateStructureCanonicalizesAppInvokeRunAs(t *testing.T) {
	t.Parallel()

	applyByDefault := false
	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Apps: map[string]*ProviderEntry{
			"source": {
				Source: ProviderSource{Path: "./manifest.yaml"},
				Invokes: []AppInvocationDependency{{
					App:            "target",
					Operation:      "tasks.create",
					CredentialMode: providermanifestv1.ConnectionModeNone,
					RunAs: &AppInvocationRunAsConfig{
						Subject: &AppInvocationRunAsSubjectConfig{
							ID: " service_account:automation ",
						},
						ApplyByDefault: &applyByDefault,
					},
				}},
			},
		},
	}

	if err := ValidateStructure(cfg); err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
	subject := cfg.Apps["source"].Invokes[0].RunAsSubject()
	if subject == nil {
		t.Fatal("RunAsSubject() = nil, want subject")
		return
	}
	if subject.SubjectID != "service_account:automation" {
		t.Fatalf("RunAsSubject().SubjectID = %q", subject.SubjectID)
	}
	if subject.CredentialSubjectID != subject.SubjectID {
		t.Fatalf("RunAsSubject() = %#v, want normalized service account subject", subject)
	}
	if cfg.Apps["source"].Invokes[0].RunAsAppliesByDefault() {
		t.Fatal("RunAsAppliesByDefault() = true, want false")
	}
}

func TestValidateStructureCredentialRefreshDurationContract(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Connections: map[string]*ConnectionDef{
			"shared": {
				Mode: providermanifestv1.ConnectionModeSubject,
				CredentialRefresh: &CredentialRefreshDef{
					RefreshInterval:     "0s",
					RefreshBeforeExpiry: "30m",
				},
			},
		},
	}

	err := ValidateStructure(cfg)
	if err == nil {
		t.Fatal("ValidateStructure() error = nil, want invalid duration")
	}
	if !strings.Contains(err.Error(), "credentialRefresh.refreshInterval") {
		t.Fatalf("ValidateStructure() error = %v, want credentialRefresh.refreshInterval", err)
	}
}

func TestValidateStructureRejectsAppInvokeRunAsOnSurface(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Apps: map[string]*ProviderEntry{
			"source": {
				Source: ProviderSource{Path: "./manifest.yaml"},
				Invokes: []AppInvocationDependency{{
					App:     "target",
					Surface: string(SpecSurfaceGraphQL),
					RunAs: &AppInvocationRunAsConfig{
						Subject: &AppInvocationRunAsSubjectConfig{
							ID: "service_account:automation",
						},
					},
				}},
			},
		},
	}

	err := ValidateStructure(cfg)
	if err == nil || !strings.Contains(err.Error(), "runAs is supported only for REST exact operation invokes") {
		t.Fatalf("ValidateStructure() error = %v, want REST exact operation error", err)
	}
}

func TestValidateStructureRejectsConnectionAliasConflict(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Connections: map[string]*ConnectionDef{
			"primary":  {Mode: providermanifestv1.ConnectionModeNone},
			"fallback": {Mode: providermanifestv1.ConnectionModeNone},
		},
		Apps: map[string]*ProviderEntry{
			"sample": {
				Source: ProviderSource{Path: "./manifest.yaml"},
				Connections: map[string]*ConnectionDef{
					core.AppConnectionAlias: {Ref: "primary"},
					core.AppConnectionName:  {Ref: "fallback"},
				},
			},
		},
	}

	err := ValidateStructure(cfg)
	if err == nil {
		t.Fatal("ValidateStructure() error = nil, want alias conflict")
	}
	if !strings.Contains(err.Error(), "conflicts with alias") {
		t.Fatalf("ValidateStructure() error = %v, want alias conflict", err)
	}
}

func TestValidateStructureRejectsInlineUserMCPOAuthConnection(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		APIVersion: ConfigAPIVersion,
		Apps: map[string]*ProviderEntry{
			"sample": {
				Connections: map[string]*ConnectionDef{
					"mcp": {
						Mode: providermanifestv1.ConnectionModeSubject,
						Auth: ConnectionAuthDef{Type: providermanifestv1.AuthTypeMCPOAuth},
					},
				},
			},
		},
	}

	err := ValidateStructure(cfg)
	if err == nil {
		t.Fatal("ValidateStructure() error = nil, want inline user mcp_oauth rejection")
	}
	if !strings.Contains(err.Error(), "user-owned inline connections are not supported") {
		t.Fatalf("ValidateStructure() error = %v, want inline user connection rejection", err)
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
apiVersion: gestaltd.config/v6
providers:
  authentication:
    authentication:
      source:
        path: ../auth-app/provider.yaml
  indexeddb:
    sqlite:
      source:
        path: ./providers/datastore/sqlite
apps:
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

	if got := cfg.Apps["service-a"].IconFile; got != iconPath {
		t.Fatalf("IconFile = %q, want %q", got, iconPath)
	}
	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
	if auth == nil {
		t.Fatal("SelectedAuthenticationProvider = nil")
		return
	}
	if got := auth.SourcePath(); got != filepath.Join(dir, "auth-app", "provider.yaml") {
		t.Fatalf("auth app source path = %q, want %q", got, filepath.Join(dir, "auth-app", "provider.yaml"))
	}
	if got := cfg.Apps["service-a"].SourcePath(); got != filepath.Join(dir, "bin", "manifest.yaml") {
		t.Fatalf("integration app source path = %q, want %q", got, filepath.Join(dir, "bin", "manifest.yaml"))
	}
}

func TestLoadPaths_ResolvesRelativePathsPerFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(basePath), 0o755); err != nil {
		t.Fatalf("MkdirAll base: %v", err)
	}
	if err := os.WriteFile(basePath, []byte(`
apiVersion: gestaltd.config/v6
server:
  artifactsDir: ../base-artifacts
providers:
apps:
    sample:
      source: ../base-app/manifest.yaml
`), 0o644); err != nil {
		t.Fatalf("WriteFile base: %v", err)
	}

	overridePath := filepath.Join(dir, "overrides", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(overridePath), 0o755); err != nil {
		t.Fatalf("MkdirAll override: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`
apiVersion: gestaltd.config/v6
server:
  artifactsDir: ./override-artifacts
providers:
apps:
    sample:
      source: ./override-app/manifest.yaml
`), 0o644); err != nil {
		t.Fatalf("WriteFile override: %v", err)
	}

	cfg, err := LoadPaths([]string{basePath, overridePath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}

	wantPath := filepath.Join(filepath.Dir(overridePath), "override-app", "manifest.yaml")
	if got := cfg.Apps["sample"].SourcePath(); got != wantPath {
		t.Fatalf("SourcePath = %q, want %q", got, wantPath)
	}
	if got, want := cfg.Server.ArtifactsDir, filepath.Join(filepath.Dir(overridePath), "override-artifacts"); got != want {
		t.Fatalf("Server.ArtifactsDir = %q, want %q", got, want)
	}
}

func TestAuthConfigMap(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v6
providers:
  authentication:
    authentication:
      source: https://github.com/valon-technologies/gestalt-providers/releases/download/auth/google/v1.0.0/provider-release.yaml
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

	_, auth := mustSelectedProvider(t, cfg, HostProviderKindAuthentication)
	if auth == nil {
		t.Fatal("SelectedAuthenticationProvider = nil")
		return
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

func TestLoadConfig_ServerAgentDefaultToolNarrowingThreshold(t *testing.T) {
	t.Parallel()

	t.Run("explicit zero is preserved", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  agent:
    defaultToolNarrowingThreshold: 0
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Server.Agent.DefaultToolNarrowingThreshold == nil {
			t.Fatal("defaultToolNarrowingThreshold = nil, want explicit zero")
		}
		if got := *cfg.Server.Agent.DefaultToolNarrowingThreshold; got != 0 {
			t.Fatalf("defaultToolNarrowingThreshold = %d, want 0", got)
		}
	})

	t.Run("positive value is parsed", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  agent:
    defaultToolNarrowingThreshold: 123
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Server.Agent.DefaultToolNarrowingThreshold == nil {
			t.Fatal("defaultToolNarrowingThreshold = nil, want parsed value")
		}
		if got := *cfg.Server.Agent.DefaultToolNarrowingThreshold; got != 123 {
			t.Fatalf("defaultToolNarrowingThreshold = %d, want 123", got)
		}
	})

	t.Run("negative value rejected", func(t *testing.T) {
		t.Parallel()
		path := mustWriteConfigFile(t, `
server:
  agent:
    defaultToolNarrowingThreshold: -1
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for negative defaultToolNarrowingThreshold")
		}
		if !strings.Contains(err.Error(), "server.agent.defaultToolNarrowingThreshold") {
			t.Fatalf("Load error = %v, want server.agent.defaultToolNarrowingThreshold", err)
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

func TestLoad_ResolvesRelativeAppSourcePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "my-app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v6
providers:
apps:
    sample:
      source:
        path: ./my-app/manifest.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	entry := loaded.Apps["sample"]
	if entry == nil {
		t.Fatal("expected app to be loaded")
	}
	if !filepath.IsAbs(entry.SourcePath()) {
		t.Fatalf("expected absolute path, got: %q", entry.SourcePath())
	}
	wantPath := filepath.Join(appDir, "manifest.yaml")
	if entry.SourcePath() != wantPath {
		t.Fatalf("entry.SourcePath() = %q, want %q", entry.SourcePath(), wantPath)
	}
}

func workflowTestAppStepConfig(name, operation string, credentialMode providermanifestv1.ConnectionMode, input map[string]any) []WorkflowStepConfig {
	step := WorkflowStepConfig{
		ID: "main",
		App: &WorkflowStepAppCallConfig{
			Name:           name,
			Operation:      operation,
			CredentialMode: credentialMode,
			Input:          workflowTestLiteralObjectValueConfig(input),
		},
	}
	return []WorkflowStepConfig{step}
}

func workflowTestLiteralObjectValueConfig(input map[string]any) WorkflowValueConfig {
	if len(input) == 0 {
		return WorkflowValueConfig{}
	}
	fields := make(map[string]WorkflowValueConfig, len(input))
	for key, value := range input {
		fields[key] = WorkflowValueConfig{Literal: value, LiteralSet: true}
	}
	return WorkflowValueConfig{Object: fields}
}
