package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func useManualMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()

	prev := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(prev)
	})
	return reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

func requireInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is %T, want Sum[int64]", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				if attrsMatch(point.Attributes, attrs) {
					if point.Value != want {
						t.Fatalf("metric %q attrs %v = %d, want %d", name, attrs, point.Value, want)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %q with attrs %v not found", name, attrs)
}

func attrsMatch(set attribute.Set, want map[string]string) bool {
	for key, expected := range want {
		value, ok := set.Value(attribute.Key(key))
		if !ok || value.AsString() != expected {
			return false
		}
	}
	return true
}

func TestCustomAuthMetrics(t *testing.T) {
	reader := useManualMeterProvider(t)

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
			StubIntegration: coretesting.StubIntegration{N: "slack"},
			authURL:         "https://slack.com/oauth/v2/authorize",
		})
		cfg.DefaultConnection = map[string]string{"slack": testDefaultConnection}
		cfg.ConnectionAuth = testConnectionAuth("slack", handler)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, oauthServer)

	startOAuth := func(code string) int {
		t.Helper()

		body := bytes.NewBufferString(`{"integration":"slack"}`)
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

	manualServer := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubManualProvider{
			StubIntegration: coretesting.StubIntegration{N: "manual-svc"},
		})
		cfg.DefaultConnection = map[string]string{"manual-svc": "default"}
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, manualServer)

	manualBody := bytes.NewBufferString(`{"integration":"manual-svc","credential":"my-api-key"}`)
	manualReq, _ := http.NewRequest(http.MethodPost, manualServer.URL+"/api/v1/auth/connect-manual", manualBody)
	manualReq.Header.Set("Content-Type", "application/json")
	manualResp, err := http.DefaultClient.Do(manualReq)
	if err != nil {
		t.Fatalf("manual connect request: %v", err)
	}
	defer func() { _ = manualResp.Body.Close() }()
	if manualResp.StatusCode != http.StatusOK {
		t.Fatalf("manual connect status = %d, want %d", manualResp.StatusCode, http.StatusOK)
	}

	rm := collectMetrics(t, reader)
	requireInt64Sum(t, rm, "gestaltd.oauth.callback.count", 1, map[string]string{
		"gestalt.provider": "slack",
		"gestalt.result":   "success",
	})
	requireInt64Sum(t, rm, "gestaltd.oauth.callback.count", 1, map[string]string{
		"gestalt.provider": "slack",
		"gestalt.result":   "error",
	})
	requireInt64Sum(t, rm, "gestaltd.integration.connect.count", 1, map[string]string{
		"gestalt.provider":    "slack",
		"gestalt.auth_method": "oauth",
		"gestalt.result":      "success",
	})
	requireInt64Sum(t, rm, "gestaltd.integration.connect.count", 1, map[string]string{
		"gestalt.provider":    "manual-svc",
		"gestalt.auth_method": "manual",
		"gestalt.result":      "success",
	})
}

func TestRefreshAndOperationResultMetrics(t *testing.T) {
	reader := useManualMeterProvider(t)

	successStub := &stubOAuthIntegration{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N: "fake",
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
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", successStub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					UserID:       "u1",
					Integration:  "fake",
					AccessToken:  "stale-access-token",
					RefreshToken: "old-refresh-token",
					ExpiresAt:    &expiresSoon,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, successServer)

	successReq, _ := http.NewRequest(http.MethodGet, successServer.URL+"/api/v1/fake/list", nil)
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
			StubIntegration: coretesting.StubIntegration{N: "fake"},
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
		cfg.DefaultConnection = map[string]string{"fake": testDefaultConnection}
		cfg.ConnectionAuth = oauthRefreshConnectionAuth("fake", errorStub.refreshTokenFn)
		cfg.Datastore = &coretesting.StubDatastore{
			FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
				return &core.User{ID: "u1", Email: email}, nil
			},
			TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
				return &core.IntegrationToken{
					UserID:       "u1",
					Integration:  "fake",
					AccessToken:  "expired-token",
					RefreshToken: "expired-refresh-token",
					ExpiresAt:    &alreadyExpired,
				}, nil
			},
		}
	})
	testutil.CloseOnCleanup(t, errorServer)

	errorReq, _ := http.NewRequest(http.MethodGet, errorServer.URL+"/api/v1/fake/list", nil)
	errorResp, err := http.DefaultClient.Do(errorReq)
	if err != nil {
		t.Fatalf("refresh error request: %v", err)
	}
	defer func() { _ = errorResp.Body.Close() }()
	if errorResp.StatusCode != http.StatusBadGateway {
		t.Fatalf("refresh error status = %d, want %d", errorResp.StatusCode, http.StatusBadGateway)
	}

	rm := collectMetrics(t, reader)
	requireInt64Sum(t, rm, "gestaltd.token_refresh.count", 1, map[string]string{
		"gestalt.provider":        "fake",
		"gestalt.connection_mode": "user",
		"gestalt.result":          "success",
	})
	requireInt64Sum(t, rm, "gestaltd.token_refresh.count", 1, map[string]string{
		"gestalt.provider":        "fake",
		"gestalt.connection_mode": "user",
		"gestalt.result":          "error",
	})
	requireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":        "fake",
		"gestalt.operation":       "list",
		"gestalt.transport":       "rest",
		"gestalt.connection_mode": "user",
		"gestalt.result":          "success",
	})
	requireInt64Sum(t, rm, "gestaltd.operation.count", 1, map[string]string{
		"gestalt.provider":        "fake",
		"gestalt.operation":       "list",
		"gestalt.transport":       "rest",
		"gestalt.connection_mode": "user",
		"gestalt.result":          "error",
	})
}
