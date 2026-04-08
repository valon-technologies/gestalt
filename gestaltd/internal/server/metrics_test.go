package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

type manualMetricsProvider struct {
	name string
}

func (p *manualMetricsProvider) Name() string                        { return p.name }
func (p *manualMetricsProvider) DisplayName() string                 { return p.name }
func (p *manualMetricsProvider) Description() string                 { return "" }
func (p *manualMetricsProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (p *manualMetricsProvider) Catalog() *catalog.Catalog {
	return serverTestCatalogFromOperations(p.name, nil)
}
func (p *manualMetricsProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
}
func (p *manualMetricsProvider) SupportsManualAuth() bool { return true }

type metricsHostIssuedSessionAuth struct {
	secret []byte
	name   string
}

func (s *metricsHostIssuedSessionAuth) Name() string { return s.name }

func (s *metricsHostIssuedSessionAuth) LoginURL(state string) (string, error) {
	return "https://idp.example.test/login?state=" + url.QueryEscape(state), nil
}

func (s *metricsHostIssuedSessionAuth) HandleCallback(context.Context, string) (*core.UserIdentity, error) {
	return nil, context.DeadlineExceeded
}

func (s *metricsHostIssuedSessionAuth) HandleCallbackWithState(_ context.Context, code, state string) (*core.UserIdentity, string, error) {
	if code != "good-code" {
		return nil, "", context.DeadlineExceeded
	}
	return &core.UserIdentity{Email: "host@example.com", DisplayName: "Host Issued"}, state, nil
}

func (s *metricsHostIssuedSessionAuth) ValidateToken(_ context.Context, token string) (*core.UserIdentity, error) {
	return session.ValidateToken(token, s.secret)
}

func (s *metricsHostIssuedSessionAuth) SessionTokenTTL() time.Duration {
	return time.Hour
}

func TestConnectionAuthMetrics(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	const providerName = "metrics-slack"

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://slack.com/oauth/v2/authorize",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			if code == "good-code" {
				return &core.TokenResponse{AccessToken: "slack-token"}, nil
			}
			return nil, context.DeadlineExceeded
		},
	}

	oauthServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: providerName},
			authURL:         "https://slack.com/oauth/v2/authorize",
		})
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth(providerName, handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, oauthServer)

	startOAuth := func(code string) int {
		t.Helper()

		body := bytes.NewBufferString(`{"integration":"` + providerName + `"}`)
		startReq, _ := http.NewRequest(http.MethodPost, oauthServer.URL+"/api/v1/auth/start-oauth", body)
		startReq.Header.Set("Content-Type", "application/json")
		startResp, err := http.DefaultClient.Do(startReq)
		if err != nil {
			t.Fatalf("start oauth request: %v", err)
		}
		defer func() { _ = startResp.Body.Close() }()

		if startResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from start-oauth, got %d", startResp.StatusCode)
		}

		var startResult map[string]string
		if err := json.NewDecoder(startResp.Body).Decode(&startResult); err != nil {
			t.Fatalf("decode start oauth response: %v", err)
		}

		noRedirect := &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		req, _ := http.NewRequest(http.MethodGet, oauthServer.URL+"/api/v1/auth/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(startResult["state"]), nil)
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("oauth callback request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	if status := startOAuth("good-code"); status != http.StatusSeeOther {
		t.Fatalf("successful oauth callback status = %d, want %d", status, http.StatusSeeOther)
	}
	if status := startOAuth("bad-code"); status != http.StatusBadGateway {
		t.Fatalf("failed oauth callback status = %d, want %d", status, http.StatusBadGateway)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 2, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "oauth",
		"gestalt.action":          "start",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 2, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "oauth",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.error_count", 1, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "oauth",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.connection.auth.duration", map[string]string{
		"gestalt.provider": providerName,
		"gestalt.type":     "oauth",
		"gestalt.action":   "complete",
	})
}

