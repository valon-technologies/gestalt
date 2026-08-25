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

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc/metadata"
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

func TestPublicRESTInvokeSanitizesBeforeProvider(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	var providerMetadata metadata.MD
	identity := testAuthStubForScopedBearer()
	identity.IntrospectFn = func(ctx context.Context, _ *core.IntrospectRequest) (*core.IntrospectResponse, error) {
		providerMetadata, _ = metadata.FromIncomingContext(ctx)
		return &core.IntrospectResponse{Active: true, Subject: "user:alice"}, nil
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(identity)
	transport.SetPublicMethods(registry)
	transport.SetPublicBaseURL("https://gestalt.test")

	ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
		cfg.Auth = identity
		cfg.PublicGatewayTransport = transport
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
			ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: []byte("ok")}, nil
			},
		})
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/example/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("example"))
	req.Header.Set(gestalt.TrustedCallerSubjectMetadataKey, "user:forged")
	req.Header.Set(gestalt.CallerBearerTokenMetadataKey, "token-forged")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invoke: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller subject = %v", providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey))
	}
	if len(providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller bearer = %v", providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey))
	}
	if got := providerMetadata.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+publicRESTTestBearer("example") {
		t.Fatalf("provider authorization = %v, want public bearer", got)
	}
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

// TestPublicRESTStreamingInvoke verifies that POST /api/v2/app/{app}/operations/{operation}
// streams when the catalog operation declares a streaming response. The
// metadata frame's media type becomes the HTTP Content-Type, and data frames
// are written and flushed incrementally as raw bytes.
func TestPublicRESTStreamingInvoke(t *testing.T) {
	t.Parallel()

	t.Run("ndjson stream", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "stream-app",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "stream-app",
					Operations: []catalog.CatalogOperation{{
						ID:     "events.watch",
						Method: "POST",
						Response: &catalog.OperationResponseSpec{
							Stream: &catalog.StreamResponseSpec{
								MediaType: "application/x-ndjson",
							},
						},
					}},
				},
				StreamFn: func(_ context.Context, _ string, _ map[string]any, _ string) (core.StreamReader, error) {
					return &sliceStreamReader{frames: []*core.InvokeFrame{
						{Metadata: &core.InvokeMetadata{Status: http.StatusOK, MediaType: "application/x-ndjson"}},
						{Data: []byte(`{"type":"started"}` + "\n")},
						{Data: []byte(`{"type":"finished"}` + "\n")},
					}}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/stream-app/operations/events.watch", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("stream-app"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
			t.Fatalf("Content-Type = %q, want %q", ct, "application/x-ndjson")
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		want := `{"type":"started"}` + "\n" + `{"type":"finished"}` + "\n"
		if string(body) != want {
			t.Fatalf("body = %q, want %q", string(body), want)
		}
	})

	t.Run("unary operation on same path is unchanged", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "mixed-app",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "mixed-app",
					Operations: []catalog.CatalogOperation{{
						ID:     "sync",
						Method: "POST",
					}},
				},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"ok":true}`)}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/mixed-app/operations/sync", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("mixed-app"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, string(body))
		}
		var payload struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unary response is not JSON envelope: %q: %v", string(body), err)
		}
		if payload.Status != http.StatusOK {
			t.Fatalf("envelope status = %d, want %d", payload.Status, http.StatusOK)
		}
		decoded, err := base64.StdEncoding.DecodeString(payload.Body)
		if err != nil {
			t.Fatalf("envelope body decode: %v", err)
		}
		if string(decoded) != `{"ok":true}` {
			t.Fatalf("envelope body = %q, want %q", string(decoded), `{"ok":true}`)
		}
	})

	t.Run("streaming operation with provider that does not implement StreamingExecutor", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &nonStreamingStub{
				N:        "no-stream-app",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "no-stream-app",
					Operations: []catalog.CatalogOperation{{
						ID:     "events.watch",
						Method: "POST",
						Response: &catalog.OperationResponseSpec{
							Stream: &catalog.StreamResponseSpec{
								MediaType: "application/x-ndjson",
							},
						},
					}},
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/no-stream-app/operations/events.watch", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+publicRESTTestBearer("no-stream-app"))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusBadRequest, string(body))
		}
	})

	t.Run("unauthenticated streaming request returns 401", func(t *testing.T) {
		t.Parallel()

		ts := startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "auth-stream-app",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{
					Name: "auth-stream-app",
					Operations: []catalog.CatalogOperation{{
						ID:     "events.watch",
						Method: "POST",
						Response: &catalog.OperationResponseSpec{
							Stream: &catalog.StreamResponseSpec{
								MediaType: "application/x-ndjson",
							},
						},
					}},
				},
				StreamFn: func(_ context.Context, _ string, _ map[string]any, _ string) (core.StreamReader, error) {
					return &sliceStreamReader{frames: []*core.InvokeFrame{
						{Metadata: &core.InvokeMetadata{Status: http.StatusOK, MediaType: "application/x-ndjson"}},
						{Data: []byte("data\n")},
					}}, nil
				},
			})
		})

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v2/app/auth-stream-app/operations/events.watch", bytes.NewReader([]byte(`{"params":{}}`)))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST invoke: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, http.StatusUnauthorized, string(body))
		}
	})
}

// sliceStreamReader is a test StreamReader that yields a fixed slice of frames.
type sliceStreamReader struct {
	frames []*core.InvokeFrame
	idx    int
}

func (r *sliceStreamReader) Recv() (*core.InvokeFrame, error) {
	if r.idx >= len(r.frames) {
		return nil, io.EOF
	}
	frame := r.frames[r.idx]
	r.idx++
	return frame, nil
}

// nonStreamingStub is a provider stub that declares a streaming operation in
// its catalog but does NOT implement core.StreamingExecutor. This verifies the
// broker rejects streaming invocations when the provider can't stream.
type nonStreamingStub struct {
	N          string
	ConnMode   core.ConnectionMode
	CatalogVal *catalog.Catalog
}

func (s *nonStreamingStub) Name() string        { return s.N }
func (s *nonStreamingStub) DisplayName() string { return s.N }
func (s *nonStreamingStub) Description() string { return "" }
func (s *nonStreamingStub) ConnectionMode() core.ConnectionMode {
	return core.NormalizeConnectionMode(s.ConnMode)
}
func (s *nonStreamingStub) AuthTypes() []string                                     { return nil }
func (s *nonStreamingStub) ConnectionParamDefs() map[string]core.ConnectionParamDef { return nil }
func (s *nonStreamingStub) CredentialFields() []core.CredentialFieldDef             { return nil }
func (s *nonStreamingStub) DiscoveryConfig() *core.DiscoveryConfig                  { return nil }
func (s *nonStreamingStub) ConnectionForOperation(string) string                    { return "" }
func (s *nonStreamingStub) AuthorizationURL(string, []string) string                { return "" }
func (s *nonStreamingStub) ExchangeCode(context.Context, string) (*core.OAuthTokenResponse, error) {
	return nil, nil
}
func (s *nonStreamingStub) RefreshToken(context.Context, string) (*core.OAuthTokenResponse, error) {
	return nil, nil
}
func (s *nonStreamingStub) Catalog() *catalog.Catalog { return s.CatalogVal }
func (s *nonStreamingStub) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, nil
}

// ExecuteStream is intentionally NOT implemented so the provider does not
// satisfy core.StreamingExecutor.
