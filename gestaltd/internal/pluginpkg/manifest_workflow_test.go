package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestManifestWorkflow_RoundTripsProviderPackagesAcrossDirectoryAndArchive(t *testing.T) {
	t.Parallel()

	t.Run("json executable provider", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		sourceDir := filepath.Join(root, "provider-json")
		artifactPath := testArtifactPath("provider")
		manifest := mustProviderManifest("github.com/acme/plugins/provider-json", "1.2.3", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider-json"))
		manifest.IconFile = "assets/icon.svg"
		manifest.Plugin.ConfigSchemaPath = "schemas/config.schema.json"

		mustWriteManifestData(t, sourceDir, ManifestFile, mustManifestJSON(t, manifest))
		mustWriteFile(t, filepath.Join(sourceDir, filepath.FromSlash(artifactPath)), []byte("provider-json"), 0o755)
		mustWriteFile(t, filepath.Join(sourceDir, "assets", "icon.svg"), []byte("<svg/>"), 0o644)
		mustWriteFile(t, filepath.Join(sourceDir, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0o644)

		dirData, dirManifest, gotPath, err := LoadManifestFromPath(sourceDir)
		if err != nil {
			t.Fatalf("LoadManifestFromPath(dir): %v", err)
		}
		if filepath.Base(gotPath) != ManifestFile {
			t.Fatalf("manifest path = %q, want %q", gotPath, ManifestFile)
		}
		if len(dirData) == 0 {
			t.Fatal("expected manifest bytes from directory")
		}
		if dirManifest.IconFile != "assets/icon.svg" {
			t.Fatalf("IconFile = %q", dirManifest.IconFile)
		}
		if dirManifest.Plugin == nil || dirManifest.Plugin.ConfigSchemaPath != "schemas/config.schema.json" {
			t.Fatalf("unexpected provider config schema: %#v", dirManifest.Plugin)
		}
		if dirManifest.Entrypoints.Plugin == nil || dirManifest.Entrypoints.Plugin.ArtifactPath != artifactPath {
			t.Fatalf("unexpected provider entrypoint: %#v", dirManifest.Entrypoints.Plugin)
		}

		archivePath := filepath.Join(root, "provider-json.tar.gz")
		if err := CreatePackageFromDir(sourceDir, archivePath); err != nil {
			t.Fatalf("CreatePackageFromDir: %v", err)
		}

		archiveData, archiveManifest, archiveSourcePath, err := LoadManifestFromPath(archivePath)
		if err != nil {
			t.Fatalf("LoadManifestFromPath(archive): %v", err)
		}
		if archiveSourcePath != archivePath {
			t.Fatalf("archive source path = %q, want %q", archiveSourcePath, archivePath)
		}
		if len(archiveData) == 0 {
			t.Fatal("expected manifest bytes from archive")
		}
		if !ManifestEqual(dirManifest, archiveManifest) {
			t.Fatalf("directory and archive manifests differ:\ndir=%#v\narchive=%#v", dirManifest, archiveManifest)
		}
	})

}

func TestManifestWorkflow_RoundTripsWebUIPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "webui")
	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/webui",
		Version: "1.0.0",
		WebUI:   &pluginmanifestv1.WebUIMetadata{AssetRoot: "ui/dist"},
	}

	mustWriteManifestData(t, sourceDir, "manifest.yml", mustManifestYAML(t, manifest))
	mustWriteFile(t, filepath.Join(sourceDir, "ui", "dist", "index.html"), []byte("<!doctype html><title>ui</title>"), 0o644)

	_, manifest, gotPath, err := LoadManifestFromPath(sourceDir)
	if err != nil {
		t.Fatalf("LoadManifestFromPath(dir): %v", err)
	}
	if filepath.Base(gotPath) != "manifest.yml" {
		t.Fatalf("manifest path = %q, want manifest.yml", gotPath)
	}
	if manifest.WebUI == nil || manifest.WebUI.AssetRoot != "ui/dist" {
		t.Fatalf("unexpected webui manifest: %#v", manifest.WebUI)
	}

	archivePath := filepath.Join(root, "webui.tar.gz")
	if err := CreatePackageFromDir(sourceDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	extractDir := filepath.Join(root, "extracted")
	if err := ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDir, "ui", "dist", "index.html")); err != nil {
		t.Fatalf("expected extracted UI asset: %v", err)
	}
}

