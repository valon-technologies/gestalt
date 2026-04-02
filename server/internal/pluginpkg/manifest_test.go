package pluginpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func mustProviderManifest(source, version, osName, arch, artifactPath, sha string) *manifestWire {
	return &manifestWire{
		Source:  source,
		Version: version,
		Provider: &providerManifestWire{
			Exec:     &providerExecWire{ArtifactPath: artifactPath},
			Surfaces: providerManifestSurfacesWire{},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     osName,
				Arch:   arch,
				Path:   artifactPath,
				SHA256: sha,
			},
		},
	}
}

func mustManifestJSON(t *testing.T, wire *manifestWire) []byte {
	t.Helper()
	return mustManifestJSONBytes(wire)
}

func mustManifestYAML(t *testing.T, wire *manifestWire) []byte {
	t.Helper()
	data, err := yaml.Marshal(wire)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	return data
}

func mustManifestJSONBytes(wire *manifestWire) []byte {
	data, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func TestDecodeManifest_ValidProviderManifest(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/provider", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.ConfigSchemaPath = "schemas/config.schema.json"
	wire.Provider.Exec.Args = []string{}
	data := mustManifestJSON(t, wire)

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

	wire := mustProviderManifest("github.com/acme/plugins/provider", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Exec.ArtifactPath = "artifacts/linux/amd64/provider"
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
}

func TestDecodeManifest_RejectsMissingArtifactSHA256(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/provider", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", "")
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
	if !strings.Contains(err.Error(), "sha256 is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeSourceManifest_AllowsMissingArtifactSHA256(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/provider", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", "")
	data := mustManifestJSON(t, wire)

	manifest, err := DecodeSourceManifestFormat(data, "json")
	if err != nil {
		t.Fatalf("DecodeSourceManifestFormat: %v", err)
	}
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Artifacts[0].SHA256 != "" {
		t.Fatalf("sha256 = %q, want empty", manifest.Artifacts[0].SHA256)
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

func TestEncodeManifest_SpecLoadedProviderDoesNotRequireEntrypoint(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/notion",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "openapi.yaml",
			MCPURL:  "https://mcp.example.com/mcp",
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
	if got.Provider == nil || got.Provider.OpenAPI != "openapi.yaml" {
		t.Fatalf("unexpected provider after round trip: %#v", got.Provider)
	}
}

func validV2JSON() []byte {
	return mustManifestJSONBytes(mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider")))
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
  "provider": {
    "exec": {
      "artifact_path": "artifacts/darwin/arm64/provider"
    }
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "path": "artifacts/darwin/arm64/provider",
      "sha256": "` + sha256Hex("provider") + `"
    }
  ]
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

			wire := mustProviderManifest(tc.source, "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
			data := mustManifestJSON(t, wire)

			_, err := DecodeManifest(data)
			if err == nil {
				t.Fatalf("expected error for source %q", tc.source)
			}
		})
	}
}

func TestDecodeManifest_V2RejectsLeadingVVersion(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/echo", "v1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for version with leading v")
	}
}

func TestDecodeManifest_RejectsProviderAndWebUICombination(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/test-org/test-repo/test-plugin", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.WebUI = &pluginmanifestv1.WebUIMetadata{AssetRoot: "out"}
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for provider+webui manifest")
	}
}

func TestDecodeManifest_V2RejectsMissingSource(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for v2 manifest without source")
	}
}

func TestDecodeManifest_V2RejectsDuplicateArtifactPlatform(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/test-org/test-repo/test-plugin", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Artifacts = append(wire.Artifacts, pluginmanifestv1.Artifact{
		OS:     "darwin",
		Arch:   "arm64",
		Path:   "artifacts/darwin/arm64/provider2",
		SHA256: sha256Hex("provider2"),
	})
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for duplicate artifact platform")
	}
}

func TestDecodeManifest_V2RejectsAbsoluteArtifactPath(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/test-org/test-repo/test-plugin", "1.0.0", "darwin", "arm64", "/absolute/path/provider", sha256Hex("provider"))
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for absolute artifact path")
	}
}

func TestDecodeManifest_RejectsMissingProviderAndWebUI(t *testing.T) {
	t.Parallel()

	data := mustManifestJSON(t, &manifestWire{
		Source:  "github.com/test-org/test-repo/test-plugin",
		Version: "1.0.0",
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				Path:   "artifacts/darwin/arm64/provider",
				SHA256: sha256Hex("provider"),
			},
		},
	})

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for manifest without provider or webui")
	}
}

func TestDecodeManifest_V2WithOAuth2Auth(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Connections = map[string]*providerManifestConnectionWire{
		"default": {
			Auth: &pluginmanifestv1.ProviderAuth{
				Type:             "oauth2",
				AuthorizationURL: "https://example.com/authorize",
				TokenURL:         "https://example.com/token",
				Scopes:           []string{"read", "write"},
				PKCE:             true,
				ClientAuth:       "header",
			},
		},
	}
	data := mustManifestJSON(t, wire)

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

	wire := mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Connections = map[string]*providerManifestConnectionWire{
		"default": {
			Auth: &pluginmanifestv1.ProviderAuth{
				Type:             "oauth2",
				AuthorizationURL: "https://example.com/authorize",
			},
		},
	}
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for oauth2 auth without token_url")
	}
}

