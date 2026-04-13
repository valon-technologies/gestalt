package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestExecutableSDKExampleProviderReceivesStartConfig(t *testing.T) {
	t.Parallel()

	bin := buildExampleProviderBinary(t)
	manifestRoot := exampleProviderRoot(t)
	manifest := newExecutableManifest("Example Provider", "A minimal example provider built with the public SDK")
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"example": {
					Command:              bin,
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					Config: mustNode(t, map[string]any{
						"greeting": "Hello from config",
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
		t.Fatalf("providers.Get(example): %v", err)
	}
	if prov.DisplayName() != "Example Provider" {
		t.Fatalf("DisplayName = %q", prov.DisplayName())
	}
	if prov.Description() != "A minimal example provider built with the public SDK" {
		t.Fatalf("Description = %q", prov.Description())
	}
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 3 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if cat.DisplayName != "Example Provider" || cat.Description != "A minimal example provider built with the public SDK" {
		t.Fatalf("unexpected catalog metadata: %+v", cat)
	}
	if cat.Operations[0].Transport != catalog.TransportPlugin {
		t.Fatalf("unexpected catalog transport: %+v", cat.Operations[0])
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Gestalt"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d", result.Status)
	}
	if result.Body != `{"message":"Hello from config, Gestalt!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}

	result, err = prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status status = %d", result.Status)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(result.Body), &got); err != nil {
		t.Fatalf("json.Unmarshal(status): %v", err)
	}
	if got["name"] != "example" {
		t.Fatalf("status.name = %q", got["name"])
	}
	if got["greeting"] != "Hello from config" {
		t.Fatalf("status.greeting = %q", got["greeting"])
	}
}

func TestPythonSourcePluginFallsBackWithoutGoOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("source-plugin fallback fixture is POSIX-only")
	}

	bin := buildExampleProviderBinary(t)
	root := t.TempDir()
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/testowner/plugins/python-source",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Python Source",
		Description: "Python source provider fixture",
		Spec: &providermanifestv1.Spec{
			Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
		},
	}
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`[tool.gestalt]
plugin = "provider"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(pyproject.toml): %v", err)
	}
	catalogData, err := yaml.Marshal(&catalog.Catalog{
		Name: "python-source",
		Operations: []catalog.CatalogOperation{
			{ID: "greet", Method: http.MethodPost},
			{ID: "status", Method: http.MethodGet},
		},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, providerpkg.StaticCatalogFile), catalogData, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.venv/bin): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv", "bin", "python"), []byte("#!/bin/sh\nset -eu\nexec "+strconv.Quote(bin)+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(.venv/bin/python): %v", err)
	}

	t.Setenv("PATH", t.TempDir())

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"python-source": {
					ResolvedManifest:     manifest,
					ResolvedManifestPath: manifestPath,
					Config: mustNode(t, map[string]any{
						"greeting": "Hi",
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

	prov, err := providers.Get("python-source")
	if err != nil {
		t.Fatalf("providers.Get(python-source): %v", err)
	}

	result, err := prov.Execute(context.Background(), "greet", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("greet status = %d, want %d", result.Status, http.StatusOK)
	}
	if result.Body != `{"message":"Hi, Ada!"}` {
		t.Fatalf("greet body = %q", result.Body)
	}
}

func TestSpecLoadedOpenAPIProviderUsesConfiguredAPIBaseURL(t *testing.T) {
	t.Parallel()

	var docHits atomic.Int32
	docSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		docHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"document"}`))
	}))
	t.Cleanup(docSrv.Close)

	var manifestHits atomic.Int32
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		manifestHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"manifest"}`))
	}))
	t.Cleanup(manifestSrv.Close)

	var configHits atomic.Int32
	var configPath atomic.Value
	configSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configHits.Add(1)
		configPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"config"}`))
	}))
	t.Cleanup(configSrv.Close)

	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	openapiPath := filepath.Join(root, "openapi.yaml")
	openapiDoc := fmt.Sprintf(`openapi: "3.1.0"
info:
  title: Example
  version: "1.0.0"
servers:
  - url: %s
paths:
  /items:
    get:
      operationId: list_items
      responses:
        "200":
          description: OK
