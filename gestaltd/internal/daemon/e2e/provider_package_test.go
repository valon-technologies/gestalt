package e2e

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	secretsservice "github.com/valon-technologies/gestalt/server/services/secrets"
	"google.golang.org/grpc"
)

func TestRun_ProviderPackageRequiresVersion(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	out, err := runProviderCommandResult(pluginDir, "package")
	if err == nil {
		t.Fatal("expected error when --version missing")
	}
	if !strings.Contains(string(out), "--version is required") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsPackageOnlyFlags(t *testing.T) {
	t.Parallel()

	for _, flagName := range []string{"--output", "--platform"} {
		flagName := flagName
		t.Run(flagName, func(t *testing.T) {
			t.Parallel()

			out, err := runProviderCommandResult(t.TempDir(), "release", flagName, "value")
			if err == nil {
				t.Fatalf("expected %s rejection, got output: %s", flagName, out)
			}
			if !strings.Contains(string(out), "flag provided but not defined") {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestRun_ProviderPackageAndReleaseRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		manifestYAML string
		wantError    string
	}{
		{
			name: "rest surface requires baseUrl",
			manifestYAML: `
kind: app
source: github.com/testowner/apps/invalid
version: 0.0.1-alpha.1
spec:
  surfaces:
    rest:
      operations:
        - name: list_items
          method: GET
          path: /items
`,
			wantError: "provider.baseUrl is required",
		},
		{
			name: "exec block requires artifact path",
			manifestYAML: `
kind: app
source: github.com/testowner/apps/invalid
version: 0.0.1-alpha.1
spec: {}
entrypoint:
  artifactPath: ""
`,
			wantError: "source manifests do not declare entrypoint.artifactPath",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pluginDir := filepath.Join(t.TempDir(), "invalid-plugin")
			if err := os.MkdirAll(pluginDir, 0755); err != nil {
				t.Fatalf("MkdirAll(pluginDir): %v", err)
			}
			writeTestFile(t, pluginDir, "manifest.yaml", []byte(tc.manifestYAML), 0644)

			out, err := runProviderPackageAndReleaseCommandResult(pluginDir, "--version", "0.0.1-test")
			if err == nil {
				t.Fatal("expected invalid manifest error")
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestE2EProviderPackageAndReleaseBigquery(t *testing.T) {
	t.Parallel()

	bigqueryDir := filepath.Join(testutil.RepoRootPath(t), "plugins", "bigquery")
	if _, err := os.Stat(filepath.Join(bigqueryDir, "go.mod")); err != nil {
		t.Skipf("bigquery plugin not found: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.1-test"
	const testPlatform = "linux/amd64"

	cmd := gestaltdCommand("provider", "package",
		"--version", testVersion,
		"--platform", testPlatform,
		"--output", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider package failed: %v\n%s", err, out)
	}
	cmd = gestaltdCommand("provider", "release",
		"--version", testVersion,
		"--dist-dir", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}

	archiveName := "gestalt-app-bigquery_v" + testVersion + "_linux_amd64.tar.gz"
	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	extractDir := filepath.Join(outputDir, "extracted")
	if err := providerpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	_, manifest := readManifestFromDir(t, extractDir)
	if manifest.Version != testVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, testVersion)
	}
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(manifest.Artifacts))
	}
	artifact := manifest.Artifacts[0]
	if artifact.OS != "linux" || artifact.Arch != "amd64" {
		t.Fatalf("artifact platform = %s/%s, want linux/amd64", artifact.OS, artifact.Arch)
	}
	if artifact.Path != providerpkg.PackageExecutablePath("bigquery", "linux") {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, providerpkg.PackageExecutablePath("bigquery", "linux"))
	}

	binaryPath := filepath.Join(extractDir, artifact.Path)
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("binary not in archive: %v", err)
	}
	digest, err := providerpkg.FileSHA256(binaryPath)
	if err != nil {
		t.Fatalf("hash binary: %v", err)
	}
	if digest != artifact.SHA256 {
		t.Fatalf("binary sha256 = %s, manifest says %s", digest, artifact.SHA256)
	}

	iconPath := filepath.Join(extractDir, "assets", "icon.svg")
	if _, err := os.Stat(iconPath); err != nil {
		t.Fatalf("expected assets/icon.svg in archive: %v", err)
	}
}

func TestRun_ProviderPackageAndReleaseDefaultsSourcePluginToHostPlatform(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-default"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)
	assertReleaseDefaultsToHostPlatform(t, manifest, func(t *testing.T, artifact providermanifestv1.Artifact) {
		assertExpectedGoArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH, "")
	})
	assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "echo")
}

func TestRun_ProviderPackageAndReleaseBuildsGoSourceSecretsPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		appName:    secretsReleaseAppName,
		schemaPath: secretsReleaseSchemaPath,
		sourceFile: "secrets.go",
		sourceCode: testutil.GeneratedSecretsPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindSecrets,
			Source: secretsReleaseSource, Version: "0.0.1", DisplayName: "Secrets Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: secretsReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.19-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(secretsReleaseAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := providerpkg.PackageExecutablePath(secretsReleaseAppName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("secrets entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, secretsReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", secretsReleaseSchemaPath, err)
	}

	sm, err := secretsservice.NewExecutable(context.Background(), secretsservice.ExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    secretsReleaseAppName,
	})
	if err != nil {
		t.Fatalf("secretsservice.NewExecutable: %v", err)
	}
	defer func() {
		if closer, ok := sm.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	value, err := sm.GetSecret(context.Background(), "generated-secret")
	if err != nil {
		t.Fatalf("GetSecret(generated-secret): %v", err)
	}
	if value != "generated-secret-value" {
		t.Fatalf("GetSecret(generated-secret) = %q, want %q", value, "generated-secret-value")
	}
}

