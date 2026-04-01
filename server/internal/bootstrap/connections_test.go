package bootstrap

import (
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestResolvedNamedConnectionDefFallsBackToBaseForSurfaceAlias(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type:     pluginmanifestv1.AuthTypeOAuth2,
			ClientID: "base-client-id",
		},
		ConnectionParams: map[string]config.ConnectionParamDef{
			"tenant": {Required: true},
		},
	}

	got := resolvedNamedConnectionDef(plugin, nil, "api")

	if got.Auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		t.Fatalf("auth type = %q, want %q", got.Auth.Type, pluginmanifestv1.AuthTypeOAuth2)
	}
	if got.Auth.ClientID != "base-client-id" {
		t.Fatalf("client_id = %q, want %q", got.Auth.ClientID, "base-client-id")
	}
	if len(got.Params) != 1 || !got.Params["tenant"].Required {
		t.Fatalf("params = %#v, want tenant required", got.Params)
	}
}

func TestResolvedNamedConnectionDefDoesNotInheritTopLevelConfig(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type:         pluginmanifestv1.AuthTypeOAuth2,
			ClientID:     "base-client-id",
			ClientSecret: "base-client-secret",
		},
		ConnectionParams: map[string]config.ConnectionParamDef{
			"tenant": {Required: true},
		},
		Connections: map[string]*config.ConnectionDef{
			"api": {},
		},
	}

	got := resolvedNamedConnectionDef(plugin, nil, "api")

	if got.Auth.Type != "" {
		t.Fatalf("auth type = %q, want empty", got.Auth.Type)
	}
	if got.Auth.ClientID != "" || got.Auth.ClientSecret != "" {
		t.Fatalf("auth = %#v, want empty auth", got.Auth)
	}
	if len(got.Params) != 0 {
		t.Fatalf("params = %#v, want none", got.Params)
	}
}

func TestResolvedNamedConnectionDefMergesNamedDefsWithoutTopLevelInheritance(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Auth: &config.ConnectionAuthDef{
			Type:     pluginmanifestv1.AuthTypeOAuth2,
			ClientID: "base-client-id",
		},
		Connections: map[string]*config.ConnectionDef{
			"api": {
				Auth: config.ConnectionAuthDef{
					ClientSecret: "named-client-secret",
				},
			},
		},
	}
	manifestProvider := &pluginmanifestv1.Provider{
		Auth: &pluginmanifestv1.ProviderAuth{
			Type:     pluginmanifestv1.AuthTypeOAuth2,
			ClientID: "manifest-base-client-id",
		},
		Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
			"api": {
				Auth: &pluginmanifestv1.ProviderAuth{
					Type:             pluginmanifestv1.AuthTypeOAuth2,
					AuthorizationURL: "https://example.com/authorize",
					TokenURL:         "https://example.com/token",
				},
			},
		},
	}

	got := resolvedNamedConnectionDef(plugin, manifestProvider, "api")

	if got.Auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		t.Fatalf("auth type = %q, want %q", got.Auth.Type, pluginmanifestv1.AuthTypeOAuth2)
	}
	if got.Auth.AuthorizationURL != "https://example.com/authorize" {
		t.Fatalf("authorization_url = %q, want %q", got.Auth.AuthorizationURL, "https://example.com/authorize")
	}
	if got.Auth.TokenURL != "https://example.com/token" {
		t.Fatalf("token_url = %q, want %q", got.Auth.TokenURL, "https://example.com/token")
	}
	if got.Auth.ClientSecret != "named-client-secret" {
		t.Fatalf("client_secret = %q, want %q", got.Auth.ClientSecret, "named-client-secret")
	}
	if got.Auth.ClientID != "" {
		t.Fatalf("client_id = %q, want empty", got.Auth.ClientID)
	}
}

func TestResolveSpecSurfaceResolvesManifestRelativeSpecPath(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		ResolvedManifestPath: filepath.Join("/tmp", "plugins", "notion", "plugin.yaml"),
	}
	manifestProvider := &pluginmanifestv1.Provider{
		OpenAPI: "openapi.yaml",
	}

	resolved, ok := resolveSpecSurface(plugin, manifestProvider, specSurfaceOpenAPI)
	if !ok {
		t.Fatal("expected openapi surface to resolve")
	}

	want := filepath.Join("/tmp", "plugins", "notion", "openapi.yaml")
	if resolved.url != want {
		t.Fatalf("url = %q, want %q", resolved.url, want)
	}
}

func TestBuildConnectionMaps(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		plugin            *config.PluginDef
		wantDefault       string
		wantAPIConnection string
		wantMCPConnection string
	}{
		{
			name: "uses primary and mcp surface connections",
			plugin: &config.PluginDef{
				OpenAPI:           "https://example.com/openapi.json",
				OpenAPIConnection: "api",
				MCPURL:            "https://example.com/mcp",
				MCPConnection:     "mcp",
				Connections: map[string]*config.ConnectionDef{
					"api": {},
					"mcp": {},
				},
			},
			wantDefault:       "api",
			wantAPIConnection: "api",
			wantMCPConnection: "mcp",
		},
		{
			name: "falls back to sole named connection without base auth",
			plugin: &config.PluginDef{
				Connections: map[string]*config.ConnectionDef{
					"api": {},
				},
			},
			wantDefault:       "api",
			wantAPIConnection: "api",
			wantMCPConnection: "api",
		},
		{
			name: "does not fall back to sole named connection with base auth",
			plugin: &config.PluginDef{
				Auth: &config.ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeOAuth2},
				Connections: map[string]*config.ConnectionDef{
					"api": {},
				},
			},
			wantDefault:       config.PluginConnectionName,
			wantAPIConnection: config.PluginConnectionName,
			wantMCPConnection: config.PluginConnectionName,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Integrations: map[string]config.IntegrationDef{
					"example": {Plugin: tc.plugin},
				},
			}

			maps := BuildConnectionMaps(cfg)
			if got := maps.DefaultConnection["example"]; got != tc.wantDefault {
				t.Fatalf("default connection = %q, want %q", got, tc.wantDefault)
			}
			if got := maps.APIConnection["example"]; got != tc.wantAPIConnection {
				t.Fatalf("api connection = %q, want %q", got, tc.wantAPIConnection)
			}
			if got := maps.MCPConnection["example"]; got != tc.wantMCPConnection {
				t.Fatalf("mcp connection = %q, want %q", got, tc.wantMCPConnection)
			}
		})
	}
}
