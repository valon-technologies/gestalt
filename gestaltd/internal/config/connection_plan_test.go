package config

import (
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
