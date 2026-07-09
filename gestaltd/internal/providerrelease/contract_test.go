package providerrelease

import (
	"testing"
)

func TestParseContractFromManifestRaw(t *testing.T) {
	t.Parallel()

	requires, compatibility, err := ParseContractFromManifestRaw([]byte(`
dependencies:
  apps:
    slack:
      version: "^1.4.0"
      operations:
        chat.postMessage:
          inputSchemaHash: sha256:abc
compatibility:
  minGestaltdVersion: "0.20.0"
`))
	if err != nil {
		t.Fatalf("ParseContractFromManifestRaw: %v", err)
	}
	if requires.Apps["slack"].Version != "^1.4.0" {
		t.Fatalf("requires version = %q", requires.Apps["slack"].Version)
	}
	if requires.Apps["slack"].Operations["chat.postMessage"].InputSchemaHash != "sha256:abc" {
		t.Fatalf("requires operation hash = %q", requires.Apps["slack"].Operations["chat.postMessage"].InputSchemaHash)
	}
	if compatibility.MinGestaltdVersion != "0.20.0" {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestParseContractFromManifestRawRejectsInvalidYAML(t *testing.T) {
	t.Parallel()

	_, _, err := ParseContractFromManifestRaw([]byte("compatibility: [\n"))
	if err == nil {
		t.Fatal("expected contract decode error")
	}
}
