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

func TestDecodeManifest_V2WithOAuth2Auth(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "auth": {
      "type": "oauth2",
      "authorization_url": "https://example.com/authorize",
      "token_url": "https://example.com/token",
      "scopes": ["read", "write"],
      "pkce": true,
      "client_auth": "header"
    }
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
	if manifest.Provider.Auth == nil {
		t.Fatal("expected provider auth to be set")
	}
	if manifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
		t.Fatalf("unexpected auth type %q", manifest.Provider.Auth.Type)
	}
	if manifest.Provider.Auth.AuthorizationURL != "https://example.com/authorize" {
		t.Fatalf("unexpected authorization_url %q", manifest.Provider.Auth.AuthorizationURL)
	}
	if manifest.Provider.Auth.TokenURL != "https://example.com/token" {
		t.Fatalf("unexpected token_url %q", manifest.Provider.Auth.TokenURL)
	}
	if !manifest.Provider.Auth.PKCE {
		t.Fatal("expected pkce to be true")
	}
	if manifest.Provider.Auth.ClientAuth != "header" {
		t.Fatalf("unexpected client_auth %q", manifest.Provider.Auth.ClientAuth)
	}
	if len(manifest.Provider.Auth.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(manifest.Provider.Auth.Scopes))
	}
}

func TestDecodeManifest_V2OAuth2MissingTokenURL(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "auth": {
      "type": "oauth2",
      "authorization_url": "https://example.com/authorize"
    }
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
		t.Fatal("expected error for oauth2 auth without token_url")
	}
}

func TestDecodeManifest_V2ManualAuth(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "auth": {
      "type": "manual"
    }
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
	if manifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeManual {
		t.Fatalf("unexpected auth type %q", manifest.Provider.Auth.Type)
	}
}

func TestDecodeManifest_V2InvalidAuthType(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "auth": {
      "type": "bearer"
    }
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
		t.Fatal("expected error for invalid auth type")
	}
}

func TestDecodeManifest_V2NoAuthStillValid(t *testing.T) {
	t.Parallel()

	manifest, err := DecodeManifest(validV2JSON())
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if manifest.Provider.Auth != nil {
		t.Fatal("expected nil auth for manifest without auth block")
	}
}

func TestDecodeManifest_V2AuthRoundTrip(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion2,
		Source:        "github.com/acme/plugins/echo",
		Version:       "1.0.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol: pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
			Auth: &pluginmanifestv1.ProviderAuth{
				Type:             pluginmanifestv1.AuthTypeOAuth2,
				AuthorizationURL: "https://example.com/authorize",
				TokenURL:         "https://example.com/token",
				Scopes:           []string{"read"},
			},
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
	if got.Provider.Auth == nil {
		t.Fatal("expected auth after round trip")
	}
	if got.Provider.Auth.AuthorizationURL != "https://example.com/authorize" {
		t.Fatalf("auth lost authorization_url across round trip")
	}
}
