package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	authenticationservice "github.com/valon-technologies/gestalt/server/services/authentication"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	secretsservice "github.com/valon-technologies/gestalt/server/services/secrets"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

const (
	releaseTestAppName             = "release-test"
	releaseTestSource              = "github.com/testowner/apps/catalog/release-test"
	releaseTestModule              = "example.com/release-test"
	releaseTestIconPath            = "branding/icon.svg"
	releaseProviderSchemaPath      = "schemas/provider.schema.json"
	declarativeReleaseAppName      = "declarative-release"
	declarativeReleaseSource       = "github.com/testowner/apps/catalog/declarative-release"
	uiTestAppName                  = "ui-test"
	uiTestSource                   = "github.com/testowner/apps/catalog/ui-test"
	uiTestAssetRoot                = "out"
	prebuiltProviderAppName        = "prebuilt-provider"
	prebuiltProviderSource         = "github.com/testowner/apps/prebuilt-provider"
	prebuiltProviderBinaryPath     = "bin/provider"
	authReleaseAppName             = "auth-release"
	authReleaseSource              = "github.com/testowner/apps/auth-release"
	authReleaseSchemaPath          = "schemas/auth.schema.json"
	authorizationReleaseAppName    = "authorization-release"
	authorizationReleaseSource     = "github.com/testowner/apps/authorization-release"
	authorizationReleaseSchemaPath = "schemas/authorization.schema.json"
	secretsReleaseAppName          = "secrets-release"
	secretsReleaseSource           = "github.com/testowner/apps/secrets-release"
	secretsReleaseSchemaPath       = "schemas/secrets.schema.json"
)

func TestProviderRemoteConfigPathSynthesizesSourcePlugin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	configPaths, cleanup, err := prepareProviderRemoteConfigPaths(providerLocalCommandOptions{Path: pluginDir})
	if err != nil {
		t.Fatalf("prepareProviderRemoteConfigPaths: %v", err)
	}
	defer cleanup()

	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		t.Fatalf("config.LoadPaths: %v", err)
	}
	targets, err := collectProviderRemoteTargets(cfg, "", true)
	if err != nil {
		t.Fatalf("collectProviderRemoteTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %#v, want one source-backed plugin", targets)
	}
	if targets[0].Entry.ResolvedManifestPath == "" {
		t.Fatal("target resolved manifest path is empty")
	}
	if targets[0].Source != "github.com/test/apps/provider" {
		t.Fatalf("target source = %q, want manifest source", targets[0].Source)
	}
	if !targets[0].InheritRemoteConfig {
		t.Fatal("target InheritRemoteConfig = false, want true")
	}
}

func TestResolveProviderRemoteAttachTokenPrecedence(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	writeStoredGestaltCLICredentialForTest(t, configHome, "https://stored.example.com", "stored-token")

	t.Setenv(gestaltAPIKeyEnv, "env-token")
	token := resolveProviderRemoteAttachToken(providerLocalCommandOptions{
		Remote:      "https://remote.example.com",
		RemoteToken: "flag-token",
	})
	if token != "flag-token" {
		t.Fatalf("token = %q, want explicit flag token", token)
	}

	token = resolveProviderRemoteAttachToken(providerLocalCommandOptions{
		Remote: "https://remote.example.com",
	})
	if token != "env-token" {
		t.Fatalf("token = %q, want env token", token)
	}

	t.Setenv(gestaltAPIKeyEnv, "")
	token = resolveProviderRemoteAttachToken(providerLocalCommandOptions{
		Remote: "https://stored.example.com",
	})
	if token != "" {
		t.Fatalf("token = %q, want stored CLI credentials ignored for browser approval", token)
	}
}

func TestProviderRemoteCreateSessionErrorAddsAttachPermissionGuidance(t *testing.T) {
	t.Parallel()

	err := providerRemoteCreateSessionError(errors.New("provider dev remote POST /api/v1/provider-dev/attachments: 403 Forbidden: provider dev attach access denied"))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"remote provider-dev attach was denied",
		"dev.attach.allowedRoles",
		"permissions[].actions including provider_dev.attach",
		"browser approval",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}

	other := errors.New("network unavailable")
	if got := providerRemoteCreateSessionError(other); got != other {
		t.Fatalf("unrelated error was wrapped: %v", got)
	}
}

