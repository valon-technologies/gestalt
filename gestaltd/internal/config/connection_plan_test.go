package config

import (
	"slices"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestBuildStaticConnectionPlan_PrefersNamedDefaultConnection(t *testing.T) {
	t.Parallel()

	plan, err := BuildStaticConnectionPlan(&ProviderEntry{}, &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"default": {Mode: providermanifestv1.ConnectionModeSubject},
			"bot":     {Mode: providermanifestv1.ConnectionModeSubject},
		},
		Surfaces: &providermanifestv1.ProviderSurfaces{
			REST: &providermanifestv1.RESTSurface{
				BaseURL: "https://slack.com",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildStaticConnectionPlan() error = %v", err)
	}

	if got := plan.AuthDefaultConnection(); got != "default" {
		t.Fatalf("AuthDefaultConnection() = %q, want %q", got, "default")
	}
	if got := plan.APIConnection(); got != "default" {
		t.Fatalf("APIConnection() = %q, want %q", got, "default")
	}
	if got := plan.MCPConnection(); got != "default" {
		t.Fatalf("MCPConnection() = %q, want %q", got, "default")
	}
}

func TestBuildStaticConnectionPlan_RejectsConnectionExposure(t *testing.T) {
	t.Parallel()

	_, err := BuildStaticConnectionPlan(&ProviderEntry{}, &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"default": {Exposure: "internal"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "manifest exposure is not supported") {
		t.Fatalf("BuildStaticConnectionPlan() manifest exposure error = %v", err)
	}

	_, err = BuildStaticConnectionPlan(&ProviderEntry{
		Connections: map[string]*ConnectionDef{
			"default": {Exposure: "internal"},
		},
	}, &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{"default": {}},
	})
	if err == nil || !strings.Contains(err.Error(), "deploy exposure is not supported") {
		t.Fatalf("BuildStaticConnectionPlan() deploy exposure error = %v", err)
	}
}

func TestStaticConnectionPlan_DeclaredSurfaces(t *testing.T) {
	t.Parallel()

	connections := map[string]*providermanifestv1.ManifestConnectionDef{
		"ApiKey": {Mode: providermanifestv1.ConnectionModeSubject},
		"MCP":    {Mode: providermanifestv1.ConnectionModeSubject},
	}
	tests := []struct {
		name     string
		surfaces *providermanifestv1.ProviderSurfaces
		want     []DeclaredSurface
	}{
		{
			name:     "no surfaces",
			surfaces: nil,
			want:     []DeclaredSurface{},
		},
		{
			name: "MCP only",
			surfaces: &providermanifestv1.ProviderSurfaces{
				MCP: &providermanifestv1.MCPSurface{Connection: "MCP", URL: "https://example.com/mcp"},
			},
			want: []DeclaredSurface{{SurfaceKindMCP, "MCP"}},
		},
		{
			name: "every surface in stable order",
			surfaces: &providermanifestv1.ProviderSurfaces{
				MCP:     &providermanifestv1.MCPSurface{Connection: "MCP", URL: "https://example.com/mcp"},
				GraphQL: &providermanifestv1.GraphQLSurface{Connection: "ApiKey", URL: "https://example.com/graphql"},
				REST:    &providermanifestv1.RESTSurface{Connection: "ApiKey", BaseURL: "https://example.com"},
				OpenAPI: &providermanifestv1.OpenAPISurface{Connection: "ApiKey", Document: "https://example.com/openapi.json"},
			},
			want: []DeclaredSurface{
				{SurfaceKindOpenAPI, "ApiKey"},
				{SurfaceKindREST, "ApiKey"},
				{SurfaceKindGraphQL, "ApiKey"},
				{SurfaceKindMCP, "MCP"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, err := BuildStaticConnectionPlan(&ProviderEntry{}, &providermanifestv1.Spec{
				Connections: connections,
				Surfaces:    tt.surfaces,
			})
			if err != nil {
				t.Fatalf("BuildStaticConnectionPlan() error = %v", err)
			}
			got := plan.DeclaredSurfaces()
			if !slices.Equal(got, tt.want) {
				t.Fatalf("DeclaredSurfaces() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
