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
