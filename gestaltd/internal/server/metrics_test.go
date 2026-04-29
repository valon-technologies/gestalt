package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type manualMetricsProvider struct {
	name string
}

func (p *manualMetricsProvider) Name() string                        { return p.name }
func (p *manualMetricsProvider) DisplayName() string                 { return p.name }
func (p *manualMetricsProvider) Description() string                 { return "" }
func (p *manualMetricsProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (p *manualMetricsProvider) AuthTypes() []string                 { return []string{"manual"} }
func (p *manualMetricsProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *manualMetricsProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *manualMetricsProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *manualMetricsProvider) ConnectionForOperation(string) string        { return "" }
func (p *manualMetricsProvider) Catalog() *catalog.Catalog {
	return serverTestCatalogFromOperations(p.name, nil)
}
func (p *manualMetricsProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
}

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

func hasMetricWithPrefix(rm metricdata.ResourceMetrics, prefix string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if strings.HasPrefix(metric.Name, prefix) {
				return true
			}
		}
	}
	return false
}

func hasInt64SumMetric(rm metricdata.ResourceMetrics, name string, want int64, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if metrictest.AttrsMatch(point.Attributes, attrs) && point.Value == want {
					return true
				}
			}
		}
	}
	return false
}

func collectMetricsUntil(t *testing.T, metrics *metrictest.ManualMeterProvider, ready func(metricdata.ResourceMetrics) bool) metricdata.ResourceMetrics {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		if err := metrics.Reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect metrics: %v", err)
		}
		if ready(rm) {
			return rm
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for expected metrics")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectionAuthMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "sample-oauth"

	handler := &testOAuthHandler{
		authorizationBaseURLVal: "https://idp.example.test/authorize",
		exchangeCodeFn: func(_ context.Context, code string) (*core.TokenResponse, error) {
			if code == "good-code" {
				return &core.TokenResponse{AccessToken: "oauth-token"}, nil
			}
			return nil, context.DeadlineExceeded
		},
	}

	oauthServer := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: providerName},
			authURL:         "https://idp.example.test/authorize",
		})
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth(providerName, handler)
		cfg.Services = coretesting.NewStubServices(t)
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

	rm := metrictest.CollectMetrics(t, metrics.Reader)
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

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "sample-refresh"

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

	successSvc := coretesting.NewStubServices(t)
	u := seedUser(t, successSvc, "anonymous@gestalt")
	expired := time.Now().Add(-1 * time.Hour)
	seedToken(t, successSvc, &core.ExternalCredential{
		ID: "tok1", SubjectID: principal.UserSubjectID(u.ID), Integration: providerName,
		Connection: "default", Instance: "default",
		AccessToken: "old-access-token", RefreshToken: "old-refresh-token", ExpiresAt: &expired,
	})

	successServer := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Providers = testutil.NewProviderRegistry(t, successStub)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth(providerName, successStub.refreshTokenFn)
		cfg.Services = successSvc
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

	errorSvc := coretesting.NewStubServices(t)
	u2 := seedUser(t, errorSvc, "anonymous@gestalt")
	seedToken(t, errorSvc, &core.ExternalCredential{
		ID: "tok2", SubjectID: principal.UserSubjectID(u2.ID), Integration: providerName,
		Connection: "default", Instance: "default",
		AccessToken: "expired-access-token", RefreshToken: "expired-refresh-token", ExpiresAt: &expired,
	})

	errorServer := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Providers = testutil.NewProviderRegistry(t, errorStub)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth(providerName, errorStub.refreshTokenFn)
		cfg.Services = errorSvc
	})
	testutil.CloseOnCleanup(t, errorServer)

	errorReq, _ := http.NewRequest(http.MethodGet, errorServer.URL+"/api/v1/"+providerName+"/list", nil)
	errorResp, err := http.DefaultClient.Do(errorReq)
	if err != nil {
		t.Fatalf("refresh error request: %v", err)
	}
	defer func() { _ = errorResp.Body.Close() }()
	if errorResp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("refresh error status = %d, want %d", errorResp.StatusCode, http.StatusPreconditionFailed)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
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
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "list",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "user",
		"gestalt.result_status":       "200",
		"gestalt.result_status_class": "2xx",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "list",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "user",
		"gestalt.result_status":       "412",
		"gestalt.result_status_class": "4xx",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.error_count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "list",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "user",
		"gestalt.result_status":       "412",
		"gestalt.result_status_class": "4xx",
	})
}

