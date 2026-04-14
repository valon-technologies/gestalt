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
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
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
		Plugins: map[string]*config.ProviderEntry{
			"python-source": {
				ResolvedManifest:     manifest,
				ResolvedManifestPath: manifestPath,
				Config: mustNode(t, map[string]any{
					"greeting": "Hi",
				}),
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
		return
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
		Plugins: map[string]*config.ProviderEntry{
			"echonoauth": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
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

func TestPluginIndexedDBExposeHostSocketEnv(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	makeConfig := func(indexedDB *config.PluginIndexedDBConfig) *config.Config {
		return &config.Config{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDB:            indexedDB,
				},
			},
		}
	}

	indexedDBDefs := map[string]*config.ProviderEntry{
		"main": {
			Config: mustNode(t, map[string]any{"dsn": "postgres://main.example.test/gestalt"}),
		},
		"archive": {
			Config: mustNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
		},
	}

	checkEnv := func(t *testing.T, indexedDB *config.PluginIndexedDBConfig, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(indexedDB), NewFactoryRegistry(), Deps{
			SelectedIndexedDBName: "main",
			IndexedDBDefs:         indexedDBDefs,
			IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
				return &coretesting.StubIndexedDB{}, nil
			},
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

	if got := checkEnv(t, nil, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin omits indexeddb and inherits the host selection")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin indexeddb is explicitly empty")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{Provider: "archive"}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set when plugin explicitly selects one indexeddb provider")
	}
	if got := checkEnv(t, nil, providerhost.IndexedDBSocketEnv("main")); got {
		t.Fatal("named IndexedDB env should not be set for inherited plugin indexeddb access")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{Provider: "archive"}, providerhost.IndexedDBSocketEnv("archive")); got {
		t.Fatal("named IndexedDB env should not be set when plugins expose a single indexeddb socket")
	}
	if got := checkEnv(t, &config.PluginIndexedDBConfig{Disabled: true}, providerhost.DefaultIndexedDBSocketEnv); got {
		t.Fatal("default IndexedDB env should not be set when plugin indexeddb is disabled")
	}
}

func TestPluginCacheBindingsExposeHostSocketEnv(t *testing.T) {
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
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					Cache:                bindings,
				},
			},
		}
	}

	cacheBindings := map[string]*config.ProviderEntry{
		"session": {Config: mustNode(t, map[string]any{"namespace": "session"})},
		"rate_limit": {
			Config: mustNode(t, map[string]any{"namespace": "rate_limit"}),
		},
	}

	checkEnv := func(t *testing.T, bindings []string, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			CacheDefs: cacheBindings,
			CacheFactory: func(yaml.Node) (corecache.Cache, error) {
				return coretesting.NewStubCache(), nil
			},
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

	if got := checkEnv(t, nil, providerhost.DefaultCacheSocketEnv); got {
		t.Fatal("default cache env should not be set without plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session"}, providerhost.DefaultCacheSocketEnv); !got {
		t.Fatal("default cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session"}, providerhost.CacheSocketEnv("session")); !got {
		t.Fatal("named cache env should be set with a single plugin cache binding")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.DefaultCacheSocketEnv); got {
		t.Fatal("default cache env should not be set with multiple plugin cache bindings")
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.CacheSocketEnv("session")); !got {
		t.Fatal(`named cache env for "session" should be set with multiple plugin cache bindings`)
	}
	if got := checkEnv(t, []string{"session", "rate_limit"}, providerhost.CacheSocketEnv("rate_limit")); !got {
		t.Fatal(`named cache env for "rate_limit" should be set with multiple plugin cache bindings`)
	}
}

