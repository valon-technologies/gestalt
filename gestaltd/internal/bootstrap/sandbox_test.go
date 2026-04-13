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

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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

func echoExecutableManifest(t *testing.T, name string, ops ...catalog.CatalogOperation) (string, *providermanifestv1.Manifest) {
	t.Helper()
	root := writeStaticCatalog(t, &catalog.Catalog{
		Name:       name,
		Operations: ops,
	})
	return root, newExecutableManifest("Echo", "Echoes back the input parameters")
}

func TestSandboxedPluginCannotReadUnauthorizedFile(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	bin := buildEchoPluginBinary(t)
	hostBin := buildGestaltdBinary(t)
	manifestRoot, manifest := echoExecutableManifest(t, "sandboxed",
		catalog.CatalogOperation{ID: "echo", Method: http.MethodPost},
		catalog.CatalogOperation{ID: "read_file", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "path", Type: "string", Required: true}}},
	)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"sandboxed": {
					Command:              bin,
					Args:                 []string{"provider"},
					AllowedHosts:         []string{"localhost"},
					HostBinary:           hostBin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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
	manifestRoot, manifest := echoExecutableManifest(t, "sandboxed",
		catalog.CatalogOperation{ID: "echo", Method: http.MethodPost},
	)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"sandboxed": {
					Command:              bin,
					Args:                 []string{"provider"},
					AllowedHosts:         []string{"localhost"},
					HostBinary:           hostBin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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

func TestSandboxedSynthesizedSourcePluginCanStart(t *testing.T) {
	t.Parallel()

	hostBin := buildGestaltdBinary(t)
	manifestPath := filepath.Join(exampleProviderRoot(t), "manifest.yaml")
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile: %v", err)
	}

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"example": {
					AllowedHosts:         []string{"localhost"},
					HostBinary:           hostBin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: manifestPath,
					Config: mustNode(t, map[string]any{
						"greeting": "Hello from sandbox",
					}),
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

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Gestalt"}, "")
	if err != nil {
		t.Fatalf("Execute greet: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d, body = %s", result.Status, result.Body)
	}
	if result.Body != `{"message":"Hello from sandbox, Gestalt!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}
}

func TestSandboxDisabledByDefault(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "readable.txt")
	if err := os.WriteFile(secret, []byte("readable-content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	bin := buildEchoPluginBinary(t)
	manifestRoot, manifest := echoExecutableManifest(t, "nosandbox",
		catalog.CatalogOperation{ID: "echo", Method: http.MethodPost},
		catalog.CatalogOperation{ID: "read_file", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "path", Type: "string", Required: true}}},
	)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"nosandbox": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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
	manifestRoot, manifest := echoExecutableManifest(t, "proxied",
		catalog.CatalogOperation{ID: "make_http_request", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "url", Type: "string", Required: true}}},
	)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"proxied": {
					Command:              bin,
					Args:                 []string{"provider"},
					AllowedHosts:         []string{host},
					HostBinary:           hostBin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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
	manifestRoot, manifest := echoExecutableManifest(t, "blocked",
		catalog.CatalogOperation{ID: "make_http_request", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "url", Type: "string", Required: true}}},
	)

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"blocked": {
					Command:              bin,
					Args:                 []string{"provider"},
					AllowedHosts:         []string{"not-a-real-host.example.com"},
					HostBinary:           hostBin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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
