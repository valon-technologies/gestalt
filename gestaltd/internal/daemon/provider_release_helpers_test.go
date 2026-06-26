package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"gopkg.in/yaml.v3"
)

func defaultReleasePlatformsForTest(t *testing.T) []releasePlatform {
	t.Helper()

	platforms, err := parseReleasePlatforms(defaultPlatforms)
	if err != nil {
		t.Fatalf("parseReleasePlatforms(defaultPlatforms): %v", err)
	}
	return platforms
}

func platformArchiveNameForTest(appName, version, goos, goarch string) string {
	return fmt.Sprintf("gestalt-app-%s_v%s_%s_%s.tar.gz", appName, version, goos, goarch)
}

func assertExpectedGoArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch, _ string) {
	t.Helper()
	assertArtifactPlatform(t, artifact, goos, goarch)
}

func assertArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch string) {
	t.Helper()
	if artifact.OS != goos || artifact.Arch != goarch {
		t.Fatalf("artifact platform = %s/%s, want %s/%s", artifact.OS, artifact.Arch, goos, goarch)
	}
}

func runProviderPackageAndReleaseCommand(t *testing.T, pluginDir string, args ...string) string {
	t.Helper()

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir, args...)
	if err != nil {
		t.Fatalf("provider package+release failed: %v\n%s", err, out)
	}
	return string(out)
}

func runProviderPackageCommand(t *testing.T, pluginDir string, args ...string) string {
	t.Helper()

	out, err := runProviderPackageCommandResult(pluginDir, args...)
	if err != nil {
		t.Fatalf("provider package failed: %v\n%s", err, out)
	}
	return string(out)
}

func runProviderCommandResult(pluginDir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"provider"}, args...)
	cmd := gestaltdCommand(cmdArgs...)
	cmd.Dir = pluginDir
	return cmd.CombinedOutput()
}

// runProviderPackageAndReleaseCommandResult keeps archive-creation tests focused
// on their historical assertions while exercising the new two-step CLI.
func runProviderPackageAndReleaseCommandResult(pluginDir string, args ...string) ([]byte, error) {
	packageOut, err := runProviderPackageCommandResult(pluginDir, args...)
	if err != nil {
		return packageOut, err
	}
	version, outputDir := providerReleaseTestVersionAndOutput(args)
	releaseArgs := []string{"release", "--dist-dir", outputDir}
	if version != "" {
		releaseArgs = append(releaseArgs, "--version", version)
	}
	releaseOut, err := runProviderCommandResult(pluginDir, releaseArgs...)
	return append(packageOut, releaseOut...), err
}

func runProviderPackageCommandResult(pluginDir string, args ...string) ([]byte, error) {
	return runProviderCommandResult(pluginDir, append([]string{"package"}, args...)...)
}

func providerReleaseTestVersionAndOutput(args []string) (string, string) {
	fs := flag.NewFlagSet("provider package test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "")
	outputDir := fs.String("output", defaultReleaseOutputDir, "")
	_ = fs.String("platform", "", "")
	if err := fs.Parse(args); err != nil {
		return "", defaultReleaseOutputDir
	}
	return *version, *outputDir
}

func extractReleasedArchive(t *testing.T, outputDir, archiveName string) string {
	t.Helper()

	archivePath := filepath.Join(outputDir, archiveName)
	extractDir := t.TempDir()
	if err := providerpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("ExtractPackage(%s): %v", archiveName, err)
	}
	return extractDir
}