func TestPluginIndexedDBInheritsHostSelectionAndDefaultDBName(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	cases := []struct {
		name      string
		indexedDB *config.PluginIndexedDBConfig
	}{
		{name: "omitted indexeddb inherits host selection"},
		{name: "empty indexeddb inherits host selection", indexedDB: &config.PluginIndexedDBConfig{}},
		{name: "objectStores-only indexeddb inherits host selection", indexedDB: &config.PluginIndexedDBConfig{ObjectStores: []string{"tasks"}}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			boundDB := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
			providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
				Plugins: map[string]*config.ProviderEntry{
					"echoext": {
						Command:              bin,
						Args:                 []string{"provider"},
						ResolvedManifest:     manifest,
						ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
						IndexedDB:            tc.indexedDB,
					},
				},
			}, NewFactoryRegistry(), Deps{
				SelectedIndexedDBName: "memory",
				IndexedDBDefs: map[string]*config.ProviderEntry{
					"memory": {
						Source: config.ProviderSource{Path: "./providers/datastore/memory"},
						Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
					},
				},
				IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
					return boundDB, nil
				},
			})
			if err != nil {
				t.Fatalf("buildProvidersStrict: %v", err)
			}
			t.Cleanup(func() { _ = CloseProviders(providers) })

			prov, err := providers.Get("echoext")
			if err != nil {
				t.Fatalf("providers.Get: %v", err)
			}
			result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
				"store": "tasks",
				"id":    "task-1",
				"value": "ship-it",
			}, "")
			if err != nil {
				t.Fatalf("Execute indexeddb_roundtrip: %v", err)
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
				t.Fatalf("unmarshal record: %v", err)
			}
			if got := record["value"]; got != "ship-it" {
				t.Fatalf("record value = %#v, want %q", got, "ship-it")
			}
			if _, err := boundDB.ObjectStore("echoext_tasks").Get(context.Background(), "task-1"); err != nil {
				t.Fatalf("inherited host indexeddb should use plugin-name default db prefix: %v", err)
			}
		})
	}
}

