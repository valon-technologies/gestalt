package providerpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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
		manifest.Spec.ConfigSchemaPath = "schemas/config.schema.json"

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
		if dirManifest.Spec == nil || dirManifest.Spec.ConfigSchemaPath != "schemas/config.schema.json" {
			t.Fatalf("unexpected provider config schema: %#v", dirManifest.Spec)
		}
		if dirManifest.Entrypoint == nil || dirManifest.Entrypoint.ArtifactPath != artifactPath {
			t.Fatalf("unexpected provider entrypoint: %#v", dirManifest.Entrypoint)
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

func TestManifestWorkflow_RoundTripsUIPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "ui")
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindUI,
		Source:  "github.com/acme/plugins/ui",
		Version: "1.0.0",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "ui/dist",
			Routes: []providermanifestv1.UIRoute{
				{Path: "/admin/*", AllowedRoles: []string{"admin"}},
				{Path: "/*", AllowedRoles: []string{"viewer", "admin"}},
			},
		},
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
	if manifest.Spec == nil || manifest.Spec.AssetRoot != "ui/dist" {
		t.Fatalf("unexpected ui manifest: %#v", manifest.Spec)
	}
	if len(manifest.Spec.Routes) != 2 || manifest.Spec.Routes[0].Path != "/admin/*" || manifest.Spec.Routes[1].Path != "/*" {
		t.Fatalf("unexpected ui routes: %#v", manifest.Spec.Routes)
	}

	archivePath := filepath.Join(root, "ui.tar.gz")
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
				manifest := &providermanifestv1.Manifest{
					Kind:    providermanifestv1.KindUI,
					Source:  source,
					Version: "1.0.0",
					Spec:    &providermanifestv1.Spec{AssetRoot: "ui"},
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
		name       string
		buildData  func(t *testing.T, dir string) string
		readSource bool
		wantError  string
	}{
		{
			name: "missing provider and ui",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, ManifestFile, mustRawManifestJSON(t, &providermanifestv1.Manifest{
					Kind:    "",
					Source:  "github.com/acme/plugins/missing-kind",
					Version: "1.0.0",
				}))
			},
			wantError: "manifest kind is required",
		},
		{
			name: "rejects ui route without allowed roles",
			buildData: func(t *testing.T, dir string) string {
				mustWriteFile(t, filepath.Join(dir, "ui", "dist", "index.html"), []byte("<html/>"), 0o644)
				return mustWriteManifestData(t, dir, "manifest.yml", []byte(`
kind: ui
source: github.com/acme/plugins/ui-routes
version: 1.0.0
spec:
  assetRoot: ui/dist
  routes:
    - path: /admin
`))
			},
			wantError: "allowedRoles must not be empty",
		},
		{
			name: "rejects non-terminal wildcard ui route",
			buildData: func(t *testing.T, dir string) string {
				mustWriteFile(t, filepath.Join(dir, "ui", "dist", "index.html"), []byte("<html/>"), 0o644)
				return mustWriteManifestData(t, dir, "manifest.yml", []byte(`
kind: ui
source: github.com/acme/plugins/ui-routes
version: 1.0.0
spec:
  assetRoot: ui/dist
  routes:
    - path: /admin/*/settings
      allowedRoles: [admin]
`))
			},
			wantError: "wildcards are only supported as a terminal /*",
		},
		{
			name: "entrypoint references unknown artifact",
			buildData: func(t *testing.T, dir string) string {
				artifactPath := testArtifactPath("provider")
				manifest := mustProviderManifest("github.com/acme/plugins/bad-entrypoint", "1.0.0", testArtifactOS, testArtifactArch, artifactPath, sha256Hex("provider"))
				manifest.Entrypoint.ArtifactPath = unknownSiblingArtifactPath(artifactPath)
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
				manifest.Spec.Connections = map[string]*providermanifestv1.ManifestConnectionDef{
					"default": {
						Auth: &providermanifestv1.ProviderAuth{Type: "bogus"},
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
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/missing-token-url
version: 1.0.0
spec:
  connections:
    default:
      auth:
        type: oauth2
        authorizationUrl: https://auth.example.com/authorize
`))
			},
			readSource: true,
			wantError:  "provider.connections.default.auth.tokenUrl is required for oauth2",
		},
		{
			name: "rejects unknown nested connection auth field",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/unknown-auth-field
version: 1.0.0
spec:
  connections:
    default:
      auth:
        type: oauth2
        authorizationUrl: https://auth.example.com/authorize
        tokenUrl: https://auth.example.com/token
        extraField: nope
`))
			},
			readSource: true,
			wantError:  "field extraField not found",
		},
		{
			name: "rejects legacy auth fields alongside route auth",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/ambiguous-auth
version: 1.0.0
spec:
  auth:
    provider: server
    type: oauth2
    authorizationUrl: https://auth.example.com/authorize
    tokenUrl: https://auth.example.com/token
`))
			},
			readSource: true,
			wantError:  "field type not found",
		},
		{
			name: "rejects empty route auth object",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/empty-route-auth
version: 1.0.0
spec:
  auth: {}
`))
			},
			readSource: true,
			wantError:  "provider.auth.provider is required",
		},
		{
			name: "rejects unknown nested route auth field",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/legacy-auth-unknown-field
version: 1.0.0
spec:
  auth:
    provider: server
    typo: bad
`))
			},
			readSource: true,
			wantError:  "field typo not found",
		},
		{
			name: "rejects legacy top-level connection fields",
			buildData: func(t *testing.T, dir string) string {
				return mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/duplicate-default
version: 1.0.0
spec:
  connectionMode: user
`))
			},
			readSource: true,
			wantError:  "spec.connectionMode is not supported",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			manifestPath := tc.buildData(t, dir)

			var err error
			if tc.readSource {
				_, _, err = ReadSourceManifestFile(manifestPath)
			} else {
				_, _, err = ReadManifestFile(manifestPath)
			}
			if err == nil {
				t.Fatal("expected invalid manifest")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestManifestWorkflow_AcceptsPluginRouteAuthReference(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/route-auth
version: 1.0.0
spec:
  auth:
    provider: server
  connections:
    default:
      auth:
        type: none
`))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Spec == nil || manifest.Spec.RouteAuth == nil || manifest.Spec.RouteAuth.Provider != "server" {
		t.Fatalf("unexpected route auth: %#v", manifest.Spec)
	}
	if manifest.Spec.Connections["default"] == nil || manifest.Spec.Connections["default"].Auth == nil || manifest.Spec.Connections["default"].Auth.Type != providermanifestv1.AuthTypeNone {
		t.Fatalf("unexpected default connection: %#v", manifest.Spec.Connections["default"])
	}

	encoded, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	rendered := string(encoded)
	if !strings.Contains(rendered, "provider: server") {
		t.Fatalf("expected canonical route auth in output:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n  connectionMode:") || strings.Contains(rendered, "\n  connectionParams:") || strings.Contains(rendered, "\n  discovery:") {
		t.Fatalf("expected canonical output without legacy top-level connection fields:\n%s", rendered)
	}
}

func TestManifestWorkflow_AcceptsNullPluginRouteAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/null-route-auth
version: 1.0.0
spec:
  auth:
  connections:
    default:
      auth:
        type: none
`))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Spec == nil {
		t.Fatal("expected plugin metadata")
	}
	if manifest.Spec.RouteAuth != nil {
		t.Fatalf("RouteAuth = %#v, want nil", manifest.Spec.RouteAuth)
	}
	if manifest.Spec.Connections["default"] == nil || manifest.Spec.Connections["default"].Auth == nil || manifest.Spec.Connections["default"].Auth.Type != providermanifestv1.AuthTypeNone {
		t.Fatalf("unexpected default connection: %#v", manifest.Spec.Connections["default"])
	}
}

