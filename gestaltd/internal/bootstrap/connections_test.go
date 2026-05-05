package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestBuildConnectionRuntimePlatformManualDirectAuthMapping(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gong": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type: providermanifestv1.AuthTypeManual,
									Credentials: []providermanifestv1.CredentialField{
										{Name: "access_key_id"},
										{Name: "secret_key"},
									},
									AuthMapping: &providermanifestv1.AuthMapping{
										Basic: &providermanifestv1.BasicAuthMapping{
											Username: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "access_key_id"},
												},
											},
											Password: providermanifestv1.AuthValue{
												ValueFrom: &providermanifestv1.AuthValueFrom{
													CredentialFieldRef: &providermanifestv1.CredentialFieldRef{Name: "secret_key"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type:        providermanifestv1.AuthTypeManual,
							Credentials: []config.CredentialFieldDef{},
							AuthMapping: &config.AuthMappingDef{
								Basic: &config.BasicAuthMappingDef{
									Username: config.AuthValueDef{Value: "access-key-id"},
									Password: config.AuthValueDef{Value: "access-key-secret"},
								},
							},
						},
					},
				},
			},
		},
	}

	runtime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("gong", "default")
	if !ok {
		t.Fatal("runtime.Resolve(gong, default) not found")
	}
	if info.Mode != core.ConnectionModePlatform {
		t.Fatalf("Mode = %q, want %q", info.Mode, core.ConnectionModePlatform)
	}
	if info.Token != "{}" {
		t.Fatalf("Token = %q, want placeholder JSON token", info.Token)
	}
}

func TestBuildManualConnectionAuthMapIsSeparateFromOAuthHandlers(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{
		Auth: &config.ConnectionAuthDef{
			Type:     providermanifestv1.AuthTypeManual,
			TokenURL: "https://looker.example.com/api/4.0/login",
			Credentials: []config.CredentialFieldDef{
				{Name: "client_id"},
				{Name: "client_secret"},
			},
		},
	}

	oauthHandlers, err := buildConnectionAuthMap("looker", entry, nil, nil, nil, Deps{})
	if err != nil {
		t.Fatalf("buildConnectionAuthMap: %v", err)
	}
	if len(oauthHandlers) != 0 {
		t.Fatalf("OAuth handlers = %+v, want none", oauthHandlers)
	}

	manualHandlers, err := buildManualConnectionAuthMap("looker", entry, nil, nil)
	if err != nil {
		t.Fatalf("buildManualConnectionAuthMap: %v", err)
	}
	if manualHandlers[config.PluginConnectionName] == nil {
		t.Fatalf("manual token exchanger for plugin connection not built: %+v", manualHandlers)
	}
}

func TestBuildConnectionAuthMapSkipsPlatformOAuthRefreshTokenHandler(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{
		Connections: map[string]*config.ConnectionDef{
			"platform": {
				Mode: providermanifestv1.ConnectionModePlatform,
				Auth: config.ConnectionAuthDef{
					Type:         providermanifestv1.AuthTypeOAuth2,
					GrantType:    "refresh_token",
					TokenURL:     "https://oauth2.googleapis.com/token",
					ClientID:     "client-id",
					ClientSecret: "client-secret",
					RefreshToken: "refresh-token",
				},
			},
		},
	}

	oauthHandlers, err := buildConnectionAuthMap("gmail", entry, nil, nil, nil, Deps{})
	if err != nil {
		t.Fatalf("buildConnectionAuthMap: %v", err)
	}
	if len(oauthHandlers) != 0 {
		t.Fatalf("OAuth handlers = %+v, want none for platform refresh_token", oauthHandlers)
	}
}

func TestBuildConnectionRuntimePlatformManualCredentialRefsRequireToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"sample": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeManual,
							AuthMapping: &config.AuthMappingDef{
								Headers: map[string]config.AuthValueDef{
									"X-API-Key": {
										ValueFrom: &config.AuthValueFromDef{
											CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := BuildConnectionRuntime(cfg)
	if err == nil {
		t.Fatal("BuildConnectionRuntime() error = nil, want credential ref error")
	}
	if !strings.Contains(err.Error(), "manual auth with credential refs requires auth.token") {
		t.Fatalf("BuildConnectionRuntime() error = %v, want credential ref token error", err)
	}
}

func TestBuildConnectionRuntimeRejectsProviderNamespaceCollision(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"shared": {},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"shared": {},
			},
		},
	}

	_, err := BuildConnectionRuntime(cfg)
	if err == nil {
		t.Fatal("BuildConnectionRuntime() error = nil, want namespace collision error")
	}
	if !strings.Contains(err.Error(), "conflicts with another provider connection namespace") {
		t.Fatalf("BuildConnectionRuntime() error = %v, want namespace collision error", err)
	}
}