func TestManifestWorkflow_AllowsSourceArtifactsWithoutDigestsUntilPackaging(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source-plugin")
	artifactPath := testArtifactPath("provider")
	manifest := mustProviderManifest("github.com/acme/plugins/source-only", "0.0.1-alpha.1", testArtifactOS, testArtifactArch, artifactPath, "")
	manifestPath := mustWriteManifestData(t, sourceDir, ManifestFile, mustManifestJSON(t, manifest))
	mustWriteFile(t, filepath.Join(sourceDir, filepath.FromSlash(artifactPath)), []byte("source-only"), 0o755)

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Artifacts[0].SHA256 != "" {
		t.Fatalf("expected empty source artifact digest, got %q", manifest.Artifacts[0].SHA256)
	}

	if _, _, err := ReadManifestFile(manifestPath); err == nil || !strings.Contains(err.Error(), "sha256 is required") {
		t.Fatalf("ReadManifestFile error = %v, want missing sha256", err)
	}
	if err := CreatePackageFromDir(sourceDir, filepath.Join(root, "source-plugin.tar.gz")); err == nil || !strings.Contains(err.Error(), "sha256 is required") {
		t.Fatalf("CreatePackageFromDir error = %v, want missing sha256", err)
	}
}

func TestLoadManifestFromPath_PrefersManifestFileOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    []string
		wantBase string
		wantSrc  string
	}{
		{
			name:     "json before yaml",
			files:    []string{"manifest.json", "manifest.yaml"},
			wantBase: "manifest.json",
			wantSrc:  "github.com/acme/plugins/json-first",
		},
		{
			name:     "yaml before yml",
			files:    []string{"manifest.yaml", "manifest.yml"},
			wantBase: "manifest.yaml",
			wantSrc:  "github.com/acme/plugins/yaml-first",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, name := range tc.files {
				source := "github.com/acme/plugins/fallback"
				switch name {
				case "manifest.json":
					source = "github.com/acme/plugins/json-first"
				case "manifest.yaml":
					source = "github.com/acme/plugins/yaml-first"
				}
				manifest := &pluginmanifestv1.Manifest{
					Source:  source,
					Version: "1.0.0",
					WebUI:   &pluginmanifestv1.WebUIMetadata{AssetRoot: "ui"},
				}
				data := mustManifestYAML(t, manifest)
				if filepath.Ext(name) == ".json" {
					data = mustManifestJSON(t, manifest)
				}
				mustWriteManifestData(t, dir, name, data)
			}

			_, manifest, gotPath, err := LoadManifestFromPath(dir)
			if err != nil {
				t.Fatalf("LoadManifestFromPath: %v", err)
			}
			if filepath.Base(gotPath) != tc.wantBase {
				t.Fatalf("manifest path = %q, want %q", gotPath, tc.wantBase)
			}
			if manifest.Source != tc.wantSrc {
				t.Fatalf("manifest source = %q, want %q", manifest.Source, tc.wantSrc)
			}
		})
	}
}

