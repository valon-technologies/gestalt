package bootstrap

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

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
	if conn["provider"] != "gmail" || conn["connection"] != "default" || conn["connectionId"] != "google-workspace" || conn["mode"] != "subject" {
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
