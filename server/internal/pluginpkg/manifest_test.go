package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestDecodeManifest_ValidProviderManifest(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
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
	if manifest.Source != "github.com/acme/plugins/provider" {
		t.Fatalf("unexpected manifest source %q", manifest.Source)
	}
}

func TestDecodeManifest_RejectsMissingEntrypointArtifact(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {},
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
		Source:   "github.com/acme/plugins/provider",
		Version:  "0.1.0",
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
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
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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
}

func TestEncodeManifest_V2RoundTrip(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:   "github.com/acme/plugins/echo",
		Version:  "1.0.0",
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
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

func TestDecodeManifest_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/echo",
  "id": "acme/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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
		t.Fatal("expected error for manifest with unknown field")
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
  "source": "` + tc.source + `",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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
  "source": "github.com/acme/plugins/echo",
  "version": "v1.0.0",
  "kinds": ["provider"],
  "provider": {},
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

func TestDecodeManifest_V2RejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	data := []byte(`{
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
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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

func TestDecodeManifest_V2RejectsDuplicateArtifactPlatform(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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
  "source": "github.com/test-org/test-repo/test-plugin",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {},
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

func TestDecodeManifest_V2WithOAuth2Auth(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
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
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
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
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
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
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "auth": {
      "type": "bogus"
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
		Source:  "github.com/acme/plugins/echo",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
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

func TestDecodeManifestFormat_YAML(t *testing.T) {
	t.Parallel()

	yamlData := []byte(`
source: github.com/acme/plugins/echo
version: "1.0.0"
kinds:
  - provider
provider: {}
artifacts:
  - os: darwin
    arch: arm64
    path: artifacts/darwin/arm64/provider
    sha256: ` + sha256Hex("provider") + `
entrypoints:
  provider:
    artifact_path: artifacts/darwin/arm64/provider
`)

	manifest, err := DecodeManifestFormat(yamlData, "yaml")
	if err != nil {
		t.Fatalf("DecodeManifestFormat: %v", err)
	}
	if manifest.Source != "github.com/acme/plugins/echo" {
		t.Fatalf("unexpected source %q", manifest.Source)
	}
}

func TestDecodeManifestFormat_YAMLDeclarative(t *testing.T) {
	t.Parallel()

	yamlData := []byte(`
source: github.com/acme/plugins/testapi
version: "0.1.0"
display_name: Test API
description: A test declarative provider
kinds:
  - provider
provider:
  base_url: https://api.example.com/v1
  auth:
    type: bearer
  operations:
    - name: list_items
      description: List all items
      method: GET
      path: /items
      parameters:
        - name: limit
          type: int
          in: query
    - name: create_item
      description: Create an item
      method: POST
      path: /items
      parameters:
        - name: name
          type: string
          in: body
          required: true
`)

	manifest, err := DecodeManifestFormat(yamlData, "yaml")
	if err != nil {
		t.Fatalf("DecodeManifestFormat: %v", err)
	}
	if manifest.Provider == nil {
		t.Fatal("expected provider")
	}
	if !manifest.Provider.IsDeclarative() {
		t.Fatal("expected declarative provider")
	}
	if manifest.Provider.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected base_url %q", manifest.Provider.BaseURL)
	}
	if len(manifest.Provider.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(manifest.Provider.Operations))
	}
	if manifest.Provider.Operations[0].Name != "list_items" {
		t.Fatalf("unexpected operation name %q", manifest.Provider.Operations[0].Name)
	}
	if manifest.Provider.Operations[1].Parameters[0].Required != true {
		t.Fatal("expected required param")
	}
}

func TestDecodeManifest_DeclarativeProviderJSON(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/myapi",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "base_url": "https://api.example.com",
    "auth": { "type": "bearer" },
    "operations": [
      {
        "name": "get_stuff",
        "method": "GET",
        "path": "/stuff",
        "parameters": [
          { "name": "q", "type": "string", "in": "query" }
        ]
      }
    ]
  }
}`)

	manifest, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if !manifest.Provider.IsDeclarative() {
		t.Fatal("expected declarative provider")
	}
	if len(manifest.Artifacts) != 0 {
		t.Fatal("declarative provider should have no artifacts")
	}
}

