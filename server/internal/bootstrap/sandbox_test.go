//go:build linux || darwin

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func buildGestaltdBinary(t *testing.T) string {
	t.Helper()
	if sharedGestaltdBin == "" {
		t.Fatal("shared gestaltd binary not initialized")
	}
	return sharedGestaltdBin
}

func hostFromTestServer(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	host, _, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return host
}

func TestSandboxedPluginCannotReadUnauthorizedFile(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	bin := buildEchoPluginBinary(t)
	hostBin := buildGestaltdBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"sandboxed": {
				Plugin: &config.PluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{"localhost"},
					HostBinary:   hostBin,
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

	prov, err := providers.Get("sandboxed")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_file", map[string]any{"path": secret}, "")
	if err != nil {
		t.Fatalf("Execute read_file: %v", err)
	}

	if result.Status == http.StatusOK {
		t.Fatal("sandboxed plugin should not be able to read unauthorized file")
	}
}

func TestSandboxedPluginCanCommunicateViaGRPC(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	hostBin := buildGestaltdBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"sandboxed": {
				Plugin: &config.PluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{"localhost"},
					HostBinary:   hostBin,
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

	prov, err := providers.Get("sandboxed")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "echo", map[string]any{"message": "hello"}, "")
	if err != nil {
		t.Fatalf("Execute echo: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("echo status = %d, body = %s", result.Status, result.Body)
	}
	if result.Body != `{"message":"hello"}` {
		t.Fatalf("echo body = %q", result.Body)
	}
}

func TestSandboxDisabledByDefault(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "readable.txt")
	if err := os.WriteFile(secret, []byte("readable-content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bin := buildEchoPluginBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"nosandbox": {
				Plugin: &config.PluginDef{
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

	prov, err := providers.Get("nosandbox")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_file", map[string]any{"path": secret}, "")
	if err != nil {
		t.Fatalf("Execute read_file: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("should be able to read file without sandbox, status = %d", result.Status)
	}
}

func TestSandboxedPluginHTTPProxyAllowsConfiguredHosts(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "allowed-response")
	}))
	defer ts.Close()

	host := hostFromTestServer(t, ts)
	bin := buildEchoPluginBinary(t)
	hostBin := buildGestaltdBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"proxied": {
				Plugin: &config.PluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{host},
					HostBinary:   hostBin,
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

	prov, err := providers.Get("proxied")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "make_http_request", map[string]any{"url": ts.URL + "/test"}, "")
	if err != nil {
		t.Fatalf("Execute make_http_request: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("make_http_request status = %d, body = %s", result.Status, result.Body)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bodyStr, ok := body["body"].(string); !ok || !strings.Contains(bodyStr, "allowed-response") {
		t.Fatalf("expected allowed-response in body, got %v", body)
	}
}

func TestSandboxedPluginHTTPProxyBlocksUnconfiguredHosts(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "should-not-see-this")
	}))
	defer ts.Close()

	bin := buildEchoPluginBinary(t)
	hostBin := buildGestaltdBinary(t)

	cfg := &config.Config{
		Integrations: map[string]config.IntegrationDef{
			"blocked": {
				Plugin: &config.PluginDef{
					Command:      bin,
					Args:         []string{"provider"},
					AllowedHosts: []string{"not-a-real-host.example.com"},
					HostBinary:   hostBin,
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

	prov, err := providers.Get("blocked")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "make_http_request", map[string]any{"url": ts.URL + "/test"}, "")
	if err != nil {
		t.Fatalf("Execute make_http_request: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bodyStr, ok := body["body"].(string); ok && strings.Contains(bodyStr, "should-not-see-this") {
		t.Fatal("proxy should have blocked the request, but it reached the test server")
	}
}
