package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

// virtualHostClient creates an http.Client that dials the test server for all
// requests while preserving the request URL hostname. The cookie jar keys off
// the virtual host (e.g., http://acme.example.test), so domain-scoped cookies
// work correctly across virtual subdomains.
func virtualHostClient(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tsURL, _ := url.Parse(ts.URL)
	return &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				// Always dial the test server regardless of what host the URL says.
				return net.Dial(network, tsURL.Host)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// --- Helpers for building test servers with subdomain support ---

// --- Test fixtures ---

func acmeProvider() *stubIntegrationWithOps {
	return &stubIntegrationWithOps{
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
		},
	}
}

func otherProvider() *stubIntegrationWithOps {
	return &stubIntegrationWithOps{
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
}

// ============================================================
// Scoped API tests
// ============================================================

func TestSubdomainScopedAPI(t *testing.T) {
	t.Parallel()

	acme := acmeProvider()
	other := otherProvider()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-acme", UserID: u.ID, Integration: "acme",
		Connection: "", Instance: "default", AccessToken: "acme-token",
	})
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-other", UserID: u.ID, Integration: "other",
		Connection: "", Instance: "default", AccessToken: "other-token",
	})

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>acme ui</body></html>"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acme, other)
		cfg.Services = svc
		cfg.BaseDomain = "example.test"
		cfg.PublicBaseURL = "http://example.test"
		cfg.CookieDomain = "example.test"
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"acme": {ResolvedAssetRoot: "/fake/assets"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})
	client := virtualHostClient(t, ts)

	t.Run("subdomain integrations returns only scoped plugin", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/integrations", nil)
		resp, err := client.Do(req)
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
		if len(integrations) != 1 {
			t.Fatalf("expected exactly 1 integration on subdomain, got %d", len(integrations))
		}
		if name, _ := integrations[0]["name"].(string); name != "acme" {
			t.Fatalf("expected integration name acme, got %q", name)
		}
	})

	t.Run("subdomain operation executes scoped plugin", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/list_items", nil)
		resp, err := client.Do(req)
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

	t.Run("main domain integrations returns all providers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/api/v1/integrations", nil)
		resp, err := client.Do(req)
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
		if len(integrations) != 2 {
			t.Fatalf("expected 2 integrations on main domain, got %d", len(integrations))
		}
		var foundAcmeURL bool
		var foundOtherNoURL bool
		for _, intg := range integrations {
			name, _ := intg["name"].(string)
			subdomain, hasURL := intg["subdomainUrl"]
			if name == "acme" && hasURL {
				if got, _ := subdomain.(string); got == "http://acme.example.test" {
					foundAcmeURL = true
				}
			}
			if name == "other" && !hasURL {
				foundOtherNoURL = true
			}
		}
		if !foundAcmeURL {
			t.Error("acme should have subdomainUrl on main domain listing")
		}
		if !foundOtherNoURL {
			t.Error("other should not have subdomainUrl")
		}
	})

	t.Run("subdomain static assets served on unmatched paths", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/some/spa/path", nil)
		resp, err := client.Do(req)
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
		req, _ := http.NewRequest(http.MethodGet, "http://unknown.example.test/", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("subdomain health check", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/health", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("subdomain auth info available without auth", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/auth/info", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

// ============================================================
// Auth route isolation tests
// ============================================================

func TestSubdomainAuthIsolation(t *testing.T) {
	t.Parallel()

	acme := acmeProvider()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-acme", UserID: u.ID, Integration: "acme",
		Connection: "", Instance: "default", AccessToken: "acme-token",
	})

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("spa"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acme)
		cfg.Services = svc
		cfg.BaseDomain = "example.test"
		cfg.PublicBaseURL = "http://example.test"
		cfg.CookieDomain = "example.test"
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})
	client := virtualHostClient(t, ts)

	t.Run("login not invoked on subdomain", func(t *testing.T) {
		// POST to login on subdomain. If startLogin runs, it sets a login_state
		// cookie. The SPA fallback won't set that cookie.
		req, _ := http.NewRequest(http.MethodPost, "http://acme.example.test/api/v1/auth/login", bytes.NewBufferString(`{"state":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		u, _ := url.Parse("http://acme.example.test")
		for _, c := range client.Jar.Cookies(u) {
			if c.Name == "login_state" {
				t.Fatal("login_state cookie should not be set on subdomain -- startLogin handler was invoked")
			}
		}
	})

	t.Run("start-oauth not invoked on subdomain", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://acme.example.test/api/v1/auth/start-oauth", bytes.NewBufferString(`{"integration":"acme"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			if _, hasURL := body["url"]; hasURL {
				t.Fatal("start-oauth handler should not be invoked on subdomain")
			}
		}
	})

	t.Run("connect-manual not invoked on subdomain", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://acme.example.test/api/v1/auth/connect-manual", bytes.NewBufferString(`{"integration":"acme","credential":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			if status, _ := body["status"].(string); status == "ok" {
				t.Fatal("connect-manual handler should not be invoked on subdomain")
			}
		}
	})
}

// ============================================================
// Authorization tests
// ============================================================

func TestSubdomainAuthorization(t *testing.T) {
	t.Parallel()

	acme := acmeProvider()

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acme)
		cfg.BaseDomain = "example.test"
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	restrictedRouter := srv.BuildPluginRouter("acme", staticHandler, nil, []string{"allowed@example.com"})
	srv.SetPluginRouters(map[string]http.Handler{"acme": restrictedRouter})
	client := virtualHostClient(t, ts)

	t.Run("anonymous user blocked by allowlist", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/list_items", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("static assets bypass auth", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for static assets, got %d", resp.StatusCode)
		}
	})
}

// ============================================================
// Cross-domain session sharing tests
// ============================================================

