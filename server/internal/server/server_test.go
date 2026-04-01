package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/drivers/bindings/proxy"
	"github.com/valon-technologies/gestalt/server/internal/drivers/bindings/webhook"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

func newTestServer(t *testing.T, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	cfg := server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "none"},
		Datastore: &coretesting.StubDatastore{},
		Providers: func() *registry.PluginMap[core.Provider] {
			reg := registry.New()
			return &reg.Providers
		}(),
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	brokerOpts := []invocation.BrokerOption{}
	if cfg.DefaultConnection != nil {
		brokerOpts = append(brokerOpts, invocation.WithConnectionMapper(invocation.ConnectionMap(cfg.DefaultConnection)))
	}
	if cfg.ConnectionAuth != nil {
		authFn := cfg.ConnectionAuth
		brokerOpts = append(brokerOpts, invocation.WithConnectionAuth(func() map[string]map[string]invocation.OAuthRefresher {
			m := authFn()
			refreshers := make(map[string]map[string]invocation.OAuthRefresher, len(m))
			for intg, conns := range m {
				inner := make(map[string]invocation.OAuthRefresher, len(conns))
				for conn, h := range conns {
					inner[conn] = h
				}
				refreshers[intg] = inner
			}
			return refreshers
		}))
	}
	if cfg.Invoker == nil {
		cfg.Invoker = invocation.NewBroker(cfg.Providers, cfg.Datastore, brokerOpts...)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	return httptest.NewServer(srv)
}

// testOAuthHandler adapts a test stub into bootstrap.OAuthHandler for use in
// server tests. Only the methods actually exercised by each test need non-nil
// implementations.
type testOAuthHandler struct {
	authorizationURLFn       func(state string, scopes []string) string
	startOAuthFn             func(state string, scopes []string) (string, string)
	startOAuthWithOverrideFn func(authBaseURL, state string, scopes []string) (string, string)
	exchangeCodeFn           func(ctx context.Context, code string) (*core.TokenResponse, error)
	exchangeCodeWithVerFn    func(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	refreshTokenFn           func(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	refreshTokenWithURLFn    func(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	authorizationBaseURLVal  string
	tokenURLVal              string
}

func (h *testOAuthHandler) AuthorizationURL(state string, scopes []string) string {
	if h.authorizationURLFn != nil {
		return h.authorizationURLFn(state, scopes)
	}
	url, _ := h.StartOAuth(state, scopes)
	return url
}

func (h *testOAuthHandler) StartOAuth(state string, scopes []string) (string, string) {
	if h.startOAuthFn != nil {
		return h.startOAuthFn(state, scopes)
	}
	return h.authorizationBaseURLVal + "?state=" + state, ""
}

func (h *testOAuthHandler) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	if h.startOAuthWithOverrideFn != nil {
		return h.startOAuthWithOverrideFn(authBaseURL, state, scopes)
	}
	return authBaseURL + "?state=" + state, ""
}

func (h *testOAuthHandler) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	if h.exchangeCodeFn != nil {
		return h.exchangeCodeFn(ctx, code)
	}
	return nil, fmt.Errorf("ExchangeCode not implemented")
}

func (h *testOAuthHandler) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, opts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	if h.exchangeCodeWithVerFn != nil {
		return h.exchangeCodeWithVerFn(ctx, code, verifier, opts...)
	}
	return h.ExchangeCode(ctx, code)
}

func (h *testOAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if h.refreshTokenFn != nil {
		return h.refreshTokenFn(ctx, refreshToken)
	}
	return nil, fmt.Errorf("RefreshToken not implemented")
}

func (h *testOAuthHandler) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	if h.refreshTokenWithURLFn != nil {
		return h.refreshTokenWithURLFn(ctx, refreshToken, tokenURL)
	}
	return h.RefreshToken(ctx, refreshToken)
}

func (h *testOAuthHandler) AuthorizationBaseURL() string { return h.authorizationBaseURLVal }
func (h *testOAuthHandler) TokenURL() string             { return h.tokenURLVal }

const (
	testDefaultConnection = "default"
	testCatalogConnection = "catalog"
	testCatalogToken      = "catalog-token"
)

func testConnectionAuth(integration string, handler bootstrap.OAuthHandler) func() map[string]map[string]bootstrap.OAuthHandler {
	m := map[string]map[string]bootstrap.OAuthHandler{
		integration: {testDefaultConnection: handler},
	}
	return func() map[string]map[string]bootstrap.OAuthHandler { return m }
}

func oauthRefreshConnectionAuth(integration string, refreshFn func(context.Context, string) (*core.TokenResponse, error)) func() map[string]map[string]bootstrap.OAuthHandler {
	return testConnectionAuth(integration, &testOAuthHandler{refreshTokenFn: refreshFn})
}

func TestNewServerRequiresStateSecretWithAuth(t *testing.T) {
	t.Parallel()
	ds := &coretesting.StubDatastore{}
	providers := func() *registry.PluginMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	_, err := server.New(server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "google"},
		Datastore: ds,
		Providers: providers,
		Invoker:   invocation.NewBroker(providers, ds),
	})
	if err == nil {
		t.Fatal("expected error when auth is enabled without state secret")
	}
	if !strings.Contains(err.Error(), "state secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthCheck(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
		if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
		}
		if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q, want empty (secureCookies=false)", got)
		}
	})

	t.Run("secure_cookies", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.SecureCookies = true
		})
		testutil.CloseOnCleanup(t, ts)

		resp, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
		if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
		}
		const wantHSTS = "max-age=63072000; includeSubDomains"
		if got := resp.Header.Get("Strict-Transport-Security"); got != wantHSTS {
			t.Errorf("Strict-Transport-Security = %q, want %q", got, wantHSTS)
		}
	})
}

