package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
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

func TestProviderRemoteConfigPathSynthesizesSourceApp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	configPaths, cleanup, err := prepareProviderRemoteConfigPaths(providerLocalCommandOptions{Path: appDir})
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

	listOut, err := runProviderCommandResult(t, "", "attach", "list", "--remote", remote, "--remote-token", "owner-token")
	if err != nil {
		t.Fatalf("provider attach list: %v\n%s", err, listOut)
	}
	for _, want := range []string{"ATTACH ID", "attach-1", "roadmap", "roadmap:/roadmap", "120s"} {
		if !strings.Contains(string(listOut), want) {
			t.Fatalf("list output missing %q:\n%s", want, listOut)
		}
	}

	showOut, err := runProviderCommandResult(t, "", "attach", "show", "--remote", remote, "--remote-token", "owner-token", "attach-1")
	if err != nil {
		t.Fatalf("provider attach show: %v\n%s", err, showOut)
	}
	for _, want := range []string{"Attach ID: attach-1", "Providers:", "roadmap", "source=github.com/test/apps/roadmap", "ui=/roadmap"} {
		if !strings.Contains(string(showOut), want) {
			t.Fatalf("show output missing %q:\n%s", want, showOut)
		}
	}

	detachOut, err := runProviderCommandResult(t, "", "attach", "detach", "--remote", remote, "--remote-token", "owner-token", "attach-1")
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
			wantParts: []string{"gestaltd provider <command> [flags]", "attach", "list", "package", "release"},
			notWant:   []string{"\n  install", "\n  inspect", "\n  init"},
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
			name:      "package help",
			args:      []string{"package", "--help"},
			wantParts: []string{"gestaltd provider package", "--platform"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := runProviderCommandResult(t, "", tc.args...)
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