func TestRefreshAndOperationResultMetrics(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	const providerName = "metrics-fake"

	successStub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: providerName,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"token":"` + token + `"}`}, nil
				},
			},
			ops: []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
			if refreshToken != "old-refresh-token" {
				t.Fatalf("unexpected refresh token %q", refreshToken)
			}
			return &core.TokenResponse{AccessToken: "fresh-access-token", ExpiresIn: 3600}, nil
		},
	}

	expiresSoon := time.Now().Add(2 * time.Minute)
	successServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, successStub)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth(providerName, successStub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					UserID:       "u1",
					Integration:  providerName,
					AccessToken:  "stale-access-token",
					RefreshToken: "old-refresh-token",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, successServer)

	successReq, _ := http.NewRequest(http.MethodGet, successServer.URL+"/api/v1/"+providerName+"/list", nil)
	successResp, err := http.DefaultClient.Do(successReq)
	if err != nil {
		t.Fatalf("refresh success request: %v", err)
	}
	defer func() { _ = successResp.Body.Close() }()
	if successResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh success status = %d, want %d", successResp.StatusCode, http.StatusOK)
	}

	errorStub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: providerName},
			ops:             []core.Operation{{Name: "list", Description: "List", Method: http.MethodGet}},
		},
		refreshTokenFn: func(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
			if refreshToken != "expired-refresh-token" {
				t.Fatalf("unexpected refresh token %q", refreshToken)
			}
			return nil, context.DeadlineExceeded
		},
	}

	alreadyExpired := time.Now().Add(-10 * time.Minute)
	errorServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, errorStub)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth(providerName, errorStub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					UserID:       "u1",
					Integration:  providerName,
					AccessToken:  "expired-token",
					RefreshToken: "expired-refresh-token",
					ExpiresAt:    &alreadyExpired,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, errorServer)

	errorReq, _ := http.NewRequest(http.MethodGet, errorServer.URL+"/api/v1/"+providerName+"/list", nil)
	errorResp, err := http.DefaultClient.Do(errorReq)
	if err != nil {
		t.Fatalf("refresh error request: %v", err)
	}
	defer func() { _ = errorResp.Body.Close() }()
	if errorResp.StatusCode != http.StatusBadGateway {
		t.Fatalf("refresh error status = %d, want %d", errorResp.StatusCode, http.StatusBadGateway)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 2, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "oauth",
		"gestalt.action":          "refresh",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.error_count", 1, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "oauth",
		"gestalt.action":          "refresh",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.connection.auth.duration", map[string]string{
		"gestalt.provider": providerName,
		"gestalt.type":     "oauth",
		"gestalt.action":   "refresh",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 2, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.operation":       "list",
		"gestalt.transport":       "rest",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.error_count", 1, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.operation":       "list",
		"gestalt.transport":       "rest",
		"gestalt.connection_mode": "user",
	})
}

func TestManualConnectionMetrics(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	const providerName = "manual-metrics"

	ds := metrictest.NewNamedStubDatastore("metrics-store", coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
		StoreTokenFn: func(_ context.Context, token *core.IntegrationToken) error {
			if token.AccessToken != `{"api_key":"secret"}` {
				t.Fatalf("unexpected stored credential %q", token.AccessToken)
			}
			return nil
		},
	})

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.Datastore = ds
		cfg.Providers = testutil.NewProviderRegistry(t, &manualMetricsProvider{name: providerName})
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
	})
	testutil.CloseOnCleanup(t, srv)

	body := bytes.NewBufferString(`{"integration":"` + providerName + `","credentials":{"api_key":"secret"}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/connect-manual", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("manual connect request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manual connect status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 1, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "manual",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "metrics-store",
		"gestalt.method":   "store_token",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.datastore.duration", map[string]string{
		"gestalt.provider": "metrics-store",
		"gestalt.method":   "store_token",
	})
}