func TestBuildConnectionRuntimeClientCredentialsAuthConfig(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "client-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer tokenServer.Close()

	runtime, err := BuildConnectionRuntime(&config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"sample": {
				Egress: &config.ProviderEgressConfig{AllowedHosts: []string{"allowed.example.com"}},
				Config: mustConnectionTestYAMLNode(t, map[string]any{
					"clientId":     "config-client-id",
					"clientSecret": "config-client-secret",
				}),
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type:         providermanifestv1.AuthTypeOAuth2,
							GrantType:    "client_credentials",
							TokenURL:     tokenServer.URL,
							ClientAuth:   "header",
							AcceptHeader: "application/json",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("sample", "default")
	if !ok {
		t.Fatal("runtime.Resolve(sample, default) not found")
	}
	if info.TokenSource != nil {
		t.Fatal("TokenSource != nil; client credentials should be resolved by ExternalCredentialProvider")
	}
	if info.AuthConfig.TokenURL != tokenServer.URL {
		t.Fatalf("AuthConfig.TokenURL = %q, want %q", info.AuthConfig.TokenURL, tokenServer.URL)
	}
	if info.AuthConfig.ClientID != "config-client-id" || info.AuthConfig.ClientSecret != "config-client-secret" {
		t.Fatalf("AuthConfig client credentials = %q/%q", info.AuthConfig.ClientID, info.AuthConfig.ClientSecret)
	}
}

func TestBuildConnectionRuntimePlatformRefreshTokenRefAuthConfig(t *testing.T) {
	t.Parallel()

	cfgPath := mustWriteConnectionRuntimeConfig(t, `
providers:
  secrets:
    secrets:
      source: env
connections:
  gmail-platform-mailbox:
    mode: platform
    auth:
      type: oauth2
      grantType: refresh_token
      tokenUrl: https://oauth2.googleapis.com/token
      clientId:
        secret:
          provider: secrets
          name: google-oauth-client-id
      clientSecret:
        secret:
          provider: secrets
          name: google-oauth-client-secret
      refreshToken:
        secret:
          provider: secrets
          name: gmail-platform-mailbox-refresh-token
      refreshParams:
        audience: gmail-platform
plugins:
  gmail:
    source: ./plugins/dummy/manifest.yaml
    connections:
      platform:
        ref: gmail-platform-mailbox
        exposure: internal
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	runtime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("gmail", "platform")
	if !ok {
		t.Fatal("runtime.Resolve(gmail, platform) not found")
	}
	if info.Mode != core.ConnectionModePlatform || info.Exposure != core.ConnectionExposureInternal {
		t.Fatalf("runtime mode/exposure = %q/%q, want platform/internal", info.Mode, info.Exposure)
	}
	auth := info.AuthConfig
	if auth.GrantType != "refresh_token" || auth.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Fatalf("auth grant/url = %q/%q", auth.GrantType, auth.TokenURL)
	}
	if auth.RefreshParams["audience"] != "gmail-platform" {
		t.Fatalf("refreshParams = %#v, want audience", auth.RefreshParams)
	}
	ref, ok, err := config.ParseSecretRefTransport(auth.RefreshToken)
	if err != nil || !ok {
		t.Fatalf("refreshToken secret ref parse = %#v/%v/%v, want secret ref", ref, ok, err)
	}
	if ref.Provider != "secrets" || ref.Name != "gmail-platform-mailbox-refresh-token" {
		t.Fatalf("refreshToken secret ref = %#v", ref)
	}
	if _, ok, err := config.ParseSecretRefTransport(auth.ClientID); err != nil || !ok {
		t.Fatalf("clientId secret ref parse ok=%v err=%v", ok, err)
	}
}

func TestBuildConnectionRuntimeCarriesCredentialRefreshMetadata(t *testing.T) {
	t.Parallel()

	runtime, err := BuildConnectionRuntime(&config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gmail": {
				Config: mustConnectionTestYAMLNode(t, map[string]any{
					"clientId":     "config-client-id",
					"clientSecret": "config-client-secret",
				}),
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type:     providermanifestv1.AuthTypeOAuth2,
									TokenURL: "https://oauth2.googleapis.com/token",
								},
								CredentialRefresh: &providermanifestv1.CredentialRefreshConfig{
									RefreshInterval:     "15m",
									RefreshBeforeExpiry: "30m",
								},
							},
						},
					},
				},
				Connections: map[string]*config.ConnectionDef{
					"default": {
						ConnectionID: "google-workspace",
						ConnectionParams: map[string]config.ConnectionParamDef{
							"tenant": {Default: "valon"},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildConnectionRuntime() error = %v", err)
	}
	info, ok := runtime.Resolve("gmail", "default")
	if !ok {
		t.Fatal("runtime.Resolve(gmail, default) not found")
	}
	if info.CredentialRefresh == nil {
		t.Fatal("CredentialRefresh is nil")
	}
	if info.CredentialRefresh.RefreshInterval != "15m" || info.CredentialRefresh.RefreshBeforeExpiry != "30m" {
		t.Fatalf("CredentialRefresh = %+v, want manifest metadata", info.CredentialRefresh)
	}
	if info.AuthConfig.ClientID != "config-client-id" || info.AuthConfig.ClientSecret != "config-client-secret" {
		t.Fatalf("AuthConfig client credentials = %q/%q, want provider config overlay", info.AuthConfig.ClientID, info.AuthConfig.ClientSecret)
	}
}

func TestBuildExternalCredentialsRuntimeConfigNodeResolvedConnectionsContract(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gmail": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type:       providermanifestv1.AuthTypeOAuth2,
									TokenURL:   "https://oauth2.googleapis.com/token",
									ClientAuth: "header",
									RefreshParams: map[string]string{
										"prompt": "consent",
									},
								},
								CredentialRefresh: &providermanifestv1.CredentialRefreshConfig{
									RefreshInterval:     "15m",
									RefreshBeforeExpiry: "1h",
								},
							},
						},
					},
				},
				Config: mustConnectionTestYAMLNode(t, map[string]any{
					"clientId":     "client-id",
					"clientSecret": "client-secret",
				}),
				Connections: map[string]*config.ConnectionDef{
					"default": {
						ConnectionID: "google-workspace",
						ConnectionParams: map[string]config.ConnectionParamDef{
							"tenant": {Default: "valon"},
						},
					},
				},
			},
			"slack": {
				Connections: map[string]*config.ConnectionDef{
					"default": {
						Mode: providermanifestv1.ConnectionModeUser,
						Auth: config.ConnectionAuthDef{
							Type:     providermanifestv1.AuthTypeOAuth2,
							TokenURL: "https://slack.example.test/oauth/token",
						},
					},
				},
			},
		},
	}
	node, err := buildExternalCredentialsRuntimeConfigNode("default", &config.ProviderEntry{
		Config: mustConnectionTestYAMLNode(t, map[string]any{"indexeddb": "creds"}),
	}, []byte{0x01, 0x02, 0x03}, cfg)
	if err != nil {
		t.Fatalf("buildExternalCredentialsRuntimeConfigNode() error = %v", err)
	}
	raw, err := config.NodeToMap(node)
	if err != nil {
		t.Fatalf("NodeToMap() error = %v", err)
	}
	connections, ok := raw["resolvedConnections"].([]any)
	if !ok || len(connections) != 4 {
		t.Fatalf("resolvedConnections = %#v, want all resolved connections once any opts in", raw["resolvedConnections"])
	}
	var conn map[string]any
	for _, candidate := range connections {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			t.Fatalf("resolvedConnections entry = %#v, want map", candidate)
		}
		if candidateMap["provider"] == "gmail" && candidateMap["connection"] == "default" {
			conn = candidateMap
			break
		}
	}
	if conn == nil {
		t.Fatalf("gmail default resolved connection missing from %#v", connections)
	}
	if conn["provider"] != "gmail" || conn["connection"] != "default" || conn["connectionId"] != "google-workspace" || conn["mode"] != "user" {
		t.Fatalf("resolved connection identity = %#v", conn)
	}
	auth, ok := conn["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth = %#v, want map", conn["auth"])
	}
	if _, exists := auth["TokenURL"]; exists {
		t.Fatalf("auth has Go field key TokenURL: %#v", auth)
	}
	if auth["tokenUrl"] != "https://oauth2.googleapis.com/token" || auth["clientId"] != "client-id" || auth["clientSecret"] != "client-secret" {
		t.Fatalf("auth lower-camel fields = %#v", auth)
	}
	refresh, ok := conn["credentialRefresh"].(map[string]any)
	if !ok {
		t.Fatalf("credentialRefresh = %#v, want map", conn["credentialRefresh"])
	}
	if refresh["refreshInterval"] != "15m0s" || refresh["refreshBeforeExpiry"] != "1h0m0s" {
		t.Fatalf("credentialRefresh = %#v, want canonical durations", refresh)
	}
	params, ok := conn["connectionParams"].(map[string]any)
	if !ok || params["tenant"] != "valon" {
		t.Fatalf("connectionParams = %#v, want tenant default", conn["connectionParams"])
	}
}

func TestBuildExternalCredentialsRuntimeConfigNodeIncludesPlatformRefreshTokenAuth(t *testing.T) {
	t.Parallel()

	node, err := buildExternalCredentialsRuntimeConfigNode("default", &config.ProviderEntry{
		Config: mustConnectionTestYAMLNode(t, map[string]any{"indexeddb": "creds"}),
	}, []byte{0x01, 0x02, 0x03}, &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gmail": {
				Connections: map[string]*config.ConnectionDef{
					"platform": {
						Mode: providermanifestv1.ConnectionModePlatform,
						Auth: config.ConnectionAuthDef{
							Type:         providermanifestv1.AuthTypeOAuth2,
							GrantType:    "refresh_token",
							TokenURL:     "https://oauth2.googleapis.com/token",
							ClientID:     "client-id",
							ClientSecret: "client-secret",
							RefreshToken: "refresh-token",
							RefreshParams: map[string]string{
								"audience": "gmail-platform",
							},
						},
						CredentialRefresh: &providermanifestv1.CredentialRefreshConfig{
							RefreshInterval:     "15m",
							RefreshBeforeExpiry: "30m",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildExternalCredentialsRuntimeConfigNode() error = %v", err)
	}
	raw, err := config.NodeToMap(node)
	if err != nil {
		t.Fatalf("NodeToMap() error = %v", err)
	}
	connections, ok := raw["resolvedConnections"].([]any)
	if !ok {
		t.Fatalf("resolvedConnections = %#v, want array", raw["resolvedConnections"])
	}
	var platform map[string]any
	for _, candidate := range connections {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			t.Fatalf("resolvedConnections entry = %#v, want map", candidate)
		}
		if candidateMap["provider"] == "gmail" && candidateMap["connection"] == "platform" {
			platform = candidateMap
			break
		}
	}
	if platform == nil {
		t.Fatalf("gmail platform resolved connection missing from %#v", connections)
	}
	auth, ok := platform["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth = %#v, want map", platform["auth"])
	}
	if auth["grantType"] != "refresh_token" || auth["refreshToken"] != "refresh-token" {
		t.Fatalf("auth = %#v, want refresh_token auth with refreshToken", auth)
	}
	refreshParams, ok := auth["refreshParams"].(map[string]any)
	if !ok || refreshParams["audience"] != "gmail-platform" {
		t.Fatalf("refreshParams = %#v, want audience", auth["refreshParams"])
	}
}

func TestBuildExternalCredentialsRuntimeConfigNodeOmitsResolvedConnectionsWithoutCredentialRefresh(t *testing.T) {
	t.Parallel()

	node, err := buildExternalCredentialsRuntimeConfigNode("default", &config.ProviderEntry{
		Config: mustConnectionTestYAMLNode(t, map[string]any{"indexeddb": "creds"}),
	}, []byte{0x01, 0x02, 0x03}, &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"gmail": {
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Connections: map[string]*providermanifestv1.ManifestConnectionDef{
							"default": {
								Mode: providermanifestv1.ConnectionModeUser,
								Auth: &providermanifestv1.ProviderAuth{
									Type:     providermanifestv1.AuthTypeOAuth2,
									TokenURL: "https://oauth2.googleapis.com/token",
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildExternalCredentialsRuntimeConfigNode() error = %v", err)
	}
	raw, err := config.NodeToMap(node)
	if err != nil {
		t.Fatalf("NodeToMap() error = %v", err)
	}
	if _, exists := raw["resolvedConnections"]; exists {
		t.Fatalf("resolvedConnections present without credentialRefresh: %#v", raw["resolvedConnections"])
	}
}

func TestClientCredentialsTokenSourceHeaderAuth(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			t.Fatal("BasicAuth missing")
		}
		if clientID != "client id/" || clientSecret != "client secret+/" {
			t.Fatalf("BasicAuth = %q/%q, want raw client credentials", clientID, clientSecret)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("client_id"); got != "" {
			t.Fatalf("client_id form field = %q, want empty when clientAuth=header", got)
		}
		if got := r.Form.Get("client_secret"); got != "" {
			t.Fatalf("client_secret form field = %q, want empty when clientAuth=header", got)
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("grant_type = %q, want client_credentials", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "header-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer tokenServer.Close()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client id/",
		ClientSecret: "client secret+/",
		ClientAuth:   "header",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}
	credential, err := source.ResolveConnectionCredential(context.Background())
	if err != nil {
		t.Fatalf("ResolveConnectionCredential() error = %v", err)
	}
	if credential.Token != "header-token" {
		t.Fatalf("Token = %q, want header-token", credential.Token)
	}
	if credential.ExpiresAt == nil {
		t.Fatal("ExpiresAt = nil, want expiry from token endpoint")
	}
}

func mustConnectionTestYAMLNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal YAML: %v", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		t.Fatalf("Unmarshal YAML: %v", err)
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return *node.Content[0]
	}
	return node
}

func mustWriteConnectionRuntimeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gestalt.yaml")
	content = "\napiVersion: " + config.ConfigAPIVersion + "\n" + strings.TrimLeft(content, "\r\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestClientCredentialsTokenSourceCachesTokenWithoutExpiresIn(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "no-expiry-token",
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer tokenServer.Close()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}
	first, err := source.ResolveConnectionCredential(context.Background())
	if err != nil {
		t.Fatalf("first ResolveConnectionCredential() error = %v", err)
	}
	second, err := source.ResolveConnectionCredential(context.Background())
	if err != nil {
		t.Fatalf("second ResolveConnectionCredential() error = %v", err)
	}
	if first.Token != "no-expiry-token" || second.Token != "no-expiry-token" {
		t.Fatalf("tokens = %q/%q, want cached no-expiry token", first.Token, second.Token)
	}
	if first.ExpiresAt != nil || second.ExpiresAt != nil {
		t.Fatalf("ExpiresAt = %v/%v, want nil when token response omits expires_in", first.ExpiresAt, second.ExpiresAt)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1 cached request", got)
	}
}

func TestClientCredentialsTokenSourceCanceledCallerDoesNotCancelSharedFetch(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	secondRequest := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var requests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch requests.Add(1) {
		case 1:
			close(started)
		case 2:
			close(secondRequest)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "shared-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		tokenServer.Close()
	}()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := source.ResolveConnectionCredential(firstCtx)
		firstErr <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first token request")
	}
	cancelFirst()
	select {
	case err := <-firstErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first ResolveConnectionCredential() error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled caller")
	}

	type result struct {
		credentialToken string
		err             error
	}
	secondResult := make(chan result, 1)
	go func() {
		credential, err := source.ResolveConnectionCredential(context.Background())
		secondResult <- result{credentialToken: credential.Token, err: err}
	}()

	select {
	case <-secondRequest:
		t.Fatal("second caller started a new token request instead of sharing the in-flight fetch")
	case <-time.After(100 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })

	select {
	case result := <-secondResult:
		if result.err != nil {
			t.Fatalf("second ResolveConnectionCredential() error = %v", result.err)
		}
		if result.credentialToken != "shared-token" {
			t.Fatalf("second token = %q, want shared-token", result.credentialToken)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second caller")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1 shared request", got)
	}
}

func TestClientCredentialsTokenSourceFetchTimeoutReleasesFlight(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			time.Sleep(200 * time.Millisecond)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "retry-token",
			"expires_in":   3600,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer tokenServer.Close()

	source, err := newClientCredentialsTokenSource(config.ConnectionAuthDef{
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("newClientCredentialsTokenSource() error = %v", err)
	}
	source.fetchTimeout = 25 * time.Millisecond

	_, err = source.ResolveConnectionCredential(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first ResolveConnectionCredential() error = %v, want deadline exceeded", err)
	}
	credential, err := source.ResolveConnectionCredential(context.Background())
	if err != nil {
		t.Fatalf("second ResolveConnectionCredential() error = %v", err)
	}
	if credential.Token != "retry-token" {
		t.Fatalf("second token = %q, want retry-token", credential.Token)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("token requests = %d, want timeout plus retry", got)
	}
}