`, docSrv.URL)
	if err := os.WriteFile(openapiPath, []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"example": {
					ResolvedManifestPath: manifestPath,
					ResolvedManifest: &providermanifestv1.Manifest{
						Kind:        providermanifestv1.KindPlugin,
						DisplayName: "Example",
						Description: "OpenAPI example",
						Spec: &providermanifestv1.Spec{
							Surfaces: &providermanifestv1.ProviderSurfaces{
								OpenAPI: &providermanifestv1.OpenAPISurface{
									Document: "openapi.yaml",
									BaseURL:  manifestSrv.URL,
								},
							},
						},
					},
					Surfaces: &config.ProviderSurfaceOverrides{
						OpenAPI: &config.ProviderOpenAPISurfaceOverride{
							BaseURL: configSrv.URL,
						},
					},
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}

	result, err := prov.Execute(context.Background(), "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_items): %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusOK)
	}
	if got := result.Body; got != `{"source":"config"}` {
		t.Fatalf("body = %q, want %q", got, `{"source":"config"}`)
	}
	if got, _ := configPath.Load().(string); got != "/items" {
		t.Fatalf("request path = %q, want %q", got, "/items")
	}
	if got := configHits.Load(); got != 1 {
		t.Fatalf("configured base URL hits = %d, want 1", got)
	}
	if got := manifestHits.Load(); got != 0 {
		t.Fatalf("manifest base URL hits = %d, want 0", got)
	}
	if got := docHits.Load(); got != 0 {
		t.Fatalf("document server hits = %d, want 0", got)
	}
}

func TestSpecLoadedDualSurfaceProviderBuildsMCPOperations(t *testing.T) {
	t.Parallel()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":"api"}`))
	}))
	t.Cleanup(apiSrv.Close)

	mcpSrv := mcpserver.NewMCPServer("notion-upstream", "1.0.0")
	mcpSrv.AddTool(
		mcpgo.NewTool("search", mcpgo.WithDescription("Search Notion")),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("from-mcp"), nil
		},
	)
	mcpHTTP := httptest.NewServer(mcpserver.NewStreamableHTTPServer(
		mcpSrv,
		mcpserver.WithStateLess(true),
	))
	t.Cleanup(mcpHTTP.Close)

	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: plugin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest.yaml): %v", err)
	}
	openapiPath := filepath.Join(root, "openapi.yaml")
	openapiDoc := fmt.Sprintf(`openapi: "3.1.0"
info:
  title: Notion
  version: "1.0.0"
servers:
  - url: %s
paths:
  /pages:
    get:
      operationId: list_pages
      responses:
        "200":
          description: OK
`, apiSrv.URL)
	if err := os.WriteFile(openapiPath, []byte(openapiDoc), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"notion": {
					ResolvedManifestPath: manifestPath,
					ResolvedManifest: &providermanifestv1.Manifest{
						Kind:        providermanifestv1.KindPlugin,
						DisplayName: "Notion",
						Description: "Dual-surface provider",
						Spec: &providermanifestv1.Spec{
							Surfaces: &providermanifestv1.ProviderSurfaces{
								OpenAPI: &providermanifestv1.OpenAPISurface{
									Document: "openapi.yaml",
								},
								MCP: &providermanifestv1.MCPSurface{
									URL: mcpHTTP.URL,
								},
							},
						},
					},
				},
			},
		},
	}

	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("notion")
	if err != nil {
		t.Fatalf("providers.Get(notion): %v", err)
	}

	apiResult, err := prov.Execute(context.Background(), "list_pages", nil, "")
	if err != nil {
		t.Fatalf("Execute(list_pages): %v", err)
	}
	if apiResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", apiResult.Status, http.StatusOK)
	}
	if apiResult.Body != `{"source":"api"}` {
		t.Fatalf("body = %q, want %q", apiResult.Body, `{"source":"api"}`)
	}

	directTool, ok := any(prov).(interface {
		CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error)
	})
	if !ok {
		t.Fatalf("provider does not expose direct MCP tools: %T", prov)
	}
	mcpResult, err := directTool.CallTool(context.Background(), "search", nil)
	if err != nil {
		t.Fatalf("CallTool(search): %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected MCP tool error: %+v", mcpResult.Content)
	}
	text, ok := mcpResult.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", mcpResult.Content[0])
	}
	if text.Text != "from-mcp" {
		t.Fatalf("text = %q, want %q", text.Text, "from-mcp")
	}
}

