package server_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func configurePublicRESTTestServer(t *testing.T, cfg *server.Config) {
	t.Helper()
	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	transport := providergateway.NewProviderGatewayTransport()
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}
	transport.SetIdentityProvider(testAuthStubForScopedBearer())
	transport.SetPublicMethods(registry)
	transport.SetAuthorizationProvider(authz)
	transport.SetPublicBaseURL("https://gestalt.test")
	cfg.Auth = testAuthStubForScopedBearer()
	cfg.Authorization = authz
	cfg.PublicBaseURL = "https://gestalt.test"
	cfg.PublicGatewayTransport = transport
	cfg.StateSecret = []byte("public-rest-test-secret-01234567")
}

func startPublicRESTServer(t *testing.T, profile server.RouteProfile, configure func(*server.Config)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(newTestHandler(t, func(cfg *server.Config) {
		configurePublicRESTTestServer(t, cfg)
		cfg.RouteProfile = profile
		cfg.PublicBaseURL = "https://gestalt.test"
		cfg.ManagementBaseURL = "https://gestalt.test"
		if configure != nil {
			configure(cfg)
		}
	}))
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func publicRESTTestBearer(scope string) string {
	return scopedTestBearerToken("public-rest-user", scope)
}

func configurePublicRESTAuth(t *testing.T, cfg *server.Config, auth *coretesting.StubAuthProvider) {
	t.Helper()
	cfg.Auth = auth
	if cfg.PublicGatewayTransport != nil {
		cfg.PublicGatewayTransport.SetIdentityProvider(auth)
	}
}

func publicRESTExampleAppProvider(t *testing.T, executeFn func(context.Context, string, map[string]any, string) (*core.OperationResult, error)) *coretesting.StubIntegration {
	t.Helper()
	if executeFn == nil {
		executeFn = func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{
				Status: http.StatusTeapot,
				Body:   []byte("teapot"),
				Headers: map[string][]string{
					"X-Example":    {"rest-v2"},
					"Content-Type": {"text/plain"},
				},
			}, nil
		}
	}
	return &coretesting.StubIntegration{
		N:        "example",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "example",
			Operations: []catalog.CatalogOperation{{
				ID:     "sync",
				Method: "POST",
			}},
		},
		ExecuteFn: executeFn,
	}
}

func invokePublicRESTExampleApp(t *testing.T, ts *httptest.Server, configureReq func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if configureReq != nil {
		configureReq(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invoke: %v", err)
	}
	return resp
}

// TestPublicRESTRouting verifies /api/v2 server wiring. Bearer auth, gRPC routing,
// and per-RPC policy are covered by providergateway and host-service relay tests.
func TestPublicRESTRouting(t *testing.T) {
	t.Parallel()

	t.Run("route profile and registration", func(t *testing.T) {
		t.Parallel()

		management := startPublicRESTServer(t, server.RouteProfileManagement, nil)
		resp, err := http.Get(management.URL + "/api/v2/identity/userinfo")
		if err != nil {
			t.Fatalf("GET /api/v2/identity/userinfo: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("management status = %d, want %d", resp.StatusCode, http.StatusNotFound)
		}

		public := startPublicRESTServer(t, server.RouteProfilePublic, nil)
		for _, path := range []string{
			"/api/v2/agent/sessions",
			"/api/v2/indexeddb/databases",
			"/api/v2/external-credentials/credentials",
		} {
			resp, err := http.Get(public.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want %d (body=%s)", path, resp.StatusCode, http.StatusNotFound, string(body))
			}
		}
	})

	t.Run("app invoke forwards raw HTTP", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, publicRESTExampleAppProvider(t, nil))
		})

		resp := invokePublicRESTExampleApp(t, ts, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))
		})
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusTeapot {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusTeapot, string(body))
		}
		if got := string(body); got != "teapot" {
			t.Fatalf("body = %q, want %q", got, "teapot")
		}
		if got := resp.Header.Get("X-Example"); got != "rest-v2" {
			t.Fatalf("X-Example = %q, want %q", got, "rest-v2")
		}
		if got := resp.Header.Get("X-Gestalt-Response-Kind"); got != "operation-result" {
			t.Fatalf("X-Gestalt-Response-Kind = %q, want %q", got, "operation-result")
		}
	})

	t.Run("provider-supplied response kind is stripped", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, publicRESTExampleAppProvider(t, func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   []byte("ok"),
					Headers: map[string][]string{
						"X-Gestalt-Response-Kind": {"provider-supplied"},
					},
				}, nil
			}))
		})

		resp := invokePublicRESTExampleApp(t, ts, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))
		})
		defer func() { _ = resp.Body.Close() }()
		_ = resp.Body.Close()
		if got := resp.Header.Get("X-Gestalt-Response-Kind"); got != "operation-result" {
			t.Fatalf("X-Gestalt-Response-Kind = %q, want %q", got, "operation-result")
		}
	})

	t.Run("gateway errors omit response kind header", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, nil)
		resp, err := http.Get(ts.URL + "/api/v2/identity/userinfo")
		if err != nil {
			t.Fatalf("GET /api/v2/identity/userinfo: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
		if got := resp.Header.Get("X-Gestalt-Response-Kind"); got != "" {
			t.Fatalf("X-Gestalt-Response-Kind = %q, want empty on gateway errors", got)
		}
	})
}

func TestPublicRESTSessionBridge(t *testing.T) {
	t.Parallel()

	const cookieToken = "public-rest-cookie-token"

	t.Run("session cookie becomes authorization", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			configurePublicRESTAuth(t, cfg, authStubWithSessionTokenIntrospect(cookieToken, principal.UserSubjectID("cookie-user"), "example"))
			cfg.Providers = testutil.NewProviderRegistry(t, publicRESTExampleAppProvider(t, nil))
		})

		resp := invokePublicRESTExampleApp(t, ts, func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieToken})
		})
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusTeapot {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusTeapot, string(body))
		}
	})

	t.Run("explicit bearer wins over session cookie", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			configurePublicRESTAuth(t, cfg, testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
				if token == cookieToken {
					return testIntrospectActive(principal.UserSubjectID("cookie-user"), "wrong-scope"), nil
				}
				userID, scope, ok := parseScopedTestBearerToken(token)
				if !ok {
					return &core.IntrospectResponse{Active: false}, nil
				}
				return testIntrospectActive(principal.UserSubjectID(userID), scope), nil
			}))
			cfg.Providers = testutil.NewProviderRegistry(t, publicRESTExampleAppProvider(t, nil))
		})

		resp := invokePublicRESTExampleApp(t, ts, func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieToken})
			req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))
		})
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusTeapot {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusTeapot, string(body))
		}
	})
}