func TestCreateProviderRemoteSessionUsesBrowserApprovalWithoutAttachToken(t *testing.T) {
	t.Parallel()

	var openedURL string
	previousOpenBrowser := providerRemoteOpenBrowser
	providerRemoteOpenBrowser = func(rawURL string) error {
		openedURL = rawURL
		return nil
	}
	t.Cleanup(func() { providerRemoteOpenBrowser = previousOpenBrowser })

	const (
		authID       = "auth-1"
		clientSecret = "pdaa_secret"
		dispatcher   = "pda_dispatcher"
	)
	var createdAuthorizedSession bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == providerdev.PathAttachAuthorizations:
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("browser authorization should not send bearer token, got %q", r.Header.Get("Authorization"))
			}
			writeJSONForProviderRemoteTest(t, w, http.StatusCreated, providerdev.CreateAttachAuthorizationResponse{
				AuthorizationID:  authID,
				ClientSecret:     clientSecret,
				VerificationCode: "123-456",
				ApprovalURL:      tsURL(t, r) + "/approve",
				ExpiresAt:        time.Now().Add(time.Minute),
			})
		case r.Method == http.MethodGet && r.URL.Path == providerdev.PathAttachAuthorizations+"/"+authID+"/poll":
			if r.Header.Get(providerdev.HeaderAuthorizationSecret) != clientSecret {
				t.Fatalf("poll authorization secret = %q, want %q", r.Header.Get(providerdev.HeaderAuthorizationSecret), clientSecret)
			}
			writeJSONForProviderRemoteTest(t, w, http.StatusOK, providerdev.PollAttachAuthorizationResponse{
				Approved: true,
			})
		case r.Method == http.MethodPost && r.URL.Path == providerdev.PathAttachAuthorizations+"/"+authID+"/attachments":
			if r.Header.Get(providerdev.HeaderAuthorizationSecret) != clientSecret {
				t.Fatalf("authorized attach secret = %q, want %q", r.Header.Get(providerdev.HeaderAuthorizationSecret), clientSecret)
			}
			var req providerdev.CreateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode authorized attach request: %v", err)
			}
			if len(req.Providers) != 1 || req.Providers[0].Name != "roadmap" {
				t.Fatalf("authorized attach providers = %#v, want roadmap", req.Providers)
			}
			createdAuthorizedSession = true
			writeJSONForProviderRemoteTest(t, w, http.StatusCreated, providerdev.CreateSessionResponse{
				AttachID:         "attach-1",
				DispatcherSecret: dispatcher,
				Providers:        []providerdev.CreateSessionProvider{{Name: "roadmap"}},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	client := providerdev.Client{BaseURL: ts.URL, HTTPClient: ts.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := createProviderRemoteSession(ctx, &client, providerdev.CreateSessionRequest{
		Providers: []providerdev.AttachProvider{{Name: "roadmap"}},
	})
	if err != nil {
		t.Fatalf("createProviderRemoteSession: %v", err)
	}
	if openedURL == "" {
		t.Fatal("expected browser approval URL to be opened")
	}
	if !createdAuthorizedSession {
		t.Fatal("expected authorized browser attach session to be created")
	}
	if session.AttachID != "attach-1" || client.DispatcherSecret != dispatcher {
		t.Fatalf("session = %#v, dispatcher secret = %q", session, client.DispatcherSecret)
	}
	if client.AuthorizationSecret != "" {
		t.Fatalf("authorization secret retained after authorized session: %q", client.AuthorizationSecret)
	}
}

func TestResolveProviderAttachTokenUsesMatchingStoredCLICredential(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv(gestaltAPIKeyEnv, "")
	writeStoredGestaltCLICredentialForTest(t, configHome, "https://Valon.Tools/team-a/", "stored-token")

	token, err := resolveProviderAttachToken(providerAttachCommandOptions{
		Remote: "https://valon.tools/team-a",
	})
	if err != nil {
		t.Fatalf("resolveProviderAttachToken: %v", err)
	}
	if token != "stored-token" {
		t.Fatalf("token = %q, want stored-token", token)
	}
}

func TestProviderAttachCommandsUseOwnerAttachmentRoutes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	var sawDelete atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			http.Error(w, "missing owner token", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/team-a"+providerdev.PathAttachments:
			writeJSONForProviderRemoteTest(t, w, http.StatusOK, providerdev.ListAttachmentsResponse{
				Attachments: []providerdev.AttachmentInfo{{
					AttachID:           "attach-1",
					CreatedAt:          now.Add(-time.Minute),
					LastSeenAt:         now,
					IdleTimeoutSeconds: 120,
					Providers: []providerdev.AttachmentProviderInfo{{
						Name:   "roadmap",
						Source: "github.com/test/apps/roadmap",
						UI:     true,
						UIPath: "/roadmap",
					}},
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/team-a"+providerdev.PathAttachments+"/attach-1":
			writeJSONForProviderRemoteTest(t, w, http.StatusOK, providerdev.AttachmentInfo{
				AttachID:           "attach-1",
				CreatedAt:          now.Add(-time.Minute),
				LastSeenAt:         now,
				IdleTimeoutSeconds: 120,
				Providers: []providerdev.AttachmentProviderInfo{{
					Name:   "roadmap",
					Source: "github.com/test/apps/roadmap",
					UI:     true,
					UIPath: "/roadmap",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/team-a"+providerdev.PathAttachments+"/attach-1":
			sawDelete.Store(true)
			writeJSONForProviderRemoteTest(t, w, http.StatusOK, map[string]string{"status": "closed"})
		default:
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer ts.Close()
	remote := ts.URL + "/team-a"

	listOut, err := runProviderCommandResult("", "attach", "list", "--remote", remote, "--remote-token", "owner-token")
	if err != nil {
		t.Fatalf("provider attach list: %v\n%s", err, listOut)
	}
	for _, want := range []string{"ATTACH ID", "attach-1", "roadmap", "roadmap:/roadmap", "120s"} {
		if !strings.Contains(string(listOut), want) {
			t.Fatalf("list output missing %q:\n%s", want, listOut)
		}
	}

	showOut, err := runProviderCommandResult("", "attach", "show", "--remote", remote, "--remote-token", "owner-token", "attach-1")
	if err != nil {
		t.Fatalf("provider attach show: %v\n%s", err, showOut)
	}
	for _, want := range []string{"Attach ID: attach-1", "Providers:", "roadmap", "source=github.com/test/apps/roadmap", "ui=/roadmap"} {
		if !strings.Contains(string(showOut), want) {
			t.Fatalf("show output missing %q:\n%s", want, showOut)
		}
	}

	detachOut, err := runProviderCommandResult("", "attach", "detach", "--remote", remote, "--remote-token", "owner-token", "attach-1")
	if err != nil {
		t.Fatalf("provider attach detach: %v\n%s", err, detachOut)
	}
	if !strings.Contains(string(detachOut), "Detached provider-dev attachment attach-1") {
		t.Fatalf("unexpected detach output: %s", detachOut)
	}
	if !sawDelete.Load() {
		t.Fatal("expected owner detach request")
	}
}

func writeJSONForProviderRemoteTest(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode JSON response: %v", err)
	}
}

func tsURL(t *testing.T, r *http.Request) string {
	t.Helper()
	return "http://" + r.Host
}

func TestProviderRemoteBaseURLNormalizesDefaultPortsAndTrailingSlashes(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://Valon.Tools":       "https://valon.tools",
		"https://valon.tools:443/x": "https://valon.tools/x",
		"http://valon.tools:80":     "http://valon.tools",
		"http://127.0.0.1:8080/a":   "http://127.0.0.1:8080/a",
		"https://valon.tools/a///":  "https://valon.tools/a",
	}
	for input, want := range cases {
		input := input
		want := want
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			got, err := providerRemoteBaseURL(input)
			if err != nil {
				t.Fatalf("providerRemoteBaseURL(%q): %v", input, err)
			}
			if got != want {
				t.Fatalf("providerRemoteBaseURL(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestRun_ProviderCLIUsageAndErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		args      []string
		wantErr   bool
		wantParts []string
		notWant   []string
	}{
		{
			name:      "root help",
			args:      []string{"--help"},
			wantParts: []string{"gestaltd provider <command> [flags]", "attach", "list", "release"},
			notWant:   []string{"\n  install", "\n  inspect", "\n  init", "\n  package"},
		},
		{
			name:      "attach help",
			args:      []string{"attach", "--help"},
			wantParts: []string{"gestaltd provider attach <command> [flags]", "detach"},
		},
		{
			name:      "release help",
			args:      []string{"release", "--help"},
			wantParts: []string{"--version"},
		},
		{
			name:      "root defaults to help",
			args:      nil,
			wantParts: []string{"gestaltd provider <command> [flags]"},
		},
		{
			name:      "unknown subcommand",
			args:      []string{"bogus"},
			wantErr:   true,
			wantParts: []string{"unknown provider command", "bogus"},
		},
		{
			name:      "removed package subcommand",
			args:      []string{"package"},
			wantErr:   true,
			wantParts: []string{"unknown provider command", "package"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := runProviderCommandResult("", tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for provider %v, got output: %s", tc.args, out)
				}
			} else if err != nil {
				t.Fatalf("expected success for provider %v, got error: %v\noutput: %s", tc.args, err, out)
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(string(out), want) {
					t.Fatalf("expected output to contain %q, got: %s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(string(out), notWant) {
					t.Fatalf("expected %q absent from output, got: %s", notWant, out)
				}
			}
		})
	}
}

func TestRun_ProviderReleaseRequiresVersion(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	out, err := runProviderCommandResult(pluginDir, "release")
	if err == nil {
		t.Fatal("expected error when --version missing")
	}
	if !strings.Contains(string(out), "--version is required") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsInvalidManifest(t *testing.T) {
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

			out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.1-test")
			if err == nil {
				t.Fatal("expected invalid manifest error")
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestE2EProviderReleaseBigquery(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	bigqueryDir := filepath.Join(repoRoot, "plugins", "bigquery")
	if _, err := os.Stat(filepath.Join(bigqueryDir, "go.mod")); err != nil {
		t.Skipf("bigquery app not found: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.1-test"
	const testPlatform = "linux/amd64"

	cmd := exec.Command(gestaltdBin, "provider", "release",
		"--version", testVersion,
		"--platform", testPlatform,
		"--output", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err := cmd.CombinedOutput()
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

func TestRun_ProviderReleaseDefaultsSourcePluginToHostPlatform(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-default"

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseBuildsRequestedPlatformSets(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-all"

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseBuildsGoSourceAuthPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: authReleaseAppName,
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

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseBuildsGoSourceAuthorizationProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: authorizationReleaseAppName,
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

	runProviderReleaseCommand(t, pluginDir,
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
		Resource: &core.ResourceRef{Type: "app", Id: "github"},
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

func TestRun_ProviderReleaseBuildsGoSourceSecretsPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: secretsReleaseAppName,
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

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseBuildsGoSourceWorkflowPlugin(t *testing.T) {
	t.Parallel()

	const workflowReleaseAppName = "workflow-release"
	const workflowReleaseSource = "github.com/testowner/providers/workflow-release"
	const workflowReleaseSchemaPath = "workflow.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: workflowReleaseAppName,
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

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseBuildsGoSourceExternalCredentialsPlugin(t *testing.T) {
	t.Parallel()

	const externalCredentialReleaseAppName = "external-credentials-release"
	const externalCredentialReleaseSource = "github.com/testowner/providers/external-credentials-release"
	const externalCredentialReleaseSchemaPath = "external-credentials.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: externalCredentialReleaseAppName,
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

	runProviderReleaseCommand(t, pluginDir,
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
func TestRun_ProviderReleaseBuildsExecutableAuthProviders(t *testing.T) {
	goAuthFixture := func(t *testing.T) sourceComponentReleaseFixtureParams {
		t.Helper()
		return sourceComponentReleaseFixtureParams{
			pluginName: authReleaseAppName,
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
		pluginName          string
		version             string
		skipOnWindowsReason string
		prepare             func(t *testing.T) string
		archiveName         func(version string) string
		assertArtifact      func(t *testing.T, artifact providermanifestv1.Artifact)
		assertSessionTTL    bool
		assertExternalJWT   bool
	}{
		{
			name:       "go_source",
			pluginName: authReleaseAppName,
			version:    "0.0.15-test",
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
			runProviderReleaseCommand(t, pluginDir,
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

			assertExecutableAuthProviderWorks(t, filepath.Join(extractDir, binaryName), tc.pluginName, tc.assertSessionTTL, tc.assertExternalJWT)
		})
	}
}

func TestRun_ProviderReleaseCopiesCompiledSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.2-test"

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseCopiesUISupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.3-test"

	runProviderReleaseCommand(t, pluginDir,
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

func TestRun_ProviderReleaseStagesOwnedUIPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fixture       func(*testing.T, string) string
		wantFiles     []string
		wantAssetRoot string
		skipOnWin     bool
	}{
		{
			name:          "checked-in owned ui assets with build command",
			fixture:       newSourceProviderReleaseFixtureWithOwnedUI,
			wantFiles:     []string{"_owned_ui/roadmap-ui/branding/icon.svg", "_owned_ui/roadmap-ui/dist/index.html", "_owned_ui/roadmap-ui/dist/static/app.js"},
			wantAssetRoot: filepath.Join("_owned_ui", "roadmap-ui", "dist"),
		},
		{
			name:          "source-built owned ui assets",
			fixture:       newSourceProviderReleaseFixtureWithSourceBuiltOwnedUI,
			wantFiles:     []string{"_owned_ui/roadmap-ui/branding/icon.svg", "_owned_ui/roadmap-ui/ui/dist/index.html", "_owned_ui/roadmap-ui/ui/dist/static/app.js"},
			wantAssetRoot: filepath.Join("_owned_ui", "roadmap-ui", "ui", "dist"),
			skipOnWin:     true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("owned ui release-build fixture uses POSIX shell")
			}

			pluginDir := tc.fixture(t, t.TempDir())
			outputDir := t.TempDir()
			testVersion := "0.0.3-owned-ui"

			runProviderReleaseCommand(t, pluginDir,
				"--version", testVersion,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
			extractDir := extractReleasedArchive(t, outputDir, archiveName)
			manifest := readReleasedManifest(t, outputDir, archiveName)
			if manifest.Spec == nil || manifest.Spec.UI == nil {
				t.Fatalf("released manifest spec.ui = %+v", manifest.Spec)
			}
			const wantOwnedUIPath = "_owned_ui/roadmap-ui/manifest.json"
			if got := manifest.Spec.UI.Path; got != wantOwnedUIPath {
				t.Fatalf("spec.ui.path = %q, want %q", got, wantOwnedUIPath)
			}
			for _, rel := range append([]string{wantOwnedUIPath}, tc.wantFiles...) {
				if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("expected %s in archive: %v", rel, err)
				}
			}
			_, ownedUIManifest, err := providerpkg.ReadManifestFile(filepath.Join(extractDir, filepath.FromSlash(wantOwnedUIPath)))
			if err != nil {
				t.Fatalf("read owned ui manifest: %v", err)
			}
			if ownedUIManifest.Build != nil {
				t.Fatalf("owned ui manifest unexpectedly retained build metadata: %+v", ownedUIManifest.Build)
			}
			metadata := readProviderReleaseMetadata(t, outputDir)
			if metadata.Schema != providerReleaseSchemaName {
				t.Fatalf("release metadata schema = %q, want %q", metadata.Schema, providerReleaseSchemaName)
			}
			if metadata.SchemaVersion != providerReleaseSchemaVersion {
				t.Fatalf("release metadata schemaVersion = %d, want %d", metadata.SchemaVersion, providerReleaseSchemaVersion)
			}
			if metadata.Package != releaseTestSource {
				t.Fatalf("release metadata package = %q, want %q", metadata.Package, releaseTestSource)
			}
			if metadata.Kind != providermanifestv1.KindApp {
				t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
			}
			if metadata.Version != testVersion {
				t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
			}
			if metadata.Runtime != providerReleaseRuntimeKindExecutable {
				t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
			}
			if len(metadata.Artifacts) != 1 {
				t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
			}
			artifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
			if !ok {
				t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
			}
			if got := artifact.Path; got != archiveName {
				t.Fatalf("release metadata artifact path = %q, want %q", got, archiveName)
			}
			digest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
			if err != nil {
				t.Fatalf("hash archive: %v", err)
			}
			if got := artifact.SHA256; got != digest {
				t.Fatalf("release metadata artifact sha256 = %q, want %q", got, digest)
			}

			releaseServer := httptest.NewServer(http.FileServer(http.Dir(outputDir)))
			defer releaseServer.Close()

			configDir := t.TempDir()
			configPath := writeManagedPluginConfigForTest(t, configDir, "roadmap", releaseServer.URL+"/provider-release.yaml", "/create-customer-roadmap-review")
			lc := operator.NewLifecycle().WithHTTPClient(releaseServer.Client())
			if _, err := lc.PrepareAtPath(configPath); err != nil {
				t.Fatalf("PrepareAtPath: %v", err)
			}

			loaded, _, err := lc.LoadForExecutionAtPath(configPath, true)
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
			}
			app := loaded.Apps["roadmap"]
			if app == nil || app.ResolvedManifest == nil {
				t.Fatalf("ResolvedManifest = %+v", app)
			}
			if app.Command == "" {
				t.Fatalf("app.Command = %q, want packaged executable path", app.Command)
			}
			if got := app.ResolvedManifest.Version; got != testVersion {
				t.Fatalf("ResolvedManifest.Version = %q, want %q", got, testVersion)
			}

			uiEntry := loaded.Providers.UI["roadmap"]
			if uiEntry == nil || uiEntry.ResolvedManifest == nil {
				t.Fatalf("Resolved plugin-owned UI = %+v", uiEntry)
			}
			if uiEntry.Path != "/create-customer-roadmap-review" {
				t.Fatalf("uiEntry.Path = %q, want %q", uiEntry.Path, "/create-customer-roadmap-review")
			}
			if got := filepath.ToSlash(uiEntry.ResolvedManifestPath); !strings.HasSuffix(got, filepath.ToSlash(filepath.Join("_owned_ui", "roadmap-ui", providerpkg.ManifestFile))) {
				t.Fatalf("ResolvedManifestPath = %q, want owned-ui manifest suffix", got)
			}
			if got := filepath.ToSlash(uiEntry.ResolvedAssetRoot); !strings.HasSuffix(got, filepath.ToSlash(tc.wantAssetRoot)) {
				t.Fatalf("ResolvedAssetRoot = %q, want owned-ui asset root suffix %q", got, tc.wantAssetRoot)
			}

			lock, err := operator.ReadLockfile(filepath.Join(configDir, operator.LockfileName))
			if err != nil {
				t.Fatalf("ReadLockfile: %v", err)
			}
			if len(lock.UIs) != 0 {
				t.Fatalf("lock.UIs = %#v, want no separate UI entries for packaged owned UI", lock.UIs)
			}
		})
	}
}

func TestRun_ProviderReleaseBuildsProviderSupportFilesBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("release build fixture uses POSIX shell")
	}

	pluginDir := newBuiltSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-build-provider"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	_ = readReleasedManifest(t, outputDir, archiveName)
	if _, err := os.Stat(filepath.Join(extractDir, releaseProviderSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", releaseProviderSchemaPath, err)
	}
}

func TestRun_ProviderReleaseRejectsDeletedReleaseBuild(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("legacy release-build fixture uses POSIX shell")
	}

	pluginDir := newBuiltUIReleaseFixture(t, t.TempDir())
	out, err := runProviderReleaseCommandResult(pluginDir,
		"--version", "0.0.3-legacy-build-ui",
		"--output", t.TempDir(),
	)
	if err == nil {
		t.Fatalf("expected provider release to reject deleted release field\n%s", out)
	}
	if !strings.Contains(string(out), "unknown field") {
		t.Fatalf("expected deleted release field rejection, got: %s", out)
	}
}

func TestRun_ProviderReleaseBuildsSourceUIAssetsBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source build fixture uses POSIX shell")
	}

	pluginDir := newSourceBuiltUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-source-build-ui"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-test_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if manifest.Build != nil {
		t.Fatalf("released manifest unexpectedly retained build metadata: %+v", manifest.Build)
	}
	if manifest.Spec == nil || manifest.Spec.AssetRoot != "ui/out" {
		t.Fatalf("released manifest spec.assetRoot = %+v, want ui/out", manifest.Spec)
	}
	for _, rel := range []string{
		"branding/icon.svg",
		"ui/out/index.html",
		"ui/out/static/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestSourceUIHandlerBuildsSourceUIOutput(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source build fixture uses POSIX shell")
	}

	uiDir := newSourceBuiltUIReleaseFixture(t, t.TempDir())
	manifestPath := filepath.Join(uiDir, providerpkg.ManifestFile)

	handler, err := sourceUIHandler(manifestPath)
	if err != nil {
		t.Fatalf("sourceUIHandler: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uiDir, "ui", "out", "index.html")); err != nil {
		t.Fatalf("expected built index.html: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<html>") {
		t.Fatalf("GET / body = %q, want built html", rec.Body.String())
	}
}

func TestRun_ProviderReleaseAllowsOverlappingSupportPaths(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "ui-overlap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/apps/ui-overlap",
		Version:     "0.0.1",
		DisplayName: "UI Overlap",
		IconFile:    "out/icon.svg",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "out"},
	})
	writeTestFile(t, pluginDir, "out/icon.svg", []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, pluginDir, "out/index.html", []byte("<html></html>\n"), 0o644)
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p out\nprintf '<svg></svg>\\n' > out/icon.svg\nprintf '<html></html>\\n' > out/index.html\n"), 0o755)

	outputDir := t.TempDir()
	const testVersion = "0.0.3-overlap.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-overlap_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	for _, rel := range []string{"out/icon.svg", "out/index.html"} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderReleaseTreatsGoModWithoutProviderPackageAsDeclarative(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.4-test"

	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/ui-test\n\ngo 1.22\n"), 0644)

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-test_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected declarative archive %s to exist: %v", archiveName, err)
	}

	compiledArchiveName := "gestalt-app-ui-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, compiledArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected compiled archive %s: %v", compiledArchiveName, err)
	}
}

func TestRun_ProviderReleaseWritesProviderReleaseMetadataForDeclarativePlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-declarative.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + declarativeReleaseAppName + "_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != declarativeReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, declarativeReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindApp {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
	}
	if metadata.Version != testVersion {
		t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
	}
	if metadata.Runtime != providerReleaseRuntimeKindDeclarative {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindDeclarative)
	}
	if len(metadata.Artifacts) != 1 {
		t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
	}
	artifact, ok := metadata.Artifacts[providerReleaseGenericTarget]
	if !ok {
		t.Fatalf("release metadata artifacts missing generic key: %+v", metadata.Artifacts)
	}
	if got := artifact.Path; got != archiveName {
		t.Fatalf("release metadata artifact path = %q, want %q", got, archiveName)
	}
	digest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	if got := artifact.SHA256; got != digest {
		t.Fatalf("release metadata artifact sha256 = %q, want %q", got, digest)
	}
}

func TestRun_ProviderReleasePreservesYAMLManifestFormatAndConnectionDefaults(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/testowner/apps/provider-yaml",
		Version:     "0.0.1",
		DisplayName: "Provider YAML",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Mode: providermanifestv1.ConnectionModeUser,
					Params: map[string]providermanifestv1.ProviderConnectionParam{
						"tenant": {Required: true},
					},
				},
			},
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: ".gestalt/build/provider"},
	})
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove manifest.json: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.4-yaml.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-provider-yaml_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "manifest.yaml" {
		t.Fatalf("released manifest = %q, want manifest.yaml", filepath.Base(manifestPath))
	}
	if manifest.Spec == nil || manifest.Spec.Connections["default"] == nil || len(manifest.Spec.Connections["default"].Params) != 1 || !manifest.Spec.Connections["default"].Params["tenant"].Required {
		t.Fatalf("provider connection_params = %+v", manifest.Spec)
	}
	if manifest.Spec.Connections["default"].Mode != providermanifestv1.ConnectionModeSubject {
		t.Fatalf("provider default connection mode = %q, want %q", manifest.Spec.Connections["default"].Mode, providermanifestv1.ConnectionModeSubject)
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	for _, expected := range []string{
		"spec:",
		"connections:",
		"default:",
		"mode: subject",
		"params:",
		"mcp: true",
		"entrypoint:",
		"artifactPath:",
	} {
		if !strings.Contains(string(manifestData), expected) {
			t.Fatalf("expected released manifest to contain canonical field %q, got: %s", expected, manifestData)
		}
	}
	for _, unsupported := range []string{
		"connectionMode:",
		"connectionParams:",
	} {
		if strings.Contains(string(manifestData), unsupported) {
			t.Fatalf("expected released manifest to emit only canonical connection fields; found %q in: %s", unsupported, manifestData)
		}
	}
}

func TestRun_ProviderReleaseSupportsSourcePackageManifestFile(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove %s: %v", providerpkg.ManifestFile, err)
	}
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/testowner/apps/source-manifest",
		Version:     "0.0.1",
		DisplayName: "Source Manifest",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: ".gestalt/build/provider"},
	})

	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-source-manifest_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "manifest.yaml" {
		t.Fatalf("released manifest = %q, want manifest.yaml", filepath.Base(manifestPath))
	}
	if manifest.Source != "github.com/testowner/apps/source-manifest" {
		t.Fatalf("manifest source = %q, want %q", manifest.Source, "github.com/testowner/apps/source-manifest")
	}
}