func TestExecutableSDKExampleProviderAppliesConfigMetadataOverrides(t *testing.T) {
	t.Parallel()

	const iconSVG = `<svg viewBox="0 0 10 10"><rect x="1" y="1" width="8" height="8"/></svg>`

	bin := buildExampleProviderBinary(t)
	iconPath := t.TempDir() + "/override.svg"
	if err := os.WriteFile(iconPath, []byte(iconSVG), 0o644); err != nil {
		t.Fatalf("WriteFile(icon): %v", err)
	}

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name:        "example",
		DisplayName: "Catalog Display",
		Description: "Catalog Description",
		Operations: []catalog.CatalogOperation{
			{ID: "status", Method: http.MethodGet},
		},
	})
	manifest := newExecutableManifest("Manifest Display", "Manifest Description")

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"example": {
					DisplayName:          "Config Display",
					Description:          "Config Description",
					IconFile:             iconPath,
					Command:              bin,
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

	prov, err := providers.Get("example")
	if err != nil {
		t.Fatalf("providers.Get(example): %v", err)
	}
	if prov.DisplayName() != "Config Display" {
		t.Fatalf("DisplayName = %q, want %q", prov.DisplayName(), "Config Display")
	}
	if prov.Description() != "Config Description" {
		t.Fatalf("Description = %q, want %q", prov.Description(), "Config Description")
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if cat.DisplayName != "Config Display" {
		t.Fatalf("catalog DisplayName = %q, want %q", cat.DisplayName, "Config Display")
	}
	if cat.Description != "Config Description" {
		t.Fatalf("catalog Description = %q, want %q", cat.Description, "Config Description")
	}
	if cat.IconSVG != iconSVG {
		t.Fatalf("catalog IconSVG = %q, want %q", cat.IconSVG, iconSVG)
	}
}

func buildEchoPluginBinary(t *testing.T) string {
	t.Helper()
	if sharedEchoPluginBin == "" {
		t.Fatal("shared echo plugin binary not initialized")
	}
	return sharedEchoPluginBin
}

func buildExampleProviderBinary(t *testing.T) string {
	t.Helper()
	if sharedExampleProviderBin == "" {
		t.Fatal("shared example provider binary not initialized")
	}
	return sharedExampleProviderBin
}

func exampleProviderRoot(t *testing.T) string {
	t.Helper()
	return testutil.ExampleProviderPluginPath(t)
}

func mustNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func writeStaticCatalog(t *testing.T, cat *catalog.Catalog) string {
	t.Helper()
	data, err := yaml.Marshal(cat)
	if err != nil {
		t.Fatalf("yaml.Marshal(catalog): %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, providerpkg.StaticCatalogFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog): %v", err)
	}
	return dir
}

func newExecutableManifest(displayName, description string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/acme/plugins/test",
		Version:     "1.0.0",
		DisplayName: displayName,
		Description: description,
		Spec:        &providermanifestv1.Spec{},
	}
}

func TestPluginManifestOAuthWiresConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	manifest.Spec.Auth = &providermanifestv1.ProviderAuth{
		Type:             providermanifestv1.AuthTypeOAuth2,
		AuthorizationURL: "https://example.com/authorize",
		TokenURL:         "https://example.com/token",
		Scopes:           []string{"read", "write"},
	}
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoauth": {
					Command: bin,
					Args:    []string{"provider"},
					Config: mustNode(t, map[string]any{
						"clientId":     "test-client-id",
						"clientSecret": "test-client-secret",
					}),
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(
		context.Background(), cfg, factories,
		Deps{BaseURL: "https://gestalt.example.com"},
	)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoauth")
	if err != nil {
		t.Fatalf("providers.Get(echoauth): %v", err)
	}
	if cat := prov.Catalog(); cat == nil || len(cat.Operations) == 0 {
		t.Fatal("expected at least one operation from the echo provider")
	}

	handlers, ok := connAuth["echoauth"]
	if !ok {
		t.Fatal("expected connection auth entry for echoauth")
	}
	handler, ok := handlers[config.PluginConnectionName]
	if !ok {
		t.Fatalf("expected handler for connection %q", config.PluginConnectionName)
	}
	if handler.AuthorizationBaseURL() != "https://example.com/authorize" {
		t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/authorize")
	}
	if handler.TokenURL() != "https://example.com/token" {
		t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/token")
	}
}

func TestPluginManifestNoAuthSkipsConnectionAuth(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echonoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echonoauth": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, connAuth, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	if _, ok := connAuth["echonoauth"]; ok {
		t.Fatal("expected no connection auth for plugin without oauth2 auth")
	}
}