func TestRun_ProviderPackageAndReleaseBuildsGoSourceWorkflowProvider(t *testing.T) {
	t.Parallel()

	const workflowReleaseAppName = "workflow-release"
	const workflowReleaseSource = "github.com/testowner/providers/workflow-release"
	const workflowReleaseSchemaPath = "workflow.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		appName:    workflowReleaseAppName,
		schemaPath: workflowReleaseSchemaPath,
		sourceFile: "workflow.go",
		sourceCode: testutil.GeneratedWorkflowPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindWorkflow,
			Source: workflowReleaseSource, Version: "0.0.1", DisplayName: "Workflow Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: workflowReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.20-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(workflowReleaseAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := providerpkg.PackageExecutablePath(workflowReleaseAppName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("workflow entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, workflowReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", workflowReleaseSchemaPath, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != workflowReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, workflowReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindWorkflow {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindWorkflow)
	}
	workflowArtifact := providerReleaseArtifactForTarget(t, metadata, providerpkg.CurrentPlatformString())
	workflowDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash workflow archive: %v", err)
	}
	if workflowArtifact.Path != archiveName || workflowArtifact.SHA256 != workflowDigest {
		t.Fatalf("release metadata workflow artifact = %+v, want path %q sha %q", workflowArtifact, archiveName, workflowDigest)
	}
}

func TestRun_ProviderPackageAndReleaseBuildsGoSourceExternalCredentialsPlugin(t *testing.T) {
	t.Parallel()

	const externalCredentialReleaseAppName = "external-credentials-release"
	const externalCredentialReleaseSource = "github.com/testowner/providers/external-credentials-release"
	const externalCredentialReleaseSchemaPath = "external-credentials.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		appName:    externalCredentialReleaseAppName,
		schemaPath: externalCredentialReleaseSchemaPath,
		sourceFile: "externalcredentials.go",
		sourceCode: testutil.GeneratedExternalCredentialPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindExternalCredentials,
			Source: externalCredentialReleaseSource, Version: "0.0.1", DisplayName: "External Credentials Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: externalCredentialReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.21-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(externalCredentialReleaseAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := providerpkg.PackageExecutablePath(externalCredentialReleaseAppName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("external credentials entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, externalCredentialReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", externalCredentialReleaseSchemaPath, err)
	}

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(services)

	provider, err := externalcredentialsservice.NewExecutable(context.Background(), externalcredentialsservice.ExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    externalCredentialReleaseAppName,
		HostServices: []runtimehost.HostService{{
			Name: "external-credentials",
			Register: func(srv *grpc.Server) {
				proto.RegisterExternalCredentialsServer(srv, externalcredentialsservice.NewProviderServer(services.ExternalCredentials))
			},
		}},
	})
	if err != nil {
		t.Fatalf("externalcredentialsservice.NewExecutable: %v", err)
	}
	defer func() {
		if closer, ok := provider.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	credential := &core.ExternalCredential{
		Subject:   "user:user-123",
		Audience:  "slack:default",
		Qualifier: "workspace-1",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "xoxb-123", RefreshToken: "refresh-123", Scope: "channels:read chat:write"},
	}
	if err := provider.UpsertCredential(context.Background(), credential); err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	if credential.ID == "" {
		t.Fatal("UpsertCredential returned empty credential id")
	}
	if credential.CreatedAt.IsZero() || credential.UpdatedAt.IsZero() {
		t.Fatalf("credential timestamps = created_at:%v updated_at:%v", credential.CreatedAt, credential.UpdatedAt)
	}

	got, err := provider.GetCredential(context.Background(), credential.Subject, credential.Audience, credential.Qualifier)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.Grant == nil || got.Grant.AccessToken != credential.Grant.AccessToken || got.Grant.RefreshToken != credential.Grant.RefreshToken {
		t.Fatalf("credential grant = %+v, want %+v", got.Grant, credential.Grant)
	}

	listed, err := provider.ListCredentials(context.Background(), credential.Subject, credential.Audience)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != credential.ID {
		t.Fatalf("listed credentials = %+v", listed)
	}

	if err := provider.DeleteCredential(context.Background(), credential.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, err = provider.GetCredential(context.Background(), credential.Subject, credential.Audience, credential.Qualifier)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCredential after delete error = %v, want core.ErrNotFound", err)
	}
}