func TestRun_ProviderReleaseChecksumsOnlyCurrentArchives(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	runProviderReleaseCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)
	runProviderReleaseCommand(t, pluginDir,
		"--version", "1.0.1",
		"--output", outputDir,
	)

	checksumPath := filepath.Join(outputDir, "checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if got := string(checksumData); strings.Contains(got, "gestalt-app-ui-test_v1.0.0.tar.gz") {
		t.Fatalf("checksums.txt unexpectedly included stale archive: %s", got)
	} else if !strings.Contains(got, "gestalt-app-ui-test_v1.0.1.tar.gz") {
		t.Fatalf("checksums.txt missing current archive: %s", got)
	}
}

func TestRun_ProviderReleaseRejectsOutputInsideUIAssetRoot(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixtureWithAssetRoot(t, t.TempDir(), "release-output")
	outputDir := filepath.Join(pluginDir, "release-output", "nested")

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "1.0.0", "--output", outputDir)
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), "must not be inside ui asset root") {
		t.Fatalf("expected overlap error, got: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsHybridExecutableDuplicateEffectiveOperation(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
	}
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestPath))
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "openapi.yaml"), []byte(`openapi: "3.1.0"
info:
  title: Hybrid Duplicate
  version: "1.0.0"
paths:
  /external-op:
    get:
      operationId: generated_op
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatalf("WriteFile openapi.yaml: %v", err)
	}
	writeTestFile(t, pluginDir, providerpkg.StaticCatalogFile, []byte("name: release-test\noperations:\n  - id: generated_op\n    method: GET\n"), 0o644)

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.4-source.1", "--platform", runtime.GOOS+"/"+runtime.GOARCH, "--output", t.TempDir())
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), `duplicate operation \"generated_op\" across merged catalogs`) {
		t.Fatalf("expected duplicate effective operation error, got: %s", out)
	}
}

