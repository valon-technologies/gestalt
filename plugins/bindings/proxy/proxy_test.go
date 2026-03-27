package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/principal"
	"gopkg.in/yaml.v3"
)

func TestProxyRoutes(t *testing.T) {
	t.Parallel()

	b := testBinding(t, "/proxy", nil)
	routes := b.Routes()
	if len(routes) != 14 {
		t.Fatalf("expected 14 routes (7 methods x 2 patterns), got %d", len(routes))
	}

	patterns := map[string]int{}
	for _, route := range routes {
		patterns[route.Pattern]++
	}
	if patterns["/proxy"] != 7 {
		t.Fatalf("expected 7 exact routes, got %d", patterns["/proxy"])
	}
	if patterns["/proxy/*"] != 7 {
		t.Fatalf("expected 7 wildcard routes, got %d", patterns["/proxy/*"])
	}
}

func TestProxyForwardsResponseHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-abc")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":   r.URL.Path,
			"method": r.Method,
		})
	}))
	t.Cleanup(upstream.Close)

	b := testBinding(t, "/proxy", upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/v1/items", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if rid := w.Header().Get("X-Request-Id"); rid != "req-abc" {
		t.Fatalf("X-Request-Id = %q, want req-abc", rid)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["path"] != "/v1/items" {
		t.Fatalf("upstream path = %q, want /v1/items", body["path"])
	}
	if body["method"] != http.MethodGet {
		t.Fatalf("upstream method = %q, want GET", body["method"])
	}
}

func TestProxyForwardsRedirectHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://example.com/new-path")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	t.Cleanup(upstream.Close)

	noRedirectClient := upstream.Client()
	noRedirectClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	b := testBinding(t, "/proxy", noRedirectClient)

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/old-path", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://example.com/new-path" {
		t.Fatalf("Location = %q, want https://example.com/new-path", loc)
	}
}

func TestProxyForwardsPostBody(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"echo":         payload["message"],
			"content_type": r.Header.Get("Content-Type"),
		})
	}))
	t.Cleanup(upstream.Close)

	b := testBinding(t, "/proxy", upstream.Client())

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/proxy/echo", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["echo"] != "hello" {
		t.Fatalf("echo = %q, want hello", resp["echo"])
	}
	if resp["content_type"] != "application/json" {
		t.Fatalf("upstream content_type = %q, want application/json", resp["content_type"])
	}
}

func TestProxyStripsHopByHopHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-Custom", "preserved")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	b := testBinding(t, "/proxy", upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/test", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Header().Get("Connection") != "" {
		t.Fatal("hop-by-hop Connection header should be stripped")
	}
	if w.Header().Get("Keep-Alive") != "" {
		t.Fatal("hop-by-hop Keep-Alive header should be stripped")
	}
	if w.Header().Get("X-Custom") != "preserved" {
		t.Fatalf("X-Custom = %q, want preserved", w.Header().Get("X-Custom"))
	}
}