func TestRun_ProviderPackageAndReleaseBuildsExecutableAuthProviders(t *testing.T) {
	t.Parallel()

	goAuthFixture := func(t *testing.T) sourceComponentReleaseFixtureParams {
		t.Helper()
		return sourceComponentReleaseFixtureParams{
			appName:    authReleaseAppName,
			schemaPath: authReleaseSchemaPath,
			sourceFile: "auth.go",
			sourceCode: testutil.GeneratedAuthPackageSource(),
			manifest: &providermanifestv1.Manifest{
				Kind:   providermanifestv1.KindIdentity,
				Source: authReleaseSource, Version: "0.0.1", DisplayName: "Auth Release",
				Spec: &providermanifestv1.Spec{ConfigSchemaPath: authReleaseSchemaPath},
			},
		}
	}

	cases := []struct {
		name                string
		appName             string
		version             string
		skipOnWindowsReason string
		prepare             func(t *testing.T) string
		archiveName         func(version string) string
		assertArtifact      func(t *testing.T, artifact providermanifestv1.Artifact)
		assertIntrospectJWT bool
	}{
		{
			name:    "go_source",
			appName: authReleaseAppName,
			version: "0.0.15-test",
			prepare: func(t *testing.T) string {
				t.Helper()
				return newSourceComponentReleaseFixture(t, t.TempDir(), goAuthFixture(t))
			},
			archiveName: func(version string) string {
				return platformArchiveNameForTest(authReleaseAppName, version, runtime.GOOS, runtime.GOARCH)
			},
			assertArtifact: func(t *testing.T, artifact providermanifestv1.Artifact) {
				t.Helper()
				assertExpectedGoArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH, "")
			},
			assertIntrospectJWT: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if runtime.GOOS == "windows" && tc.skipOnWindowsReason != "" {
				t.Skip(tc.skipOnWindowsReason)
			}

			pluginDir := tc.prepare(t)
			outputDir := t.TempDir()
			runProviderPackageAndReleaseCommand(t, pluginDir,
				"--version", tc.version,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := tc.archiveName(tc.version)
			extractDir := extractReleasedArchive(t, outputDir, archiveName)
			_, manifest := readManifestFromDir(t, extractDir)
			binaryName := providerpkg.PackageExecutablePath(tc.appName, runtime.GOOS)

			if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
				t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
			}
			tc.assertArtifact(t, manifest.Artifacts[0])
			if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
				t.Fatalf("auth entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
			}
			if _, err := os.Stat(filepath.Join(extractDir, authReleaseSchemaPath)); err != nil {
				t.Fatalf("expected %s in archive: %v", authReleaseSchemaPath, err)
			}

			metadata := readProviderReleaseMetadata(t, outputDir)
			if metadata.Package != authReleaseSource {
				t.Fatalf("release metadata package = %q, want %q", metadata.Package, authReleaseSource)
			}
			if metadata.Kind != providermanifestv1.KindIdentity {
				t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindIdentity)
			}
			authArtifact := providerReleaseArtifactForTarget(t, metadata, providerpkg.CurrentPlatformString())
			authDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
			if err != nil {
				t.Fatalf("hash auth archive: %v", err)
			}
			if authArtifact.Path != archiveName || authArtifact.SHA256 != authDigest {
				t.Fatalf("release metadata auth artifact = %+v, want path %q sha %q", authArtifact, archiveName, authDigest)
			}

			assertExecutableAuthProviderWorks(t, filepath.Join(extractDir, binaryName), tc.appName, tc.assertIntrospectJWT)
		})
	}
}

func TestRun_ProviderPackageAndReleaseCopiesCompiledSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.2-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	if _, err := providerpkg.ValidatePackageDir(extractDir); err != nil {
		t.Fatalf("validate extracted package: %v", err)
	}
	for _, rel := range []string{
		"branding/icon.svg",
		"schemas/provider.schema.json",
		providerpkg.PackageExecutablePath(releaseTestAppName, runtime.GOOS),
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderPackageAndReleaseCopiesUISupportFiles(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("static bundle release-build fixture uses POSIX shell")
	}

	pluginDir := newSourceBuiltStaticAppReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.3-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := staticBuildArchiveName(testVersion)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	for _, rel := range []string{
		"branding/icon.svg",
		"static/index.html",
		"static/assets/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != uiTestSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, uiTestSource)
	}
	if metadata.Kind != providermanifestv1.KindApp {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
	}
	uiArtifact := providerReleaseArtifactForTarget(t, metadata, providerrelease.GenericTarget)
	uiDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash ui archive: %v", err)
	}
	if uiArtifact.Path != archiveName || uiArtifact.SHA256 != uiDigest {
		t.Fatalf("release metadata ui artifact = %+v, want path %q sha %q", uiArtifact, archiveName, uiDigest)
	}
}