func TestRun_ProviderReleaseCompilesProviderWithoutSourceArtifacts(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != ".gestalt/build/provider" {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != ".gestalt/build/provider" {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoint)
	}
	if manifest.Spec == nil || manifest.Spec.ConfigSchemaPath != releaseProviderSchemaPath {
		t.Fatalf("provider metadata = %#v, want config schema path %q", manifest.Spec, releaseProviderSchemaPath)
	}
	data, err := os.ReadFile(filepath.Join(extractDir, providerpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(data), "generated_op") {
		t.Fatalf("unexpected generated catalog: %s", data)
	}
}

func TestRun_ProviderReleaseCompilesSDKSourceProviderWithoutBuildCommand(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
	})
	_ = os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile))
	_ = os.Remove(filepath.Join(pluginDir, "build.sh"))
	_ = os.RemoveAll(filepath.Join(pluginDir, "cmd"))

	outputDir := t.TempDir()
	const testVersion = "0.0.4-sdk-source.1"
	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	wantBinary := "gestalt-app-" + releaseTestAppName
	if runtime.GOOS == "windows" {
		wantBinary += ".exe"
	}
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != wantBinary {
		t.Fatalf("artifacts = %+v, want one artifact at %q", manifest.Artifacts, wantBinary)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != wantBinary {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, wantBinary)
	}
	data, err := os.ReadFile(filepath.Join(extractDir, providerpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(data), "generated_op") {
		t.Fatalf("unexpected generated catalog: %s", data)
	}
}