func TestReadinessCheck(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestReadinessCheck_NotReady(t *testing.T) {
	t.Parallel()
	var ready atomic.Bool
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Readiness = func() string {
			if !ready.Load() {
				return "providers loading"
			}
			return ""
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 while not ready, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "providers loading" {
		t.Fatalf("expected status 'providers loading', got %q", body["status"])
	}

	ready.Store(true)

	resp2, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready after ready: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after ready, got %d", resp2.StatusCode)
	}
}

func TestReadinessCheck_DatastoreDown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Readiness = func() string {
			return "datastore unavailable"
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "datastore unavailable" {
		t.Fatalf("expected status 'datastore unavailable', got %q", body["status"])
	}
}

func TestAuthMiddleware_ValidSession(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token == "valid-session" {
					return &core.UserIdentity{Email: "user@example.com"}, nil
				}
				return nil, fmt.Errorf("invalid token")
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer valid-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_ValidAPIToken(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "u1", Name: "test-key"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "user@example.com", DisplayName: "Test User"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestAuthMiddleware_UnprefixedTokenRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, _ string) (*core.APIToken, error) {
				return &core.APIToken{UserID: "u1", Name: "legacy-key"}, nil
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "legacy@example.test"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer unprefixed-legacy-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unprefixed token, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_PrefixedAPITokenSkipsOAuth(t *testing.T) {
	t.Parallel()

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				t.Fatal("OAuth ValidateToken must not be called for prefixed API tokens")
				return nil, nil
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "u1", Name: "test-key"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "api@example.test"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListIntegrations(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Connected   bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].Name != "slack" {
		t.Fatalf("expected slack, got %q", integrations[0].Name)
	}
	if integrations[0].DisplayName != "Slack" {
		t.Fatalf("expected display name Slack, got %q", integrations[0].DisplayName)
	}
	if integrations[0].Connected {
		t.Fatal("expected connected=false when no tokens stored")
	}
}

func TestListIntegrationsShowsConnected(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			ListTokensFn: func(_ context.Context, userID string) ([]*core.IntegrationToken, error) {
				return []*core.IntegrationToken{
					{UserID: userID, Integration: "slack", Instance: "default", AccessToken: "tok"},
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if !integrations[0].Connected {
		t.Fatal("expected connected=true when token exists")
	}
}

func TestListIntegrations_AuthTypes(t *testing.T) {
	t.Parallel()

	oauthStub := &coretesting.StubIntegration{N: "oauth-svc", DN: "OAuth Service"}
	manualStub := &stubManualProvider{
		StubIntegration: coretesting.StubIntegration{N: "manual-svc", DN: "Manual Service"},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, oauthStub, manualStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string   `json:"name"`
		AuthTypes []string `json:"auth_types"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(integrations))
	}

	authTypes := make(map[string][]string)
	for _, i := range integrations {
		authTypes[i.Name] = i.AuthTypes
	}
	if len(authTypes["manual-svc"]) != 1 || authTypes["manual-svc"][0] != "manual" {
		t.Fatalf("expected manual-svc auth_types=[manual], got %v", authTypes["manual-svc"])
	}
	if len(authTypes["oauth-svc"]) != 1 || authTypes["oauth-svc"][0] != "oauth" {
		t.Fatalf("expected oauth-svc auth_types=[oauth], got %v", authTypes["oauth-svc"])
	}
}

func TestListIntegrationsWithIcon(t *testing.T) {
	t.Parallel()

	const testSVG = `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`
	def := &provider.Definition{
		Provider:    "iconprov",
		DisplayName: "Icon Provider",
		Description: "Has an icon",
		IconSVG:     testSVG,
		BaseURL:     "https://api.example.com",
		Auth:        provider.AuthDef{Type: "manual"},
		Operations: map[string]provider.OperationDef{
			"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
		},
	}
	prov, err := provider.Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name    string `json:"name"`
		IconSVG string `json:"icon_svg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(integrations))
	}
	if integrations[0].IconSVG != testSVG {
		t.Fatalf("icon_svg = %q, want %q", integrations[0].IconSVG, testSVG)
	}
}

func TestListIntegrations_ShowsConnectedStatus(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	stub2 := &coretesting.StubIntegration{N: "github", DN: "GitHub"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub, stub2)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
				return &core.User{ID: "u1", Email: "dev@example.com"}, nil
			},
			ListTokensFn: func(_ context.Context, userID string) ([]*core.IntegrationToken, error) {
				if userID == "u1" {
					return []*core.IntegrationToken{
						{ID: "tok-1", UserID: "u1", Integration: "slack"},
					}, nil
				}
				return nil, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var integrations []struct {
		Name      string `json:"name"`
		Connected bool   `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(integrations) != 2 {
		t.Fatalf("expected 2 integrations, got %d", len(integrations))
	}

	connected := make(map[string]bool)
	for _, i := range integrations {
		connected[i.Name] = i.Connected
	}
	if !connected["slack"] {
		t.Fatal("expected slack to be connected")
	}
	if connected["github"] {
		t.Fatal("expected github to be disconnected")
	}
}

func TestListIntegrations_FindOrCreateUserError(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "test-integ", DN: "Test"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
				return nil, fmt.Errorf("database unavailable")
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestListIntegrations_ListTokensError(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "test-integ", DN: "Test"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			ListTokensFn: func(_ context.Context, _ string) ([]*core.IntegrationToken, error) {
				return nil, fmt.Errorf("token store unavailable")
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestDisconnectIntegration(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	var deletedID string
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
				return &core.User{ID: "u1", Email: "dev@example.com"}, nil
			},
			ListTokensFn: func(_ context.Context, _ string) ([]*core.IntegrationToken, error) {
				return []*core.IntegrationToken{
					{ID: "tok-1", UserID: "u1", Integration: "slack", Instance: "default"},
				}, nil
			},
			ListTokensForIntegrationFn: func(_ context.Context, _, _ string) ([]*core.IntegrationToken, error) {
				return []*core.IntegrationToken{
					{ID: "tok-1", UserID: "u1", Integration: "slack", Instance: "default"},
				}, nil
			},
			DeleteTokenFn: func(_ context.Context, id string) error {
				deletedID = id
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if deletedID != "tok-1" {
		t.Fatalf("expected token tok-1 to be deleted, got %q", deletedID)
	}
}

func TestDisconnectIntegration_NotConnected(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
				return &core.User{ID: "u1", Email: "dev@example.com"}, nil
			},
			ListTokensFn: func(_ context.Context, _ string) ([]*core.IntegrationToken, error) {
				return nil, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/integrations/slack", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListOperations(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int"},
		},
		catalog: &catalog.Catalog{
			Name: "test-int",
			Operations: []catalog.CatalogOperation{
				{ID: "do_thing", Description: "Do a thing", Method: http.MethodGet, Path: "/do-thing"},
			},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if _, ok := ops[0]["id"]; !ok {
		t.Fatal("expected camelCase 'id' key in response")
	}
	if _, ok := ops[0]["Name"]; ok {
		t.Fatal("expected camelCase keys, found PascalCase 'Name'")
	}
	if ops[0]["id"] != "do_thing" {
		t.Fatalf("expected id 'do_thing', got %v", ops[0]["id"])
	}
}

func TestListOperations_UsesCatalogConnectionOverride(t *testing.T) {
	t.Parallel()

	var gotConnection string
	stub := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int"},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != testCatalogToken {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{
				Name: "test-int",
				Operations: []catalog.CatalogOperation{
					{ID: "session_only", Description: "Session-only op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
				},
			}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"test-int": testDefaultConnection}
		cfg.CatalogConnection = map[string]string{"test-int": testCatalogConnection}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, integration, connection, _ string) (*core.IntegrationToken, error) {
				if integration != "test-int" {
					return nil, fmt.Errorf("unexpected integration %q", integration)
				}
				gotConnection = connection
				return &core.IntegrationToken{AccessToken: testCatalogToken}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ops []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ops); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0]["id"] != "session_only" {
		t.Fatalf("expected id 'session_only', got %v", ops[0]["id"])
	}
	if gotConnection != testCatalogConnection {
		t.Fatalf("connection = %q, want %q", gotConnection, testCatalogConnection)
	}
}

func TestListOperations_NotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/nonexistent/operations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"operation":%q}`, op),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
			{Name: "create_thing", Description: "Create a thing", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "stored-token"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing?foo=bar", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["operation"] != "do_thing" {
		t.Fatalf("expected operation do_thing, got %q", body["operation"])
	}
}

func TestExecuteOperation_UsesInjectedInvoker(t *testing.T) {
	t.Parallel()

	var called bool
	var gotProvider string
	var gotOperation string
	var gotParams map[string]any

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Invoker = &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, p *principal.Principal, providerName, _, operation string, params map[string]any) (*core.OperationResult, error) {
				called = true
				gotProvider = providerName
				gotOperation = operation
				gotParams = params
				if p == nil || p.Identity == nil || p.Identity.Email == "" {
					t.Fatal("expected authenticated principal")
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		}
	})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/custom-provider/custom-operation", bytes.NewBufferString(`{"foo":"bar"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !called {
		t.Fatal("expected injected invoker to be called")
	}
	if gotProvider != "custom-provider" {
		t.Fatalf("expected provider custom-provider, got %q", gotProvider)
	}
	if gotOperation != "custom-operation" {
		t.Fatalf("expected operation custom-operation, got %q", gotOperation)
	}
	if gotParams["foo"] != "bar" {
		t.Fatalf("expected params to include foo=bar, got %v", gotParams)
	}
}

func TestExecuteOperation_UnknownIntegration(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/nonexistent/some_op", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation_UnknownOperation(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation_NoStoredToken(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return nil, core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", resp.StatusCode)
	}
}

func TestStartLogin(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithLoginURL{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
			loginURL:         "https://auth.example.com/login?state=abc",
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"state":"abc"}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["url"] != "https://auth.example.com/login?state=abc" {
		t.Fatalf("unexpected url: %q", result["url"])
	}
}

func TestStartLoginWithCallbackPort(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithLoginURL{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		loginURL:         "https://auth.example.com/login",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"state":"abc","callback_port":12345}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if stub.capturedState != "cli:12345:abc" {
		t.Fatalf("expected state 'cli:12345:abc', got %q", stub.capturedState)
	}
}

