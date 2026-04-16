package config

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestProviderEntryMCPExposure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		entry       *ProviderEntry
		declaresMCP bool
		hasSurface  bool
		exposesMCP  bool
		includeREST bool
	}{
		{
			name: "explicit config mcp",
			entry: &ProviderEntry{
				MCP: true,
			},
			declaresMCP: true,
			exposesMCP:  true,
			includeREST: true,
		},
		{
			name: "manifest mcp flag",
			entry: &ProviderEntry{
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{MCP: true},
				},
			},
			declaresMCP: true,
			exposesMCP:  true,
			includeREST: true,
		},
		{
			name: "manifest mcp surface only",
			entry: &ProviderEntry{
				ResolvedManifest: &providermanifestv1.Manifest{
					Spec: &providermanifestv1.Spec{
						Surfaces: &providermanifestv1.ProviderSurfaces{
							MCP: &providermanifestv1.MCPSurface{URL: "https://mcp.example"},
						},
					},
				},
			},
			hasSurface: true,
			exposesMCP: true,
		},
		{
			name: "config mcp override surface only",
			entry: &ProviderEntry{
				Surfaces: &ProviderSurfaceOverrides{
					MCP: &ProviderMCPSurfaceOverride{URL: "https://override.example/mcp"},
				},
			},
			hasSurface: true,
			exposesMCP: true,
		},
		{
			name:        "nil entry",
			entry:       nil,
			declaresMCP: false,
			hasSurface:  false,
			exposesMCP:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.entry.DeclaresMCP(); got != tc.declaresMCP {
				t.Fatalf("DeclaresMCP() = %v, want %v", got, tc.declaresMCP)
			}
			if got := tc.entry.HasMCPSurface(); got != tc.hasSurface {
				t.Fatalf("HasMCPSurface() = %v, want %v", got, tc.hasSurface)
			}
			if got := tc.entry.ExposesMCP(); got != tc.exposesMCP {
				t.Fatalf("ExposesMCP() = %v, want %v", got, tc.exposesMCP)
			}
			if got := tc.entry.IncludeRESTInMCP(); got != tc.includeREST {
				t.Fatalf("IncludeRESTInMCP() = %v, want %v", got, tc.includeREST)
			}
		})
	}
}

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