func TestOperationMetricsDefaultRESTTransportFromCatalogContext(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "sample-rest"

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Services = coretesting.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithCatalog{
			StubIntegration: coretesting.StubIntegration{
				N:        providerName,
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
					if operation == "lookup" {
						return &core.OperationResult{Status: http.StatusNotFound, Body: `{"error":"missing"}`}, nil
					}
					return &core.OperationResult{Status: http.StatusOK, Body: `{"operation":"` + operation + `"}`}, nil
				},
			},
			catalog: &catalog.Catalog{
				Name: providerName,
				Operations: []catalog.CatalogOperation{
					{ID: "list", Method: http.MethodGet, Path: "/list"},
					{ID: "lookup", Method: http.MethodGet, Path: "/lookup"},
				},
			},
		})
	})
	testutil.CloseOnCleanup(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/"+providerName+"/list", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("operation request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operation status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	errorReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/"+providerName+"/lookup", nil)
	errorResp, err := http.DefaultClient.Do(errorReq)
	if err != nil {
		t.Fatalf("operation error request: %v", err)
	}
	defer func() { _ = errorResp.Body.Close() }()
	if errorResp.StatusCode != http.StatusNotFound {
		t.Fatalf("operation error status = %d, want %d", errorResp.StatusCode, http.StatusNotFound)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "list",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.result_status":       "200",
		"gestalt.result_status_class": "2xx",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.operation.duration", map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "list",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.result_status":       "200",
		"gestalt.result_status_class": "2xx",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "lookup",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.result_status":       "404",
		"gestalt.result_status_class": "4xx",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.error_count", 1, map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "lookup",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.result_status":       "404",
		"gestalt.result_status_class": "4xx",
	})
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.operation.duration", map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "lookup",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.result_status":       "404",
		"gestalt.result_status_class": "4xx",
	})
	httpAttrs := map[string]string{
		"http.route":                   "/api/v1/{integration}/{operation}",
		"gestaltd.provider.name":       providerName,
		"gestaltd.operation.name":      "list",
		"gestaltd.operation.transport": "rest",
		"gestaltd.connection.mode":     "none",
		"gestaltd.invocation.surface":  "http",
	}
	metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", httpAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.provider")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.operation")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.result_status")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.result_status_class")
}

func TestManualConnectionMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "manual-metrics"

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Services = coretesting.NewStubServices(t)
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

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.connection.auth.count", 1, map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.type":            "manual",
		"gestalt.action":          "complete",
		"gestalt.connection_mode": "user",
	})
}

func TestConnectionAuthMetricsUseUnknownProviderForMissingIntegration(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
	})
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

	rm := metrictest.CollectMetrics(t, metrics.Reader)
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

	metrics := metrictest.NewManualMeterProvider(t)
	secret := []byte("0123456789abcdef0123456789abcdef")
	var auditBuf bytes.Buffer

	client := &http.Client{}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client.Jar = jar

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Auth = &metricsHostIssuedSessionAuth{secret: secret, name: "metrics-host-issued"}
		cfg.AuditSink = invocation.NewSlogAuditSink(&auditBuf)
		cfg.StateSecret = secret
		cfg.Services = coretesting.NewStubServices(t)
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
	lines := bytes.Split(bytes.TrimSpace(auditBuf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected exactly one audit record for api token inventory read, got %d\nraw: %s", len(lines), auditBuf.String())
	}
	var auditRecord map[string]any
	if err := json.Unmarshal(lines[0], &auditRecord); err != nil {
		t.Fatalf("parse audit record: %v\nraw: %s", err, auditBuf.String())
	}
	if auditRecord["operation"] != "api_token.list" {
		t.Fatalf("expected audit operation api_token.list, got %v", auditRecord["operation"])
	}
	if auditRecord["auth_source"] != "session" {
		t.Fatalf("expected audit auth_source session, got %v", auditRecord["auth_source"])
	}
	if auditRecord["allowed"] != true {
		t.Fatalf("expected audit allowed=true, got %v", auditRecord["allowed"])
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
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
}

func TestHTTPMiddlewareMetricsUseConfiguredMeterProvider(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
	})
	testutil.CloseOnCleanup(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ready", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	if !hasMetricWithPrefix(rm, "http.server.") {
		t.Fatalf("expected otelhttp metrics in configured meter provider, got %+v", rm.ScopeMetrics)
	}
	metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", map[string]string{
		"http.route": "/ready",
	})
}

func TestHTTPMetricsDoNotLabelUnknownPluginRouteParams(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
	})
	testutil.CloseOnCleanup(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/not-a-provider/not-an-operation", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 for unknown provider route")
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	routeAttrs := map[string]string{"http.route": "/api/v1/{integration}/{operation}"}
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", routeAttrs, "gestalt.provider")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", routeAttrs, "gestalt.operation")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", routeAttrs, "gestaltd.provider.name")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", routeAttrs, "gestaltd.operation.name")
}