func TestSubdomainSessionSharing(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &stubHostIssuedSessionAuth{secret: secret}
	acme := acmeProvider()

	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.IntegrationToken{
		ID: "tok-acme", UserID: u.ID, Integration: "acme",
		Connection: "", Instance: "default", AccessToken: "acme-token",
	})

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("spa"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Providers = testutil.NewProviderRegistry(t, acme)
		cfg.Services = svc
		cfg.BaseDomain = "example.test"
		cfg.PublicBaseURL = "http://example.test"
		cfg.CookieDomain = "example.test"
		cfg.PluginDefs = map[string]*config.ProviderEntry{
			"acme": {ResolvedAssetRoot: "/fake/assets"},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})
	client := virtualHostClient(t, ts)

	// Step 1: Login on main domain
	startBody := bytes.NewBufferString(`{"state":"test-state"}`)
	startReq, _ := http.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := client.Do(startReq)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = startResp.Body.Close()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("start login status = %d, want 200", startResp.StatusCode)
	}

	// Step 2: Callback on main domain
	callbackResp, err := client.Get("http://example.test/api/v1/auth/login/callback?code=good-code&state=test-state")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	_ = callbackResp.Body.Close()
	if callbackResp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", callbackResp.StatusCode)
	}

	// Step 3: Verify session cookie exists for the main domain
	mainURL, _ := url.Parse("http://example.test")
	var hasSession bool
	for _, c := range client.Jar.Cookies(mainURL) {
		if c.Name == "session_token" && c.Value != "" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Fatal("expected session_token cookie after login")
	}

	// Step 4: Plugin subdomain should be authenticated
	t.Run("subdomain authenticated after main-domain login", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/integrations", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusUnauthorized {
			t.Fatal("subdomain should be authenticated via shared session cookie")
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	// Step 5: Logout from plugin subdomain
	logoutReq, _ := http.NewRequest(http.MethodPost, "http://acme.example.test/api/v1/auth/logout", nil)
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	_ = logoutResp.Body.Close()

	// Step 6: Both domains should now be unauthenticated
	t.Run("subdomain unauthenticated after logout", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.example.test/api/v1/integrations", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
		}
	})

	t.Run("main domain unauthenticated after subdomain logout", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/api/v1/integrations", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
		}
	})
}

// TestSubdomainSessionSharing_Localhost verifies that login on localhost sets
// a session cookie with Domain=localhost. Real browsers share cookies across
// *.localhost per RFC 6761, but Go's cookiejar doesn't model single-label
// domain sharing, so we verify the cookie attribute rather than jar propagation.
// The cross-subdomain cookie-sharing flow is fully tested via example.test above.
func TestSubdomainSessionSharing_Localhost(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	auth := &stubHostIssuedSessionAuth{secret: secret}
	acme := acmeProvider()

	svc := coretesting.NewStubServices(t)
	_ = seedUser(t, svc, "anonymous@gestalt")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = auth
		cfg.StateSecret = secret
		cfg.Providers = testutil.NewProviderRegistry(t, acme)
		cfg.Services = svc
		cfg.BaseDomain = "localhost"
		cfg.PublicBaseURL = "http://localhost"
		cfg.CookieDomain = "localhost"
	})
	testutil.CloseOnCleanup(t, ts)
	client := virtualHostClient(t, ts)

	// Login on localhost
	startBody := bytes.NewBufferString(`{"state":"lh-state"}`)
	startReq, _ := http.NewRequest(http.MethodPost, "http://localhost/api/v1/auth/login", startBody)
	startReq.Header.Set("Content-Type", "application/json")
	startResp, err := client.Do(startReq)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	_ = startResp.Body.Close()
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("start login status = %d, want 200", startResp.StatusCode)
	}

	callbackResp, err := client.Get("http://localhost/api/v1/auth/login/callback?code=good-code&state=lh-state")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", callbackResp.StatusCode)
	}

	// Verify the session cookie has Domain=localhost, which browsers use for
	// *.localhost sharing. Go's cookiejar can't model this for single-label
	// domains, but the attribute is what matters for real browsers.
	var foundDomainCookie bool
	for _, setCookie := range callbackResp.Header.Values("Set-Cookie") {
		if strings.Contains(setCookie, "session_token=") && strings.Contains(setCookie, "Domain=localhost") {
			foundDomainCookie = true
		}
	}
	if !foundDomainCookie {
		t.Fatalf("expected session_token cookie with Domain=localhost, got headers: %v", callbackResp.Header.Values("Set-Cookie"))
	}
}


// ============================================================
// Localhost routing tests
// ============================================================

func TestSubdomainLocalhostRouting(t *testing.T) {
	t.Parallel()

	acme := acmeProvider()
	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, acme)
		cfg.BaseDomain = "localhost"
	})
	testutil.CloseOnCleanup(t, ts)

	srv := tsServer(t, ts)
	pluginRouter := srv.BuildPluginRouter("acme", staticHandler, nil, nil)
	srv.SetPluginRouters(map[string]http.Handler{"acme": pluginRouter})
	client := virtualHostClient(t, ts)

	t.Run("localhost subdomain with port", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://acme.localhost:8080/health", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("localhost without subdomain routes to main", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://localhost:8080/health", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

// ============================================================
// Helpers
// ============================================================

func tsServer(t *testing.T, ts *httptest.Server) *server.Server {
	t.Helper()
	srv, ok := ts.Config.Handler.(*server.Server)
	if !ok {
		t.Fatalf("expected *server.Server handler, got %T", ts.Config.Handler)
	}
	return srv
}

// stubHostIssuedSessionAuth is defined in server_test.go. Import the
// session package for ValidateToken used by cross-domain session tests.
var _ = session.ValidateToken
