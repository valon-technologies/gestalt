package server_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

	t.Run("app invoke returns an OperationResult envelope", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "example",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "example",
					Operations: []catalog.CatalogOperation{{
						ID:     "sync",
						Method: "POST",
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{
						Status: http.StatusTeapot,
						Body:   []byte("teapot"),
						Headers: map[string][]string{
							"X-Example":    {"rest-v2"},
							"Content-Type": {"text/plain"},
						},
					}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
		}
		var payload struct {
			Status  int    `json:"status"`
			Body    string `json:"body"`
			Headers map[string]struct {
				Values []string `json:"values"`
			} `json:"headers"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("OperationResult JSON = %q: %v", string(body), err)
		}
		if payload.Status != http.StatusTeapot {
			t.Fatalf("OperationResult.status = %d, want %d", payload.Status, http.StatusTeapot)
		}
		decodedBody, err := base64.StdEncoding.DecodeString(payload.Body)
		if err != nil {
			t.Fatalf("OperationResult.body = %q: %v", payload.Body, err)
		}
		if got := string(decodedBody); got != "teapot" {
			t.Fatalf("OperationResult.body = %q, want %q", got, "teapot")
		}
		if got := payload.Headers["X-Example"].Values; len(got) != 1 || got[0] != "rest-v2" {
			t.Fatalf("OperationResult X-Example = %#v, want [rest-v2]", got)
		}
		if got := payload.Headers["Content-Type"].Values; len(got) != 1 || got[0] != "text/plain" {
			t.Fatalf("OperationResult Content-Type = %#v, want [text/plain]", got)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
		}
		if got := resp.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
			t.Fatalf("X-Frame-Options = %q, want %q", got, "SAMEORIGIN")
		}
		if got := resp.Header.Get("Content-Security-Policy"); got == "" {
			t.Fatal("Content-Security-Policy missing from OperationResult response")
		}
	})

	t.Run("gateway failures use canonical errors", func(t *testing.T) {
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
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		var payload struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Error == "" {
			t.Fatalf("body = %q, want canonical error JSON", string(body))
		}
		if payload.Code != "Unauthenticated" {
			t.Fatalf("code = %q, want Unauthenticated", payload.Code)
		}
	})

	t.Run("session cookie authenticates app invoke", func(t *testing.T) {
		t.Parallel()

		const cookieToken = "public-rest-cookie-token"
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			auth := authStubWithSessionTokenIntrospect(cookieToken, principal.UserSubjectID("cookie-user"), "example")
			cfg.Auth = auth
			cfg.PublicGatewayTransport.SetIdentityProvider(auth)
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "example",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "example",
					Operations: []catalog.CatalogOperation{{
						ID:     "sync",
						Method: "POST",
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: []byte("ok")}, nil
				},
			})
		})

		for _, authorization := range []string{"", "   "} {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieToken})

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST invoke authorization=%q: %v", authorization, err)
			}
			func() {
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("authorization=%q status = %d, want %d (body=%s)", authorization, resp.StatusCode, http.StatusOK, string(body))
				}
			}()
		}
	})

	t.Run("malformed authorization header with session cookie authenticates", func(t *testing.T) {
		t.Parallel()

		const cookieToken = "public-rest-cookie-token"
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			auth := authStubWithSessionTokenIntrospect(cookieToken, principal.UserSubjectID("cookie-user"), "example")
			cfg.Auth = auth
			cfg.PublicGatewayTransport.SetIdentityProvider(auth)
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "example",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "example",
					Operations: []catalog.CatalogOperation{{
						ID:     "sync",
						Method: "POST",
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: []byte("ok")}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "not-a-bearer-token")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieToken})

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
		}
	})

	t.Run("explicit bearer wins over session cookie", func(t *testing.T) {
		t.Parallel()

		const cookieToken = "public-rest-cookie-token"
		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			auth := testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
				if token == cookieToken {
					return testIntrospectActive(principal.UserSubjectID("cookie-user"), "wrong-scope"), nil
				}
				userID, scope, ok := parseScopedTestBearerToken(token)
				if !ok {
					return &core.IntrospectResponse{Active: false}, nil
				}
				return testIntrospectActive(principal.UserSubjectID(userID), scope), nil
			})
			cfg.Auth = auth
			cfg.PublicGatewayTransport.SetIdentityProvider(auth)
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "example",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "example",
					Operations: []catalog.CatalogOperation{{
						ID:     "sync",
						Method: "POST",
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusTeapot, Body: []byte("teapot")}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieToken})
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
		}
	})
}