func TestProxyForwardsUpstreamErrors(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"access denied"}`))
	}))
	t.Cleanup(upstream.Close)

	b := testBinding(t, "/proxy", upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/secret", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestProxySanitizesInboundAuthHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorization":       r.Header.Get("Authorization"),
			"cookie":              r.Header.Get("Cookie"),
			"proxy_authorization": r.Header.Get("Proxy-Authorization"),
			"x_dev_user_email":    r.Header.Get("X-Dev-User-Email"),
			"x_custom":            r.Header.Get("X-Custom"),
		})
	}))
	t.Cleanup(upstream.Close)

	b := testBinding(t, "/proxy", upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/api", nil)
	req.Header.Set("Authorization", "Bearer inbound-token")
	req.Header.Set("Cookie", "session=abc123")
	req.Header.Set("Proxy-Authorization", "Basic creds")
	req.Header.Set("X-Dev-User-Email", "user@example.com")
	req.Header.Set("X-Custom", "should-pass")
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["authorization"] != "" {
		t.Fatalf("inbound Authorization should be stripped, got %q", resp["authorization"])
	}
	if resp["cookie"] != "" {
		t.Fatalf("inbound Cookie should be stripped, got %q", resp["cookie"])
	}
	if resp["proxy_authorization"] != "" {
		t.Fatalf("inbound Proxy-Authorization should be stripped, got %q", resp["proxy_authorization"])
	}
	if resp["x_dev_user_email"] != "" {
		t.Fatalf("inbound X-Dev-User-Email should be stripped, got %q", resp["x_dev_user_email"])
	}
	if resp["x_custom"] != "should-pass" {
		t.Fatalf("X-Custom = %q, want should-pass", resp["x_custom"])
	}
}

func TestProxyInjectsProviderCredential(t *testing.T) {
	t.Parallel()

	const providerToken = "resolved-provider-token"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorization": r.Header.Get("Authorization"),
		})
	}))
	t.Cleanup(upstream.Close)

	resolver := egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
		Credentials: staticCredentialResolver{
			Credential: egress.CredentialMaterialization{
				Authorization: "Bearer " + providerToken,
			},
		},
	}

	b := New("test-proxy", "test-provider", proxyConfig{Path: "/proxy"}, resolver, upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/data", nil)
	req.Header.Set("Authorization", "Bearer inbound-should-be-stripped")
	ctx := principal.WithPrincipal(req.Context(), &principal.Principal{UserID: "user-42"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["authorization"] != "Bearer "+providerToken {
		t.Fatalf("outbound Authorization = %q, want Bearer %s", resp["authorization"], providerToken)
	}
}

func TestProxyDerivesPrincipalSubject(t *testing.T) {
	t.Parallel()

	var capturedSubject egress.Subject
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	resolver := egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
		Credentials: subjectCapturingResolver{
			capture: func(s egress.Subject) {
				capturedSubject = s
			},
		},
	}

	b := New("test-proxy", "test-provider", proxyConfig{Path: "/proxy"}, resolver, upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/check", nil)
	ctx := principal.WithPrincipal(req.Context(), &principal.Principal{UserID: "user-99"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if capturedSubject.Kind != egress.SubjectUser {
		t.Fatalf("subject kind = %q, want %q", capturedSubject.Kind, egress.SubjectUser)
	}
	if capturedSubject.ID != "user-99" {
		t.Fatalf("subject ID = %q, want user-99", capturedSubject.ID)
	}
}

func TestProxyFallsBackToSubjectSystem(t *testing.T) {
	t.Parallel()

	var capturedSubject egress.Subject
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	resolver := egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
		Credentials: subjectCapturingResolver{
			capture: func(s egress.Subject) {
				capturedSubject = s
			},
		},
	}

	b := New("my-proxy", "test-provider", proxyConfig{Path: "/proxy"}, resolver, upstream.Client())

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/proxy/check", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if capturedSubject.Kind != egress.SubjectSystem {
		t.Fatalf("subject kind = %q, want %q", capturedSubject.Kind, egress.SubjectSystem)
	}
	if capturedSubject.ID != "my-proxy" {
		t.Fatalf("subject ID = %q, want my-proxy", capturedSubject.ID)
	}
}

func TestProxyFactory(t *testing.T) {
	t.Parallel()

	cfgYAML := `path: /proxy`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	def := config.BindingDef{
		Type:      "proxy",
		Providers: []string{"my-provider"},
		Config:    *node.Content[0],
	}

	binding, err := Factory(context.Background(), "proxy-surface", def, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}

	if binding.Name() != "proxy-surface" {
		t.Fatalf("name = %q, want proxy-surface", binding.Name())
	}
}

func TestProxyFactoryValidation(t *testing.T) {
	t.Parallel()

	cfgYAML := `path: proxy`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	_, err := Factory(context.Background(), "bad-proxy", config.BindingDef{
		Type:      "proxy",
		Providers: []string{"some-provider"},
		Config:    *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestProxyFactoryAcceptsZeroProviders(t *testing.T) {
	t.Parallel()

	cfgYAML := `path: /proxy`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	binding, err := Factory(context.Background(), "no-prov", config.BindingDef{
		Type:   "proxy",
		Config: *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if binding.Name() != "no-prov" {
		t.Fatalf("name = %q, want no-prov", binding.Name())
	}
}

func TestProxyFactoryRejectsMultipleProviders(t *testing.T) {
	t.Parallel()

	cfgYAML := `path: /proxy`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	_, err := Factory(context.Background(), "multi-prov", config.BindingDef{
		Type:      "proxy",
		Providers: []string{"alpha", "beta"},
		Config:    *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err == nil {
		t.Fatal("expected error for multiple providers")
	}
	if !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), "at most one")
	}
}

func TestProxyZeroProviderForwardsRequest(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":   r.URL.Path,
			"method": r.Method,
		})
	}))
	t.Cleanup(upstream.Close)

	cfgYAML := `path: /gw`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatal(err)
	}

	binding, err := Factory(context.Background(), "gw-proxy", config.BindingDef{
		Type:   "proxy",
		Config: *node.Content[0],
	}, bootstrap.BindingDeps{})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}

	routes := binding.Routes()
	if len(routes) == 0 {
		t.Fatal("expected at least one route")
	}

	req := httptest.NewRequest(http.MethodGet, upstream.URL+"/gw/v1/healthz", nil)
	w := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["path"] != "/v1/healthz" {
		t.Fatalf("upstream path = %q, want /v1/healthz", body["path"])
	}
	if body["method"] != http.MethodGet {
		t.Fatalf("upstream method = %q, want GET", body["method"])
	}
}

func testBinding(t *testing.T, path string, client *http.Client) *Binding {
	t.Helper()
	return New("test-proxy", "test-provider", proxyConfig{Path: path}, egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
	}, client)
}

type staticCredentialResolver struct {
	Credential egress.CredentialMaterialization
}

func (r staticCredentialResolver) ResolveCredential(_ context.Context, _ egress.Subject, _ egress.Target) (egress.CredentialMaterialization, error) {
	return r.Credential, nil
}

type subjectCapturingResolver struct {
	capture func(egress.Subject)
}

func (r subjectCapturingResolver) ResolveCredential(_ context.Context, subject egress.Subject, _ egress.Target) (egress.CredentialMaterialization, error) {
	r.capture(subject)
	return egress.CredentialMaterialization{}, nil
}
