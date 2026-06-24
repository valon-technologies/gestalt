package providerrelease

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const releaseMetadataWithoutManifestIdentityYAML = `schema: gestaltd-provider-release
schemaVersion: 1
package: github.com/acme/providers/app
kind: app
version: 1.0.0
runtime: executable
artifacts:
  linux/amd64:
    path: app-linux-amd64.tar.gz
    sha256: abc123
staticValidation:
  manifest:
    spec: {}
  catalog:
    name: app
    operations:
      - id: echo
        method: POST
`

func TestMarshalMetadataOmitsStaticValidationManifestIdentity(t *testing.T) {
	t.Parallel()

	data, err := yaml.Marshal(validReleaseMetadataForTest())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	staticValidation := raw["staticValidation"].(map[string]any)
	manifest := staticValidation["manifest"].(map[string]any)
	for _, key := range []string{"kind", "source", "version"} {
		if _, ok := manifest[key]; ok {
			t.Fatalf("staticValidation.manifest contains duplicated %q in:\n%s", key, data)
		}
	}
}

func TestDecodeMetadataInheritsStaticValidationManifestIdentity(t *testing.T) {
	t.Parallel()

	metadata, err := Decode([]byte(releaseMetadataWithoutManifestIdentityYAML))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	manifest := metadata.StaticValidation.Manifest
	if manifest.Kind != providermanifestv1.KindApp || manifest.Source != metadata.Package || manifest.Version != metadata.Version {
		t.Fatalf("manifest identity = kind %q source %q version %q, want inherited top-level identity", manifest.Kind, manifest.Source, manifest.Version)
	}
}

func TestValidateMetadataAllowsExecutableForHybridProjection(t *testing.T) {
	t.Parallel()

	// A hybrid app provider (manifest-backed spec surface plus a hosted binary) projects to a
	// validation manifest whose entrypoint is stripped, so RuntimeForManifest on the projection
	// returns declarative. The release metadata runtime is executable (the binary exists), and
	// ValidateMetadata must accept this because the projection cannot carry the entrypoint signal.
	metadata := validReleaseMetadataForTest()
	metadata.Runtime = RuntimeExecutable
	metadata.StaticValidation.Manifest = &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  metadata.Package,
		Version: metadata.Version,
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					Connection: "default",
					BaseURL:    "https://api.example.test",
					Operations: []providermanifestv1.ProviderOperation{{
						Name:   "listThings",
						Method: "GET",
						Path:   "/things",
					}},
				},
			},
		},
	}

	if err := ValidateMetadata(metadata); err != nil {
		t.Fatalf("ValidateMetadata error = %v, want nil for executable release with declarative projection", err)
	}
}

func TestValidateMetadataRejectsRuntimeMismatch(t *testing.T) {
	t.Parallel()

	metadata := validReleaseMetadataForTest()
	metadata.Runtime = RuntimeDeclarative

	err := ValidateMetadata(metadata)
	if err == nil || !strings.Contains(err.Error(), `runtime "declarative" does not match validation manifest runtime "executable"`) {
		t.Fatalf("ValidateMetadata error = %v, want runtime mismatch", err)
	}
}

func TestValidateMetadataRejectsInvalidCatalogSessionOnly(t *testing.T) {
	t.Parallel()

	metadata := validReleaseMetadataForTest()
	metadata.StaticValidation.Catalog = nil
	metadata.StaticValidation.CatalogSessionOnly = true

	err := ValidateMetadata(metadata)
	if err == nil || !strings.Contains(err.Error(), "catalogSessionOnly is only valid for MCP-only validation manifests") {
		t.Fatalf("ValidateMetadata error = %v, want catalogSessionOnly validation failure", err)
	}
}

func validReleaseMetadataForTest() *Metadata {
	return &Metadata{
		Schema:        SchemaName,
		SchemaVersion: SchemaVersion,
		Package:       "github.com/acme/providers/app",
		Kind:          providermanifestv1.KindApp,
		Version:       "1.0.0",
		Runtime:       RuntimeExecutable,
		Artifacts: Artifacts{
			"linux/amd64": {Path: "app-linux-amd64.tar.gz", SHA256: "abc123"},
		},
		StaticValidation: &StaticValidation{
			Manifest: &providermanifestv1.Manifest{
				Kind:    providermanifestv1.KindApp,
				Source:  "github.com/acme/providers/app",
				Version: "1.0.0",
				Spec:    &providermanifestv1.Spec{},
			},
			Catalog: &catalog.Catalog{
				Name: "app",
				Operations: []catalog.CatalogOperation{{
					ID:     "echo",
					Method: "POST",
				}},
			},
		},
	}
}
