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
			"default": {Mode: providermanifestv1.ConnectionModeUser},
			"bot":     {Mode: providermanifestv1.ConnectionModeUser},
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

func TestBuildStaticConnectionPlan_ConnectionExposureMergeContract(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"user": {
				Mode:     providermanifestv1.ConnectionModePlatform,
				Exposure: providermanifestv1.ConnectionExposureUser,
			},
			"internal": {
				Mode:     providermanifestv1.ConnectionModePlatform,
				Exposure: providermanifestv1.ConnectionExposureInternal,
			},
		},
	}
	plan, err := BuildStaticConnectionPlan(&ProviderEntry{
		Connections: map[string]*ConnectionDef{
			"user": {Exposure: providermanifestv1.ConnectionExposureInternal},
		},
	}, manifest)
	if err != nil {
		t.Fatalf("BuildStaticConnectionPlan() narrowing error = %v", err)
	}
	userConn, ok := plan.ResolvedNamedConnectionDef("user")
	if !ok {
		t.Fatal("resolved user connection missing")
	}
	if userConn.Exposure != providermanifestv1.ConnectionExposureInternal || !userConn.Source.NarrowedByDeploy {
		t.Fatalf("resolved user connection = %+v, want deploy-narrowed internal exposure", userConn)
	}

	_, err = BuildStaticConnectionPlan(&ProviderEntry{
		Connections: map[string]*ConnectionDef{
			"internal": {Exposure: providermanifestv1.ConnectionExposureUser},
		},
	}, manifest)
	if err == nil || !strings.Contains(err.Error(), "cannot widen") {
		t.Fatalf("BuildStaticConnectionPlan() widening error = %v, want cannot widen", err)
	}
}

func TestBuildStaticConnectionPlan_RejectsInternalUserConnection(t *testing.T) {
	t.Parallel()

	_, err := BuildStaticConnectionPlan(&ProviderEntry{}, &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"default": {
				Mode:     providermanifestv1.ConnectionModeUser,
				Exposure: providermanifestv1.ConnectionExposureInternal,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported for user-owned connections") {
		t.Fatalf("BuildStaticConnectionPlan() error = %v, want internal user rejection", err)
	}
}
