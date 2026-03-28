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

func validV2JSON() []byte {
	return []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)
}

func TestDecodeManifest_V2ValidSource(t *testing.T) {
	t.Parallel()

	manifest, err := DecodeManifest(validV2JSON())
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if manifest.Source != "github.com/acme/plugins/echo" {
		t.Fatalf("unexpected source %q", manifest.Source)
	}
	if manifest.SchemaVersion != pluginmanifestv1.SchemaVersion2 {
		t.Fatalf("unexpected schema_version %d", manifest.SchemaVersion)
	}
}

func TestEncodeManifest_V2RoundTrip(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion2,
		Source:        "github.com/acme/plugins/echo",
		Version:       "1.0.0",
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

func TestDecodeManifest_V2RejectsID(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "id": "acme/echo",
  "version": "1.0.0",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for v2 manifest with id set")
	}
}

func TestDecodeManifest_V2RejectsInvalidSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{"uppercase", "github.com/Acme/plugins/echo"},
		{"missing segment", "github.com/acme/plugins"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := []byte(`{
  "schema_version": 2,
  "source": "` + tc.source + `",
  "version": "1.0.0",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

			_, err := DecodeManifest(data)
			if err == nil {
				t.Fatalf("expected error for source %q", tc.source)
			}
		})
	}
}

func TestDecodeManifest_V2RejectsLeadingVVersion(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "v1.0.0",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for version with leading v")
	}
}

func TestDecodeManifest_V1StillWorks(t *testing.T) {
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	manifest, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if manifest.ID != "acme/provider" {
		t.Fatalf("unexpected id %q", manifest.ID)
	}
	if manifest.SchemaVersion != pluginmanifestv1.SchemaVersion {
		t.Fatalf("unexpected schema_version %d", manifest.SchemaVersion)
	}
}

func TestDecodeManifest_V2RejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["unknown_kind"],
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ],
  "entrypoints": {}
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestDecodeManifest_V2RejectsMissingSource(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "version": "1.0.0",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for v2 manifest without source")
	}
}

func TestDecodeManifest_RejectsSchemaVersion3(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 3,
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["provider"]
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for unsupported schema_version 3")
	}
}

func TestDecodeManifest_V2RejectsDuplicateArtifactPlatform(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
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
    },
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider2",
      "sha256": "` + sha256Hex("provider2") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for duplicate artifact platform")
	}
}

func TestDecodeManifest_V2RejectsAbsoluteArtifactPath(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 }
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "/absolute/path/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "/absolute/path/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for absolute artifact path")
	}
}

func TestDecodeManifest_V2ProviderWithoutMetadataRejected(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["provider"],
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for provider kind without provider metadata")
	}
}

func TestValidateManifest_ProtocolMinGreaterThanMax(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            "acme/provider",
		Version:       "0.1.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 5, Max: 1},
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

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected error for protocol min > max")
	}
}

func TestDecodeManifest_V1RejectsSource(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 1,
  "id": "acme/provider",
  "source": "github.com/acme/plugins/echo",
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
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for v1 manifest with source set")
	}
}