func TestPluginManifestNamedOAuthKeepsProviderTokenMode(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)

	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoauth",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoauth": {
					Command:           bin,
					Args:              []string{"provider"},
					Source:            config.ProviderSource{Ref: "github.com/acme/plugins/test", Version: "1.0.0"},
					DefaultConnection: "workspace",
					Connections: map[string]*config.ConnectionDef{
						"workspace": {
							Auth: config.ConnectionAuthDef{
								Type:             providermanifestv1.AuthTypeOAuth2,
								AuthorizationURL: "https://example.com/authorize",
								TokenURL:         "https://example.com/token",
							},
						},
					},
					Config: mustNode(t, map[string]any{
						"clientId":     "test-client-id",
						"clientSecret": "test-client-secret",
					}),
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	providers, _, err := buildProvidersStrict(
		context.Background(), cfg, factories,
		Deps{BaseURL: "https://gestalt.example.com"},
	)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	prov, err := providers.Get("echoauth")
	if err != nil {
		t.Fatalf("providers.Get(echoauth): %v", err)
	}
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode = %q, want %q", prov.ConnectionMode(), core.ConnectionModeUser)
	}
}

func TestPluginProcessEnvIsolation(t *testing.T) {
	t.Parallel()
	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: http.MethodPost},
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
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

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": "USER"}, "")
	if err != nil {
		t.Fatalf("Execute read_env: %v", err)
	}

	var env struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Found {
		t.Fatalf("plugin process should not see USER, but got %q", env.Value)
	}

	result, err = prov.Execute(context.Background(), "read_env", map[string]any{"name": "PATH"}, "")
	if err != nil {
		t.Fatalf("Execute read_env PATH: %v", err)
	}
	if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Found || env.Value == "" {
		t.Fatal("plugin process should see PATH")
	}
}

func TestPluginIndexedDBBindingsExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	makeConfig := func(bindings []string) *config.Config {
		return &config.Config{
			Providers: config.ProvidersConfig{
				Plugins: map[string]*config.ProviderEntry{
					"echoext": {
						Command:              bin,
						Args:                 []string{"provider"},
						ResolvedManifest:     manifest,
						ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
						IndexedDBs:           bindings,
					},
				},
			},
		}
	}

	services := coretesting.NewStubServices(t)
	indexedDBBindings := map[string]indexeddb.IndexedDB{
		"main":    services.DB,
		"archive": &coretesting.StubIndexedDB{},
	}

	checkEnv := func(t *testing.T, bindings []string, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			Services:   services,
			IndexedDBs: indexedDBBindings,
		})
		if err != nil {
			t.Fatalf("buildProvidersStrict: %v", err)
		}
		defer func() { _ = CloseProviders(providers) }()

		prov, err := providers.Get("echoext")
		if err != nil {
			t.Fatalf("providers.Get: %v", err)
		}
		result, err := prov.Execute(context.Background(), "read_env", map[string]any{"name": envName}, "")
		if err != nil {
			t.Fatalf("Execute read_env: %v", err)
		}
		var env struct {
			Value string `json:"value"`
			Found bool   `json:"found"`
		}
		if err := json.Unmarshal([]byte(result.Body), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return env.Found && env.Value != ""
	}

	if got := checkEnv(t, nil, providerhost.DefaultIndexedDBSocketEnv); got {
		t.Fatal("default IndexedDB env should not be set without plugin indexeddb bindings")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set with a single plugin indexeddb binding")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.IndexedDBSocketEnv("main")); !got {
		t.Fatal("named IndexedDB env should be set with a single plugin indexeddb binding")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.DefaultIndexedDBSocketEnv); got {
		t.Fatal("default IndexedDB env should not be set with multiple plugin indexeddb bindings")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.IndexedDBSocketEnv("main")); !got {
		t.Fatal(`named IndexedDB env for "main" should be set with multiple plugin indexeddb bindings`)
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.IndexedDBSocketEnv("archive")); !got {
		t.Fatal(`named IndexedDB env for "archive" should be set with multiple plugin indexeddb bindings`)
	}
}

func TestIndexedDBNamespaceUsesProviderName(t *testing.T) {
	t.Parallel()

	if got := indexedDBNamespace("roadmap-review"); got != "roadmap-review" {
		t.Fatalf("indexedDBNamespace() = %q, want %q", got, "roadmap-review")
	}
	if got := indexedDBNamespace("source-backed"); got != "source-backed" {
		t.Fatalf("indexedDBNamespace() = %q, want %q", got, "source-backed")
	}
}

func TestExecutablePluginRequiresManifest(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command: bin,
					Args:    []string{"provider"},
				},
			},
		},
	}

	factories := NewFactoryRegistry()
	_, _, err := buildProvidersStrict(context.Background(), cfg, factories, Deps{})
	if err == nil {
		t.Fatal("expected buildProvidersStrict to reject executable plugin without manifest")
	}
	if got := err.Error(); got != `bootstrap: provider validation failed: integration "echoext": integration "echoext" must resolve to a provider manifest` {
		t.Fatalf("unexpected error: %v", err)
	}
}
