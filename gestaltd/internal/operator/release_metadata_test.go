package operator

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
)

func TestDecodeProviderReleaseMetadataAcceptsStaticValidation(t *testing.T) {
	t.Parallel()

	metadata, err := decodeProviderReleaseMetadata([]byte(`
schema: gestaltd-provider-release
schemaVersion: 1
package: github.com/acme/providers/provider
kind: app
version: 1.2.3
runtime: executable
artifacts:
  linux/amd64:
    path: provider-linux-amd64.tar.gz
    sha256: linux-sha
staticValidation:
  manifest:
    kind: app
    source: github.com/acme/providers/provider
    version: 1.2.3
    entrypoint:
      artifactPath: static-validation-placeholder
    spec: {}
  catalog:
    name: provider
    operations:
      - id: echo
        method: POST
        path: /echo
  catalogSessionOnly: true
`))
	if err != nil {
		t.Fatalf("decodeProviderReleaseMetadata: %v", err)
	}
	if metadata.StaticValidation == nil {
		t.Fatal("StaticValidation = nil, want decoded metadata")
	}
	if metadata.StaticValidation.Manifest == nil || metadata.StaticValidation.Manifest.Entrypoint == nil {
		t.Fatalf("static manifest = %+v, want entrypoint", metadata.StaticValidation.Manifest)
	}
	if got := metadata.StaticValidation.Manifest.Entrypoint.ArtifactPath; got != staticvalidation.EntrypointPlaceholder {
		t.Fatalf("static entrypoint artifactPath = %q, want %q", got, staticvalidation.EntrypointPlaceholder)
	}
	if metadata.StaticValidation.Catalog == nil || len(metadata.StaticValidation.Catalog.Operations) != 1 {
		t.Fatalf("static catalog = %+v, want one operation", metadata.StaticValidation.Catalog)
	}
	if !metadata.StaticValidation.CatalogSessionOnly {
		t.Fatal("CatalogSessionOnly = false, want true")
	}
}

func TestDecodeProviderReleaseMetadataRejectsPlatformSpecificStaticManifest(t *testing.T) {
	t.Parallel()

	_, err := decodeProviderReleaseMetadata([]byte(`
schema: gestaltd-provider-release
schemaVersion: 1
package: github.com/acme/providers/provider
kind: app
version: 1.2.3
runtime: executable
artifacts:
  linux/amd64:
    path: provider-linux-amd64.tar.gz
    sha256: linux-sha
staticValidation:
  manifest:
    kind: app
    source: github.com/acme/providers/provider
    version: 1.2.3
    artifacts:
      - os: linux
        arch: amd64
        path: bin/provider
    entrypoint:
      artifactPath: bin/provider
    spec: {}
`))
	if err == nil || !strings.Contains(err.Error(), "staticValidation.manifest must not include platform artifacts") {
		t.Fatalf("decodeProviderReleaseMetadata error = %v, want platform artifact rejection", err)
	}
}