func TestPluginIndexedDBBuildScopedConfig(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{ID: "read_env", Method: http.MethodGet, Parameters: []catalog.CatalogParameter{{Name: "name", Type: "string", Required: true}}},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	type capturedIndexedDBConfig struct {
		Config map[string]any `yaml:"config"`
	}

	makeConfig := func(indexedDB *config.PluginIndexedDBConfig) *config.Config {
		return &config.Config{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDB:            indexedDB,
				},
			},
		}
	}

	indexedDBDefs := map[string]*config.ProviderEntry{
		"postgres": {
			Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb"},
			Config: mustNode(t, map[string]any{
				"dsn":                 "postgres://db.example.test/gestalt",
				"schema":              "host_schema",
				"namespace":           "host_schema_alias_should_be_removed",
				"legacy_table_prefix": "host_legacy_should_be_replaced_",
				"legacy_prefix":       "host_legacy_alias_should_be_removed_",
			}),
		},
		"sqlite": {
			Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb"},
			Config: mustNode(t, map[string]any{
				"dsn":                 "sqlite://plugin-state.db",
				"table_prefix":        "host_",
				"prefix":              "host_",
				"schema":              "should_be_removed",
				"namespace":           "should_be_removed",
				"legacy_table_prefix": "host_legacy_should_be_replaced_",
				"legacy_prefix":       "host_legacy_alias_should_be_removed_",
			}),
		},
		"local-postgres": {
			Source: config.ProviderSource{Path: "./relationaldb/manifest.yaml"},
			Config: mustNode(t, map[string]any{
				"dsn":                 "postgres://local.example.test/gestalt",
				"schema":              "host_local",
				"namespace":           "host_local_alias_should_be_removed",
				"legacy_table_prefix": "host_local_legacy_should_be_replaced_",
				"legacy_prefix":       "host_local_legacy_alias_should_be_removed_",
			}),
		},
	}

	cases := []struct {
		name       string
		indexedDB  *config.PluginIndexedDBConfig
		wantDSN    string
		wantDB     string
		wantSQLite bool
	}{
		{
			name:      "defaults db to plugin name for postgres",
			indexedDB: &config.PluginIndexedDBConfig{Provider: "postgres"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "echoext",
		},
		{
			name:      "uses db override for postgres",
			indexedDB: &config.PluginIndexedDBConfig{Provider: "postgres", DB: "roadmap_state"},
			wantDSN:   "postgres://db.example.test/gestalt",
			wantDB:    "roadmap_state",
		},
		{
			name:       "uses db override for sqlite table prefixes",
			indexedDB:  &config.PluginIndexedDBConfig{Provider: "sqlite", DB: "roadmap_state"},
			wantDSN:    "sqlite://plugin-state.db",
			wantDB:     "roadmap_state",
			wantSQLite: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var closeCount atomic.Int32
			captured := make(map[string]capturedIndexedDBConfig)
			providers, _, err := buildProvidersStrict(context.Background(), makeConfig(tc.indexedDB), NewFactoryRegistry(), Deps{
				SelectedIndexedDBName: "postgres",
				IndexedDBDefs:         indexedDBDefs,
				IndexedDBFactory: func(node yaml.Node) (indexeddb.IndexedDB, error) {
					var decoded capturedIndexedDBConfig
					if err := node.Decode(&decoded); err != nil {
						return nil, err
					}
					dsn, _ := decoded.Config["dsn"].(string)
					captured[dsn] = decoded
					return &trackedIndexedDB{
						StubIndexedDB: coretesting.StubIndexedDB{},
						onClose:       closeCount.Add,
					}, nil
				},
			})
			if err != nil {
				t.Fatalf("buildProvidersStrict: %v", err)
			}
			t.Cleanup(func() {
				if providers != nil {
					_ = CloseProviders(providers)
				}
			})

			cfg, ok := captured[tc.wantDSN]
			if !ok {
				t.Fatalf("missing captured indexeddb config for %q", tc.wantDSN)
			}
			if tc.wantSQLite {
				wantPrefix := tc.wantDB + "_"
				if got := cfg.Config["table_prefix"]; got != wantPrefix {
					t.Fatalf("sqlite table_prefix = %#v, want %q", got, wantPrefix)
				}
				if got := cfg.Config["prefix"]; got != wantPrefix {
					t.Fatalf("sqlite prefix = %#v, want %q", got, wantPrefix)
				}
				if _, ok := cfg.Config["schema"]; ok {
					t.Fatalf("sqlite schema should be removed, got %#v", cfg.Config["schema"])
				}
			} else {
				if got := cfg.Config["schema"]; got != tc.wantDB {
					t.Fatalf("schema = %#v, want %q", got, tc.wantDB)
				}
				if _, ok := cfg.Config["table_prefix"]; ok {
					t.Fatalf("table_prefix should be removed, got %#v", cfg.Config["table_prefix"])
				}
				if _, ok := cfg.Config["prefix"]; ok {
					t.Fatalf("prefix should be removed, got %#v", cfg.Config["prefix"])
				}
			}
			if _, ok := cfg.Config["namespace"]; ok {
				t.Fatalf("namespace should be removed, got %#v", cfg.Config["namespace"])
			}
			if _, ok := cfg.Config["legacy_table_prefix"]; ok {
				t.Fatalf("legacy_table_prefix should be removed, got %#v", cfg.Config["legacy_table_prefix"])
			}
			if _, ok := cfg.Config["legacy_prefix"]; ok {
				t.Fatalf("legacy_prefix should be removed, got %#v", cfg.Config["legacy_prefix"])
			}

			_ = CloseProviders(providers)
			providers = nil
			if got := closeCount.Load(); got != 1 {
				t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
			}
		})
	}
}

