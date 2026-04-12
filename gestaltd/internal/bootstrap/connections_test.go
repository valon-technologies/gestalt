package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestBuildConnectionMaps_InferSingleNamedConnectionAsDefault(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"slack": {
					ResolvedManifest: &pluginmanifestv1.Manifest{
						Spec: &pluginmanifestv1.Spec{
							Surfaces: &pluginmanifestv1.PluginSurfaces{
								REST: &pluginmanifestv1.RESTSurface{
									BaseURL: "https://slack.com",
								},
							},
							Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
								"default": {
									Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeOAuth2},
								},
							},
						},
					},
				},
			},
		},
	}

	entry := cfg.Providers.Plugins["slack"]
	if entry == nil || entry.ResolvedManifest == nil || entry.ResolvedManifest.Spec == nil {
		t.Fatalf("slack entry manifest not populated: %#v", entry)
	}
	if entry.ManifestSpec() == nil {
		t.Fatal("entry.ManifestSpec() returned nil")
	}

	plan, err := buildPluginConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		t.Fatalf("buildPluginConnectionPlan: %v", err)
	}
	if got := plan.authDefaultConnection(); got != "default" {
		t.Fatalf("plan.authDefaultConnection() = %q, want %q", got, "default")
	}
	if got := plan.apiConnection(); got != "default" {
		t.Fatalf("plan.apiConnection() = %q, want %q", got, "default")
	}

	maps, err := BuildConnectionMaps(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionMaps: %v", err)
	}
	if got := maps.DefaultConnection["slack"]; got != "default" {
		t.Fatalf("DefaultConnection[slack] = %q, want %q", got, "default")
	}
	if got := maps.APIConnection["slack"]; got != "default" {
		t.Fatalf("APIConnection[slack] = %q, want %q", got, "default")
	}
	if got := maps.MCPConnection["slack"]; got != "default" {
		t.Fatalf("MCPConnection[slack] = %q, want %q", got, "default")
	}
}

func TestBuildConnectionMaps_DeclarativeRESTSurfaceUsesDeclaredConnection(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"example": {
					ResolvedManifest: &pluginmanifestv1.Manifest{
						Spec: &pluginmanifestv1.Spec{
							Surfaces: &pluginmanifestv1.PluginSurfaces{
								REST: &pluginmanifestv1.RESTSurface{
									BaseURL:    "https://api.example.com",
									Connection: "workspace",
								},
							},
							Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
								"workspace": {
									Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeOAuth2},
								},
								"backup": {
									Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeOAuth2},
								},
							},
						},
					},
				},
			},
		},
	}

	entry := cfg.Providers.Plugins["example"]
	if entry == nil || entry.ResolvedManifest == nil || entry.ResolvedManifest.Spec == nil {
		t.Fatalf("example entry manifest not populated: %#v", entry)
	}
	if entry.ManifestSpec() == nil {
		t.Fatal("entry.ManifestSpec() returned nil")
	}

	plan, err := buildPluginConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		t.Fatalf("buildPluginConnectionPlan: %v", err)
	}
	if got := plan.apiConnection(); got != "workspace" {
		t.Fatalf("plan.apiConnection() = %q, want %q", got, "workspace")
	}

	maps, err := BuildConnectionMaps(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionMaps: %v", err)
	}
	if got := maps.APIConnection["example"]; got != "workspace" {
		t.Fatalf("APIConnection[example] = %q, want %q", got, "workspace")
	}
	if got := maps.DefaultConnection["example"]; got != "" {
		t.Fatalf("DefaultConnection[example] = %q, want empty", got)
	}
}
