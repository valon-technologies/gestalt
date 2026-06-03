package operator

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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
	if metadata.StaticValidation.Manifest == nil {
		t.Fatalf("static manifest = %+v, want manifest", metadata.StaticValidation.Manifest)
	}
	if metadata.StaticValidation.Manifest.Entrypoint != nil {
		t.Fatalf("static manifest entrypoint = %+v, want nil", metadata.StaticValidation.Manifest.Entrypoint)
	}
	if metadata.StaticValidation.Catalog == nil || len(metadata.StaticValidation.Catalog.Operations) != 1 {
		t.Fatalf("static catalog = %+v, want one operation", metadata.StaticValidation.Catalog)
	}
	if !metadata.StaticValidation.CatalogSessionOnly {
		t.Fatal("CatalogSessionOnly = false, want true")
	}
}

func TestDecodeProviderReleaseMetadataRejectsInvalidStaticValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		block   *string
		wantErr string
	}{
		{
			name: "platform-specific static manifest",
			block: ptr(`
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
    spec: {}`),
			wantErr: "staticValidation.manifest must not include platform artifacts",
		},
		{
			name: "static manifest entrypoint",
			block: ptr(`
  manifest:
    kind: app
    source: github.com/acme/providers/provider
    version: 1.2.3
    entrypoint:
      artifactPath: bin/provider
    spec: {}`),
			wantErr: "staticValidation.manifest must not include entrypoint",
		},
		{
			name: "session-only flag without metadata",
			block: ptr(`
  catalogSessionOnly: true`),
			wantErr: "staticValidation must include manifest or catalog metadata",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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
` + staticValidationBlock(tc.block)))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("decodeProviderReleaseMetadata error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func staticValidationBlock(block *string) string {
	if block == nil {
		return ""
	}
	return "staticValidation:" + *block + "\n"
}

func ptr(value string) *string {
	return &value
}

func (metadata providerReleaseMetadata) MarshalYAML() (any, error) {
	type providerReleaseMetadataYAML providerReleaseMetadata
	out := providerReleaseMetadataYAML(metadata)
	if out.StaticValidation == nil {
		out.StaticValidation = defaultProviderReleaseStaticValidationForTest(&metadata)
	}
	return out, nil
}

func defaultProviderReleaseStaticValidationForTest(metadata *providerReleaseMetadata) *providerReleaseStaticValidationData {
	if metadata == nil {
		return nil
	}
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.NormalizeKind(metadata.Kind),
		Source:  strings.TrimSpace(metadata.Package),
		Version: strings.TrimSpace(metadata.Version),
		Spec:    &providermanifestv1.Spec{},
	}
	return &providerReleaseStaticValidationData{Manifest: manifest}
}
