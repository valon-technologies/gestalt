package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

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

func TestProxyCONNECTNotImplemented(t *testing.T) {
	t.Parallel()

	b := testBinding(t, "/proxy", nil)
	req := httptest.NewRequest(http.MethodConnect, "/proxy/tunnel", nil)
	w := httptest.NewRecorder()
	b.Routes()[0].Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}
}

func testBinding(t *testing.T, path string, client *http.Client) *Binding {
	t.Helper()
	return New("test-proxy", proxyConfig{Path: path}, egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
	}, client)
}
