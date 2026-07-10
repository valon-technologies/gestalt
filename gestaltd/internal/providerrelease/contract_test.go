package providerrelease

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestParseContract_PrefersManifestStruct(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Dependencies: &providermanifestv1.ManifestDependencies{
			Apps: map[string]providermanifestv1.ManifestAppDependency{
				"github.com/acme/apps/base": {
					Version: "1.0.0",
					Operations: map[string]providermanifestv1.ManifestOperationDependency{
						"listItems": {InputSchemaHash: "abc123"},
					},
				},
			},
		},
		Compatibility: &providermanifestv1.ManifestCompatibility{
			MinGestaltdVersion: "0.5.0",
		},
	}
	raw := []byte(`dependencies:
  apps:
    github.com/other/apps/ignored:
      version: 9.9.9
compatibility:
  minGestaltdVersion: 9.9.9
`)

	requires, compatibility, err := ParseContract(manifest, raw)
	if err != nil {
		t.Fatalf("ParseContract: %v", err)
	}
	if len(requires.Apps) != 1 {
		t.Fatalf("requires.Apps = %#v, want one app", requires.Apps)
	}
	app, ok := requires.Apps["github.com/acme/apps/base"]
	if !ok {
		t.Fatalf("requires.Apps = %#v", requires.Apps)
	}
	if app.Version != "1.0.0" {
		t.Fatalf("app version = %q", app.Version)
	}
	if app.Operations["listItems"].InputSchemaHash != "abc123" {
		t.Fatalf("operations = %#v", app.Operations)
	}
	if compatibility.MinGestaltdVersion != "0.5.0" {
		t.Fatalf("minGestaltdVersion = %q", compatibility.MinGestaltdVersion)
	}
}

func TestParseContract_FallsBackToRawManifest(t *testing.T) {
	t.Parallel()

	raw := []byte(`kind: app
source: github.com/testowner/apps/contract
version: 1.0.0
dependencies:
  apps:
    github.com/acme/apps/base:
      version: 2.0.0
compatibility:
  minGestaltdVersion: 0.4.0
`)

	requires, compatibility, err := ParseContract(nil, raw)
	if err != nil {
		t.Fatalf("ParseContract: %v", err)
	}
	if requires.Apps["github.com/acme/apps/base"].Version != "2.0.0" {
		t.Fatalf("requires.Apps = %#v", requires.Apps)
	}
	if compatibility.MinGestaltdVersion != "0.4.0" {
		t.Fatalf("minGestaltdVersion = %q", compatibility.MinGestaltdVersion)
	}
}
