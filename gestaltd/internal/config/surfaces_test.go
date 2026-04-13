package config

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestProviderSurfaceURLOverride(t *testing.T) {
	t.Parallel()

	entry := &ProviderEntry{
		Surfaces: &ProviderSurfaceOverrides{
			GraphQL: &ProviderGraphQLSurfaceOverride{URL: " https://config.example/graphql "},
			MCP:     &ProviderMCPSurfaceOverride{URL: "https://config.example/mcp"},
		},
	}

	if got := ProviderSurfaceURLOverride(entry, SpecSurfaceOpenAPI); got != "" {
		t.Fatalf("ProviderSurfaceURLOverride(openapi) = %q, want empty", got)
	}
	if got := ProviderSurfaceURLOverride(entry, SpecSurfaceGraphQL); got != "https://config.example/graphql" {
		t.Fatalf("ProviderSurfaceURLOverride(graphql) = %q, want %q", got, "https://config.example/graphql")
	}
	if got := ProviderSurfaceURLOverride(entry, SpecSurfaceMCP); got != "https://config.example/mcp" {
		t.Fatalf("ProviderSurfaceURLOverride(mcp) = %q, want %q", got, "https://config.example/mcp")
	}
}

func TestEffectiveProviderSpecBaseURL(t *testing.T) {
	t.Parallel()

	t.Run("prefers configured rest override", func(t *testing.T) {
		t.Parallel()

		entry := &ProviderEntry{
			Surfaces: &ProviderSurfaceOverrides{
				REST:    &ProviderRESTSurfaceOverride{BaseURL: " https://config.example/rest "},
				OpenAPI: &ProviderOpenAPISurfaceOverride{BaseURL: "https://config.example/openapi"},
			},
		}
		manifest := &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{BaseURL: "https://manifest.example/rest"},
				OpenAPI: &providermanifestv1.OpenAPISurface{
					Document: "openapi.yaml",
					BaseURL:  "https://manifest.example/openapi",
				},
			},
		}

		if got := EffectiveProviderSpecBaseURL(entry, manifest); got != "https://config.example/rest" {
			t.Fatalf("EffectiveProviderSpecBaseURL() = %q, want %q", got, "https://config.example/rest")
		}
	})

	t.Run("falls back to configured openapi override", func(t *testing.T) {
		t.Parallel()

		entry := &ProviderEntry{
			Surfaces: &ProviderSurfaceOverrides{
				OpenAPI: &ProviderOpenAPISurfaceOverride{BaseURL: "https://config.example/openapi"},
			},
		}
		manifest := &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{
					Document: "openapi.yaml",
					BaseURL:  "https://manifest.example/openapi",
				},
			},
		}

		if got := EffectiveProviderSpecBaseURL(entry, manifest); got != "https://config.example/openapi" {
			t.Fatalf("EffectiveProviderSpecBaseURL() = %q, want %q", got, "https://config.example/openapi")
		}
	})

	t.Run("falls back to manifest base urls", func(t *testing.T) {
		t.Parallel()

		manifest := &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{
					Document: "openapi.yaml",
					BaseURL:  "https://manifest.example/openapi",
				},
			},
		}

		if got := EffectiveProviderSpecBaseURL(nil, manifest); got != "https://manifest.example/openapi" {
			t.Fatalf("EffectiveProviderSpecBaseURL() = %q, want %q", got, "https://manifest.example/openapi")
		}
	})

	t.Run("returns empty when nothing is configured", func(t *testing.T) {
		t.Parallel()

		if got := EffectiveProviderSpecBaseURL(nil, nil); got != "" {
			t.Fatalf("EffectiveProviderSpecBaseURL() = %q, want empty", got)
		}
	})
}