func TestStartLoginWithInvalidCallbackPort(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithLoginURL{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		loginURL:         "https://auth.example.com/login",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"state":"abc","callback_port":99999}`)
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if stub.capturedState != "abc" {
		t.Fatalf("expected state 'abc', got %q", stub.capturedState)
	}
}

func TestLoginCallback(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{
				N: "test",
				HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
					if code == "good-code" {
						return &core.UserIdentity{Email: "user@example.com", DisplayName: "User"}, nil
					}
					return nil, fmt.Errorf("bad code")
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["email"] != "user@example.com" {
		t.Fatalf("unexpected email: %v", result["email"])
	}
}

func TestLoginCallbackForCLI(t *testing.T) {
	t.Parallel()

	var stored *core.APIToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			HandleCallbackFn: func(_ context.Context, code string) (*core.UserIdentity, error) {
				if code == "good-code" {
					return &core.UserIdentity{Email: "user@example.com", DisplayName: "User"}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			StoreAPITokenFn: func(_ context.Context, token *core.APIToken) error {
				stored = token
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state&cli=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["id"] == "" {
		t.Fatal("expected id in CLI login response")
	}
	if result["token"] == "" {
		t.Fatal("expected token in CLI login response")
	}
	if result["name"] != "cli-token" {
		t.Fatalf("expected cli-token name in CLI login response, got %v", result["name"])
	}

	if stored == nil {
		t.Fatal("expected API token to be stored")
	}
	if stored.Name != "cli-token" {
		t.Fatalf("expected cli token name, got %q", stored.Name)
	}
	if stored.ExpiresAt != nil {
		t.Fatalf("expected non-expiring CLI token, got %v", stored.ExpiresAt)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_token" {
			t.Fatalf("did not expect session cookie for CLI login, got %q", cookie.Value)
		}
	}
}

func TestLoginCallbackStateMismatch(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"correct-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=wrong-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallbackMissingStateCookie(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=anything")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallbackExpiredState(t *testing.T) {
	t.Parallel()

	nowVal := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Now = func() time.Time { return nowVal }
		cfg.Auth = &stubAuthWithToken{
			StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"test-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	nowVal = nowVal.Add(11 * time.Minute)

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestLoginCallbackWithStatefulHandler(t *testing.T) {
	t.Parallel()

	stub := &stubStatefulAuth{
		StubAuthProvider: coretesting.StubAuthProvider{N: "test"},
		handleWithState: func(_ context.Context, code, state string) (*core.UserIdentity, string, error) {
			if code == "good-code" && state == "encrypted-state" {
				return &core.UserIdentity{Email: "pkce@example.com"}, "original-state", nil
			}
			return nil, "", fmt.Errorf("bad code or state")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	body := bytes.NewBufferString(`{"state":"original-state"}`)
	loginResp, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = loginResp.Body.Close()

	resp, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=encrypted-state")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["email"] != "pkce@example.com" {
		t.Fatalf("unexpected email: %v", result["email"])
	}
}

func TestStartIntegrationOAuth(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		authURL:         "https://slack.com/oauth/v2/authorize",
	}

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","scopes":["channels:read"]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("Authorization", "Bearer ignored")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["url"] == "" {
		t.Fatal("expected non-empty url")
	}
	if result["state"] == "" {
		t.Fatal("expected non-empty state")
	}
	parsedURL, err := url.Parse(result["url"])
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if parsedURL.Query().Get("state") != result["state"] {
		t.Fatal("expected auth URL state to match returned state")
	}
}

func TestIntegrationOAuthCallback(t *testing.T) {
	t.Parallel()

	var stored *core.IntegrationToken

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			if code == "good-code" {
				return &core.TokenResponse{AccessToken: "slack-token"}, nil
			}
			return nil, fmt.Errorf("bad code")
		},
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		authURL:         "https://slack.com/oauth/v2/authorize",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				stored = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"slack"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()

	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
	}

	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decoding start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/integrations?connected=slack" {
		t.Fatalf("expected redirect to /integrations?connected=slack, got %q", loc)
	}
	if stored == nil {
		t.Fatal("expected token to be stored")
	}
	if stored.UserID != "u1" {
		t.Fatalf("stored token user ID = %q, want %q", stored.UserID, "u1")
	}
	if stored.Integration != "slack" {
		t.Fatalf("stored token integration = %q, want %q", stored.Integration, "slack")
	}
	if stored.AccessToken != "slack-token" {
		t.Fatalf("stored access token = %q, want %q", stored.AccessToken, "slack-token")
	}
}

func TestIntegrationOAuthCallback_InvalidState(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack"}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state=not-valid", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["error"] == "" {
		t.Fatal("expected error response")
	}
}

func TestCreateAndListAPITokens(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"my-token"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["token"] == "" {
		t.Fatal("expected non-empty token in response")
	}
	if result["name"] != "my-token" {
		t.Fatalf("expected name my-token, got %q", result["name"])
	}
}

func TestRevokeAPIToken(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			RevokeAPITokenFn: func(_ context.Context, userID, id string) error {
				if userID == "u1" && id == "tok-123" {
					return nil
				}
				return core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/tok-123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "revoked" {
		t.Fatalf("expected revoked, got %q", result["status"])
	}
}

func TestRevokeAPIToken_WrongUser(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				if email == "owner@example.com" {
					return &core.User{ID: "u-owner", Email: email}, nil
				}
				return &core.User{ID: "u-other", Email: email}, nil
			},
			RevokeAPITokenFn: func(_ context.Context, userID, id string) error {
				if userID == "u-owner" {
					return nil
				}
				return core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens/tok-owned-by-a", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
}

func TestCreateAPIToken_DefaultExpiry(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Now = func() time.Time { return fixedNow }
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"expiry-test"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	expiresAtRaw, ok := result["expires_at"]
	if !ok || expiresAtRaw == nil {
		t.Fatal("expected expires_at in response, got nil")
	}
	expiresAtStr, ok := expiresAtRaw.(string)
	if !ok {
		t.Fatalf("expected expires_at to be a string, got %T", expiresAtRaw)
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		t.Fatalf("parsing expires_at: %v", err)
	}
	expected := fixedNow.Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expires_at %v, got %v", expected, expiresAt)
	}
}

func TestCreateAPIToken_ConfigurableTTL(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	customTTL := 7 * 24 * time.Hour

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Now = func() time.Time { return fixedNow }
		cfg.APITokenTTL = customTTL
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"ttl-test"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	expiresAtStr, ok := result["expires_at"].(string)
	if !ok {
		t.Fatal("expected expires_at string in response")
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		t.Fatalf("parsing expires_at: %v", err)
	}
	expected := fixedNow.Add(customTTL).UTC().Truncate(time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expires_at %v, got %v", expected, expiresAt)
	}
}

func TestRevokeAllAPITokens(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			RevokeAllAPITokensFn: func(_ context.Context, userID string) (int64, error) {
				if userID == "u1" {
					return 3, nil
				}
				return 0, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "revoked" {
		t.Fatalf("expected status revoked, got %q", result["status"])
	}
	if count, ok := result["count"].(float64); !ok || count != 3 {
		t.Fatalf("expected count 3, got %v", result["count"])
	}
}

func TestRevokeAllAPITokens_NoneExist(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/tokens", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if count, ok := result["count"].(float64); !ok || count != 0 {
		t.Fatalf("expected count 0, got %v", result["count"])
	}
}

func TestExecuteOperation_POST(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				text, _ := params["text"].(string)
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"text":%q}`, text),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "send", Description: "Send", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"text":"hello"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/send", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["text"] != "hello" {
		t.Fatalf("expected hello, got %q", result["text"])
	}
}

func TestAuthInfo(t *testing.T) {
	t.Parallel()

	stub := &stubAuthWithDisplayName{
		StubAuthProvider: coretesting.StubAuthProvider{N: "google"},
		displayName:      "Google",
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/info")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["provider"] != "google" {
		t.Fatalf("expected provider google, got %q", body["provider"])
	}
	if body["display_name"] != "Google" {
		t.Fatalf("expected display_name Google, got %q", body["display_name"])
	}
}

func TestAuthInfoFallback(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "custom"}
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/auth/info")
	if err != nil {
		t.Fatalf("GET /api/v1/auth/info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["provider"] != "custom" {
		t.Fatalf("expected provider custom, got %q", body["provider"])
	}
	if body["display_name"] != "custom" {
		t.Fatalf("expected display_name to fall back to name custom, got %q", body["display_name"])
	}
}

type stubAuthWithDisplayName struct {
	coretesting.StubAuthProvider
	displayName string
}

func (s *stubAuthWithDisplayName) DisplayName() string {
	return s.displayName
}

type stubIntegrationWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubIntegrationWithOps) Catalog() *catalog.Catalog {
	return serverTestCatalogFromOperations(s.N, s.ops)
}

type stubIntegrationWithSessionCatalog struct {
	stubIntegrationWithOps
	catalog             *catalog.Catalog
	catalogForRequestFn func(context.Context, string) (*catalog.Catalog, error)
}

func (s *stubIntegrationWithSessionCatalog) Catalog() *catalog.Catalog {
	return s.catalog
}

func (s *stubIntegrationWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if s.catalogForRequestFn != nil {
		return s.catalogForRequestFn(ctx, token)
	}
	return s.catalog, nil
}

type stubAuthWithLoginURL struct {
	coretesting.StubAuthProvider
	loginURL      string
	capturedState string
}