func TestManifestWorkflow_AcceptsHostedHTTPBindingsAndSecuritySchemes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/signed
version: 1.0.0
spec:
  securitySchemes:
    signed:
      type: hmac
      secret:
        env: REQUEST_SIGNING_SECRET
      signatureHeader: X-Request-Signature
      signaturePrefix: v0=
      payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
      timestampHeader: X-Request-Timestamp
      maxAgeSeconds: 300
  http:
    command:
      path: /command
      method: POST
      security: signed
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded: {}
      target: handle_command
      ack:
        body:
          status: accepted
`))

	_, manifest, err := ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}
	if manifest.Spec == nil {
		t.Fatal("expected plugin metadata")
	}
	if manifest.Spec.SecuritySchemes["signed"] == nil || manifest.Spec.SecuritySchemes["signed"].Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		t.Fatalf("unexpected security scheme: %#v", manifest.Spec.SecuritySchemes["signed"])
	}
	if manifest.Spec.HTTP["command"] == nil {
		t.Fatal("expected http binding")
	}
	if got, want := manifest.Spec.HTTP["command"].Path, "/command"; got != want {
		t.Fatalf("http path = %q, want %q", got, want)
	}
	if got, want := manifest.Spec.HTTP["command"].Target, "handle_command"; got != want {
		t.Fatalf("http target = %q, want %q", got, want)
	}

	encoded, err := EncodeSourceManifestFormat(manifest, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	rendered := string(encoded)
	if !strings.Contains(rendered, "securitySchemes:") || !strings.Contains(rendered, "http:") {
		t.Fatalf("expected hosted http metadata in canonical output:\n%s", rendered)
	}
}

func TestManifestWorkflow_RejectsNon2xxHTTPAckStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/bad-http-ack
version: 1.0.0
spec:
  securitySchemes:
    none:
      type: none
  http:
    command:
      path: /command
      method: POST
      security: none
      target: handle_command
      ack:
        status: 500
`))

	_, _, err := ReadSourceManifestFile(manifestPath)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
	if !strings.Contains(err.Error(), `provider.http.command.ack.status must be a 2xx status`) {
		t.Fatalf("error = %v", err)
	}
}

