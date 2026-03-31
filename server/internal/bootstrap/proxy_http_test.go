package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
)

const (
	testProxyHTTPOp     = "proxy_http"
	testProxyHTTPMethod = "GET"
	testResponseBody    = "hello from test server"
)

func TestPluginHTTPProxyAllowsConfiguredHosts(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, testResponseBody)
	}))
	defer ts.Close()

	host := hostFromURL(t, ts.URL)

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"proxytest": {
				Plugin: &config.ExecutablePluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{host},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("proxytest")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), testProxyHTTPOp, map[string]any{
		"url":    ts.URL + "/test",
		"method": testProxyHTTPMethod,
	}, "")
	if err != nil {
		t.Fatalf("Execute proxy_http: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("proxy_http status = %d, body = %s", result.Status, result.Body)
	}

	var resp proxyHTTPResult
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied status_code = %d", resp.StatusCode)
	}
	if resp.Body != testResponseBody {
		t.Fatalf("proxied body = %q, want %q", resp.Body, testResponseBody)
	}
}

func TestPluginHTTPProxyBlocksUnconfiguredHosts(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, testResponseBody)
	}))
	defer ts.Close()

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"proxyblocked": {
				Plugin: &config.ExecutablePluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{"allowed.example.com"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("proxyblocked")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), testProxyHTTPOp, map[string]any{
		"url":    ts.URL + "/test",
		"method": testProxyHTTPMethod,
	}, "")
	if err != nil {
		t.Fatalf("Execute proxy_http: %v", err)
	}
	if result.Status != http.StatusBadGateway {
		t.Fatalf("expected bad gateway for blocked host, got status = %d, body = %s", result.Status, result.Body)
	}

	var resp proxyHTTPErrorResult
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !strings.Contains(resp.Error, "not in allowed_hosts") {
		t.Fatalf("expected allowed_hosts error, got: %s", resp.Error)
	}
}

func TestPluginHTTPProxyAllowsAllWhenNoAllowedHosts(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, testResponseBody)
	}))
	defer ts.Close()

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"proxyopen": {
				Plugin: &config.ExecutablePluginDef{
					Command: bin,
					Args:    []string{"provider"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("proxyopen")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), testProxyHTTPOp, map[string]any{
		"url":    ts.URL + "/test",
		"method": testProxyHTTPMethod,
	}, "")
	if err != nil {
		t.Fatalf("Execute proxy_http: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("proxy_http status = %d, body = %s", result.Status, result.Body)
	}

	var resp proxyHTTPResult
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied status_code = %d", resp.StatusCode)
	}
	if resp.Body != testResponseBody {
		t.Fatalf("proxied body = %q, want %q", resp.Body, testResponseBody)
	}
}

func TestPluginHTTPProxyWildcardHost(t *testing.T) {
	t.Parallel()

	srv := pluginhost.NewPluginHostServer([]string{"*.example.com"})

	tests := []struct {
		name    string
		host    string
		allowed bool
	}{
		{"subdomain match", "api.example.com", true},
		{"deep subdomain match", "v1.api.example.com", true},
		{"exact mismatch", "example.com", false},
		{"different domain", "other.test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			url := fmt.Sprintf("http://%s/test", tt.host)
			_, err := srv.ProxyHTTP(context.Background(), &proto.ProxyHTTPRequest{
				Method: testProxyHTTPMethod,
				Url:    url,
			})
			if tt.allowed {
				if err != nil && strings.Contains(err.Error(), "not in allowed_hosts") {
					t.Fatalf("expected host %q to be allowed, got: %v", tt.host, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "not in allowed_hosts") {
					t.Fatalf("expected host %q to be blocked, got err: %v", tt.host, err)
				}
			}
		})
	}
}

type proxyHTTPResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type proxyHTTPErrorResult struct {
	Error string `json:"error"`
}

func hostFromURL(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("invalid URL %q: %v", rawURL, err)
	}
	return u.Hostname()
}