func (s *stubAuthWithLoginURL) LoginURL(state string) (string, error) {
	s.capturedState = state
	return s.loginURL, nil
}

type stubIntegrationWithAuthURL struct {
	coretesting.StubIntegration
	authURL string
}

func (s *stubIntegrationWithAuthURL) AuthorizationURL(_ string, _ []string) string {
	return s.authURL
}

type stubPKCEIntegration struct {
	coretesting.StubIntegration
	authURL      string
	wantVerifier string
	gotVerifier  string
}

func (s *stubPKCEIntegration) AuthorizationURL(state string, _ []string) string {
	return s.authURL + "?state=" + url.QueryEscape(state)
}

func (s *stubPKCEIntegration) StartOAuth(state string, _ []string) (string, string) {
	return s.AuthorizationURL(state, nil), s.wantVerifier
}

func (s *stubPKCEIntegration) ExchangeCodeWithVerifier(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	s.gotVerifier = verifier
	if code != "good-code" {
		return nil, fmt.Errorf("bad code")
	}
	return &core.TokenResponse{AccessToken: "pkce-token"}, nil
}

func TestIntegrationOAuthCallback_PKCEUsesVerifier(t *testing.T) {
	t.Parallel()

	stub := &stubPKCEIntegration{
		StubIntegration: coretesting.StubIntegration{N: "gitlab"},
		authURL:         "https://gitlab.com/oauth/authorize",
		wantVerifier:    "verifier-123",
	}

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://gitlab.com/oauth/authorize",
		startOAuthFn: func(state string, _ []string) (string, string) {
			return "https://gitlab.com/oauth/authorize?state=" + state, "verifier-123"
		},
		exchangeCodeWithVerFn: func(_ context.Context, code, verifier string, _ ...oauth.ExchangeOption) (*core.TokenResponse, error) {
			stub.gotVerifier = verifier
			if code != "good-code" {
				return nil, fmt.Errorf("bad code")
			}
			return &core.TokenResponse{AccessToken: "pkce-token"}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"gitlab": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("gitlab", handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"gitlab"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()

	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
	}

	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decoding start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if stub.gotVerifier != stub.wantVerifier {
		t.Fatalf("got verifier %q, want %q", stub.gotVerifier, stub.wantVerifier)
	}
}

func TestCallbackPathConstants(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	// Auth login callback: should not 404 (it will return 400 for missing code,
	// which proves the route exists).
	resp, err := http.Get(ts.URL + config.AuthCallbackPath)
	if err != nil {
		t.Fatalf("GET %s: %v", config.AuthCallbackPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("config.AuthCallbackPath %q is not a registered route (got 404)", config.AuthCallbackPath)
	}

	// Integration callback: should be public and return 400 for missing params,
	// which proves the route exists without auth middleware.
	resp, err = http.Get(ts.URL + config.IntegrationCallbackPath)
	if err != nil {
		t.Fatalf("GET %s: %v", config.IntegrationCallbackPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("config.IntegrationCallbackPath %q is not a registered route (got 404)", config.IntegrationCallbackPath)
	}
}

type stubOAuthIntegration struct {
	stubIntegrationWithOps
	refreshTokenFn func(context.Context, string) (*core.TokenResponse, error)
}

func (s *stubOAuthIntegration) RefreshToken(ctx context.Context, token string) (*core.TokenResponse, error) {
	if s.refreshTokenFn != nil {
		return s.refreshTokenFn(ctx, token)
	}
	return nil, nil
}

// stubNonOAuthProvider implements core.Provider but NOT core.OAuthProvider.
type stubNonOAuthProvider struct {
	name   string
	ops    []core.Operation
	execFn func(context.Context, string, map[string]any, string) (*core.OperationResult, error)
}

func (s *stubNonOAuthProvider) Name() string                        { return s.name }
func (s *stubNonOAuthProvider) DisplayName() string                 { return s.name }
func (s *stubNonOAuthProvider) Description() string                 { return "" }
func (s *stubNonOAuthProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (s *stubNonOAuthProvider) Catalog() *catalog.Catalog {
	return serverTestCatalogFromOperations(s.name, s.ops)
}
func (s *stubNonOAuthProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.execFn != nil {
		return s.execFn(ctx, op, params, token)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
}

func serverTestCatalogFromOperations(name string, ops []core.Operation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(ops)),
	}
	for _, op := range ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
			Parameters:  params,
			Transport:   catalog.TransportREST,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func TestExecuteOperation_RefreshesExpiredToken(t *testing.T) {
	t.Parallel()

	var refreshedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					refreshedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, rt string) (*core.TokenResponse, error) {
			if rt == "old-refresh-token" {
				return &core.TokenResponse{AccessToken: "fresh-access-token", ExpiresIn: 3600}, nil
			}
			return nil, fmt.Errorf("unexpected refresh token")
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "stale-access-token",
					RefreshToken: "old-refresh-token",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				storedToken = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if refreshedToken != "fresh-access-token" {
		t.Fatalf("expected operation to use refreshed token, got %q", refreshedToken)
	}
	if storedToken == nil {
		t.Fatal("expected token to be persisted after refresh")
	}
	if storedToken.AccessToken != "fresh-access-token" {
		t.Fatalf("expected stored access token to be updated, got %q", storedToken.AccessToken)
	}
	if storedToken.RefreshErrorCount != 0 {
		t.Fatalf("expected refresh error count to be 0, got %d", storedToken.RefreshErrorCount)
	}
	if storedToken.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set after refresh")
	}
}

func TestExecuteOperation_RefreshFailsButTokenStillValid(t *testing.T) {
	t.Parallel()

	var usedToken string
	var storedToken *core.IntegrationToken
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	// Token expires in 3 minutes (within threshold) but still valid
	expiresInThree := time.Now().Add(3 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "still-valid-token",
					RefreshToken: "rf",
					ExpiresAt:    &expiresInThree,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				storedToken = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (graceful degradation), got %d", resp.StatusCode)
	}
	if usedToken != "still-valid-token" {
		t.Fatalf("expected operation to use old token, got %q", usedToken)
	}
	if storedToken == nil {
		t.Fatal("expected token to be persisted after refresh error")
	}
	if storedToken.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set on refresh error path")
	}
}

func TestExecuteOperation_RefreshFailsAndTokenExpired(t *testing.T) {
	t.Parallel()

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "fake"},
			ops:             []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("refresh token revoked")
		},
	}

	alreadyExpired := time.Now().Add(-10 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "expired-token",
					RefreshToken: "rf",
					ExpiresAt:    &alreadyExpired,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 for expired token + failed refresh, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation_NoRefreshTokenSkipsRefresh(t *testing.T) {
	t.Parallel()

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			t.Fatal("RefreshToken should not be called when no refresh token stored")
			return nil, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken: "no-refresh-token",
					ExpiresAt:   &expiresSoon,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if usedToken != "no-refresh-token" {
		t.Fatalf("expected original token, got %q", usedToken)
	}
}

func TestExecuteOperation_NoExpiresAtSkipsRefresh(t *testing.T) {
	t.Parallel()

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			t.Fatal("RefreshToken should not be called when no expiry info")
			return nil, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "no-expiry-token",
					RefreshToken: "rf",
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if usedToken != "no-expiry-token" {
		t.Fatalf("expected original token, got %q", usedToken)
	}
}

func TestExecuteOperation_NonOAuthProviderSkipsRefresh(t *testing.T) {
	t.Parallel()

	var usedToken string
	stub := &stubNonOAuthProvider{
		name: "manual-api",
		ops:  []core.Operation{{Name: "get", Description: "Get", Method: http.MethodGet}},
		execFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
			usedToken = token
			return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "manual-token",
					RefreshToken: "rf",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/manual-api/get", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if usedToken != "manual-token" {
		t.Fatalf("expected original token, got %q", usedToken)
	}
}

func TestExecuteOperation_RefreshTokenRotation(t *testing.T) {
	t.Parallel()

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			return &core.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "rotated-refresh",
				ExpiresIn:    7200,
			}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "old-access",
					RefreshToken: "old-refresh",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				storedToken = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if storedToken == nil {
		t.Fatal("expected token to be persisted")
	}
	if storedToken.RefreshToken != "rotated-refresh" {
		t.Fatalf("expected rotated refresh token, got %q", storedToken.RefreshToken)
	}
	if storedToken.AccessToken != "new-access" {
		t.Fatalf("expected new access token, got %q", storedToken.AccessToken)
	}
}