func TestManifestWorkflow_RejectsDuplicateNormalizedHTTPContentTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/bad-http-content
version: 1.0.0
spec:
  securitySchemes:
    none:
      type: none
  http:
    command:
      path: /command
      method: POST
      security: none
      target: handle_command
      requestBody:
        content:
          application/json: {}
          application/json; charset=utf-8: {}
`))

	_, _, err := ReadSourceManifestFile(manifestPath)
	if err == nil {
		t.Fatal("expected invalid manifest")
	}
	if !strings.Contains(err.Error(), `provider.http.command.requestBody.content "application/json" is duplicated after normalization`) {
		t.Fatalf("error = %v", err)
	}
}

func TestManifestWorkflow_RejectsInvalidHMACPayloadTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		payloadTemplate string
		want            string
	}{
		{
			name:            "missing timestamp placeholder",
			payloadTemplate: "{raw_body}",
			want:            `provider.securitySchemes.signed.payloadTemplate must include a header placeholder for "X-Request-Timestamp"`,
		},
		{
			name:            "unsupported placeholder",
			payloadTemplate: "v0:{query:id}:{raw_body}",
			want:            `provider.securitySchemes.signed.payloadTemplate placeholder "query:id" is not supported`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/bad-http-template
version: 1.0.0
spec:
  securitySchemes:
    signed:
      type: hmac
      secret:
        env: REQUEST_SIGNING_SECRET
      signatureHeader: X-Request-Signature
      signaturePrefix: v0=
      payloadTemplate: "`+tt.payloadTemplate+`"
      timestampHeader: X-Request-Timestamp
      maxAgeSeconds: 300
  http:
    command:
      path: /command
      method: POST
      security: signed
      target: handle_command
`))

			_, _, err := ReadSourceManifestFile(manifestPath)
			if err == nil {
				t.Fatal("expected invalid manifest")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestManifestWorkflow_EncodesCanonicalProgrammaticDefaultConnection(t *testing.T) {
	t.Parallel()

	programmatic := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/acme/plugins/programmatic-default-connection",
		Version: "1.0.0",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Mode: providermanifestv1.ConnectionModeUser,
					Auth: &providermanifestv1.ProviderAuth{
						Type:             providermanifestv1.AuthTypeOAuth2,
						AuthorizationURL: "https://auth.example.com/authorize",
						TokenURL:         "https://auth.example.com/token",
					},
					Params: map[string]providermanifestv1.ProviderConnectionParam{
						"workspace_id": {
							Required:    true,
							Description: "Workspace ID",
						},
					},
					Discovery: &providermanifestv1.ProviderDiscovery{
						URL:    "https://api.example.com/workspaces",
						IDPath: "id",
					},
				},
			},
		},
	}
	programmaticEncoded, err := EncodeSourceManifestFormat(programmatic, ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat(programmatic): %v", err)
	}
	programmaticRendered := string(programmaticEncoded)
	if !strings.Contains(programmaticRendered, "connections:") || !strings.Contains(programmaticRendered, "default:") {
		t.Fatalf("expected canonical default connection in programmatic output:\n%s", programmaticRendered)
	}
	if strings.Contains(programmaticRendered, "\n  connectionMode:") || strings.Contains(programmaticRendered, "\n  connectionParams:") || strings.Contains(programmaticRendered, "\n  discovery:") {
		t.Fatalf("expected programmatic canonical output without legacy top-level connection fields:\n%s", programmaticRendered)
	}
	def := programmatic.Spec.Connections["default"]
	if def == nil || def.Auth == nil || def.Auth.Type != providermanifestv1.AuthTypeOAuth2 {
		t.Fatalf("programmatic manifest mutated unexpectedly: %#v", programmatic.Spec.Connections)
	}
}