func TestDecodeManifest_DeclarativeRejectsMissingBaseURL(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/myapi",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "operations": [
      {
        "name": "get_stuff",
        "method": "GET",
        "path": "/stuff"
      }
    ]
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestDecodeManifest_DeclarativeRejectsDuplicateOpNames(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/myapi",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "base_url": "https://api.example.com",
    "operations": [
      { "name": "do_thing", "method": "GET", "path": "/a" },
      { "name": "do_thing", "method": "POST", "path": "/b" }
    ]
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for duplicate operation names")
	}
}

func TestDecodeManifest_DeclarativeRejectsOrphanPathParam(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/myapi",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "base_url": "https://api.example.com",
    "operations": [
      {
        "name": "get_item",
        "method": "GET",
        "path": "/items",
        "parameters": [
          { "name": "id", "type": "string", "in": "path" }
        ]
      }
    ]
  }
}`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for path param without matching placeholder")
	}
}

func TestDecodeManifest_IconFile(t *testing.T) {
	t.Parallel()

	t.Run("valid icon_file decodes and validates", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "icon_file": "assets/icon.svg",
  "kinds": ["provider"],
  "provider": {
    "base_url": "https://api.example.com",
    "operations": [
      {"name": "get_thing", "method": "GET", "path": "/thing"}
    ]
  }
}`)
		manifest, err := DecodeManifest(data)
		if err != nil {
			t.Fatalf("DecodeManifest: %v", err)
		}
		if manifest.IconFile != "assets/icon.svg" {
			t.Fatalf("IconFile = %q, want %q", manifest.IconFile, "assets/icon.svg")
		}
	})

	t.Run("omitted icon_file is valid", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
  "source": "github.com/acme/plugins/echo",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "base_url": "https://api.example.com",
    "operations": [
      {"name": "get_thing", "method": "GET", "path": "/thing"}
    ]
  }
}`)
		manifest, err := DecodeManifest(data)
		if err != nil {
			t.Fatalf("DecodeManifest: %v", err)
		}
		if manifest.IconFile != "" {
			t.Fatalf("IconFile = %q, want empty", manifest.IconFile)
		}
	})

	t.Run("traversal path rejected", func(t *testing.T) {
		t.Parallel()

		manifest := &pluginmanifestv1.Manifest{
			Source:  "github.com/acme/plugins/echo",
			Version: "1.0.0",
			Kinds:   []string{pluginmanifestv1.KindProvider},
			Provider: &pluginmanifestv1.Provider{
				BaseURL: "https://api.example.com",
				Operations: []pluginmanifestv1.ProviderOperation{
					{Name: "get_thing", Method: "GET", Path: "/thing"},
				},
			},
			IconFile: "../escape.svg",
		}
		if err := ValidateManifest(manifest); err == nil {
			t.Fatal("expected error for traversal icon_file path")
		}
	})
}

func TestFindManifestFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    []string
		wantBase string
	}{
		{"json only", []string{"plugin.json"}, "plugin.json"},
		{"yaml only", []string{"plugin.yaml"}, "plugin.yaml"},
		{"yml only", []string{"plugin.yml"}, "plugin.yml"},
		{"json takes priority", []string{"plugin.json", "plugin.yaml"}, "plugin.json"},
		{"yaml before yml", []string{"plugin.yaml", "plugin.yml"}, "plugin.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := FindManifestFile(dir)
			if err != nil {
				t.Fatalf("FindManifestFile: %v", err)
			}
			if filepath.Base(got) != tc.wantBase {
				t.Errorf("FindManifestFile = %s, want %s", filepath.Base(got), tc.wantBase)
			}
		})
	}
}

func TestFindManifestFile_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := FindManifestFile(dir)
	if err == nil {
		t.Fatal("expected error when no manifest found")
	}
}
