package pluginhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
)

func TestProxyHTTPInjectsToken(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	var tokens sync.Map
	tokens.Store("inv-1", "my-secret-token")

	server := &ProviderHostServer{
		tokens:      &tokens,
		httpClient:  upstream.Client(),
		validateURL: func(*url.URL) error { return nil },
	}

	resp, err := server.ProxyHTTP(context.Background(), &pluginapiv1.ProxyHTTPRequest{
		InvocationId: "inv-1",
		Method:       http.MethodGet,
		Url:          upstream.URL + "/data",
	})
	if err != nil {
		t.Fatalf("ProxyHTTP: %v", err)
	}
	if resp.GetStatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.GetStatusCode())
	}
	if gotAuth != "Bearer my-secret-token" {
		t.Fatalf("upstream auth = %q, want Bearer my-secret-token", gotAuth)
	}
}

func TestProxyHTTPStripsBlockedHeaders(t *testing.T) {
	t.Parallel()

	var gotHeaders http.Header
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	var tokens sync.Map
	tokens.Store("inv-1", "tok")

	server := &ProviderHostServer{
		tokens:      &tokens,
		httpClient:  upstream.Client(),
		validateURL: func(*url.URL) error { return nil },
	}

	_, err := server.ProxyHTTP(context.Background(), &pluginapiv1.ProxyHTTPRequest{
		InvocationId: "inv-1",
		Method:       http.MethodGet,
		Url:          upstream.URL + "/test",
		Headers: map[string]string{
			"X-Custom":        "allowed",
			"X-Forwarded-For": "spoofed",
			"Host":            "evil.com",
			"Authorization":   "Bearer override",
		},
	})
	if err != nil {
		t.Fatalf("ProxyHTTP: %v", err)
	}
	if gotHeaders.Get("X-Custom") != "allowed" {
		t.Fatalf("X-Custom not passed through")
	}
	if gotHeaders.Get("X-Forwarded-For") == "spoofed" {
		t.Fatal("X-Forwarded-For should have been stripped")
	}
	if gotHeaders.Get("Authorization") != "Bearer tok" {
		t.Fatalf("Authorization = %q, want Bearer tok (host-injected, not plugin-supplied)", gotHeaders.Get("Authorization"))
	}
}

func TestProxyHTTPRejectsUnknownInvocation(t *testing.T) {
	t.Parallel()

	var tokens sync.Map
	server := &ProviderHostServer{
		tokens:     &tokens,
		httpClient: http.DefaultClient,
	}

	_, err := server.ProxyHTTP(context.Background(), &pluginapiv1.ProxyHTTPRequest{
		InvocationId: "nonexistent",
		Method:       http.MethodGet,
		Url:          "https://example.com",
	})
	if err == nil {
		t.Fatal("expected error for unknown invocation_id")
	}
}

func TestProxyHTTPEnforcesAllowedHosts(t *testing.T) {
	t.Parallel()

	var tokens sync.Map
	tokens.Store("inv-1", "tok")

	server := &ProviderHostServer{
		tokens:       &tokens,
		httpClient:   http.DefaultClient,
		allowedHosts: []string{"api.slack.com"},
	}

	_, err := server.ProxyHTTP(context.Background(), &pluginapiv1.ProxyHTTPRequest{
		InvocationId: "inv-1",
		Method:       http.MethodGet,
		Url:          "https://evil.com/steal",
	})
	if err == nil {
		t.Fatal("expected error for disallowed host")
	}
}
