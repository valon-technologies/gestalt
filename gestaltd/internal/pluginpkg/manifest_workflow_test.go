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
		wire := mustProviderManifest("github.com/acme/plugins/provider-json", "1.2.3", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider-json"))
		wire.IconFile = "assets/icon.svg"
		wire.Provider.ConfigSchemaPath = "schemas/config.schema.json"

		mustWriteManifestData(t, sourceDir, ManifestFile, mustManifestJSON(t, wire))
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
		if dirManifest.Provider == nil || dirManifest.Provider.ConfigSchemaPath != "schemas/config.schema.json" {
			t.Fatalf("unexpected provider config schema: %#v", dirManifest.Provider)
		}
		if dirManifest.Entrypoints.Provider == nil || dirManifest.Entrypoints.Provider.ArtifactPath != artifactPath {
			t.Fatalf("unexpected provider entrypoint: %#v", dirManifest.Entrypoints.Provider)
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

	t.Run("yaml spec provider", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		sourceDir := filepath.Join(root, "provider-yaml")
		wire := &manifestWire{
			Source:      "github.com/acme/plugins/provider-yaml",
			Version:     "2.0.0",
			DisplayName: "Support",
			Description: "Spec-backed provider",
			Provider: &providerManifestWire{
				Headers: map[string]string{"X-App-Version": "2026-04-01"},
				ManagedParameters: []pluginmanifestv1.ManagedParameter{
					{In: "header", Name: "x-api-version", Value: "2026-04-01"},
				},
				ResponseMapping: &pluginmanifestv1.ManifestResponseMapping{
					DataPath: "items",
					Pagination: &pluginmanifestv1.ManifestPaginationMapping{
						HasMorePath: "has_more",
						CursorPath:  "next_cursor",
					},
				},
				AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
					"tickets.list": {Alias: "list_tickets"},
				},
				Connections: map[string]*providerManifestConnectionWire{
					"default": {
						Mode: "user",
						Auth: &pluginmanifestv1.ProviderAuth{
							Type:             pluginmanifestv1.AuthTypeOAuth2,
							AuthorizationURL: "https://auth.example.com/authorize",
							TokenURL:         "https://auth.example.com/token",
						},
					},
					"mcp": {
						Mode: "user",
						Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeMCPOAuth},
					},
				},
				Surfaces: providerManifestSurfacesWire{
					OpenAPI: &providerManifestOpenAPISurfaceWire{Document: "openapi.yaml"},
					MCP:     &providerManifestMCPSurfaceWire{URL: "https://mcp.example.com/mcp", Connection: "mcp"},
				},
				MCP: &providerManifestMCPWire{Enabled: true},
			},
		}

		mustWriteManifestData(t, sourceDir, "plugin.yaml", mustManifestYAML(t, wire))
		mustWriteFile(t, filepath.Join(sourceDir, "openapi.yaml"), []byte("openapi: 3.1.0\ninfo:\n  title: Example\n  version: 1.0.0\npaths: {}\n"), 0o644)

		_, dirManifest, gotPath, err := LoadManifestFromPath(sourceDir)
		if err != nil {
			t.Fatalf("LoadManifestFromPath(dir): %v", err)
		}
		if filepath.Base(gotPath) != "plugin.yaml" {
			t.Fatalf("manifest path = %q, want plugin.yaml", gotPath)
		}
		if dirManifest.Provider == nil {
			t.Fatal("expected provider manifest")
		}
		if !dirManifest.Provider.IsSpecLoaded() {
			t.Fatal("expected spec-backed provider")
		}
		if !dirManifest.Provider.MCP {
			t.Fatal("expected mcp to be enabled")
		}
		if dirManifest.Provider.ConnectionMode != "user" {
			t.Fatalf("ConnectionMode = %q", dirManifest.Provider.ConnectionMode)
		}
		if dirManifest.Provider.Auth == nil || dirManifest.Provider.Auth.Type != pluginmanifestv1.AuthTypeOAuth2 {
			t.Fatalf("unexpected default auth: %#v", dirManifest.Provider.Auth)
		}
		if dirManifest.Provider.MCPURL != "https://mcp.example.com/mcp" || dirManifest.Provider.MCPConnection != "mcp" {
			t.Fatalf("unexpected mcp surface: %#v", dirManifest.Provider)
		}
		if got := dirManifest.Provider.AllowedOperations["tickets.list"]; got == nil || got.Alias != "list_tickets" {
			t.Fatalf("unexpected allowed operations: %#v", dirManifest.Provider.AllowedOperations)
		}
		if dirManifest.Provider.ResponseMapping == nil || dirManifest.Provider.ResponseMapping.DataPath != "items" {
			t.Fatalf("unexpected response mapping: %#v", dirManifest.Provider.ResponseMapping)
		}

		archivePath := filepath.Join(root, "provider-yaml.tar.gz")
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
	})
}