func TestRun_ProviderReleaseRejectsPrebuiltExecutableProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	out, err := runProviderReleaseCommandResult(pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject executable provider without build.command\n%s", out)
	}
	if !strings.Contains(string(out), "provider release requires build.command for executable source providers") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsExplicitPlatformForPrebuiltProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	out, err := runProviderReleaseCommandResult(pluginDir,
		"--version", "0.0.5-platform-test",
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject explicit platform for prebuilt provider\n%s", out)
	}
	if !strings.Contains(string(out), "--platform requires build.command for executable source providers") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsGoModuleWithoutBuildCommand(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/prebuilt-provider\n\ngo 1.22\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.6-test"

	out, err := runProviderReleaseCommandResult(pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject executable provider without build.command\n%s", out)
	}
	if !strings.Contains(string(out), "provider release requires build.command for executable source providers") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseWindowsArtifactUsesExe(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.9-test"
	const windowsPlatform = "windows/amd64"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", windowsPlatform,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_windows_amd64.tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := ".gestalt/build/provider"

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, binaryName)); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}
}

func defaultReleasePlatformsForTest(t *testing.T) []releasePlatform {
	t.Helper()

	platforms, err := parseReleasePlatforms(defaultPlatforms)
	if err != nil {
		t.Fatalf("parseReleasePlatforms(defaultPlatforms): %v", err)
	}
	return platforms
}