func assertExecutableAuthProviderWorks(t *testing.T, command, providerName string, assertIntrospectJWT bool) {
	t.Helper()

	auth, err := identityservice.NewExecutable(context.Background(), identityservice.ExecConfig{
		Command:     command,
		Name:        providerName,
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
	})
	if err != nil {
		t.Fatalf("identityservice.NewExecutable: %v", err)
	}
	defer func() {
		if closer, ok := auth.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	ctx := context.Background()
	callbackURL := "https://gestalt.example.test/api/v1/auth/login/callback"
	authorizeResp, err := auth.Authorize(ctx, &core.AuthorizeRequest{
		ResponseType: "code",
		ClientID:     core.DefaultOAuthClientID,
		RedirectURI:  callbackURL,
		State:        "host-state",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	parsed, err := url.Parse(authorizeResp.RedirectURI)
	if err != nil {
		t.Fatalf("url.Parse(redirect): %v", err)
	}
	if parsed.Query().Get("state") != "host-state" {
		t.Fatalf("authorize state = %q, want host-state", parsed.Query().Get("state"))
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatal("authorize redirect did not include code")
	}

	tokenResp, err := auth.Token(ctx, &core.TokenRequest{
		GrantType:   "authorization_code",
		Code:        code,
		RedirectURI: callbackURL,
		ClientID:    core.DefaultOAuthClientID,
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	introspectResp, err := auth.Introspect(ctx, &core.IntrospectRequest{Token: tokenResp.AccessToken})
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if introspectResp == nil || !introspectResp.Active || introspectResp.Subject != "user:generated-auth@example.com" {
		t.Fatalf("introspect = %+v, want active generated-auth subject", introspectResp)
	}
	if assertIntrospectJWT {
		externalJWT, err := session.IssueToken(&core.UserIdentity{Email: "jwt@example.com"}, []byte("abcdef0123456789abcdef0123456789"), 24*time.Hour)
		if err != nil {
			t.Fatalf("IssueToken: %v", err)
		}
		jwtIntrospect, err := auth.Introspect(ctx, &core.IntrospectRequest{Token: externalJWT})
		if err != nil {
			t.Fatalf("Introspect(external jwt): %v", err)
		}
		if jwtIntrospect == nil || !jwtIntrospect.Active || jwtIntrospect.Subject != "user:jwt@example.com" {
			t.Fatalf("jwt introspect = %+v", jwtIntrospect)
		}
	}
}

func assertReleaseDefaultsToHostPlatform(t *testing.T, manifest *providermanifestv1.Manifest, assertPlatform func(*testing.T, providermanifestv1.Artifact)) {
	t.Helper()

	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want exactly one host-platform artifact", manifest.Artifacts)
	}
	assertPlatform(t, manifest.Artifacts[0])
}

func assertReleasedManifestHasHostedHTTPMetadata(t *testing.T, manifest *providermanifestv1.Manifest, target string) {
	t.Helper()

	if manifest == nil || manifest.Spec == nil {
		t.Fatalf("manifest = %+v, want populated spec", manifest)
		return
	}

	scheme := manifest.Spec.SecuritySchemes["signed"]
	if scheme == nil {
		t.Fatal(`manifest.Spec.SecuritySchemes["signed"] = nil, want manifest scheme`)
		return
	}
	if scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		t.Fatalf("scheme.Type = %q, want %q", scheme.Type, providermanifestv1.HTTPSecuritySchemeTypeHMAC)
	}
	if scheme.Secret == nil || scheme.Secret.Env != "REQUEST_SIGNING_SECRET" {
		t.Fatalf("scheme.Secret = %+v, want env-backed secret", scheme.Secret)
		return
	}
	if scheme.SignatureHeader != "X-Request-Signature" {
		t.Fatalf("scheme.SignatureHeader = %q, want %q", scheme.SignatureHeader, "X-Request-Signature")
	}
	if scheme.SignaturePrefix != "v0=" {
		t.Fatalf("scheme.SignaturePrefix = %q, want %q", scheme.SignaturePrefix, "v0=")
	}
	if scheme.PayloadTemplate != "v0:{header:X-Request-Timestamp}:{raw_body}" {
		t.Fatalf("scheme.PayloadTemplate = %q, want %q", scheme.PayloadTemplate, "v0:{header:X-Request-Timestamp}:{raw_body}")
	}
	if scheme.TimestampHeader != "X-Request-Timestamp" {
		t.Fatalf("scheme.TimestampHeader = %q, want %q", scheme.TimestampHeader, "X-Request-Timestamp")
	}
	if scheme.MaxAgeSeconds != 300 {
		t.Fatalf("scheme.MaxAgeSeconds = %d, want %d", scheme.MaxAgeSeconds, 300)
	}

	binding := manifest.Spec.HTTP["command"]
	if binding == nil {
		t.Fatal(`manifest.Spec.HTTP["command"] = nil, want manifest HTTP binding`)
		return
	}
	if binding.Path != "/command" {
		t.Fatalf("binding.Path = %q, want %q", binding.Path, "/command")
	}
	if binding.Method != http.MethodPost {
		t.Fatalf("binding.Method = %q, want %q", binding.Method, http.MethodPost)
	}
	if binding.Security != "signed" {
		t.Fatalf("binding.Security = %q, want %q", binding.Security, "signed")
	}
	if binding.Target != target {
		t.Fatalf("binding.Target = %q, want %q", binding.Target, target)
	}
	if binding.RequestBody == nil {
		t.Fatal("binding.RequestBody = nil, want form request body metadata")
	}
	if _, ok := binding.RequestBody.Content["application/x-www-form-urlencoded"]; !ok {
		t.Fatalf("binding.RequestBody.Content = %#v, want form content type", binding.RequestBody.Content)
	}
}

func writeManagedPluginConfigForTest(t *testing.T, dir, pluginKey, metadataURL, mountPath string) string {
	t.Helper()

	indexedDBManifest := writeStubIndexedDBManifestForTest(t, dir)
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	configData := fmt.Sprintf(`apiVersion: %s
providers:
  externalCredentials:
    default:
      source:
        path: %q
  indexeddb:
    sqlite:
      source:
        path: %q
      config:
        path: %q
apps:
  %s:
    source: %q
    ui:
      path: %q
server:
  providers:
    externalCredentials: default
    indexeddb: sqlite
  artifactsDir: %q
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.ConfigAPIVersion, externalCredentialsManifest, indexedDBManifest, filepath.Join(dir, "gestalt.db"), pluginKey, metadataURL, mountPath, filepath.Join(dir, "prepared-artifacts"))
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func writeStubIndexedDBManifestForTest(t *testing.T, dir string) string {
	t.Helper()

	providerDir := filepath.Join(dir, "indexeddb-stub-src")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir indexeddb stub dir: %v", err)
	}
	stagingBinary := filepath.Join(providerDir, "indexeddb-stub-binary")
	artifactContent := []byte("indexeddb-stub-binary")
	if err := os.WriteFile(stagingBinary, artifactContent, 0o755); err != nil {
		t.Fatalf("write indexeddb staging binary: %v", err)
	}
	buildOutput := ".gestaltd/bin/indexeddb-stub"
	buildScript := fmt.Sprintf("mkdir -p .gestaltd/bin\ncp indexeddb-stub-binary %s\nchmod +x %s\n", buildOutput, buildOutput)
	if err := os.WriteFile(filepath.Join(providerDir, "build.sh"), []byte(buildScript), 0o755); err != nil {
		t.Fatalf("write indexeddb build script: %v", err)
	}
	manifestPath := filepath.Join(providerDir, "manifest.yaml")
	data, err := encodeTestManifestFormat(&providermanifestv1.Manifest{
		Source:      "github.com/test/providers/indexeddb-stub",
		Version:     "0.0.1-alpha.1",
		Kind:        providermanifestv1.KindIndexedDB,
		DisplayName: "IndexedDB Stub",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh", "indexeddb-stub-binary"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode indexeddb manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write indexeddb manifest: %v", err)
	}
	return manifestPath
}

type sourceComponentReleaseFixtureParams struct {
	appName    string
	schemaPath string
	sourceFile string
	sourceCode string
	manifest   *providermanifestv1.Manifest
}

func newSourceComponentReleaseFixture(t *testing.T, dir string, p sourceComponentReleaseFixtureParams) string {
	t.Helper()

	pluginDir := filepath.Join(dir, p.appName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/"+p.appName)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, pluginDir, p.sourceFile, []byte(p.sourceCode), 0o644)
	artifactRel := ".gestaltd/bin/" + p.appName
	writeGoComponentBuildFixture(t, pluginDir, "example.com/"+p.appName, p.manifest.Kind, artifactRel)
	p.manifest.Build = &providermanifestv1.SourceBuild{
		Command: []string{"sh", "./build.sh"},
		Inputs:  []string{"go.mod", "go.sum", p.sourceFile, "cmd", "build.sh"},
	}
	p.manifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactRel}
	writeReleaseTestManifest(t, pluginDir, p.manifest)
	writeTestFile(t, pluginDir, p.schemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func hostedHTTPMetadataSpec(target string) *providermanifestv1.Spec {
	return &providermanifestv1.Spec{
		Connections: map[string]*providermanifestv1.ManifestConnectionDef{
			"default": {
				Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
			},
		},
		SecuritySchemes: map[string]*providermanifestv1.HTTPSecurityScheme{
			"signed": {
				Type:            providermanifestv1.HTTPSecuritySchemeTypeHMAC,
				Secret:          &providermanifestv1.HTTPSecretRef{Env: "REQUEST_SIGNING_SECRET"},
				SignatureHeader: "X-Request-Signature",
				SignaturePrefix: "v0=",
				PayloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
				TimestampHeader: "X-Request-Timestamp",
				MaxAgeSeconds:   300,
			},
		},
		HTTP: map[string]*providermanifestv1.HTTPBinding{
			"command": {
				Path:     "/command",
				Method:   http.MethodPost,
				Security: "signed",
				Target:   target,
				RequestBody: &providermanifestv1.HTTPRequestBody{
					Required: true,
					Content: map[string]*providermanifestv1.HTTPMediaType{
						"application/x-www-form-urlencoded": {},
					},
				},
			},
		},
	}
}

func newSourceProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestAppName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, releaseTestModule)), 0644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0644)
	writeStaticCatalogProviderMain(t, pluginDir)
	artifactRel := ".gestaltd/bin/" + releaseTestAppName
	writeGoAppBuildFixture(t, pluginDir, releaseTestModule, releaseTestAppName, artifactRel)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0644)
	return pluginDir
}

func newBuiltSourceProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	if err := os.Remove(filepath.Join(pluginDir, releaseProviderSchemaPath)); err != nil {
		t.Fatalf("Remove(%s): %v", releaseProviderSchemaPath, err)
	}
	buildScript := "mkdir -p .gestaltd/bin\ngo build -o .gestaltd/bin/" + releaseTestAppName + " ./cmd/provider\nmkdir -p schemas\nprintf '{\"type\":\"object\"}\\n' > " + releaseProviderSchemaPath + "\n"
	writeTestFile(t, pluginDir, "build.sh", []byte(buildScript), 0o755)
	return pluginDir
}

func newGoSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestAppName)
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	artifactRel := ".gestaltd/bin/" + releaseTestAppName
	writeGoAppBuildFixture(t, pluginDir, "github.com/valon-technologies/gestalt/testdata/provider-go", releaseTestAppName, artifactRel)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		Spec:        hostedHTTPMetadataSpec("echo"),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return pluginDir
}

func newDeclarativeProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, declarativeReleaseAppName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      declarativeReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Declarative Release",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.test",
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:   "list_widgets",
							Method: "GET",
							Path:   "/widgets",
						},
					},
				},
			},
		},
	})
	return pluginDir
}

func newSourceProviderReleaseFixtureWithoutCatalog(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	_ = os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile))

	return pluginDir
}

func writeStaticCatalogProviderMain(t *testing.T, dir string) {
	t.Helper()
	writeStaticCatalogProviderMainAt(t, dir, "provider.go")
}

func writeStaticCatalogProviderMainAt(t *testing.T, dir, rel string) {
	t.Helper()
	writeTestFile(t, dir, rel, []byte(testutil.GeneratedProviderPackageSource()), 0644)
}

func newPrebuiltProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, prebuiltProviderAppName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, prebuiltProviderBinaryPath, []byte("prebuilt-provider"), 0755)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      prebuiltProviderSource,
		Version:     "0.0.1",
		DisplayName: "Prebuilt Provider",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: prebuiltProviderBinaryPath,
		},
	})
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newUIReleaseFixture(t *testing.T, dir string) string {
	return newUIReleaseFixtureWithAssetRoot(t, dir, uiTestAssetRoot)
}

func newBuiltUIReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, uiTestAppName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, providerpkg.ManifestFile, []byte(fmt.Sprintf(`{
  "kind": "ui",
  "source": %q,
  "version": "0.0.1",
  "displayName": "UI Test",
  "iconFile": %q,
  "release": {"build": {"workdir": "ui", "command": ["sh", "./build.sh"]}},
  "spec": {"assetRoot": "ui/out"}
}
`, uiTestSource, releaseTestIconPath)), 0o644)
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeReleaseBuildScript(t, pluginDir, filepath.Join("ui", "build.sh"), "mkdir -p out/static\nprintf '<html></html>\\n' > out/index.html\nprintf 'console.log(\"ok\")\\n' > out/static/app.js\n")
	return pluginDir
}

func newSourceBuiltUIReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, uiTestAppName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      uiTestSource,
		Version:     "0.0.1",
		DisplayName: "UI Test",
		IconFile:    releaseTestIconPath,
		Build: &providermanifestv1.SourceBuild{
			Workdir: "ui",
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"ui/build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "ui/out"},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0o644)
	writeReleaseBuildScript(t, pluginDir, filepath.Join("ui", "build.sh"), "mkdir -p out/static\nprintf '<html></html>\\n' > out/index.html\nprintf 'console.log(\"ok\")\\n' > out/static/app.js\n")
	return pluginDir
}

func newSourceProviderReleaseFixtureWithOwnedUI(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	uiDir := filepath.Join(dir, "roadmap-ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(uiDir): %v", err)
	}
	writeReleaseTestManifest(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap-ui",
		Version:     "0.0.1",
		DisplayName: "Roadmap UI",
		IconFile:    releaseTestIconPath,
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
	})
	writeTestFile(t, uiDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, uiDir, "dist/index.html", []byte("<html>roadmap</html>\n"), 0o644)
	writeTestFile(t, uiDir, "dist/static/app.js", []byte("console.log('roadmap')\n"), 0o644)
	writeReleaseBuildScript(t, uiDir, "build.sh", "mkdir -p dist/static\nprintf '<html>roadmap</html>\\n' > dist/index.html\nprintf 'console.log(\"roadmap\")\\n' > dist/static/app.js\n")

	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	manifest.Spec.UI = &providermanifestv1.OwnedUI{Path: "../roadmap-ui/" + providerpkg.ManifestFile}
	writeReleaseTestManifest(t, pluginDir, manifest)

	return pluginDir
}

func newSourceProviderReleaseFixtureWithSourceBuiltOwnedUI(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	uiDir := filepath.Join(dir, "roadmap-ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(uiDir): %v", err)
	}
	writeReleaseTestManifest(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap-ui",
		Version:     "0.0.1",
		DisplayName: "Roadmap UI",
		IconFile:    releaseTestIconPath,
		Build: &providermanifestv1.SourceBuild{
			Workdir: "ui",
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"ui/build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "ui/dist"},
	})
	writeTestFile(t, uiDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0o644)
	writeReleaseBuildScript(t, uiDir, filepath.Join("ui", "build.sh"), "mkdir -p dist/static\nprintf '<html>roadmap</html>\\n' > dist/index.html\nprintf 'console.log(\"roadmap\")\\n' > dist/static/app.js\n")

	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	manifest.Spec.UI = &providermanifestv1.OwnedUI{Path: "../roadmap-ui/" + providerpkg.ManifestFile}
	writeReleaseTestManifest(t, pluginDir, manifest)

	return pluginDir
}

func newUIReleaseFixtureWithAssetRoot(t *testing.T, dir, assetRoot string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, uiTestAppName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      uiTestSource,
		Version:     "0.0.1",
		DisplayName: "UI Test",
		IconFile:    releaseTestIconPath,
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: assetRoot},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/index.html", []byte("<html></html>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/static/app.js", []byte("console.log('ok')\n"), 0644)
	writeReleaseBuildScript(t, pluginDir, "build.sh", "mkdir -p "+assetRoot+"/static\nprintf '<html></html>\\n' > "+assetRoot+"/index.html\nprintf 'console.log(\"ok\")\\n' > "+assetRoot+"/static/app.js\n")
	return pluginDir
}

func writeReleaseBuildScript(t *testing.T, dir, rel, body string) {
	t.Helper()

	writeTestFile(t, dir, rel, []byte("#!/bin/sh\nset -eu\n"+body), 0o755)
}

func readReleasedManifest(t *testing.T, outputDir, archiveName string) *providermanifestv1.Manifest {
	t.Helper()

	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, err := providerpkg.FindManifestFile(extractDir)
	if err != nil {
		t.Fatalf("find released manifest: %v", err)
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	return manifest
}

func readProviderReleaseMetadata(t *testing.T, outputDir string) *providerrelease.Metadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(outputDir, providerrelease.MetadataFile))
	if err != nil {
		t.Fatalf("read %s: %v", providerrelease.MetadataFile, err)
	}
	var metadata providerrelease.Metadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode %s: %v", providerrelease.MetadataFile, err)
	}
	return &metadata
}

func providerReleaseArtifactForTarget(t *testing.T, metadata *providerrelease.Metadata, target string) providerrelease.Artifact {
	t.Helper()

	if artifact, ok := metadata.Artifacts[target]; ok {
		return artifact
	}
	t.Fatalf("release metadata artifacts missing target %q: %+v", target, metadata.Artifacts)
	return providerrelease.Artifact{}
}

func writeProviderReleaseArchiveForTest(t *testing.T, outputDir, archiveName string, manifest *providermanifestv1.Manifest) string {
	t.Helper()

	packageDir := t.TempDir()
	writeProviderReleaseManifestSupportFilesForTest(t, packageDir, manifest)
	writeReleasedManifestForArchiveTest(t, packageDir, manifest)

	archivePath := filepath.Join(outputDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(packageDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir(%s): %v", archiveName, err)
	}
	return archivePath
}

func writeProviderReleaseManifestSupportFilesForTest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	if manifest == nil {
		return
	}
	if manifest.IconFile != "" {
		writeTestFile(t, dir, manifest.IconFile, []byte("<svg></svg>\n"), 0o644)
	}
	if manifest.Spec != nil {
		if manifest.Spec.ConfigSchemaPath != "" {
			writeTestFile(t, dir, manifest.Spec.ConfigSchemaPath, []byte(`{"type":"object"}`), 0o644)
		}
		if manifest.Spec.AssetRoot != "" {
			writeTestFile(t, dir, filepath.Join(filepath.FromSlash(manifest.Spec.AssetRoot), "index.html"), []byte("<html></html>\n"), 0o644)
		}
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "" {
			continue
		}
		writeTestFile(t, dir, artifact.Path, []byte("artifact:"+artifact.Path), 0o755)
	}
}

func writeReleasedManifestForArchiveTest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	populateMissingArtifactDigests(t, dir, manifest)
	data, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatJSON)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile(t, dir, providerpkg.ManifestFile, data, 0o644)
	if manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil {
		writeTestFile(t, dir, providerpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644)
	}
}

func readManifestFromDir(t *testing.T, dir string) (string, *providermanifestv1.Manifest) {
	t.Helper()

	manifestPath, err := providerpkg.FindManifestFile(dir)
	if err != nil {
		t.Fatalf("find manifest: %v", err)
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return manifestPath, manifest
}

func writeReleaseTestManifest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	writeReleaseTestManifestFormat(t, dir, providerpkg.ManifestFile, manifest)
}

func writeReleaseTestManifestFormat(t *testing.T, dir, manifestFile string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	populateMissingArtifactDigests(t, dir, manifest)
	data, err := encodeTestManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestFile))
	if err != nil {
		t.Fatalf("encodeTestManifestFormat(%s): %v", manifestFile, err)
	}
	writeTestFile(t, dir, manifestFile, data, 0644)
	if manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil {
		writeTestFile(t, dir, providerpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644)
	}
}

func populateMissingArtifactDigests(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].SHA256 != "" {
			continue
		}

		path := filepath.Join(dir, filepath.FromSlash(manifest.Artifacts[i].Path))
		data, err := os.ReadFile(path)
		if err == nil {
			manifest.Artifacts[i].SHA256 = sha256HexForTest(string(data))
			continue
		}

		manifest.Artifacts[i].SHA256 = sha256HexForTest(manifest.Artifacts[i].Path)
	}
}

func encodeTestManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	if manifest == nil {
		return providerpkg.EncodeSourceManifestFormat(nil, format)
	}
	clone := *manifest
	clone.Entrypoint = nil
	return providerpkg.EncodeSourceManifestFormat(&clone, format)
}

func writeTestFile(t *testing.T, dir, rel string, data []byte, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rel, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}