func TestPluginIndexedDBRouteObjectStoresAndTransportPrefix(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	var (
		closeCount atomic.Int32
		boundDB    *trackedIndexedDB
	)
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.PluginIndexedDBConfig{
					Provider:     "memory",
					DB:           "roadmap",
					ObjectStores: []string{"tasks"},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "memory",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.ProviderSource{Path: "./providers/datastore/memory"},
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			boundDB = &trackedIndexedDB{
				StubIndexedDB: coretesting.StubIndexedDB{},
				onClose:       closeCount.Add,
			}
			return boundDB, nil
		},
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-1",
		"value": "ship-it",
	}, "")
	if err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got := record["value"]; got != "ship-it" {
		t.Fatalf("record value = %#v, want %q", got, "ship-it")
	}
	if _, err := boundDB.ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err != nil {
		t.Fatalf("prefixed backing store should contain task: %v", err)
	}
	if _, err := boundDB.ObjectStore("tasks").Get(context.Background(), "task-1"); err == nil {
		t.Fatal("unprefixed backing store should remain empty")
	}

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "events",
		"id":    "evt-1",
		"value": "blocked",
	}, ""); err == nil {
		t.Fatal("indexeddb_roundtrip on disallowed object store should fail")
	}

	_ = CloseProviders(providers)
	providers = nil
	if got := closeCount.Load(); got != 1 {
		t.Fatalf("closeCount after provider shutdown = %d, want 1", got)
	}
}

func TestPluginIndexedDBProviderOverrideUsesExplicitHostIndexedDB(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "store", Type: "string", Required: true},
					{Name: "id", Type: "string", Required: true},
					{Name: "value", Type: "string", Required: true},
				},
			},
		},
	})
	manifest := newExecutableManifest("Echo", "Echoes back the input parameters")

	boundDBs := make(map[string]*trackedIndexedDB)
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command:              bin,
				Args:                 []string{"provider"},
				ResolvedManifest:     manifest,
				ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
				IndexedDB: &config.PluginIndexedDBConfig{
					Provider: "archive",
					DB:       "roadmap",
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		SelectedIndexedDBName: "main",
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.ProviderSource{Path: "./providers/datastore/main"},
				Config: mustNode(t, map[string]any{"bucket": "main"}),
			},
			"archive": {
				Source: config.ProviderSource{Path: "./providers/datastore/archive"},
				Config: mustNode(t, map[string]any{"bucket": "archive"}),
			},
		},
		IndexedDBFactory: func(node yaml.Node) (indexeddb.IndexedDB, error) {
			var decoded struct {
				Config map[string]any `yaml:"config"`
			}
			if err := node.Decode(&decoded); err != nil {
				return nil, err
			}
			bucket, _ := decoded.Config["bucket"].(string)
			db := &trackedIndexedDB{StubIndexedDB: coretesting.StubIndexedDB{}}
			boundDBs[bucket] = db
			return db, nil
		},
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}

	result, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "events",
		"id":    "evt-1",
		"value": "stored",
	}, "")
	if err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(result.Body), &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if got := record["value"]; got != "stored" {
		t.Fatalf("record value = %#v, want %q", got, "stored")
	}
	if len(boundDBs) != 1 {
		t.Fatalf("boundDBs = %d, want 1 explicit provider build", len(boundDBs))
	}
	if _, ok := boundDBs["main"]; ok {
		t.Fatal("main indexeddb should not be rebuilt when plugin explicitly selects archive")
	}
	if _, err := boundDBs["archive"].ObjectStore("roadmap_events").Get(context.Background(), "evt-1"); err != nil {
		t.Fatalf("archive backing store should contain event: %v", err)
	}
}

type trackedIndexedDB struct {
	coretesting.StubIndexedDB
	onClose func(int32) int32
}

func (t *trackedIndexedDB) Close() error {
	if t.onClose != nil {
		t.onClose(1)
	}
	return t.StubIndexedDB.Close()
}

func TestExecutablePluginRequiresManifest(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	cfg := &config.Config{
		Plugins: map[string]*config.ProviderEntry{
			"echoext": {
				Command: bin,
				Args:    []string{"provider"},
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