func TestExecuteOperation_RefreshClearsExpiresAtWhenOmitted(t *testing.T) {
	t.Parallel()

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			return &core.TokenResponse{AccessToken: "new-access", ExpiresIn: 0}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "old-access",
					RefreshToken: "rf",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				storedToken = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if storedToken == nil {
		t.Fatal("expected token to be persisted")
	}
	if storedToken.AccessToken != "new-access" {
		t.Fatalf("expected new access token, got %q", storedToken.AccessToken)
	}
	if storedToken.ExpiresAt != nil {
		t.Fatalf("expected ExpiresAt to be nil when provider omits expires_in, got %v", *storedToken.ExpiresAt)
	}
}

func TestExecuteOperation_RefreshErrorSkipsStoreOnConcurrentRefresh(t *testing.T) {
	t.Parallel()

	var usedToken string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					usedToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	expiresSoon := time.Now().Add(3 * time.Minute)
	tokenCallCount := 0
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				tokenCallCount++
				if tokenCallCount == 1 {
					return &core.IntegrationToken{
						AccessToken:  "stale-token",
						RefreshToken: "rf",
						ExpiresAt:    &expiresSoon,
					}, nil
				}
				// Simulate concurrent refresh: DB now has a fresh token.
				return &core.IntegrationToken{
					AccessToken:  "concurrently-refreshed-token",
					RefreshToken: "new-rf",
				}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				storedToken = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if usedToken != "concurrently-refreshed-token" {
		t.Fatalf("expected concurrently refreshed token, got %q", usedToken)
	}
	if storedToken != nil {
		t.Fatal("expected StoreToken not to be called when concurrent refresh detected")
	}
}

func TestExecuteOperation_StoreTokenFailureReturnsError(t *testing.T) {
	t.Parallel()

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "fake"},
			ops:             []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			return &core.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "rotated-refresh",
				ExpiresIn:    3600,
			}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "old-access",
					RefreshToken: "old-refresh",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, _ *core.IntegrationToken) error {
				return fmt.Errorf("database unavailable")
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when StoreToken fails after refresh, got %d", resp.StatusCode)
	}
}

func TestExecuteOperation_RefreshErrorHandlesDeletedToken(t *testing.T) {
	t.Parallel()

	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	expiresSoon := time.Now().Add(3 * time.Minute)
	tokenCallCount := 0
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				tokenCallCount++
				if tokenCallCount == 1 {
					return &core.IntegrationToken{
						AccessToken:  "stale-token",
						RefreshToken: "rf",
						ExpiresAt:    &expiresSoon,
					}, nil
				}
				// Token was deleted between reads.
				return nil, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should gracefully degrade (token still valid) instead of panicking.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (graceful degradation), got %d", resp.StatusCode)
	}
}

type stubStatefulAuth struct {
	coretesting.StubAuthProvider
	handleWithState func(context.Context, string, string) (*core.UserIdentity, string, error)
}

func (s *stubStatefulAuth) HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error) {
	return s.handleWithState(ctx, code, state)
}

func (s *stubStatefulAuth) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return "session-token-" + identity.Email, nil
}

func TestExecuteOperation_ConnectionModeNone(t *testing.T) {
	t.Parallel()

	tokenCalled := false
	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "noop",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				if token != "" {
					t.Errorf("expected empty token for ConnectionModeNone, got %q", token)
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{
			{Name: "ping", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				tokenCalled = true
				return nil, core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/noop/ping", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if tokenCalled {
		t.Fatal("datastore.Token should not be called for ConnectionModeNone")
	}
}

func TestExecuteOperation_EchoProvider(t *testing.T) {
	t.Parallel()

	echoProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "echo",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, params map[string]any, _ string) (*core.OperationResult, error) {
				body, _ := json.Marshal(params)
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		ops: []core.Operation{
			{Name: "echo", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, echoProvider)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"message":"hello"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/echo/echo", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["message"] != "hello" {
		t.Fatalf("expected message hello, got %v", result["message"])
	}
}

func TestExecuteOperation_HTTPAndMCPEquivalent(t *testing.T) {
	t.Parallel()

	echoProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "echo",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
				body, _ := json.Marshal(map[string]any{
					"op":    op,
					"query": params["q"],
					"token": token,
				})
				return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil
			},
		},
		ops: []core.Operation{{Name: "search", Method: http.MethodGet}},
	}

	providers := testutil.NewProviderRegistry(t, echoProvider)
	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
	}

	httpSrv := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Datastore = ds
	})
	defer httpSrv.Close()

	httpReq, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/echo/search?q=hello", nil)
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", httpResp.StatusCode)
	}
	var httpBody map[string]any
	if err := json.NewDecoder(httpResp.Body).Decode(&httpBody); err != nil {
		t.Fatalf("decode HTTP body: %v", err)
	}

	invoker := invocation.NewBroker(providers, ds)
	mcpSrv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
	})
	tool := mcpSrv.GetTool("echo_search")
	if tool == nil {
		t.Fatal("expected echo_search tool")
	}

	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		Identity: &core.UserIdentity{Email: "dev@example.com"},
		UserID:   "u1",
		Source:   principal.SourceSession,
	})
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "echo_search"
	req.Params.Arguments = map[string]any{"q": "hello"}

	mcpResult, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("MCP tool call: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected MCP error result: %v", mcpResult.Content)
	}
	if len(mcpResult.Content) != 1 {
		t.Fatalf("expected one MCP content item, got %d", len(mcpResult.Content))
	}
	text, ok := mcpgo.AsTextContent(mcpResult.Content[0])
	if !ok {
		t.Fatalf("expected MCP text content, got %T", mcpResult.Content[0])
	}

	httpJSON, _ := json.Marshal(httpBody)
	if text.Text != string(httpJSON) {
		t.Fatalf("expected MCP body %s to match HTTP body %s", text.Text, string(httpJSON))
	}
}

func TestListRuntimes_NoRuntimes(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/runtimes", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}
}

func TestListRuntimes_WithRuntimes(t *testing.T) {
	t.Parallel()

	runtimes := registry.NewRuntimeMap()
	if err := runtimes.Register("echo-1", &coretesting.StubRuntime{N: "echo-1"}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Runtimes = runtimes
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/runtimes", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(names) != 1 || names[0] != "echo-1" {
		t.Fatalf("expected [echo-1], got %v", names)
	}
}

type stubManualProvider struct {
	coretesting.StubIntegration
}

func (s *stubManualProvider) SupportsManualAuth() bool { return true }

func TestConnectManual(t *testing.T) {
	t.Parallel()

	var stored *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
		})
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				stored = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["status"] != "connected" {
		t.Fatalf("expected connected, got %q", result["status"])
	}
	if stored == nil {
		t.Fatal("expected StoreToken to be called")
	}
	if stored.UserID != "u1" {
		t.Fatalf("expected user u1, got %q", stored.UserID)
	}
	if stored.Integration != "manual-svc" {
		t.Fatalf("expected integration manual-svc, got %q", stored.Integration)
	}
	if stored.AccessToken != "my-api-key" {
		t.Fatalf("expected credential my-api-key, got %q", stored.AccessToken)
	}
}

func TestConnectManual_OAuthProviderRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack"})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","credential":"some-key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectManual_MissingFields(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectManual_UnknownIntegration(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"nonexistent","credential":"key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStartOAuth_ManualProviderRejected(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
		})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"manual-svc","scopes":[]}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestStartOAuth_MultiConnection_SelectsByConnectionName(t *testing.T) {
	t.Parallel()

	connAHandler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth/a",
	}
	connBHandler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth/b",
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth/a",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"multi": "conn-a"}
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"multi": {
					"conn-a": connAHandler,
					"conn-b": connBHandler,
				},
			}
		}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"multi","connection":"conn-b"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, bodyBytes)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(result["url"], "provider.example/oauth/b") {
		t.Fatalf("expected conn-b auth URL, got %q", result["url"])
	}
}

func TestStartOAuth_MissingConnection_FailsCleanly(t *testing.T) {
	t.Parallel()

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth",
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "myint"},
		authURL:         "https://provider.example/oauth",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"myint": "conn-a"}
		cfg.ConnectionAuth = testConnectionAuth("myint", handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"myint","connection":"nonexistent"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(result["error"], "nonexistent") {
		t.Fatalf("expected error to mention missing connection, got %q", result["error"])
	}
}