func TestManifestWorkflow_RejectsInvalidPackageInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		buildData func(t *testing.T, dir string) string
		wantError string
	}{
		{
			name: "missing provider and webui",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, &pluginmanifestv1.Manifest{
					Source:  "github.com/acme/plugins/missing-kind",
					Version: "1.0.0",
				}))
			},
			wantError: "manifest must define exactly one of plugin, auth, datastore, secrets, or webui",
		},
		{
			name: "multiple metadata blocks",
			buildData: func(t *testing.T, dir string) string {
				manifest := mustProviderManifest("github.com/acme/plugins/too-many-kinds", "1.0.0", testArtifactOS, testArtifactArch, testArtifactPath("provider"), sha256Hex("provider"))
				manifest.Auth = &pluginmanifestv1.AuthMetadata{}
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, manifest))
			},
			wantError: "manifest must define exactly one of plugin, auth, datastore, secrets, or webui",
		},
		{
			name: "entrypoint references unknown artifact",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				manifest := mustProviderManifest("github.com/acme/plugins/bad-entrypoint", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				manifest.Entrypoints.Plugin.ArtifactPath = unknownSiblingArtifactPath(artifactPath)
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, manifest))
			},
			wantError: "references unknown artifact",
		},
		{
			name: "icon file escapes package root",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				manifest := mustProviderManifest("github.com/acme/plugins/bad-icon", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				manifest.IconFile = "../icon.svg"
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, manifest))
			},
			wantError: "iconFile must stay within the package",
		},
		{
			name: "rejects unsupported auth type",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				manifest := mustProviderManifest("github.com/acme/plugins/bad-auth", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				manifest.Plugin.Connections = map[string]*pluginmanifestv1.ManifestConnectionDef{
					"default": {
						Auth: &pluginmanifestv1.ProviderAuth{Type: "bogus"},
					},
				}
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, manifest))
			},
			wantError: "unsupported provider.connections.default.auth.type",
		},
		{
			name: "rejects oauth2 auth without token url",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				manifest := mustProviderManifest("github.com/acme/plugins/missing-token-url", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				manifest.Plugin.Auth = &pluginmanifestv1.ProviderAuth{
					Type:             pluginmanifestv1.AuthTypeOAuth2,
					AuthorizationURL: "https://auth.example.com/authorize",
				}
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, manifest))
			},
			wantError: "provider.auth.tokenUrl is required for oauth2",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			manifestPath := tc.buildData(t, dir)

			_, _, err := ReadManifestFile(manifestPath)
			if err == nil {
				t.Fatal("expected invalid manifest")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestManifestWorkflow_AcceptsProviderWireSurfaceManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
source: github.com/acme/plugins/provider-wire
version: 1.0.0
displayName: Provider Wire
plugin:
  configSchemaPath: schemas/config.schema.json
  connections:
    default:
      auth:
        type: none
    api:
      auth:
        type: oauth2
        authorizationUrl: https://auth.example.com/authorize
        tokenUrl: https://auth.example.com/token
  managedParameters:
    - in: path
      name: workspaceId
      value: ws_123
  pagination:
    style: cursor
    cursorParam: cursor
    cursor:
      source: header
      path: X-After-Cursor
    resultsPath: results
    maxPages: 10
  allowedOperations:
    items.list:
      paginate: true
    items.info: {}
  openapi: openapi.yaml
  openapiConnection: api
`))
	mustWriteFile(t, filepath.Join(dir, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0o644)
	mustWriteFile(t, filepath.Join(dir, "openapi.yaml"), []byte("openapi: 3.1.0\ninfo:\n  title: Example\n  version: 1.0.0\npaths: {}\n"), 0o644)

	_, manifest, err := ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	if manifest.Plugin == nil {
		t.Fatal("expected provider metadata")
	}
	if manifest.Plugin.OpenAPI != "openapi.yaml" {
		t.Fatalf("provider openapi = %q", manifest.Plugin.OpenAPI)
	}
	if manifest.Plugin.OpenAPIConnection != "api" {
		t.Fatalf("provider openapi_connection = %q, want api", manifest.Plugin.OpenAPIConnection)
	}
	if len(manifest.Plugin.ManagedParameters) != 1 {
		t.Fatalf("managed_parameters = %+v", manifest.Plugin.ManagedParameters)
	}
	if manifest.Entrypoints.Plugin != nil {
		t.Fatalf("expected declarative/spec provider to omit provider entrypoint, got %+v", manifest.Entrypoints.Plugin)
	}
	if pgn := manifest.Plugin.Pagination; pgn == nil || pgn.Style != "cursor" || pgn.Cursor == nil || pgn.Cursor.Source != "header" || pgn.Cursor.Path != "X-After-Cursor" || pgn.MaxPages != 10 {
		t.Fatalf("unexpected pagination config: %+v", manifest.Plugin.Pagination)
	}
	if op := manifest.Plugin.AllowedOperations["items.list"]; op == nil || !op.Paginate {
		t.Fatalf("items.list should have paginate=true, got %+v", manifest.Plugin.AllowedOperations["items.list"])
	}
}

func TestManifestWorkflow_AcceptsProviderWireMCPOAuthManifestAcrossDirectoryAndArchive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "plugin-mcp-oauth")
	manifestPath := mustWriteManifestData(t, sourceDir, "manifest.yaml", []byte(`
source: github.com/acme/plugins/notion
version: 0.0.1-alpha.1
displayName: Notion
plugin:
  connections:
    mcp:
      mode: user
      auth:
        type: mcp_oauth
  mcpUrl: https://mcp.notion.com/mcp
  mcpConnection: mcp
`))

	_, dirManifest, err := ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(dir): %v", err)
	}
	if dirManifest.Plugin == nil {
		t.Fatal("expected plugin metadata")
	}
	if dirManifest.Plugin.MCPURL != "https://mcp.notion.com/mcp" {
		t.Fatalf("plugin mcp_url = %q", dirManifest.Plugin.MCPURL)
	}
	if dirManifest.Plugin.MCPConnection != "mcp" {
		t.Fatalf("plugin mcp_connection = %q, want mcp", dirManifest.Plugin.MCPConnection)
	}
	if conn := dirManifest.Plugin.Connections["mcp"]; conn == nil || conn.Auth == nil || conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth {
		t.Fatalf("plugin connection auth = %#v", dirManifest.Plugin.Connections["mcp"])
	}

	archivePath := filepath.Join(root, "plugin-mcp-oauth.tar.gz")
	if err := CreatePackageFromDir(sourceDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	_, archiveManifest, _, err := LoadManifestFromPath(archivePath)
	if err != nil {
		t.Fatalf("LoadManifestFromPath(archive): %v", err)
	}
	if !ManifestEqual(dirManifest, archiveManifest) {
		t.Fatalf("directory and archive manifests differ:\ndir=%#v\narchive=%#v", dirManifest, archiveManifest)
	}
}

func TestManifestWorkflow_RejectsMCPOAuthManifestWithoutMCPSurface(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
source: github.com/acme/plugins/bad-mcp-oauth
version: 0.0.1-alpha.1
plugin:
  connections:
    mcp:
      auth:
        type: mcp_oauth
`))

	_, _, err := ReadManifestFile(manifestPath)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
	if !strings.Contains(err.Error(), `provider.connections.mcp.auth.type "mcp_oauth" requires an MCP surface`) {
		t.Fatalf("error = %v", err)
	}
}