func TestHTTPBindingOperationMetricsIncludeBinding(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "webhook-metrics"
	bindingsSeen := make(chan string, 1)
	operationDone := make(chan struct{}, 1)

	provider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        providerName,
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(ctx context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
				defer func() { operationDone <- struct{}{} }()
				bindingsSeen <- invocation.HTTPBindingFromContext(ctx)
				if operation == "receive_event" {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				}
				return &core.OperationResult{Status: http.StatusNotFound, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "receive_event", Method: http.MethodPost}},
	}

	srv := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Providers = testutil.NewProviderRegistry(t, provider)
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			providerName: {
				SecuritySchemes: map[string]*config.HTTPSecurityScheme{
					"public": {Type: providermanifestv1.HTTPSecuritySchemeTypeNone},
				},
				HTTP: map[string]*config.HTTPBinding{
					"delivery": {
						Path:     "/delivery",
						Method:   http.MethodPost,
						Security: "public",
						Target:   "receive_event",
						Ack: &providermanifestv1.HTTPAck{
							Status: http.StatusAccepted,
							Body:   map[string]any{"accepted": true},
						},
					},
				},
			},
		}
		cfg.Authorizer = mustAuthorizer(t, config.AuthorizationConfig{}, cfg.PluginDefs)
	})
	testutil.CloseOnCleanup(t, srv)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/"+providerName+"/delivery", strings.NewReader(`{"event":"opened"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http binding request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("http binding status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	select {
	case got := <-bindingsSeen:
		if got != "delivery" {
			t.Fatalf("HTTPBindingFromContext = %q, want %q", got, "delivery")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for http binding invocation")
	}
	select {
	case <-operationDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for http binding operation completion")
	}

	attrs := map[string]string{
		"gestalt.provider":            providerName,
		"gestalt.operation":           "receive_event",
		"gestalt.transport":           "rest",
		"gestalt.connection_mode":     "none",
		"gestalt.invocation_surface":  "http",
		"gestalt.http_binding":        "delivery",
		"gestalt.result_status":       "200",
		"gestalt.result_status_class": "2xx",
	}
	rm := collectMetricsUntil(t, metrics, func(rm metricdata.ResourceMetrics) bool {
		return hasInt64SumMetric(rm, "gestaltd.operation.count", 1, attrs)
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.operation.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.operation.duration", attrs)
	httpAttrs := map[string]string{
		"http.route":                  "/api/v1/" + providerName + "/delivery",
		"gestaltd.provider.name":      providerName,
		"gestaltd.operation.name":     "receive_event",
		"gestaltd.invocation.surface": "http_binding",
		"gestaltd.http.binding.name":  "delivery",
	}
	metrictest.RequireFloat64Histogram(t, rm, "http.server.request.duration", httpAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.provider")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.operation")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "http.server.request.duration", httpAttrs, "gestalt.http_binding")
}

func TestHTTPDiscoveryMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "metrics-session-catalog"

	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: providerName, ConnMode: core.ConnectionModeUser},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "identity-token" {
				t.Fatalf("catalog token = %q, want %q", token, "identity-token")
			}
			return &catalog.Catalog{
				Name: providerName,
				Operations: []catalog.CatalogOperation{
					{ID: "list_projects", Description: "List projects", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			}, nil
		},
	}

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "user@example.com")
	seedSubjectToken(t, svc, principal.UserSubjectID(u.ID), providerName, testDefaultConnection, "default", "identity-token")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/"+providerName+"/operations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list operations request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list operations status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.action":          "list_operations",
		"gestalt.connection_mode": "user",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.discovery.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.discovery.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.discovery.duration", attrs)
}

func TestHTTPDiscoveryMetrics_FailureRecordsErrorCount(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "metrics-session-catalog-error"

	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: providerName, ConnMode: core.ConnectionModeUser},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "identity-token" {
				t.Fatalf("catalog token = %q, want %q", token, "identity-token")
			}
			return nil, context.DeadlineExceeded
		},
	}

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "user@example.com")
	seedSubjectToken(t, svc, principal.UserSubjectID(u.ID), providerName, testDefaultConnection, "default", "identity-token")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "user@example.com"}, nil
			},
		}
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.DefaultConnection = map[string]string{providerName: testDefaultConnection}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations/"+providerName+"/operations", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list operations request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("list operations status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestalt.provider":        providerName,
		"gestalt.action":          "list_operations",
		"gestalt.connection_mode": "user",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.discovery.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.discovery.error_count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.discovery.duration", attrs)
}