func TestManifestWorkflow_RoundTripsWebUIPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "webui")
	wire := &manifestWire{
		Source:  "github.com/acme/plugins/webui",
		Version: "1.0.0",
		WebUI:   &pluginmanifestv1.WebUIMetadata{AssetRoot: "ui/dist"},
	}

	mustWriteManifestData(t, sourceDir, "plugin.yml", mustManifestYAML(t, wire))
	mustWriteFile(t, filepath.Join(sourceDir, "ui", "dist", "index.html"), []byte("<!doctype html><title>ui</title>"), 0o644)

	_, manifest, gotPath, err := LoadManifestFromPath(sourceDir)
	if err != nil {
		t.Fatalf("LoadManifestFromPath(dir): %v", err)
	}
	if filepath.Base(gotPath) != "plugin.yml" {
		t.Fatalf("manifest path = %q, want plugin.yml", gotPath)
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
	wire := mustProviderManifest("github.com/acme/plugins/source-only", "0.1.0", testArtifactOS, testArtifactArch, artifactPath, "")
	manifestPath := mustWriteManifestData(t, sourceDir, ManifestFile, mustManifestJSON(t, wire))
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
			files:    []string{"plugin.json", "plugin.yaml"},
			wantBase: "plugin.json",
			wantSrc:  "github.com/acme/plugins/json-first",
		},
		{
			name:     "yaml before yml",
			files:    []string{"plugin.yaml", "plugin.yml"},
			wantBase: "plugin.yaml",
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
				case "plugin.json":
					source = "github.com/acme/plugins/json-first"
				case "plugin.yaml":
					source = "github.com/acme/plugins/yaml-first"
				}
				wire := &manifestWire{
					Source:  source,
					Version: "1.0.0",
					WebUI:   &pluginmanifestv1.WebUIMetadata{AssetRoot: "ui"},
				}
				data := mustManifestYAML(t, wire)
				if filepath.Ext(name) == ".json" {
					data = mustManifestJSON(t, wire)
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
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, &manifestWire{
					Source:  "github.com/acme/plugins/missing-kind",
					Version: "1.0.0",
				}))
			},
			wantError: "manifest validation failed",
		},
		{
			name: "entrypoint references unknown artifact",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				wire := mustProviderManifest("github.com/acme/plugins/bad-entrypoint", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				wire.Provider.Exec.ArtifactPath = unknownSiblingArtifactPath(artifactPath)
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, wire))
			},
			wantError: "references unknown artifact",
		},
		{
			name: "icon file escapes package root",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				wire := mustProviderManifest("github.com/acme/plugins/bad-icon", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				wire.IconFile = "../icon.svg"
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, wire))
			},
			wantError: "icon_file must stay within the package",
		},
		{
			name: "rejects unsupported auth type",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				wire := mustProviderManifest("github.com/acme/plugins/bad-auth", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				wire.Provider.Connections = map[string]*providerManifestConnectionWire{
					"default": {
						Auth: &pluginmanifestv1.ProviderAuth{Type: "bogus"},
					},
				}
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, wire))
			},
			wantError: "value must be one of 'oauth2', 'mcp_oauth', 'bearer', 'manual', 'none'",
		},
		{
			name: "rejects oauth2 auth without token url",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				wire := mustProviderManifest("github.com/acme/plugins/missing-token-url", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				wire.Provider.Connections = map[string]*providerManifestConnectionWire{
					"default": {
						Auth: &pluginmanifestv1.ProviderAuth{
							Type:             pluginmanifestv1.AuthTypeOAuth2,
							AuthorizationURL: "https://auth.example.com/authorize",
						},
					},
				}
				mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(artifactPath)), []byte("provider"), 0o755)
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, wire))
			},
			wantError: "missing property 'token_url'",
		},
		{
			name: "rejects duplicate declarative operation names",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, &manifestWire{
					Source:  "github.com/acme/plugins/duplicate-ops",
					Version: "1.0.0",
					Provider: &providerManifestWire{
						Surfaces: providerManifestSurfacesWire{
							REST: &providerManifestRESTSurfaceWire{
								BaseURL: "https://api.example.com",
								Operations: []pluginmanifestv1.ProviderOperation{
									{Name: "list_items", Method: "GET", Path: "/items"},
									{Name: "list_items", Method: "POST", Path: "/items"},
								},
							},
						},
					},
				}))
			},
			wantError: `duplicate operation name "list_items"`,
		},
		{
			name: "rejects orphaned path parameters",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, ManifestFile, mustManifestJSON(t, &manifestWire{
					Source:  "github.com/acme/plugins/orphaned-path-param",
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
				}))
			},
			wantError: `declared as path param but "/items" has no {id} placeholder`,
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
