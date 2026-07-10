package packageio

import (
	"strings"
	"testing"
)

func TestValidateManifest_RejectsSourceEntrypoint(t *testing.T) {
	t.Parallel()

	manifestYAML := []byte(`kind: agent
source: github.com/valon-technologies/gestalt-providers/agent/claude
version: 0.0.1
entrypoint:
  artifactPath: .gestalt/build/claude
build:
  command:
    - npm
    - run
    - build
`)
	_, err := DecodeSourceManifestFormat(manifestYAML, ManifestFormatYAML)
	if err == nil || !strings.Contains(err.Error(), sourceEntrypointReject) {
		t.Fatalf("DecodeSourceManifestFormat err = %v, want source entrypoint rejection", err)
	}
}

func TestDecodeSourceManifestFormat_AcceptsContractFields(t *testing.T) {
	t.Parallel()

	manifestYAML := []byte(`kind: workflow
source: github.com/testowner/workflows/contract
version: 1.0.0
dependencies:
  apps:
    github.com/acme/apps/base:
      version: 1.0.0
      operations:
        listItems:
          inputSchemaHash: abc123
compatibility:
  minGestaltdVersion: 0.5.0
build:
  command:
    - echo
    - build
`)
	manifest, err := DecodeSourceManifestFormat(manifestYAML, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat: %v", err)
	}
	if manifest.Dependencies == nil || manifest.Dependencies.Apps["github.com/acme/apps/base"].Version != "1.0.0" {
		t.Fatalf("dependencies = %#v", manifest.Dependencies)
	}
	if manifest.Compatibility == nil || manifest.Compatibility.MinGestaltdVersion != "0.5.0" {
		t.Fatalf("compatibility = %#v", manifest.Compatibility)
	}
}

func TestDecodeSourceManifestFormat_RejectsLegacyReleaseField(t *testing.T) {
	t.Parallel()

	manifestJSON := []byte(`{
  "kind": "workflow",
  "source": "github.com/testowner/workflows/legacy",
  "version": "1.0.0",
  "release": {
    "build": "deleted"
  }
}`)
	_, err := DecodeSourceManifestFormat(manifestJSON, ManifestFormatJSON)
	if err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("DecodeSourceManifestFormat err = %v, want unknown release field rejection", err)
	}
}
