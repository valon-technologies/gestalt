package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/registry"
	"github.com/valon-technologies/toolshed/internal/server"
)

func newTestRegistry(t *testing.T, providers ...core.Provider) *registry.PluginMap[core.Provider] {
	t.Helper()
	reg := registry.New()
	for _, prov := range providers {
		if err := reg.Providers.Register(prov.Name(), prov); err != nil {
			t.Fatalf("registering provider: %v", err)
		}
	}
	return &reg.Providers
}

func newTestServer(t *testing.T, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	cfg := server.Config{
		Auth:      &coretesting.StubAuthProvider{N: "test"},
		Datastore: &coretesting.StubDatastore{},
		Providers: func() *registry.PluginMap[core.Provider] {
			reg := registry.New()
			return &reg.Providers
		}(),
		DevMode: false,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	srv := server.New(cfg)
	return httptest.NewServer(srv)
}

func TestHealthCheck(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	defer ts.Close()

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
	defer ts.Close()

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

func TestReadinessCheck_DatastoreDown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = &coretesting.StubDatastore{
			PingFn: func(context.Context) error {
				return fmt.Errorf("connection refused")
			},
		}
	})
	defer ts.Close()

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
	if body["status"] != "unavailable" {
		t.Fatalf("expected status unavailable, got %q", body["status"])
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
	})
	defer ts.Close()

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
	defer ts.Close()

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
	defer ts.Close()

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
}

func TestListIntegrations(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = newTestRegistry(t, stub)
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
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
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
}

func TestListOperations(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{N: "test-int"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = newTestRegistry(t, stub)
	})
	defer ts.Close()

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
	defer ts.Close()

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
		cfg.Providers = newTestRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "stored-token"}, nil
			},
		}
	})
	defer ts.Close()

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

func TestExecuteOperation_UnknownIntegration(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	defer ts.Close()

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
		cfg.Providers = newTestRegistry(t, fullStub)
	})
	defer ts.Close()

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
		cfg.Providers = newTestRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return nil, core.ErrNotFound
			},
		}
	})
	defer ts.Close()

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
	defer ts.Close()

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

func TestLoginCallback(t *testing.T) {
	t.Parallel()

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
	})
	defer ts.Close()

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
	defer ts.Close()

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
		cfg.Providers = newTestRegistry(t, stub)
	})
	defer ts.Close()

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
}

func TestIntegrationOAuthCallback(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{
		N: "slack",
		ExchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			if code == "good-code" {
				return &core.TokenResponse{AccessToken: "slack-token"}, nil
			}
			return nil, fmt.Errorf("bad code")
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
		cfg.Providers = newTestRegistry(t, stub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/callback?code=good-code&integration=slack", nil)
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
	if result["status"] != "connected" {
		t.Fatalf("expected connected, got %q", result["status"])
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
	defer ts.Close()

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
	})
	defer ts.Close()

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
		cfg.Providers = newTestRegistry(t, fullStub)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{AccessToken: "tok"}, nil
			},
		}
	})
	defer ts.Close()

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

type stubIntegrationWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubIntegrationWithOps) ListOperations() []core.Operation {
	return s.ops
}

type stubAuthWithLoginURL struct {
	coretesting.StubAuthProvider
	loginURL string
}

func (s *stubAuthWithLoginURL) LoginURL(_ string) string {
	return s.loginURL
}

type stubIntegrationWithAuthURL struct {
	coretesting.StubIntegration
	authURL string
}

func (s *stubIntegrationWithAuthURL) AuthorizationURL(_ string, _ []string) string {
	return s.authURL
}

func TestCallbackPathConstants(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.DevMode = true
	})
	defer ts.Close()

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

	// Integration callback: requires auth, so with dev mode + header it should
	// return 400 (missing params), not 404.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+config.IntegrationCallbackPath, nil)
	req.Header.Set("X-Dev-User-Email", "dev@example.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", config.IntegrationCallbackPath, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("config.IntegrationCallbackPath %q is not a registered route (got 404)", config.IntegrationCallbackPath)
	}
}

type stubStatefulAuth struct {
	coretesting.StubAuthProvider
	handleWithState func(context.Context, string, string) (*core.UserIdentity, string, error)
}

func (s *stubStatefulAuth) HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error) {
	return s.handleWithState(ctx, code, state)
}
