package operator

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestDecodeProviderReleaseMetadataNormalizesLegacyPluginKind(t *testing.T) {
	t.Parallel()

	metadata, err := decodeProviderReleaseMetadata([]byte(`
schema: gestaltd-provider-release
schemaVersion: 1
package: github.com/valon-technologies/token-pile/tokenpile
kind: plugin
version: 0.2.2
runtime: executable
artifacts:
  linux/amd64:
    path: token-pile_linux_amd64.tar.gz
    sha256: abc123
`))
	if err != nil {
		t.Fatalf("decodeProviderReleaseMetadata: %v", err)
	}
	if metadata.Kind != providermanifestv1.KindApp {
		t.Fatalf("Kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
	}
}