func TestConnectionAuthMetricsUseUnknownProviderForMissingIntegration(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	srv := newTestServer(t)
	testutil.CloseOnCleanup(t, srv)

	startBody := bytes.NewBufferString(`{"integration":"typo-oauth"}`)
	startReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/start-oauth", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		t.Fatalf("start oauth request: %v", err)
	}
	defer func() { _ = startResp.Body.Close() }()
	if startResp.StatusCode != http.StatusNotFound {
		t.Fatalf("start oauth status = %d, want %d", startResp.StatusCode, http.StatusNotFound)
	}

	manualBody := bytes.NewBufferString(`{"integration":"typo-manual","credential":"secret"}`)
	manualReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/connect-manual", manualBody)
	manualReq.Header.Set("Content-Type", "application/json")
	manualResp, err := http.DefaultClient.Do(manualReq)
	if err != nil {
		t.Fatalf("manual connect request: %v", err)
	}
	defer func() { _ = manualResp.Body.Close() }()
	if manualResp.StatusCode != http.StatusNotFound {
		t.Fatalf("manual connect status = %d, want %d", manualResp.StatusCode, http.StatusNotFound)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 1, map[string]string{
		"gestalt.provider":        "unknown",
		"gestalt.type":            "oauth",
		"gestalt.action":          "start",
		"gestalt.connection_mode": "unknown",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.error_count", 1, map[string]string{
		"gestalt.provider":        "unknown",
		"gestalt.type":            "oauth",
		"gestalt.action":          "start",
		"gestalt.connection_mode": "unknown",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 1, map[string]string{
		"gestalt.provider":        "unknown",
		"gestalt.type":            "manual",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "unknown",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.error_count", 1, map[string]string{
		"gestalt.provider":        "unknown",
		"gestalt.type":            "manual",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "unknown",
	})
}

func TestPlatformAuthMetrics(t *testing.T) {
	t.Parallel()

	reader := metrictest.UseManualMeterProvider(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	var auditBuf bytes.Buffer

	ds := metrictest.NewNamedStubDatastore("auth-metrics-store", coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u1", Email: email}, nil
		},
	})
	ds.ListAPITokensFn = func(context.Context, string) ([]*core.APIToken, error) { return nil, nil }

	client := &http.Client{}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client.Jar = jar

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = &metricsHostIssuedSessionAuth{secret: secret, name: "metrics-host-issued"}
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.StateSecret = secret
		cfg.Datastore = ds
	})
	testutil.CloseOnCleanup(t, srv)

	loginBody := bytes.NewBufferString(`{"state":"login-state"}`)
	loginReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}

	var loginResult map[string]string
	if err := json.NewDecoder(loginResp.Body).Decode(&loginResult); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	loginURL, err := url.Parse(loginResult["url"])
	if err != nil {
		t.Fatalf("parse login url: %v", err)
	}

	callbackReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/login/callback?code=good-code&state="+url.QueryEscape(loginURL.Query().Get("state")), nil)
	callbackResp, err := client.Do(callbackReq)
	if err != nil {
		t.Fatalf("login callback request: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusOK {
		t.Fatalf("login callback status = %d, want %d", callbackResp.StatusCode, http.StatusOK)
	}
	auditBuf.Reset()

	tokensReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/tokens", nil)
	tokensResp, err := client.Do(tokensReq)
	if err != nil {
		t.Fatalf("list tokens request: %v", err)
	}
	defer func() { _ = tokensResp.Body.Close() }()
	if tokensResp.StatusCode != http.StatusOK {
		t.Fatalf("list tokens status = %d, want %d", tokensResp.StatusCode, http.StatusOK)
	}
	if got := bytes.TrimSpace(auditBuf.Bytes()); len(got) != 0 {
		t.Fatalf("validate-token and datastore read should not emit audit logs, got: %s", got)
	}

	rm := metrictest.CollectMetrics(t, reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.auth.count", 1, map[string]string{
		"gestalt.provider": "metrics-host-issued",
		"gestalt.action":   "begin_login",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.auth.count", 1, map[string]string{
		"gestalt.provider": "metrics-host-issued",
		"gestalt.action":   "complete_login",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.auth.count", 1, map[string]string{
		"gestalt.provider": "metrics-host-issued",
		"gestalt.action":   "validate_token",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.auth.duration", map[string]string{
		"gestalt.provider": "metrics-host-issued",
		"gestalt.action":   "complete_login",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 2, map[string]string{
		"gestalt.provider": "auth-metrics-store",
		"gestalt.method":   "find_or_create_user",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "auth-metrics-store",
		"gestalt.method":   "list_api_tokens",
	})
}
