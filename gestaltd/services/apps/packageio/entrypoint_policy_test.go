package packageio

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestEffectiveSourceEntrypointForKind_DefaultsForDeclaredBuild(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindAgent,
		Source: "github.com/valon-technologies/gestalt-providers/agent/example-agent",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"./build.sh"},
		},
	}
	entry, err := EffectiveSourceEntrypointForKind(manifest, providermanifestv1.KindAgent)
	if err != nil {
		t.Fatalf("EffectiveSourceEntrypointForKind: %v", err)
	}
	if entry == nil || entry.ArtifactPath != ".gestalt/build/example-agent" {
		t.Fatalf("entry = %#v, want default .gestalt/build/example-agent", entry)
	}
}

func TestEffectiveSourceEntrypointForKind_ExplicitOverrideWins(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindAgent,
		Source: "github.com/valon-technologies/gestalt-providers/agent/example-agent",
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: "dist/custom-agent",
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"./build.sh"},
		},
	}
	entry, err := EffectiveSourceEntrypointForKind(manifest, providermanifestv1.KindAgent)
	if err != nil {
		t.Fatalf("EffectiveSourceEntrypointForKind: %v", err)
	}
	if entry == nil || entry.ArtifactPath != "dist/custom-agent" {
		t.Fatalf("entry = %#v, want explicit dist/custom-agent", entry)
	}
}

func TestEffectiveSourceEntrypointForKind_UIWithBuildHasNoDefault(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindUI,
		Source: "github.com/valon-technologies/gestalt-providers/ui/foo-ui",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"npm", "run", "build"},
		},
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	}
	_, err := EffectiveSourceEntrypointForKind(manifest, providermanifestv1.KindUI)
	if err == nil || !strings.Contains(err.Error(), "entrypoint.artifactPath is required") {
		t.Fatalf("EffectiveSourceEntrypointForKind err = %v, want no default for ui", err)
	}
	if SourceEntrypointMayDefault(manifest, providermanifestv1.KindUI) {
		t.Fatal("ui provider with build should not allow default entrypoint")
	}
}

func TestEffectiveSourceEntrypointForKind_ManifestBackedAppHasNoDefault(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:   providermanifestv1.KindApp,
		Source: "github.com/test/apps/manifest-backed",
		Run:    []string{"npm", "run", "dev"},
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.com",
					Operations: []providermanifestv1.ProviderOperation{{
						Name:   "get_status",
						Method: "GET",
						Path:   "/status",
					}},
				},
			},
		},
	}
	_, err := EffectiveSourceEntrypointForKind(manifest, providermanifestv1.KindApp)
	if err == nil || !strings.Contains(err.Error(), "entrypoint.artifactPath is required") {
		t.Fatalf("EffectiveSourceEntrypointForKind err = %v, want no default for manifest-backed app", err)
	}
}