func TestManifestWorkflow_AcceptsProviderWireSurfaceManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := mustWriteManifestData(t, dir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/provider-wire
version: 1.0.0
displayName: Provider Wire
spec:
  configSchemaPath: schemas/config.schema.json
  connections:
    default:
      auth:
        type: none
    api:
      displayName: OAuth
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
  surfaces:
    openapi:
      document: openapi.yaml
      connection: api
`))
	mustWriteFile(t, filepath.Join(dir, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0o644)
	mustWriteFile(t, filepath.Join(dir, "openapi.yaml"), []byte("openapi: 3.1.0\ninfo:\n  title: Example\n  version: 1.0.0\npaths: {}\n"), 0o644)

	_, manifest, err := ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	if manifest.Spec == nil {
		t.Fatal("expected provider metadata")
	}
	if manifest.Spec.OpenAPIDocument() != "openapi.yaml" {
		t.Fatalf("provider openapi document = %q", manifest.Spec.OpenAPIDocument())
	}
	if manifest.Spec.SurfaceConnectionName("openapi") != "api" {
		t.Fatalf("provider openapi connection = %q, want api", manifest.Spec.SurfaceConnectionName("openapi"))
	}
	if manifest.Spec.Connections["api"] == nil || manifest.Spec.Connections["api"].DisplayName != "OAuth" {
		t.Fatalf("provider api connection displayName = %#v", manifest.Spec.Connections["api"])
	}
	if len(manifest.Spec.ManagedParameters) != 1 {
		t.Fatalf("managed_parameters = %+v", manifest.Spec.ManagedParameters)
	}
	if manifest.Entrypoint != nil {
		t.Fatalf("expected declarative/spec provider to omit provider entrypoint, got %+v", manifest.Entrypoint)
	}
	if pgn := manifest.Spec.Pagination; pgn == nil || pgn.Style != providermanifestv1.PaginationStyleCursor || pgn.Cursor == nil || pgn.Cursor.Source != "header" || pgn.Cursor.Path != "X-After-Cursor" || pgn.MaxPages != 10 {
		t.Fatalf("unexpected pagination config: %+v", manifest.Spec.Pagination)
	}
	if op := manifest.Spec.AllowedOperations["items.list"]; op == nil || !op.Paginate {
		t.Fatalf("items.list should have paginate=true, got %+v", manifest.Spec.AllowedOperations["items.list"])
	}
}

func TestManifestWorkflow_AcceptsProviderWireMCPOAuthManifestAcrossDirectoryAndArchive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "plugin-mcp-oauth")
	manifestPath := mustWriteManifestData(t, sourceDir, "manifest.yaml", []byte(`
kind: plugin
source: github.com/acme/plugins/notion
version: 0.0.1-alpha.1
displayName: Notion
spec:
  connections:
    mcp:
      displayName: MCP
      mode: user
      auth:
        type: mcp_oauth
  surfaces:
    mcp:
      url: https://mcp.notion.com/mcp
      connection: mcp
`))

	_, dirManifest, err := ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(dir): %v", err)
	}
	if dirManifest.Spec == nil {
		t.Fatal("expected plugin metadata")
	}
	if dirManifest.Spec.MCPURL() != "https://mcp.notion.com/mcp" {
		t.Fatalf("plugin mcp_url = %q", dirManifest.Spec.MCPURL())
	}
	if dirManifest.Spec.SurfaceConnectionName("mcp") != "mcp" {
		t.Fatalf("plugin mcp_connection = %q, want mcp", dirManifest.Spec.SurfaceConnectionName("mcp"))
	}
	if conn := dirManifest.Spec.Connections["mcp"]; conn == nil || conn.Auth == nil || conn.Auth.Type != providermanifestv1.AuthTypeMCPOAuth {
		t.Fatalf("plugin connection auth = %#v", dirManifest.Spec.Connections["mcp"])
	}
	if conn := dirManifest.Spec.Connections["mcp"]; conn == nil || conn.DisplayName != "MCP" {
		t.Fatalf("plugin connection displayName = %#v", dirManifest.Spec.Connections["mcp"])
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
kind: plugin
source: github.com/acme/plugins/bad-mcp-oauth
version: 0.0.1-alpha.1
spec:
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
kind: plugin
source: github.com/acme/plugins/multi-conn
version: 1.0.0
displayName: Multi Connection
spec:
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
  surfaces:
    openapi:
      document: openapi.yaml
      connection: api
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