func TestDecodeManifest_V2ManualAuth(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Connections = map[string]*providerManifestConnectionWire{
		"default": {
			Auth: &pluginmanifestv1.ProviderAuth{Type: "manual"},
		},
	}
	data := mustManifestJSON(t, wire)

	manifest, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if manifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeManual {
		t.Fatalf("unexpected auth type %q", manifest.Provider.Auth.Type)
	}
}

func TestDecodeManifest_V2WithMCPOAuth(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "source": "github.com/acme/plugins/linear",
  "version": "1.0.0",
  "kinds": ["provider"],
  "provider": {
    "graphql_url": "https://api.linear.app/graphql",
    "mcp_url": "https://mcp.linear.app/mcp",
    "auth": {
      "type": "mcp_oauth"
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
	if manifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth {
		t.Fatalf("unexpected auth type %q", manifest.Provider.Auth.Type)
	}
}

func TestDecodeManifest_V2InvalidAuthType(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Connections = map[string]*providerManifestConnectionWire{
		"default": {
			Auth: &pluginmanifestv1.ProviderAuth{Type: "bogus"},
		},
	}
	data := mustManifestJSON(t, wire)

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

	yamlData := mustManifestYAML(t, mustProviderManifest("github.com/acme/plugins/echo", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider")))

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

	yamlData := mustManifestYAML(t, &manifestWire{
		Source:      "github.com/acme/plugins/testapi",
		Version:     "0.1.0",
		DisplayName: "Test API",
		Description: "A test declarative provider",
		Provider: &providerManifestWire{
			Connections: map[string]*providerManifestConnectionWire{
				"default": {Auth: &pluginmanifestv1.ProviderAuth{Type: "bearer"}},
			},
			Surfaces: providerManifestSurfacesWire{
				REST: &providerManifestRESTSurfaceWire{
					BaseURL: "https://api.example.com/v1",
					Operations: []pluginmanifestv1.ProviderOperation{
						{
							Name:        "list_items",
							Description: "List all items",
							Method:      "GET",
							Path:        "/items",
							Parameters: []pluginmanifestv1.ProviderParameter{
								{Name: "limit", Type: "int", In: "query"},
							},
						},
						{
							Name:        "create_item",
							Description: "Create an item",
							Method:      "POST",
							Path:        "/items",
							Parameters: []pluginmanifestv1.ProviderParameter{
								{Name: "name", Type: "string", In: "body", Required: true},
							},
						},
					},
				},
			},
		},
	})

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

func TestDecodeManifestFormat_YAMLAllowedOperations(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/testapi", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Surfaces.OpenAPI = &providerManifestOpenAPISurfaceWire{Document: "https://api.example.com/openapi.json"}
	wire.Provider.AllowedOperations = map[string]*pluginmanifestv1.ManifestOperationOverride{
		"api.items.list": {Alias: "list_items"},
	}
	yamlData := mustManifestYAML(t, wire)

	manifest, err := DecodeManifestFormat(yamlData, "yaml")
	if err != nil {
		t.Fatalf("DecodeManifestFormat: %v", err)
	}
	if manifest.Provider == nil {
		t.Fatal("expected provider")
	}
	override := manifest.Provider.AllowedOperations["api.items.list"]
	if override == nil {
		t.Fatal("expected allowed_operations entry")
	}
	if override.Alias != "list_items" {
		t.Fatalf("unexpected alias %q", override.Alias)
	}
}

func TestDecodeManifest_DeclarativeProviderJSON(t *testing.T) {
	t.Parallel()

	data := mustManifestJSON(t, &manifestWire{
		Source:  "github.com/acme/plugins/myapi",
		Version: "1.0.0",
		Provider: &providerManifestWire{
			Connections: map[string]*providerManifestConnectionWire{
				"default": {Auth: &pluginmanifestv1.ProviderAuth{Type: "bearer"}},
			},
			Surfaces: providerManifestSurfacesWire{
				REST: &providerManifestRESTSurfaceWire{
					BaseURL: "https://api.example.com",
					Operations: []pluginmanifestv1.ProviderOperation{
						{
							Name:   "get_stuff",
							Method: "GET",
							Path:   "/stuff",
							Parameters: []pluginmanifestv1.ProviderParameter{
								{Name: "q", Type: "string", In: "query"},
							},
						},
					},
				},
			},
		},
	})

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

	data := mustManifestJSON(t, &manifestWire{
		Source:  "github.com/acme/plugins/myapi",
		Version: "1.0.0",
		Provider: &providerManifestWire{
			Surfaces: providerManifestSurfacesWire{
				REST: &providerManifestRESTSurfaceWire{
					Operations: []pluginmanifestv1.ProviderOperation{
						{Name: "get_stuff", Method: "GET", Path: "/stuff"},
					},
				},
			},
		},
	})

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestDecodeManifest_DeclarativeRejectsDuplicateOpNames(t *testing.T) {
	t.Parallel()

	data := mustManifestJSON(t, &manifestWire{
		Source:  "github.com/acme/plugins/myapi",
		Version: "1.0.0",
		Provider: &providerManifestWire{
			Surfaces: providerManifestSurfacesWire{
				REST: &providerManifestRESTSurfaceWire{
					BaseURL: "https://api.example.com",
					Operations: []pluginmanifestv1.ProviderOperation{
						{Name: "do_thing", Method: "GET", Path: "/a"},
						{Name: "do_thing", Method: "POST", Path: "/b"},
					},
				},
			},
		},
	})

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for duplicate operation names")
	}
}

func TestDecodeManifestFormat_YAMLManagedParameters(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/testapi", "0.1.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Surfaces.OpenAPI = &providerManifestOpenAPISurfaceWire{Document: "https://api.example.com/openapi.json"}
	wire.Provider.ManagedParameters = []pluginmanifestv1.ManagedParameter{
		{In: "header", Name: "intercom-version", Value: "2.11"},
	}
	yamlData := mustManifestYAML(t, wire)

	manifest, err := DecodeManifestFormat(yamlData, "yaml")
	if err != nil {
		t.Fatalf("DecodeManifestFormat: %v", err)
	}
	if manifest.Provider == nil {
		t.Fatal("expected provider")
	}
	if len(manifest.Provider.ManagedParameters) != 1 {
		t.Fatalf("expected 1 managed parameter, got %d", len(manifest.Provider.ManagedParameters))
	}
	if got := manifest.Provider.ManagedParameters[0].Name; got != "intercom-version" {
		t.Fatalf("unexpected managed parameter name %q", got)
	}
}

func TestDecodeManifest_RejectsManagedParameterHeaderConflict(t *testing.T) {
	t.Parallel()

	wire := mustProviderManifest("github.com/acme/plugins/myapi", "1.0.0", "darwin", "arm64", "artifacts/darwin/arm64/provider", sha256Hex("provider"))
	wire.Provider.Surfaces.OpenAPI = &providerManifestOpenAPISurfaceWire{Document: "https://api.example.com/openapi.json"}
	wire.Provider.Headers = map[string]string{"Intercom-Version": "2.15"}
	wire.Provider.ManagedParameters = []pluginmanifestv1.ManagedParameter{
		{In: "header", Name: "intercom-version", Value: "2.11"},
	}
	data := mustManifestJSON(t, wire)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
	if !strings.Contains(err.Error(), "conflicts with configured header") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeManifest_DeclarativeRejectsOrphanPathParam(t *testing.T) {
	t.Parallel()

	data := mustManifestJSON(t, &manifestWire{
		Source:  "github.com/acme/plugins/myapi",
		Version: "1.0.0",
		Provider: &providerManifestWire{
			Surfaces: providerManifestSurfacesWire{
				REST: &providerManifestRESTSurfaceWire{
					BaseURL: "https://api.example.com",
					Operations: []pluginmanifestv1.ProviderOperation{
						{
							Name:   "get_item",
							Method: "GET",
							Path:   "/items",
							Parameters: []pluginmanifestv1.ProviderParameter{
								{Name: "id", Type: "string", In: "path"},
							},
						},
					},
				},
			},
		},
	})

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("expected error for path param without matching placeholder")
	}
}

func TestDecodeManifest_IconFile(t *testing.T) {
	t.Parallel()

	t.Run("valid icon_file decodes and validates", func(t *testing.T) {
		t.Parallel()

		data := mustManifestJSON(t, &manifestWire{
			Source:   "github.com/acme/plugins/echo",
			Version:  "1.0.0",
			IconFile: "assets/icon.svg",
			Provider: &providerManifestWire{
				Surfaces: providerManifestSurfacesWire{
					REST: &providerManifestRESTSurfaceWire{
						BaseURL: "https://api.example.com",
						Operations: []pluginmanifestv1.ProviderOperation{
							{Name: "get_thing", Method: "GET", Path: "/thing"},
						},
					},
				},
			},
		})
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

		data := mustManifestJSON(t, &manifestWire{
			Source:  "github.com/acme/plugins/echo",
			Version: "1.0.0",
			Provider: &providerManifestWire{
				Surfaces: providerManifestSurfacesWire{
					REST: &providerManifestRESTSurfaceWire{
						BaseURL: "https://api.example.com",
						Operations: []pluginmanifestv1.ProviderOperation{
							{Name: "get_thing", Method: "GET", Path: "/thing"},
						},
					},
				},
			},
		})
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
