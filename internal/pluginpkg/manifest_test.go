package pluginpkg

import (
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func TestDecodeManifest_ValidProviderManifest(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 1,
  "id": "acme/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "config_schema_path": "schemas/config.schema.json"
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/darwin/arm64/provider",
      "args": []
    }
  }
}`)

	manifest, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if manifest.ID != "acme/provider" {
		t.Fatalf("unexpected manifest id %q", manifest.ID)
	}
}

func TestDecodeManifest_RejectsMissingEntrypointArtifact(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 1,
  "id": "acme/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 }
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/linux/amd64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
}

func TestEncodeManifest_RoundTrip(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            "acme/provider",
		Version:       "0.1.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				Path:   "artifacts/darwin/arm64/provider",
				SHA256: sha256Hex("provider"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: "artifacts/darwin/arm64/provider",
			},
		},
	}

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	got, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if !ManifestEqual(manifest, got) {
		t.Fatal("manifest changed across round trip")
	}
}
