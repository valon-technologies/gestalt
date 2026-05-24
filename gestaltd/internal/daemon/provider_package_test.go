package daemon

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	authenticationservice "github.com/valon-technologies/gestalt/server/services/authentication"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
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
			wantError: "entrypoint.artifactPath is required",
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

	repoRoot := filepath.Join("..", "..", "..")
	bigqueryDir := filepath.Join(repoRoot, "plugins", "bigquery")
	if _, err := os.Stat(filepath.Join(bigqueryDir, "go.mod")); err != nil {
		t.Skipf("bigquery plugin not found: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.1-test"
	const testPlatform = "linux/amd64"

	cmd := exec.Command(gestaltdBin, "provider", "package",
		"--version", testVersion,
		"--platform", testPlatform,
		"--output", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider package failed: %v\n%s", err, out)
	}
	cmd = exec.Command(gestaltdBin, "provider", "release",
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
	if artifact.Path != "gestalt-app-bigquery" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, "gestalt-app-bigquery")
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

	checksumPath := filepath.Join(outputDir, "checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if !strings.Contains(string(checksumData), archiveName) {
		t.Fatalf("checksums.txt does not reference %s: %s", archiveName, checksumData)
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

func TestRun_ProviderPackageAndReleaseBuildsRequestedPlatformSets(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-all"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", allPlatformsValue,
		"--output", outputDir,
	)

	assertReleasePlatforms(t, outputDir, defaultReleasePlatformsForTest(t), func(platform releasePlatform) string {
		return "gestalt-app-release-test_v" + testVersion + "_" + platform.GOOS + "_" + platform.GOARCH + ".tar.gz"
	}, func(t *testing.T, artifact providermanifestv1.Artifact, platform releasePlatform) {
		assertExpectedGoArtifactPlatform(t, artifact, platform.GOOS, platform.GOARCH, "")
	})
}

func TestRun_ProviderPackageAndReleaseBuildsGoSourceAuthPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		appName:    authReleaseAppName,
		schemaPath: authReleaseSchemaPath,
		sourceFile: "auth.go",
		sourceCode: testutil.GeneratedAuthPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindAuthentication,
			Source: authReleaseSource, Version: "0.0.1", DisplayName: "Auth Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: authReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.15-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(authReleaseAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := ".gestalt/build/provider"

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
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
	if metadata.Kind != providermanifestv1.KindAuthentication {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindAuthentication)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	authArtifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	authDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash auth archive: %v", err)
	}
	if authArtifact.Path != archiveName || authArtifact.SHA256 != authDigest {
		t.Fatalf("release metadata auth artifact = %+v, want path %q sha %q", authArtifact, archiveName, authDigest)
	}

	auth, err := authenticationservice.NewExecutable(context.Background(), authenticationservice.ExecConfig{
		Command:     filepath.Join(extractDir, binaryName),
		Name:        "auth-release",
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("authenticationservice.NewExecutable: %v", err)
	}
	defer func() {
		if closer, ok := auth.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	loginURL, err := auth.LoginURL("host-state")
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("url.Parse(loginURL): %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("login URL did not include state")
	}

	callbackHandler, ok := auth.(interface {
		HandleCallbackRequest(context.Context, url.Values) (*core.UserIdentity, string, error)
	})
	if !ok {
		t.Fatal("auth provider did not expose HandleCallbackRequest")
	}
	identity, originalState, err := callbackHandler.HandleCallbackRequest(context.Background(), url.Values{
		"code":   {"callback-code"},
		"state":  {state},
		"prompt": {parsed.Query().Get("prompt")},
	})
	if err != nil {
		t.Fatalf("HandleCallbackRequest: %v", err)
	}
	if originalState != "host-state" {
		t.Fatalf("original state = %q, want %q", originalState, "host-state")
	}
	if identity == nil || identity.Email != "generated-auth@example.com" {
		t.Fatalf("identity = %+v", identity)
	}
	if ttlProvider, ok := auth.(interface{ SessionTokenTTL() time.Duration }); !ok || ttlProvider.SessionTokenTTL() != 90*time.Minute {
		t.Fatalf("SessionTokenTTL = %v", ttlProvider)
	}

	externalJWT, err := session.IssueToken(&core.UserIdentity{Email: "jwt@example.com"}, []byte("abcdef0123456789abcdef0123456789"), 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	validated, err := auth.ValidateToken(context.Background(), externalJWT)
	if err != nil {
		t.Fatalf("ValidateToken(external jwt): %v", err)
	}
	if validated == nil || validated.Email != "jwt@example.com" {
		t.Fatalf("validated = %+v", validated)
	}
}

func TestRun_ProviderPackageAndReleaseBuildsGoSourceAuthorizationProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		appName:    authorizationReleaseAppName,
		schemaPath: authorizationReleaseSchemaPath,
		sourceFile: "authorization.go",
		sourceCode: testutil.GeneratedAuthorizationPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindAuthorization,
			Source: authorizationReleaseSource, Version: "0.0.1", DisplayName: "Authorization Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: authorizationReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.18-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(authorizationReleaseAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := ".gestalt/build/provider"

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("authorization entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, authorizationReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", authorizationReleaseSchemaPath, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != authorizationReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, authorizationReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindAuthorization {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindAuthorization)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}

	authz, err := authorizationservice.NewExecutable(context.Background(), authorizationservice.ExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    "authorization-release",
	})
	if err != nil {
		t.Fatalf("authorizationservice.NewExecutable: %v", err)
	}
	defer func() {
		if closer, ok := authz.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	decision, err := authz.Evaluate(context.Background(), &core.AccessEvaluationRequest{
		Subject:  &core.SubjectRef{Type: "user", Id: "generated-user"},
		Action:   &core.ActionRef{Name: "invoke"},
		Resource: &core.ResourceRef{Type: "plugin", Id: "github"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision == nil || !decision.Allowed || decision.ModelId != "model-v1" {
		t.Fatalf("decision = %+v", decision)
	}

	providerMetadata, err := authz.GetMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if providerMetadata == nil || providerMetadata.ActiveModelId != "model-v1" {
		t.Fatalf("metadata = %+v", providerMetadata)
	}

	activeModel, err := authz.GetActiveModel(context.Background())
	if err != nil {
		t.Fatalf("GetActiveModel: %v", err)
	}
	if activeModel == nil || activeModel.Model == nil || activeModel.Model.Id != "model-v1" {
		t.Fatalf("active model = %+v", activeModel)
	}

	relationships, err := authz.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{})
	if err != nil {
		t.Fatalf("ReadRelationships: %v", err)
	}
	if relationships == nil || len(relationships.Relationships) != 1 {
		t.Fatalf("relationships = %+v", relationships)
	}
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
	binaryName := ".gestalt/build/provider"

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
	binaryName := ".gestalt/build/provider"

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
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	workflowArtifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
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
	binaryName := ".gestalt/build/provider"

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
				proto.RegisterExternalCredentialProviderServer(srv, externalcredentialsservice.NewProviderServer(services.ExternalCredentials))
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
		SubjectID:    "user:user-123",
		ConnectionID: "slack:default",
		Integration:  "slack",
		Connection:   "default",
		Instance:     "workspace-1",
		AccessToken:  "xoxb-123",
		RefreshToken: "refresh-123",
		Scopes:       "channels:read chat:write",
	}
	if err := provider.PutCredential(context.Background(), credential); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	if credential.ID == "" {
		t.Fatal("PutCredential returned empty credential id")
	}
	if credential.CreatedAt.IsZero() || credential.UpdatedAt.IsZero() {
		t.Fatalf("credential timestamps = created_at:%v updated_at:%v", credential.CreatedAt, credential.UpdatedAt)
	}

	got, err := provider.GetCredential(context.Background(), credential.SubjectID, credential.ConnectionID, credential.Instance)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.AccessToken != credential.AccessToken || got.RefreshToken != credential.RefreshToken {
		t.Fatalf("credential tokens = access:%q refresh:%q", got.AccessToken, got.RefreshToken)
	}

	listed, err := provider.ListCredentialsForConnection(context.Background(), credential.SubjectID, credential.ConnectionID)
	if err != nil {
		t.Fatalf("ListCredentialsForConnection: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != credential.ID {
		t.Fatalf("listed credentials = %+v", listed)
	}

	if err := provider.DeleteCredential(context.Background(), credential.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, err = provider.GetCredential(context.Background(), credential.SubjectID, credential.ConnectionID, credential.Instance)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCredential after delete error = %v, want core.ErrNotFound", err)
	}
}

//nolint:paralleltest // Uses t.Setenv in table-driven subtests, which cannot run under parallel ancestors.
func TestRun_ProviderPackageAndReleaseBuildsExecutableAuthProviders(t *testing.T) {
	goAuthFixture := func(t *testing.T) sourceComponentReleaseFixtureParams {
		t.Helper()
		return sourceComponentReleaseFixtureParams{
			appName:    authReleaseAppName,
			schemaPath: authReleaseSchemaPath,
			sourceFile: "auth.go",
			sourceCode: testutil.GeneratedAuthPackageSource(),
			manifest: &providermanifestv1.Manifest{
				Kind:   providermanifestv1.KindAuthentication,
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
		assertSessionTTL    bool
		assertExternalJWT   bool
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
			assertSessionTTL:  true,
			assertExternalJWT: true,
		},
	}

	//nolint:paralleltest // The subtests share process-wide env mutation through t.Setenv in selected cases.
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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
			binaryName := ".gestalt/build/provider"

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

			assertExecutableAuthProviderWorks(t, filepath.Join(extractDir, binaryName), tc.appName, tc.assertSessionTTL, tc.assertExternalJWT)
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
		".gestalt/build/provider",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderPackageAndReleaseCopiesUISupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.3-test"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-test_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	for _, rel := range []string{
		"branding/icon.svg",
		"out/index.html",
		"out/static/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != uiTestSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, uiTestSource)
	}
	if metadata.Kind != providermanifestv1.KindUI {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindUI)
	}
	if metadata.Runtime != providerReleaseRuntimeKindUI {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindUI)
	}
	uiArtifact, ok := metadata.Artifacts[providerReleaseGenericTarget]
	if !ok {
		t.Fatalf("release metadata artifacts missing generic key: %+v", metadata.Artifacts)
	}
	uiDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash ui archive: %v", err)
	}
	if uiArtifact.Path != archiveName || uiArtifact.SHA256 != uiDigest {
		t.Fatalf("release metadata ui artifact = %+v, want path %q sha %q", uiArtifact, archiveName, uiDigest)
	}
}
