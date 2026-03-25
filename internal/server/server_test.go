package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/provider"
	"github.com/valon-technologies/gestalt/internal/registry"
	"github.com/valon-technologies/gestalt/internal/server"
	"github.com/valon-technologies/gestalt/internal/testutil"
	"github.com/valon-technologies/gestalt/plugins/bindings/proxy"
	"gopkg.in/yaml.v3"
)

func newTestServer(t *testing.T, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	cfg := server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "test"},
		Datastore: &coretesting.StubDatastore{},
		Providers: func() *registry.PluginMap[core.Provider] {
			reg := registry.New()
			return &reg.Providers
		}(),
		DevMode:     false,
		StateSecret: []byte("0123456789abcdef0123456789abcdef"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Invoker == nil {
		cfg.Invoker = invocation.NewBroker(cfg.Providers, cfg.Datastore)
	}
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	return httptest.NewServer(srv)
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
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "test",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, fmt.Errorf("not a session token")
			},
		}
		cfg.Datastore = &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, _ string) (*core.APIToken, error) {
				return &core.APIToken{UserID: "u1", Name: "test-key"}, nil
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "user@example.com", DisplayName: "Test User"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("Authorization", "Bearer some-api-key")
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
	ts := newTestServer(t)
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

func TestAuthMiddleware_DevMode(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, oauthStub, manualStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			"op": {Description: "An op", Method: "GET", Path: "/op"},
		},
	}
	prov, err := provider.Build(def, config.IntegrationDef{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, _ string) (*core.User, error) {
				return nil, fmt.Errorf("database unavailable")
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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

	stub := &coretesting.StubIntegration{N: "test-int"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/test-int/operations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListOperations_NotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/nonexistent/operations", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "do_thing", Description: "Do a thing", Method: "GET"},
			{Name: "create_thing", Description: "Create a thing", Method: "POST"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "stored-token"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing?foo=bar", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/nonexistent/some_op", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "do_thing", Description: "Do a thing", Method: "GET"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/nonexistent", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "do_thing", Description: "Do a thing", Method: "GET"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return nil, core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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

	resp, err := http.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code")
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

	resp, err := http.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=encrypted-state")
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

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
	stub := &stubIntegrationWithAuthURL{
		StubIntegration: coretesting.StubIntegration{
			N: "slack",
			ExchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
				if code == "good-code" {
					return &core.TokenResponse{AccessToken: "slack-token"}, nil
				}
				return nil, fmt.Errorf("bad code")
			},
		},
		authURL: "https://slack.com/oauth/v2/authorize",
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
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
		cfg.DevMode = true
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
		cfg.DevMode = true
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"my-token","scopes":"read"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "attacker@example.com")
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
		cfg.DevMode = true
		cfg.Now = func() time.Time { return fixedNow }
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"name":"expiry-test","scopes":"read"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/tokens", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
	expected := fixedNow.Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	if !expiresAt.Equal(expected) {
		t.Fatalf("expected expires_at %v, got %v", expected, expiresAt)
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
			{Name: "send", Description: "Send", Method: "POST"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"text":"hello"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/send", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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

func (s *stubIntegrationWithOps) ListOperations() []core.Operation {
	return s.ops
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

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"gitlab"}`)
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
		cfg.DevMode = true
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
func (s *stubNonOAuthProvider) ListOperations() []core.Operation    { return s.ops }
func (s *stubNonOAuthProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.execFn != nil {
		return s.execFn(ctx, op, params, token)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	// Token expires in 3 minutes (within threshold) but still valid
	expiresInThree := time.Now().Add(3 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops:             []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("refresh token revoked")
		},
	}

	alreadyExpired := time.Now().Add(-10 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			t.Fatal("RefreshToken should not be called when no refresh token stored")
			return nil, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken: "no-refresh-token",
					ExpiresAt:   &expiresSoon,
				}, nil
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			t.Fatal("RefreshToken should not be called when no expiry info")
			return nil, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					AccessToken:  "no-expiry-token",
					RefreshToken: "rf",
				}, nil
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
	if usedToken != "no-expiry-token" {
		t.Fatalf("expected original token, got %q", usedToken)
	}
}

func TestExecuteOperation_NonOAuthProviderSkipsRefresh(t *testing.T) {
	t.Parallel()

	var usedToken string
	stub := &stubNonOAuthProvider{
		name: "manual-api",
		ops:  []core.Operation{{Name: "get", Description: "Get", Method: "GET"}},
		execFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
			usedToken = token
			return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(_ context.Context, _ string) (*core.TokenResponse, error) {
			return &core.TokenResponse{AccessToken: "new-access", ExpiresIn: 0}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	expiresSoon := time.Now().Add(3 * time.Minute)
	tokenCallCount := 0
	var storedToken *core.IntegrationToken
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops:             []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			ops: []core.Operation{{Name: "list", Description: "List", Method: "GET"}},
		},
		refreshTokenFn: func(context.Context, string) (*core.TokenResponse, error) {
			return nil, fmt.Errorf("upstream error")
		},
	}

	expiresSoon := time.Now().Add(3 * time.Minute)
	tokenCallCount := 0
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "ping", Method: "GET"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				tokenCalled = true
				return nil, core.ErrNotFound
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/noop/ping", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "echo", Method: "POST"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		ops: []core.Operation{{Name: "search", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, echoProvider)
	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
	}

	httpSrv := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = providers
		cfg.Datastore = ds
	})
	defer httpSrv.Close()

	httpReq, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/echo/search?q=hello", nil)
	httpReq.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/runtimes", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Runtimes = runtimes
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/runtimes", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
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
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack"})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"slack","credential":"some-key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
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
}

func TestConnectManual_MissingFields(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
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
}

func TestConnectManual_UnknownIntegration(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"nonexistent","credential":"key"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
		})
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"integration":"manual-svc","scopes":[]}`)
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
	if result["error"] == "" {
		t.Fatal("expected error message in response")
	}
}

func TestListBindings_NoBindings(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		K: core.BindingTrigger,
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Bindings = bindings
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/bindings", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
		K: core.BindingTrigger,
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/incoming", Handler: handler},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
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
		K: core.BindingSurface,
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/invoke", Handler: handler},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ts := newTestServer(t, func(cfg *server.Config) {
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
		K: core.BindingTrigger,
		R: []core.Route{
			{Method: http.MethodPost, Pattern: "/incoming", Handler: handler},
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
				cfg.DevMode = true
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
			req.Header.Set("X-Dev-User-Email", "dev@example.com")

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
			exactReq.Header.Set("X-Dev-User-Email", "dev@example.com")

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
		Type:   "proxy",
		Config: *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("proxy factory: %v", err)
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
			{Name: "search_issues", Description: "Search issues", Method: "GET"},
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
		cfg.DevMode = true
		cfg.Providers = providers
		cfg.Datastore = ds
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	devHeaders := map[string]string{"X-Dev-User-Email": "dev@example.com"}

	status, resp := mcpJSONRPC(t, ts, devHeaders, map[string]any{
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

	status, resp = mcpJSONRPC(t, ts, devHeaders, map[string]any{
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

func (p *mcpPassthroughProvider) ListOperations() []core.Operation { return p.ops }
func (p *mcpPassthroughProvider) Catalog() *catalog.Catalog        { return p.catVal }
func (p *mcpPassthroughProvider) SupportsManualAuth() bool         { return true }
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
		cfg.DevMode = true
		cfg.Providers = providers
		cfg.Datastore = ds
		cfg.MCPHandler = mcpHandler
	})
	defer ts.Close()

	devHeaders := map[string]string{"X-Dev-User-Email": "dev@example.com"}

	mcpJSONRPC(t, ts, devHeaders, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})

	status, resp := mcpJSONRPC(t, ts, devHeaders, map[string]any{
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

	status, resp = mcpJSONRPC(t, ts, devHeaders, map[string]any{
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
		cfg.DevMode = true
	})
	defer ts.Close()

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "do_thing", Description: "Do a thing", Method: "POST"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	largeBody := bytes.NewReader(bytes.Repeat([]byte("A"), (1<<20)+1))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/do_thing", largeBody)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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
			{Name: "do_thing", Description: "Do a thing", Method: "GET"},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/do_thing", nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
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

type stubAuthWithToken struct {
	coretesting.StubAuthProvider
}

func (s *stubAuthWithToken) IssueSessionToken(identity *core.UserIdentity) (string, error) {
	return "dev-token-" + identity.Email, nil
}

func (s *stubAuthWithToken) SessionTokenTTL() time.Duration {
	return time.Hour
}

func TestDevLogin(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Auth = &stubAuthWithToken{StubAuthProvider: coretesting.StubAuthProvider{N: "test"}}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"email":"dev@test.local"}`)
	resp, err := http.Post(ts.URL+"/api/dev-login", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/dev-login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result["email"] != "dev@test.local" {
		t.Fatalf("expected email dev@test.local, got %v", result["email"])
	}
	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_token" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session_token cookie to be set")
	}
	if sessionCookie.Value != "dev-token-dev@test.local" {
		t.Fatalf("expected cookie value dev-token-dev@test.local, got %q", sessionCookie.Value)
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("expected session cookie to be HttpOnly")
	}
}

func TestDevLogin_NotRegisteredWithoutDevMode(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = false
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"email":"dev@test.local"}`)
	resp, err := http.Post(ts.URL+"/api/dev-login", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/dev-login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404/405 when dev mode disabled, got %d", resp.StatusCode)
	}
}

func TestDevLogin_EmptyEmail(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Auth = &stubAuthWithToken{StubAuthProvider: coretesting.StubAuthProvider{N: "test"}}
	})
	testutil.CloseOnCleanup(t, ts)

	body := bytes.NewBufferString(`{"email":""}`)
	resp, err := http.Post(ts.URL+"/api/dev-login", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/dev-login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
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
	reqNoCookie, _ := http.NewRequest("GET", ts.URL+"/api/v1/integrations", nil)
	noAuthResp, err := http.DefaultClient.Do(reqNoCookie)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = noAuthResp.Body.Close() }()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", noAuthResp.StatusCode)
	}

	// Request with cookie should pass auth middleware.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/integrations", nil)
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

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/auth/logout", nil)
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

type stubOAuthPostConnect struct {
	coretesting.StubIntegration
	hookFn func(context.Context, *core.IntegrationToken, *http.Client) (map[string]string, error)
}

func (s *stubOAuthPostConnect) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/authorize?state=" + url.QueryEscape(state)
}

func (s *stubOAuthPostConnect) ExchangeCode(_ context.Context, code string) (*core.TokenResponse, error) {
	if code == "good-code" {
		return &core.TokenResponse{AccessToken: "tok-123"}, nil
	}
	return nil, fmt.Errorf("bad code")
}

func (s *stubOAuthPostConnect) RefreshToken(_ context.Context, _ string) (*core.TokenResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubOAuthPostConnect) PostConnectHook() core.PostConnectHook {
	return s.hookFn
}

func TestOAuthCallback_PostConnectHook(t *testing.T) {
	t.Parallel()

	var storedTokens []*core.IntegrationToken

	stub := &stubOAuthPostConnect{
		StubIntegration: coretesting.StubIntegration{N: "hook-test"},
		hookFn: func(_ context.Context, tok *core.IntegrationToken, _ *http.Client) (map[string]string, error) {
			return map[string]string{"cloud_id": "abc123"}, nil
		},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
		StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
			storedTokens = append(storedTokens, tok)
			return nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = ds
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"hook-test"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("X-Dev-User-Email", "dev@example.com")
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()

	var startResult map[string]string
	_ = json.NewDecoder(startResp.Body).Decode(&startResult)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cbReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	cbResp, err := noRedirect.Do(cbReq)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = cbResp.Body.Close() }()

	if cbResp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("expected 303, got %d: %s", cbResp.StatusCode, body)
	}

	if len(storedTokens) < 2 {
		t.Fatalf("expected at least 2 StoreToken calls (initial + metadata update), got %d", len(storedTokens))
	}
	lastTok := storedTokens[len(storedTokens)-1]
	if !strings.Contains(lastTok.MetadataJSON, "cloud_id") {
		t.Fatalf("expected MetadataJSON to contain cloud_id, got %s", lastTok.MetadataJSON)
	}
}

func TestOAuthCallback_PostConnectHookFailure(t *testing.T) {
	t.Parallel()

	var deleted []string

	stub := &stubOAuthPostConnect{
		StubIntegration: coretesting.StubIntegration{N: "fail-hook"},
		hookFn: func(_ context.Context, _ *core.IntegrationToken, _ *http.Client) (map[string]string, error) {
			return nil, fmt.Errorf("discovery failed")
		},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
		StoreTokenFn: func(_ context.Context, tok *core.IntegrationToken) error {
			return nil
		},
		DeleteTokenFn: func(_ context.Context, id string) error {
			deleted = append(deleted, id)
			return nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.Datastore = ds
	})
	testutil.CloseOnCleanup(t, ts)

	startBody := bytes.NewBufferString(`{"integration":"fail-hook"}`)
	startReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("X-Dev-User-Email", "dev@example.com")
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()

	var startResult map[string]string
	_ = json.NewDecoder(startResp.Body).Decode(&startResult)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cbReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&state="+url.QueryEscape(startResult["state"]), nil)
	cbResp, err := noRedirect.Do(cbReq)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = cbResp.Body.Close() }()

	if cbResp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 on hook failure, got %d", cbResp.StatusCode)
	}
	if len(deleted) == 0 {
		t.Fatal("expected token to be deleted on hook failure (rollback)")
	}
}
