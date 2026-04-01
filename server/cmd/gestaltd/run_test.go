package main

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestBuildMCPSurfaceIncludesManifestDeclaredProviders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		manifest *pluginmanifestv1.Manifest
	}{
		{
			name: "manifest catalog",
			manifest: &pluginmanifestv1.Manifest{
				Source:      "github.com/testowner/plugins/example",
				Version:     "0.0.1",
				DisplayName: "Example",
				Kinds:       []string{pluginmanifestv1.KindProvider},
				Provider: &pluginmanifestv1.Provider{
					BaseURL: "https://example.com",
					Operations: []pluginmanifestv1.ProviderOperation{
						{Name: "search", Method: "GET", Path: "/search"},
					},
				},
			},
		},
		{
			name: "manifest spec surface",
			manifest: &pluginmanifestv1.Manifest{
				Source:      "github.com/testowner/plugins/example",
				Version:     "0.0.1",
				DisplayName: "Example",
				Kinds:       []string{pluginmanifestv1.KindProvider},
				Provider: &pluginmanifestv1.Provider{
					OpenAPI: "https://example.com/openapi.json",
				},
				Artifacts: []pluginmanifestv1.Artifact{
					{
						OS:     "darwin",
						Arch:   "arm64",
						Path:   "artifacts/darwin/arm64/provider",
						SHA256: sha256HexForTest("provider"),
					},
				},
				Entrypoints: pluginmanifestv1.Entrypoints{
					Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/darwin/arm64/provider"},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Integrations: map[string]config.IntegrationDef{
					"example": {
						Plugin: &config.PluginDef{
							Source:           "github.com/testowner/plugins/example",
							ResolvedManifest: tc.manifest,
						},
					},
				},
			}

			surface := buildMCPSurface(cfg, bootstrap.BuildConnectionMaps(cfg))
			if len(surface.providers) != 1 || surface.providers[0] != "example" {
				t.Fatalf("providers = %v", surface.providers)
			}
			if got := surface.toolPrefixes["example"]; got != "example_" {
				t.Fatalf("tool prefix = %q, want %q", got, "example_")
			}
			if got := surface.apiConnection["example"]; got != config.PluginConnectionName {
				t.Fatalf("api connection = %q, want %q", got, config.PluginConnectionName)
			}
			if got := surface.mcpConnection["example"]; got != config.PluginConnectionName {
				t.Fatalf("mcp connection = %q, want %q", got, config.PluginConnectionName)
			}
		})
	}
}