func TestOAuthCallback_UsesStateConnection(t *testing.T) {
	t.Parallel()

	var exchangedConnection string
	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://provider.example/oauth",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			exchangedConnection = "conn-b"
			return &core.TokenResponse{AccessToken: "token-for-b"}, nil
		},
	}

	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{N: "multi"},
		authURL:         "https://provider.example/oauth",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"multi": "conn-a"}
		cfg.ConnectionAuth = func() map[string]map[string]bootstrap.OAuthHandler {
			return map[string]map[string]bootstrap.OAuthHandler{
				"multi": {
					"conn-a": &testOAuthHandler{authorizationBaseURLVal: "https://provider.example/oauth/a"},
					"conn-b": handler,
				},
			}
		}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			StoreTokenFn: func(_ context.Context, _ *core.IntegrationToken) error {
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"multi","connection":"conn-b"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("X-Dev-User-Email", "dev@example.com")
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
	}
	var startResult map[string]string
	if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
		t.Fatalf("decoding start response: %v", err)
	}

	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=ok&state="+url.QueryEscape(startResult["state"]), nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", resp.StatusCode)
	}
	if exchangedConnection != "conn-b" {
		t.Fatalf("expected conn-b handler to be used for exchange, got %q", exchangedConnection)
	}
}

func TestRefresh_UsesConnectionAuth(t *testing.T) {
	t.Parallel()

	var refreshedVia string
	stub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, rt string) (*core.TokenResponse, error) {
			refreshedVia = "connection-handler"
			return &core.TokenResponse{AccessToken: "refreshed-token", ExpiresIn: 3600}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", stub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "stale",
					RefreshToken: "rf",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
			StoreTokenFn: func(_ context.Context, _ *core.IntegrationToken) error {
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/fake/list", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if refreshedVia != "connection-handler" {
		t.Fatalf("expected refresh via connection handler, got %q", refreshedVia)
	}
}

func TestListBindings_NoBindings(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}
}