func TestManifestWorkflow_NamedConnectionParamsAndDiscovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "plugin.yaml", []byte(`
source: github.com/acme/plugins/multi-conn
version: 1.0.0
displayName: Multi Connection
plugin:
  connections:
    default:
      auth:
        type: none
    api:
      mode: user
      auth:
        type: oauth2
        authorizationUrl: https://auth.example.com/authorize
        tokenUrl: https://auth.example.com/token
      params:
        workspace_id:
          required: true
          description: The workspace ID
        region:
          from: discovery
      discovery:
        url: https://api.example.com/workspaces
        idPath: id
        namePath: name
        metadata:
          region: region
  openapi: openapi.yaml
  openapiConnection: api
`))
	mustWriteFile(t, filepath.Join(dir, "openapi.yaml"), []byte("openapi: 3.1.0\ninfo:\n  title: Example\n  version: 1.0.0\npaths: {}\n"), 0o644)

	_, manifest, err := ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}

	encoded, err := EncodeManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	roundTripped, err := DecodeManifestFormat(encoded, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("DecodeManifestFormat: %v", err)
	}
	if !ManifestEqual(manifest, roundTripped) {
		t.Fatalf("round-trip mismatch:\noriginal=%#v\nround-tripped=%#v", manifest, roundTripped)
	}
}
