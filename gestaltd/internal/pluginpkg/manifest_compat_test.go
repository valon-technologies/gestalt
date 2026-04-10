package pluginpkg

import (
	"runtime"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestDecodeManifestCompatibility_AllowsLegacyKindsField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   []byte
		decode func([]byte, string) (*testManifestCompat, error)
		assert func(t *testing.T, manifest *testManifestCompat)
	}{
		{
			name: "source auth manifest",
			data: []byte(`
source: github.com/acme/providers/auth
version: 0.0.1-alpha.1
kinds:
  - auth
auth: {}
`),
			decode: decodeSourceManifestCompat,
			assert: func(t *testing.T, manifest *testManifestCompat) {
				t.Helper()
				if !manifest.Auth {
					t.Fatal("expected auth metadata")
				}
			},
		},
		{
			name: "packaged datastore manifest",
			data: []byte(`
source: github.com/acme/providers/datastore
version: 0.0.1-alpha.1
displayName: Datastore
kinds:
  - datastore
datastore: {}
artifacts:
  - os: linux
    arch: amd64
    path: gestalt-plugin-datastore
    sha256: deadbeef
entrypoints:
  datastore:
    artifactPath: gestalt-plugin-datastore
`),
			decode: decodeManifestCompat,
			assert: func(t *testing.T, manifest *testManifestCompat) {
				t.Helper()
				if !manifest.Datastore {
					t.Fatal("expected datastore metadata")
				}
				if manifest.DatastoreEntrypoint != "gestalt-plugin-datastore" {
					t.Fatalf("datastore entrypoint = %q", manifest.DatastoreEntrypoint)
				}
			},
		},
		{
			name: "packaged secrets manifest",
			data: []byte(`
source: github.com/acme/providers/secrets
version: 0.0.1-alpha.1
kinds:
  - secrets
secrets: {}
artifacts:
  - os: linux
    arch: amd64
    path: gestalt-plugin-secrets
    sha256: cafebabe
entrypoints:
  secrets:
    artifactPath: gestalt-plugin-secrets
`),
			decode: decodeManifestCompat,
			assert: func(t *testing.T, manifest *testManifestCompat) {
				t.Helper()
				if !manifest.Secrets {
					t.Fatal("expected secrets metadata")
				}
				if manifest.SecretsEntrypoint != "gestalt-plugin-secrets" {
					t.Fatalf("secrets entrypoint = %q", manifest.SecretsEntrypoint)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest, err := tc.decode(tc.data, ManifestFormatYAML)
			if err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			tc.assert(t, manifest)
		})
	}
}

func TestDecodeManifestCompatibility_MapsLegacyProviderResponsePaginationPaths(t *testing.T) {
	t.Parallel()

	manifest, err := DecodeSourceManifestFormat([]byte(`
source: github.com/acme/providers/ashby
version: 0.0.1-alpha.1
provider:
  responseMapping:
    dataPath: results
    pagination:
      has_more_path: moreDataAvailable
      cursor_path: nextCursor
  surfaces:
    openapi:
      document: openapi.yaml
`), ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat: %v", err)
	}
	if manifest.Plugin == nil || manifest.Plugin.ResponseMapping == nil || manifest.Plugin.ResponseMapping.Pagination == nil {
		t.Fatalf("unexpected provider response mapping: %#v", manifest.Plugin)
	}
	if got := manifest.Plugin.ResponseMapping.Pagination.HasMore; got == nil || got.Source != "body" || got.Path != "moreDataAvailable" {
		t.Fatalf("has_more selector = %#v", got)
	}
	if got := manifest.Plugin.ResponseMapping.Pagination.Cursor; got == nil || got.Source != "body" || got.Path != "nextCursor" {
		t.Fatalf("cursor selector = %#v", got)
	}
}

func TestCurrentPlatformArtifact_AllowsSingleLinuxLibCSpecificArtifactWhenLibCUnknown(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:    "github.com/acme/providers/datastore",
		Version:   "0.0.1-alpha.1",
		Datastore: &pluginmanifestv1.DatastoreMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCGLibC,
				Path:   "gestalt-plugin-datastore",
				SHA256: "deadbeef",
			},
		},
	}

	artifact, err := CurrentPlatformArtifact(manifest, "")
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-linux platform")
		}
		return
	}
	if err != nil {
		t.Fatalf("CurrentPlatformArtifact: %v", err)
	}
	if artifact.LibC != LinuxLibCGLibC {
		t.Fatalf("artifact libc = %q", artifact.LibC)
	}
}

func TestCurrentPlatformArtifact_PrefersMuslWhenLinuxLibCUnknownAndMultipleSpecificArtifactsExist(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:    "github.com/acme/providers/datastore",
		Version:   "0.0.1-alpha.1",
		Datastore: &pluginmanifestv1.DatastoreMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCGLibC,
				Path:   "gestalt-plugin-datastore-glibc",
				SHA256: "deadbeef",
			},
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCMusl,
				Path:   "gestalt-plugin-datastore-musl",
				SHA256: "cafebabe",
			},
		},
	}

	if runtime.GOOS != "linux" {
		if _, err := CurrentPlatformArtifact(manifest, ""); err == nil {
			t.Fatal("expected error on non-linux platform")
		}
		return
	}
	artifact, err := CurrentPlatformArtifact(manifest, "")
	if err != nil {
		t.Fatalf("CurrentPlatformArtifact: %v", err)
	}
	if artifact.LibC != LinuxLibCMusl {
		t.Fatalf("artifact libc = %q, want %q", artifact.LibC, LinuxLibCMusl)
	}
}

type testManifestCompat struct {
	Auth                bool
	Datastore           bool
	Secrets             bool
	DatastoreEntrypoint string
	SecretsEntrypoint   string
}

func decodeManifestCompat(data []byte, format string) (*testManifestCompat, error) {
	manifest, err := DecodeManifestFormat(data, format)
	if err != nil {
		return nil, err
	}
	return projectManifestCompat(manifest), nil
}

func decodeSourceManifestCompat(data []byte, format string) (*testManifestCompat, error) {
	manifest, err := DecodeSourceManifestFormat(data, format)
	if err != nil {
		return nil, err
	}
	return projectManifestCompat(manifest), nil
}

func projectManifestCompat(manifest *pluginmanifestv1.Manifest) *testManifestCompat {
	if manifest == nil {
		return nil
	}
	return &testManifestCompat{
		Auth:                manifest.Auth != nil,
		Datastore:           manifest.Datastore != nil,
		Secrets:             manifest.Secrets != nil,
		DatastoreEntrypoint: entrypointPath(manifest.Entrypoints.Datastore),
		SecretsEntrypoint:   entrypointPath(manifest.Entrypoints.Secrets),
	}
}

func entrypointPath(entry *pluginmanifestv1.Entrypoint) string {
	if entry == nil {
		return ""
	}
	return entry.ArtifactPath
}