func platformArchiveNameForTest(pluginName, version, goos, goarch string) string {
	return fmt.Sprintf("gestalt-app-%s_v%s_%s_%s.tar.gz", pluginName, version, goos, goarch)
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

func assertExecutableAuthProviderWorks(t *testing.T, command, providerName string, assertSessionTTL, assertExternalJWT bool) {
	t.Helper()

	auth, err := authenticationservice.NewExecutable(context.Background(), authenticationservice.ExecConfig{
		Command:     command,
		Name:        providerName,
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
	if assertSessionTTL {
		if ttlProvider, ok := auth.(interface{ SessionTokenTTL() time.Duration }); !ok || ttlProvider.SessionTokenTTL() != 90*time.Minute {
			t.Fatalf("SessionTokenTTL = %v", ttlProvider)
		}
	}
	if assertExternalJWT {
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
	if binding.Ack == nil {
		t.Fatal("binding.Ack = nil, want hosted HTTP ack metadata")
	}
	if binding.Ack.Status != http.StatusOK {
		t.Fatalf("binding.Ack.Status = %d, want %d", binding.Ack.Status, http.StatusOK)
	}
	body, ok := binding.Ack.Body.(map[string]any)
	if !ok {
		t.Fatalf("binding.Ack.Body type = %T, want map[string]any", binding.Ack.Body)
	}
	if got := body["status"]; got != "accepted" {
		t.Fatalf("binding.Ack.Body[status] = %#v, want %#v", got, "accepted")
	}
}

func assertReleasePlatforms(
	t *testing.T,
	outputDir string,
	platforms []releasePlatform,
	archiveName func(releasePlatform) string,
	assertPlatform func(*testing.T, providermanifestv1.Artifact, releasePlatform),
) {
	t.Helper()

	for _, platform := range platforms {
		manifest := readReleasedManifest(t, outputDir, archiveName(platform))
		if len(manifest.Artifacts) != 1 {
			t.Fatalf("artifacts for %s/%s = %+v, want one artifact", platform.GOOS, platform.GOARCH, manifest.Artifacts)
		}
		assertPlatform(t, manifest.Artifacts[0], platform)
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

	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "indexeddb"))
	artifactFullPath := filepath.Join(dir, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("mkdir indexeddb artifact dir: %v", err)
	}
	artifactContent := []byte("indexeddb-stub-binary")
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("write indexeddb artifact: %v", err)
	}
	manifestPath := filepath.Join(dir, "indexeddb-manifest.yaml")
	data, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:     "github.com/test/providers/indexeddb-stub",
		Version:    "0.0.1-alpha.1",
		Kind:       providermanifestv1.KindIndexedDB,
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
		Spec:       &providermanifestv1.Spec{},
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
	pluginName string
	schemaPath string
	sourceFile string
	sourceCode string
	manifest   *providermanifestv1.Manifest
}

func newSourceComponentReleaseFixture(t *testing.T, dir string, p sourceComponentReleaseFixtureParams) string {
	t.Helper()

	pluginDir := filepath.Join(dir, p.pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/"+p.pluginName)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, pluginDir, p.sourceFile, []byte(p.sourceCode), 0o644)
	artifactRel := ".gestalt/build/provider"
	writeGoComponentBuildFixture(t, pluginDir, "example.com/"+p.pluginName, p.manifest.Kind, artifactRel)
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
				Ack: &providermanifestv1.HTTPAck{
					Status: 200,
					Body: map[string]any{
						"status": "accepted",
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
	artifactRel := ".gestalt/build/provider"
	writeGoPluginBuildFixture(t, pluginDir, releaseTestModule, releaseTestAppName, artifactRel)
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
	buildScript := "mkdir -p .gestalt/build\ngo build -o .gestalt/build/provider ./cmd/provider\nmkdir -p schemas\nprintf '{\"type\":\"object\"}\\n' > " + releaseProviderSchemaPath + "\n"
	writeTestFile(t, pluginDir, "build.sh", []byte(buildScript), 0o755)
	return pluginDir
}

func newGoSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestAppName)
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	artifactRel := ".gestalt/build/provider"
	writeGoPluginBuildFixture(t, pluginDir, "github.com/valon-technologies/gestalt/testdata/provider-go", releaseTestAppName, artifactRel)
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

func runProviderReleaseCommand(t *testing.T, pluginDir string, args ...string) string {
	t.Helper()

	out, err := runProviderReleaseCommandResult(pluginDir, args...)
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}
	return string(out)
}

func runProviderCommandResult(pluginDir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"provider"}, args...)
	cmd := exec.Command(gestaltdBin, cmdArgs...)
	cmd.Dir = pluginDir
	return cmd.CombinedOutput()
}

func runProviderReleaseCommandResult(pluginDir string, args ...string) ([]byte, error) {
	return runProviderCommandResult(pluginDir, append([]string{"release"}, args...)...)
}

func extractReleasedArchive(t *testing.T, outputDir, archiveName string) string {
	t.Helper()

	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}
	extractDir := filepath.Join(outputDir, strings.TrimSuffix(archiveName, ".tar.gz"))
	if info, err := os.Stat(extractDir); err == nil {
		if !info.IsDir() {
			t.Fatalf("extract path %s exists and is not a directory", extractDir)
		}
		return extractDir
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat extract dir %s: %v", extractDir, err)
	}
	if err := providerpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	return extractDir
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

func readProviderReleaseMetadata(t *testing.T, outputDir string) *providerReleaseMetadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(outputDir, providerReleaseMetadataFile))
	if err != nil {
		t.Fatalf("read %s: %v", providerReleaseMetadataFile, err)
	}
	var metadata providerReleaseMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode %s: %v", providerReleaseMetadataFile, err)
	}
	return &metadata
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
	return providerpkg.EncodeSourceManifestFormat(manifest, format)
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

func writeStoredGestaltCLICredentialForTest(t *testing.T, configHome, apiURL, apiToken string) {
	t.Helper()

	data, err := json.Marshal(map[string]string{
		"api_url":      apiURL,
		"api_token":    apiToken,
		"api_token_id": "tok-123",
	})
	if err != nil {
		t.Fatalf("Marshal stored CLI credential: %v", err)
	}
	writeTestFile(t, configHome, "gestalt/credentials.json", data, 0o600)
}

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