func TestListBindings_WithBindings(t *testing.T) {
	t.Parallel()

	bindings := registry.NewBindingMap()
	if err := bindings.Register("my-webhook", &coretesting.StubBinding{
		N: "my-webhook",
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(names) != 1 || names[0] != "my-webhook" {
		t.Fatalf("expected [my-webhook], got %v", names)
	}
}

func TestBindingRoutesMounted(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"binding":"reached"}`))
	})

	bindings := registry.NewBindingMap()
	if err := bindings.Register("my-webhook", &coretesting.StubBinding{
		N: "my-webhook",
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/incoming", Handler: handler, Public: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"test":true}`)
	resp, err := http.Post(ts.URL+"/api/v1/bindings/my-webhook/incoming", "application/json", body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["binding"] != "reached" {
		t.Fatalf("expected binding handler to be reached, got %v", result)
	}
}

func TestSurfaceBindingRequiresAuth(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	bindings := registry.NewBindingMap()
	if err := bindings.Register("test-surface", &coretesting.StubBinding{
		N: "test-surface",
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/invoke", Handler: handler},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Post(ts.URL+"/api/v1/bindings/test-surface/invoke", "application/json", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTriggerBindingRemainsPublic(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	bindings := registry.NewBindingMap()
	if err := bindings.Register("test-trigger", &coretesting.StubBinding{
		N: "test-trigger",
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/incoming", Handler: handler, Public: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Post(ts.URL+"/api/v1/bindings/test-trigger/incoming", "application/json", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWebhookBindingRemainsPublic(t *testing.T) {
	t.Parallel()

	bindings := registry.NewBindingMap()
	if err := bindings.Register("webhook-public", newTestWebhookBinding(t, "webhook-public", "/incoming")); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Post(ts.URL+"/api/v1/bindings/webhook-public/incoming", "application/json", bytes.NewBufferString(`{"ok":true}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestProxyBindingRequiresAuth(t *testing.T) {
	t.Parallel()

	bindings := registry.NewBindingMap()
	if err := bindings.Register("test-proxy", newTestProxyBinding(t, "test-proxy", "/proxy")); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings/test-proxy/proxy/messages", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("expected 407, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Proxy-Authenticate") != "Bearer" {
		t.Fatalf("expected Proxy-Authenticate: Bearer header, got %q", resp.Header.Get("Proxy-Authenticate"))
	}
}

func TestProxyBindingRoutesMountedAsPrefix(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"method": r.Method,
			"body":   string(body),
		})
	}))
	t.Cleanup(upstream.Close)

	upstreamHost := upstream.Listener.Addr().String()

	cases := []struct {
		name           string
		configuredPath string
		nestedURL      string
		exactURL       string
		wantPath       string
	}{
		{
			name:           "agent-proxy",
			configuredPath: "/proxy",
			nestedURL:      "/api/v1/bindings/agent-proxy/proxy/messages?cursor=123",
			exactURL:       "/api/v1/bindings/agent-proxy/proxy",
			wantPath:       "/messages",
		},
		{
			name:           "api-proxy",
			configuredPath: "/api",
			nestedURL:      "/api/v1/bindings/api-proxy/api/messages?cursor=123",
			exactURL:       "/api/v1/bindings/api-proxy/api",
			wantPath:       "/messages",
		},
		{
			name:           "root-proxy",
			configuredPath: "/",
			nestedURL:      "/api/v1/bindings/root-proxy/messages?cursor=123",
			exactURL:       "/api/v1/bindings/root-proxy/",
			wantPath:       "/messages",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			binding := newTestProxyBinding(t, tc.name, tc.configuredPath)
			bindings := registry.NewBindingMap()
			if err := bindings.Register(tc.name, binding); err != nil {
				t.Fatal(err)
			}

			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Bindings = bindings
				cfg.Datastore = &coretesting.StubDatastore{
					FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
						return &core.User{ID: "u1", Email: email}, nil
					},
				}
			})
			testutil.CloseOnCleanup(t, ts)

			req, err := http.NewRequest(http.MethodPost, ts.URL+tc.nestedURL, bytes.NewBufferString("hello"))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "text/plain")
			req.Header.Set("X-Forwarded-Host", upstreamHost)
			req.Header.Set("X-Forwarded-Proto", "http")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			if resp.Header.Get("Content-Type") != "application/json" {
				_ = resp.Body.Close()
				t.Fatalf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
			}

			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			_ = resp.Body.Close()

			if result["path"] != tc.wantPath {
				t.Fatalf("upstream path = %q, want %q", result["path"], tc.wantPath)
			}
			if result["query"] != "cursor=123" {
				t.Fatalf("upstream query = %q, want cursor=123", result["query"])
			}
			if result["method"] != http.MethodPost {
				t.Fatalf("upstream method = %q, want POST", result["method"])
			}
			if result["body"] != "hello" {
				t.Fatalf("upstream body = %q, want hello", result["body"])
			}

			exactReq, err := http.NewRequest(http.MethodGet, ts.URL+tc.exactURL, nil)
			if err != nil {
				t.Fatalf("new exact request: %v", err)
			}
			exactReq.Header.Set("X-Forwarded-Host", upstreamHost)
			exactReq.Header.Set("X-Forwarded-Proto", "http")

			resp, err = http.DefaultClient.Do(exactReq)
			if err != nil {
				t.Fatalf("exact request: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				t.Fatalf("expected exact route to return 200, got %d", resp.StatusCode)
			}

			var exact map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&exact); err != nil {
				t.Fatalf("decoding exact route: %v", err)
			}
			_ = resp.Body.Close()
			if exact["path"] != "/" {
				t.Fatalf("exact upstream path = %q, want /", exact["path"])
			}
		})
	}
}

func newTestProxyBinding(t *testing.T, name, path string) core.Binding {
	t.Helper()

	cfgYAML := "path: " + path
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatalf("unmarshal proxy config: %v", err)
	}

	binding, err := proxy.Factory(context.Background(), name, config.BindingDef{
		Type:      "proxy",
		Providers: []string{"test-provider"},
		Config:    *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("proxy factory: %v", err)
	}
	return binding
}

func newTestWebhookBinding(t *testing.T, name, path string) core.Binding {
	t.Helper()

	cfgYAML := "path: " + path
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatalf("unmarshal webhook config: %v", err)
	}

	binding, err := webhook.Factory(context.Background(), name, config.BindingDef{
		Type:   "webhook",
		Config: *node.Content[0],
	}, bootstrap.BindingDeps{Invoker: &testutil.StubInvoker{}})
	if err != nil {
		t.Fatalf("webhook factory: %v", err)
	}
	return binding
}

func newMCPHandler(t *testing.T, providers *registry.PluginMap[core.Provider], ds core.Datastore) http.Handler {
	t.Helper()
	broker := invocation.NewBroker(providers, ds)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})
	return mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
}

func mcpJSONRPC(t *testing.T, ts *httptest.Server, headers map[string]string, body map[string]any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decoding MCP response: %v\nbody: %s", err, raw)
		}
	}
	return resp.StatusCode, result
}

func TestMCPEndpoint_InitializeAndListTools(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "linear"},
		ops: []core.Operation{
			{Name: "search_issues", Description: "Search issues", Method: http.MethodGet},
		},
	}
	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
	}
	providers := testutil.NewProviderRegistry(t, stub)
	mcpHandler := newMCPHandler(t, providers, ds)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Datastore = ds
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	status, resp := mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: expected result object, got %v", resp)
	}
	if result["serverInfo"] == nil {
		t.Fatal("initialize: missing serverInfo")
	}

	status, resp = mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result object, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list: expected non-empty tools, got %v", result)
	}
	firstTool := tools[0].(map[string]any)
	if firstTool["name"] != "linear_search_issues" {
		t.Fatalf("expected tool linear_search_issues, got %v", firstTool["name"])
	}
}

func TestMCPEndpoint_RequiresAuth(t *testing.T) {
	t.Parallel()

	providers := func() *registry.PluginMap[core.Provider] {
		reg := registry.New()
		return &reg.Providers
	}()
	ds := &coretesting.StubDatastore{}
	mcpHandler := newMCPHandler(t, providers, ds)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

type mcpPassthroughProvider struct {
	coretesting.StubIntegration
	ops    []core.Operation
	catVal *catalog.Catalog
	callFn func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (p *mcpPassthroughProvider) Catalog() *catalog.Catalog { return p.catVal }
func (p *mcpPassthroughProvider) SupportsManualAuth() bool  { return true }
func (p *mcpPassthroughProvider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if p.callFn != nil {
		return p.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("passthrough:" + name), nil
}

func TestMCPEndpoint_DirectPassthrough(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "Execute a SQL query",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
			},
		},
	}

	var calledName string
	prov := &mcpPassthroughProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
		ops:             []core.Operation{{Name: "run_query", Description: "Execute a SQL query"}},
		catVal:          cat,
		callFn: func(_ context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			return mcpgo.NewToolResultText("query executed"), nil
		},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
	}
	providers := testutil.NewProviderRegistry(t, prov)
	mcpHandler := newMCPHandler(t, providers, ds)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Datastore = ds
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})

	status, resp := mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d", status)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: expected result, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools, got %v", result)
	}
	firstTool := tools[0].(map[string]any)
	if firstTool["name"] != "clickhouse_run_query" {
		t.Fatalf("expected clickhouse_run_query, got %v", firstTool["name"])
	}

	status, resp = mcpJSONRPC(t, ts, nil, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "clickhouse_run_query",
			"arguments": map[string]any{"sql": "SELECT 1"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call: expected 200, got %d", status)
	}
	result, ok = resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call: expected result, got %v", resp)
	}
	if calledName != "run_query" {
		t.Fatalf("expected direct CallTool with run_query, got %q", calledName)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content in result, got %v", result)
	}
	textBlock := content[0].(map[string]any)
	if textBlock["text"] != "query executed" {
		t.Fatalf("expected passthrough result, got %v", textBlock)
	}
}

func TestMCPEndpoint_NotMountedWhenDisabled(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404/405 when MCP not enabled, got %d", resp.StatusCode)
	}
}

func TestMaxBodySize(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodPost},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	largeBody := bytes.NewReader(bytes.Repeat([]byte("A"), (1<<20)+1))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/do_thing", largeBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}

func TestErrorSanitization(t *testing.T) {
	t.Parallel()

	sensitiveMsg := "secret-internal-db-password-leaked"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("upstream broke: %s", sensitiveMsg)
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveMsg) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "operation failed" {
		t.Fatalf("expected generic error message, got %q", errResp["error"])
	}
}

func TestUpstreamHTTPErrorPassthrough(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid parameter: limit",
			},
		})
	}))
	testutil.CloseOnCleanup(t, upstream)

	prov, err := provider.Build(&provider.Definition{
		Provider:         "test-int",
		DisplayName:      "Test Integration",
		BaseURL:          upstream.URL,
		ConnectionMode:   "none",
		Auth:             provider.AuthDef{Type: "manual"},
		ErrorMessagePath: "error.message",
		Operations: map[string]provider.OperationDef{
			"do_thing": {Description: "Do a thing", Method: http.MethodGet, Path: "/do_thing"},
		},
	}, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), `"operation failed"`) {
		t.Fatalf("expected upstream body, got generic error: %s", body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding upstream body: %v", err)
	}
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested error object, got %v", decoded)
	}
	if errObj["message"] != "invalid parameter: limit" {
		t.Fatalf("message = %v, want %q", errObj["message"], "invalid parameter: limit")
	}
}

func TestExecuteOperation_UserFacingErrorMessage(t *testing.T) {
	t.Parallel()

	sensitiveMsg := "postgres://user:secret@example.internal/db"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("%w: request failed: %s", apiexec.ErrUpstreamTimedOut, sensitiveMsg)
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveMsg) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "upstream service timed out" {
		t.Fatalf("expected user-facing message, got %q", errResp["error"])
	}
}

func TestExecuteOperation_WrappedOperationErrorMessage(t *testing.T) {
	t.Parallel()

	sensitiveContext := "postgres://user:secret@example.internal/db"
	publicMessage := "invalid parameter: limit"
	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("graphql request failed against %s: %w", sensitiveContext, &apiexec.UpstreamOperationError{
					Message: publicMessage,
				})
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), sensitiveContext) {
		t.Fatalf("response body contains sensitive error details: %s", body)
	}

	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != publicMessage {
		t.Fatalf("expected wrapped operation message, got %q", errResp["error"])
	}
}

func TestExecuteOperation_RuntimeUnavailableMessage(t *testing.T) {
	t.Parallel()

	fullStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, grpcstatus.Error(codes.Unavailable, "dial tcp 10.0.0.15: connection refused")
			},
		},
		ops: []core.Operation{
			{Name: "do_thing", Description: "Do a thing", Method: http.MethodGet},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var errResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp["error"] != "integration runtime unavailable" {
		t.Fatalf("expected runtime unavailable message, got %q", errResp["error"])
	}
}

type stubAuthWithToken struct {
	coretesting.StubAuthProvider
}

func (s *stubAuthWithToken) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return "dev-token-" + identity.Email, nil
}

func (s *stubAuthWithToken) SessionTokenTTL() time.Duration {
	return time.Hour
}

func TestCookieAuth(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token == "valid-cookie-token" {
				return &core.UserIdentity{Email: "cookie@test.local"}, nil
			}
			return nil, fmt.Errorf("invalid token")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = stub
	})
	testutil.CloseOnCleanup(t, ts)

	// Request without cookie should be rejected.
	reqNoCookie, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	noAuthResp, err := http.DefaultClient.Do(reqNoCookie)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = noAuthResp.Body.Close() }()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", noAuthResp.StatusCode)
	}

	// Request with cookie should pass auth middleware.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "valid-cookie-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("cookie auth should have passed middleware, got 401")
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == "session_token" {
			found = true
			if c.MaxAge != -1 {
				t.Fatalf("expected MaxAge -1, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Fatal("expected session_token cookie to be cleared")
	}
}

func newTestProxyBindingWithEgress(t *testing.T, name, path string, deps bootstrap.BindingDeps) core.Binding {
	t.Helper()

	cfgYAML := "path: " + path
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatalf("unmarshal proxy config: %v", err)
	}

	binding, err := proxy.Factory(context.Background(), name, config.BindingDef{
		Type:      "proxy",
		Providers: []string{"test-provider"},
		Config:    *node.Content[0],
	}, deps)
	if err != nil {
		t.Fatalf("proxy factory: %v", err)
	}
	return binding
}

func TestProxyBinding_StaticPolicyDenyBlocksRequest(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	resolver := &egress.Resolver{
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyAllow,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyDeny,
					MatchCriteria: egress.MatchCriteria{PathPrefix: "/v1/admin"},
				},
			},
		},
	}

	binding := newTestProxyBindingWithEgress(t, "policy-proxy", "/", bootstrap.BindingDeps{
		Egress: bootstrap.EgressDeps{Resolver: resolver},
	})
	bindings := registry.NewBindingMap()
	if err := bindings.Register("policy-proxy", binding); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	doReq := func(path string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings/policy-proxy"+path, nil)
		req.Header.Set("X-Forwarded-Host", upstream.Listener.Addr().String())
		req.Header.Set("X-Forwarded-Proto", "http")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if status := doReq("/v1/admin/users"); status != http.StatusBadRequest {
		t.Fatalf("denied path: expected 400, got %d", status)
	}
	if status := doReq("/v1/public/items"); status != http.StatusOK {
		t.Fatalf("allowed path: expected 200, got %d", status)
	}
}

func TestProxyBinding_StaticPolicyDefaultDenyBlocksUnmatched(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	resolver := &egress.Resolver{
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyDeny,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyAllow,
					MatchCriteria: egress.MatchCriteria{PathPrefix: "/v1/allowed"},
				},
			},
		},
	}

	binding := newTestProxyBindingWithEgress(t, "deny-proxy", "/", bootstrap.BindingDeps{
		Egress: bootstrap.EgressDeps{Resolver: resolver},
	})
	bindings := registry.NewBindingMap()
	if err := bindings.Register("deny-proxy", binding); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	doReq := func(path string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings/deny-proxy"+path, nil)
		req.Header.Set("X-Forwarded-Host", upstream.Listener.Addr().String())
		req.Header.Set("X-Forwarded-Proto", "http")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if status := doReq("/v1/allowed/items"); status != http.StatusOK {
		t.Fatalf("allowed path: expected 200, got %d", status)
	}
	if status := doReq("/v1/other/items"); status != http.StatusBadRequest {
		t.Fatalf("unmatched path: expected 400, got %d", status)
	}
}

func TestProxyBinding_StaticPolicyFirstMatchWins(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	resolver := &egress.Resolver{
		Policy: egress.StaticPolicyEnforcer{
			DefaultAction: egress.PolicyAllow,
			Rules: []egress.StaticPolicyRule{
				{
					Action:        egress.PolicyDeny,
					MatchCriteria: egress.MatchCriteria{PathPrefix: "/v1/admin"},
				},
				{
					Action:        egress.PolicyAllow,
					MatchCriteria: egress.MatchCriteria{PathPrefix: "/v1"},
				},
			},
		},
	}

	binding := newTestProxyBindingWithEgress(t, "fmw-proxy", "/", bootstrap.BindingDeps{
		Egress: bootstrap.EgressDeps{Resolver: resolver},
	})
	bindings := registry.NewBindingMap()
	if err := bindings.Register("fmw-proxy", binding); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	doReq := func(path string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings/fmw-proxy"+path, nil)
		req.Header.Set("X-Forwarded-Host", upstream.Listener.Addr().String())
		req.Header.Set("X-Forwarded-Proto", "http")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if status := doReq("/v1/admin/settings"); status != http.StatusBadRequest {
		t.Fatalf("/v1/admin should be denied by first rule, got %d", status)
	}
	if status := doReq("/v1/public/items"); status != http.StatusOK {
		t.Fatalf("/v1/public should be allowed by second rule, got %d", status)
	}
}

func TestProxyBinding_CredentialInjection(t *testing.T) {
	t.Parallel()

	var receivedAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	resolver := &egress.Resolver{
		Credentials: &egress.CredentialGrantResolver{
			Loaders: []egress.CredentialGrantLoader{
				&egress.StaticCredentialGrantLoader{
					Grants: []egress.CredentialGrant{
						{SecretRef: "inject-key", MatchCriteria: egress.MatchCriteria{Host: upstream.Listener.Addr().String()}},
					},
				},
			},
			SecretResolver: &coretesting.StubSecretManager{Secrets: map[string]string{"inject-key": "injected-token"}},
		},
	}

	binding := newTestProxyBindingWithEgress(t, "cred-proxy", "/", bootstrap.BindingDeps{
		Egress: bootstrap.EgressDeps{Resolver: resolver},
	})
	bindings := registry.NewBindingMap()
	if err := bindings.Register("cred-proxy", binding); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Bindings = bindings
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings/cred-proxy/items", nil)
	req.Header.Set("X-Forwarded-Host", upstream.Listener.Addr().String())
	req.Header.Set("X-Forwarded-Proto", "http")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	auth, _ := receivedAuth.Load().(string)
	if auth != "Bearer injected-token" {
		t.Fatalf("upstream received Authorization = %q, want %q", auth, "Bearer injected-token")
	}
}

func TestExecuteOperation_ConnectionModeIdentity(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeIdentity,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"token":%q}`, token)}, nil
			},
		},
		ops: []core.Operation{{Name: "do", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, userID, integration, _, _ string) (*core.IntegrationToken, error) {
				if userID == principal.IdentityPrincipal && integration == "svc" {
					return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
				}
				return nil, core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/do", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["token"] != "identity-tok" {
		t.Fatalf("expected identity-tok, got %v", result["token"])
	}
}

func TestExecuteOperation_ConnectionModeEither(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "svc",
			ConnMode: core.ConnectionModeEither,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"token":%q}`, token)}, nil
			},
		},
		ops: []core.Operation{{Name: "do", Method: http.MethodGet}},
	}

	t.Run("prefers user token", func(t *testing.T) {
		t.Parallel()

		apiToken, apiHash, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Auth = &coretesting.StubAuthProvider{N: "test"}
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Datastore = &coretesting.StubDatastore{
				ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
					if h == apiHash {
						return &core.APIToken{UserID: "u1", Name: "test-key"}, nil
					}
					return nil, core.ErrNotFound
				},
				GetUserFn: func(_ context.Context, id string) (*core.User, error) {
					return &core.User{ID: id, Email: "dev@example.com"}, nil
				},
				TokenFn: func(_ context.Context, userID, _, _, _ string) (*core.IntegrationToken, error) {
					switch userID {
					case "u1":
						return &core.IntegrationToken{AccessToken: "user-tok"}, nil
					case principal.IdentityPrincipal:
						return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
					}
					return nil, core.ErrNotFound
				},
			}
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/do", nil)
		req.Header.Set("Authorization", "Bearer "+apiToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if result["token"] != "user-tok" {
			t.Fatalf("expected user-tok (preferred), got %v", result["token"])
		}
	})

	t.Run("falls back to identity", func(t *testing.T) {
		t.Parallel()

		ts := newTestServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, stub)
			cfg.Datastore = &coretesting.StubDatastore{
				FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
					return &core.User{ID: "u1", Email: email}, nil
				},
				TokenFn: func(_ context.Context, userID, _, _, _ string) (*core.IntegrationToken, error) {
					if userID == principal.IdentityPrincipal {
						return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
					}
					return nil, core.ErrNotFound
				},
			}
		})
		testutil.CloseOnCleanup(t, ts)

		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/svc/do", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if result["token"] != "identity-tok" {
			t.Fatalf("expected identity-tok (fallback), got %v", result["token"])
		}
	})
}

func TestConnectManual_MultiCredential(t *testing.T) {
	t.Parallel()

	var stored *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "multi-key-svc"},
		})
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
				stored = tok
				return nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"multi-key-svc","credentials":{"api_key":"k1","app_key":"k2"}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if stored == nil {
		t.Fatal("expected StoreToken to be called")
	}

	var tokenData map[string]string
	if err := json.Unmarshal([]byte(stored.AccessToken), &tokenData); err != nil {
		t.Fatalf("stored token is not valid JSON: %v", err)
	}
	if tokenData["api_key"] != "k1" {
		t.Errorf("api_key = %q, want k1", tokenData["api_key"])
	}
	if tokenData["app_key"] != "k2" {
		t.Errorf("app_key = %q, want k2", tokenData["app_key"])
	}
}

func TestAPITokenScopes_EnforcedDuringInvocation(t *testing.T) {
	t.Parallel()

	alphaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}
	betaStub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "beta",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, alphaStub, betaStub)
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "u1", Name: "scoped-key", Scopes: "alpha"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "user@test.com"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	t.Run("allowed provider succeeds", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/alpha/do_thing", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("denied provider returns 403", func(t *testing.T) {
		t.Parallel()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/beta/do_thing", nil)
		req.Header.Set("Authorization", "Bearer "+plaintext)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})
}

func TestAPITokenScopes_EmptyScopesAllowAll(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "any-provider",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "u1", Name: "unscoped-key", Scopes: ""}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "user@test.com"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/any-provider/do_thing", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateAPIToken_InvalidScope(t *testing.T) {
	t.Parallel()

	stub := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "real-provider"},
		ops:             []core.Operation{{Name: "op", Method: http.MethodGet}},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"test-token","scopes":"nonexistent"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
