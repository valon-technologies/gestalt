package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestSubdomainRouting(t *testing.T) {
	t.Parallel()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok1", UserID: u.ID, Integration: "acme",
		Connection: "", Instance: "default", AccessToken: "test-token",
	})

	acmeProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "acme",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"operation":%q}`, op),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "list_items", Description: "List items", Method: http.MethodGet},
			{Name: "create_item", Description: "Create an item", Method: http.MethodPost},
		},
	}

	otherProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "other",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"operation":%q}`, op),
				}, nil
			},
		},
		ops: []core.Operation{
			{Name: "do_stuff", Description: "Do stuff", Method: http.MethodGet},
		},
	}

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>acme plugin ui</body></html>"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acmeProvider, otherProvider)
		cfg.Services = svc
		cfg.BaseDomain = "example.com"
		cfg.PublicBaseURL = "https://example.com"
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"acme": {ResolvedAssetRoot: "/fake/assets"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})

	t.Run("main domain operation via traditional path", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/acme/list_items", nil)
		req.Host = "example.com"
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
		if body["operation"] != "list_items" {
			t.Fatalf("expected list_items, got %q", body["operation"])
		}
	})

	t.Run("subdomain scoped GET operation", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/list_items", nil)
		req.Host = "acme.example.com"
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
		if body["operation"] != "list_items" {
			t.Fatalf("expected list_items, got %q", body["operation"])
		}
	})

	t.Run("subdomain scoped POST operation", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/create_item", strings.NewReader(`{}`))
		req.Host = "acme.example.com"
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("subdomain serves static assets on fallback", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/some/spa/path", nil)
		req.Host = "acme.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
			t.Fatalf("expected text/html, got %q", ct)
		}
	})

	t.Run("unknown subdomain returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		req.Host = "unknown.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("subdomain health check", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		req.Host = "acme.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("integration listing includes subdomain URL", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/integrations", nil)
		req.Host = "example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var integrations []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		for _, intg := range integrations {
			name, _ := intg["name"].(string)
			switch name {
			case "acme":
				got, _ := intg["subdomainUrl"].(string)
				want := "https://acme.example.com"
				if got != want {
					t.Errorf("acme subdomainUrl = %q, want %q", got, want)
				}
			case "other":
				if _, ok := intg["subdomainUrl"]; ok {
					t.Errorf("other should not have subdomainUrl")
				}
			}
		}
	})

	t.Run("no subdomain when host matches base domain", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		req.Host = "example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from main domain health, got %d", resp.StatusCode)
		}
	})
}

func TestSubdomainAuthorization(t *testing.T) {
	t.Parallel()

	acmeProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "acme"},
		ops: []core.Operation{
			{Name: "list_items", Description: "List items", Method: http.MethodGet},
		},
	}

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acmeProvider)
		cfg.BaseDomain = "example.com"
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	restrictedRouter := srv.BuildPluginRouter("acme", staticHandler, nil, []string{"allowed@example.com"})
	srv.SetPluginRouters(map[string]http.Handler{"acme": restrictedRouter})

	t.Run("anonymous user blocked by allowlist", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/list_items", nil)
		req.Host = "acme.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		// In noAuth mode, anonymous user (anonymous@gestalt) is not in the allowlist
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("static assets still blocked by authz", func(t *testing.T) {
		// Static assets are served on NotFound, which bypasses auth middleware.
		// Only API/MCP routes are protected by the allowlist.
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		req.Host = "acme.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		// Static assets are served directly (no auth on NotFound handler)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for static assets, got %d", resp.StatusCode)
		}
	})
}

func TestSubdomainWithLocalhost(t *testing.T) {
	t.Parallel()

	acmeProvider := &stubIntegrationWithOps{
		StubIntegration: coretesting.StubIntegration{N: "acme"},
		ops: []core.Operation{
			{Name: "list_items", Description: "List items", Method: http.MethodGet},
		},
	}

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acmeProvider)
		cfg.BaseDomain = "localhost"
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})

	t.Run("localhost subdomain with port", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		req.Host = "acme.localhost:8080"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("localhost without subdomain routes to main", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
		req.Host = "localhost:8080"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func tsServer(t *testing.T, ts *httptest.Server) *server.Server {
	t.Helper()
	srv, ok := ts.Config.Handler.(*server.Server)
	if !ok {
		t.Fatalf("expected *server.Server handler, got %T", ts.Config.Handler)
	}
	return srv
}
