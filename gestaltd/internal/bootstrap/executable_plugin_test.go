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
	"github.com/valon-technologies/gestalt/server/core/fileapi"
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

	makeConfig := func(bindings []config.PluginIndexedDBBinding) *config.Config {
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

	indexedDBBindings := map[string]*config.ProviderEntry{
		"main": {
			Config: mustNode(t, map[string]any{"dsn": "postgres://main.example.test/gestalt"}),
		},
		"archive": {
			Config: mustNode(t, map[string]any{"dsn": "sqlite://archive.db"}),
		},
	}

	checkEnv := func(t *testing.T, bindings []config.PluginIndexedDBBinding, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			IndexedDBDefs: indexedDBBindings,
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

	if got := checkEnv(t, nil, providerhost.DefaultIndexedDBSocketEnv); got {
		t.Fatal("default IndexedDB env should not be set without plugin indexeddb bindings")
	}
	if got := checkEnv(t, []config.PluginIndexedDBBinding{{Name: "main"}}, providerhost.DefaultIndexedDBSocketEnv); !got {
		t.Fatal("default IndexedDB env should be set with a single plugin indexeddb binding")
	}
	if got := checkEnv(t, []config.PluginIndexedDBBinding{{Name: "main"}}, providerhost.IndexedDBSocketEnv("main")); !got {
		t.Fatal("named IndexedDB env should be set with a single plugin indexeddb binding")
	}
	if got := checkEnv(t, []config.PluginIndexedDBBinding{{Name: "main"}, {Name: "archive"}}, providerhost.DefaultIndexedDBSocketEnv); got {
		t.Fatal("default IndexedDB env should not be set with multiple plugin indexeddb bindings")
	}
	if got := checkEnv(t, []config.PluginIndexedDBBinding{{Name: "main"}, {Name: "archive"}}, providerhost.IndexedDBSocketEnv("main")); !got {
		t.Fatal(`named IndexedDB env for "main" should be set with multiple plugin indexeddb bindings`)
	}
	if got := checkEnv(t, []config.PluginIndexedDBBinding{{Name: "main"}, {Name: "archive"}}, providerhost.IndexedDBSocketEnv("archive")); !got {
		t.Fatal(`named IndexedDB env for "archive" should be set with multiple plugin indexeddb bindings`)
	}
}

func TestPluginFileAPIBindingsExposeHostSocketEnv(t *testing.T) {
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
						FileAPIs:             bindings,
					},
				},
			},
		}
	}

	checkEnv := func(t *testing.T, bindings []string, envName string) bool {
		t.Helper()
		providers, _, err := buildProvidersStrict(context.Background(), makeConfig(bindings), NewFactoryRegistry(), Deps{
			FileAPIs: map[string]fileapi.FileAPI{
				"main":    &coretesting.StubFileAPI{},
				"archive": &coretesting.StubFileAPI{},
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

	if got := checkEnv(t, nil, providerhost.DefaultFileAPISocketEnv); got {
		t.Fatal("default FileAPI env should not be set without plugin fileapi bindings")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.DefaultFileAPISocketEnv); !got {
		t.Fatal("default FileAPI env should be set with a single plugin fileapi binding")
	}
	if got := checkEnv(t, []string{"main"}, providerhost.FileAPISocketEnv("main")); !got {
		t.Fatal("named FileAPI env should be set with a single plugin fileapi binding")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.DefaultFileAPISocketEnv); got {
		t.Fatal("default FileAPI env should not be set with multiple plugin fileapi bindings")
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.FileAPISocketEnv("main")); !got {
		t.Fatal(`named FileAPI env for "main" should be set with multiple plugin fileapi bindings`)
	}
	if got := checkEnv(t, []string{"main", "archive"}, providerhost.FileAPISocketEnv("archive")); !got {
		t.Fatal(`named FileAPI env for "archive" should be set with multiple plugin fileapi bindings`)
	}
}

func TestPluginIndexedDBBindingsBuildScopedConfig(t *testing.T) {
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

	makeConfig := func(schema string) *config.Config {
		entry := &config.ProviderEntry{
			Command:              bin,
			Args:                 []string{"provider"},
			ResolvedManifest:     manifest,
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			IndexedDBs: []config.PluginIndexedDBBinding{
				{Name: "postgres"},
				{Name: "sqlite"},
				{Name: "local-postgres"},
			},
		}
		if schema != "" {
			entry.IndexedDBSchema = schema
		}
		return &config.Config{
			Providers: config.ProvidersConfig{
				Plugins: map[string]*config.ProviderEntry{
					"echoext": entry,
				},
			},
		}
	}

	bindingDefs := map[string]*config.ProviderEntry{
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
		schema     string
		wantSchema string
	}{
		{name: "defaults to plugin name", wantSchema: "echoext"},
		{name: "uses plugin override", schema: "roadmap_state", wantSchema: "roadmap_state"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var closeCount atomic.Int32
			captured := make(map[string]capturedIndexedDBConfig)
			providers, _, err := buildProvidersStrict(context.Background(), makeConfig(tc.schema), NewFactoryRegistry(), Deps{
				IndexedDBDefs: bindingDefs,
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

			if got := closeCount.Load(); got != 0 {
				t.Fatalf("closeCount before provider shutdown = %d, want 0", got)
			}

			postgresCfg, ok := captured["postgres://db.example.test/gestalt"]
			if !ok {
				t.Fatal("missing captured postgres indexeddb config")
			}
			if got := postgresCfg.Config["schema"]; got != tc.wantSchema {
				t.Fatalf("postgres schema = %#v, want %q", got, tc.wantSchema)
			}
			if _, ok := postgresCfg.Config["namespace"]; ok {
				t.Fatalf("postgres namespace should be removed, got %#v", postgresCfg.Config["namespace"])
			}
			if got := postgresCfg.Config["legacy_table_prefix"]; got != "plugin_echoext_" {
				t.Fatalf("postgres legacy_table_prefix = %#v, want %q", got, "plugin_echoext_")
			}
			if _, ok := postgresCfg.Config["legacy_prefix"]; ok {
				t.Fatalf("postgres legacy_prefix should be removed, got %#v", postgresCfg.Config["legacy_prefix"])
			}
			if _, ok := postgresCfg.Config["table_prefix"]; ok {
				t.Fatalf("postgres table_prefix should be removed, got %#v", postgresCfg.Config["table_prefix"])
			}
			if _, ok := postgresCfg.Config["prefix"]; ok {
				t.Fatalf("postgres prefix should be removed, got %#v", postgresCfg.Config["prefix"])
			}

			localPostgresCfg, ok := captured["postgres://local.example.test/gestalt"]
			if !ok {
				t.Fatal("missing captured local postgres indexeddb config")
			}
			if got := localPostgresCfg.Config["schema"]; got != tc.wantSchema {
				t.Fatalf("local postgres schema = %#v, want %q", got, tc.wantSchema)
			}
			if _, ok := localPostgresCfg.Config["namespace"]; ok {
				t.Fatalf("local postgres namespace should be removed, got %#v", localPostgresCfg.Config["namespace"])
			}
			if got := localPostgresCfg.Config["legacy_table_prefix"]; got != "plugin_echoext_" {
				t.Fatalf("local postgres legacy_table_prefix = %#v, want %q", got, "plugin_echoext_")
			}
			if _, ok := localPostgresCfg.Config["legacy_prefix"]; ok {
				t.Fatalf("local postgres legacy_prefix should be removed, got %#v", localPostgresCfg.Config["legacy_prefix"])
			}

			sqliteCfg, ok := captured["sqlite://plugin-state.db"]
			if !ok {
				t.Fatal("missing captured sqlite indexeddb config")
			}
			wantPrefix := tc.wantSchema + "_"
			if got := sqliteCfg.Config["table_prefix"]; got != wantPrefix {
				t.Fatalf("sqlite table_prefix = %#v, want %q", got, wantPrefix)
			}
			if got := sqliteCfg.Config["prefix"]; got != wantPrefix {
				t.Fatalf("sqlite prefix = %#v, want %q", got, wantPrefix)
			}
			if _, ok := sqliteCfg.Config["schema"]; ok {
				t.Fatalf("sqlite schema should be removed, got %#v", sqliteCfg.Config["schema"])
			}
			if _, ok := sqliteCfg.Config["namespace"]; ok {
				t.Fatalf("sqlite namespace should be removed, got %#v", sqliteCfg.Config["namespace"])
			}
			if got := sqliteCfg.Config["legacy_table_prefix"]; got != "plugin_echoext_" {
				t.Fatalf("sqlite legacy_table_prefix = %#v, want %q", got, "plugin_echoext_")
			}
			if _, ok := sqliteCfg.Config["legacy_prefix"]; ok {
				t.Fatalf("sqlite legacy_prefix should be removed, got %#v", sqliteCfg.Config["legacy_prefix"])
			}

			_ = CloseProviders(providers)
			providers = nil
			if got := closeCount.Load(); got != 3 {
				t.Fatalf("closeCount after provider shutdown = %d, want 3", got)
			}
		})
	}
}

func TestPluginIndexedDBBindingsRouteObjectStoresAndTransportPrefix(t *testing.T) {
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
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDBSchema:      "roadmap",
					IndexedDBs: []config.PluginIndexedDBBinding{
						{Name: "memory", ObjectStores: []string{"tasks"}},
					},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
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

func TestPluginIndexedDBBindingsPreserveLegacyTransportPrefixedData(t *testing.T) {
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

	var boundDB *trackedIndexedDB
	providers, _, err := buildProvidersStrict(context.Background(), &config.Config{
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDBSchema:      "roadmap",
					IndexedDBs: []config.PluginIndexedDBBinding{
						{Name: "memory", ObjectStores: []string{"tasks"}},
					},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"memory": {
				Source: config.ProviderSource{Path: "./providers/datastore/memory"},
				Config: mustNode(t, map[string]any{"bucket": "plugin-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			boundDB = &trackedIndexedDB{
				StubIndexedDB: coretesting.StubIndexedDB{},
			}
			return boundDB, nil
		},
	})
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(providers) })

	if err := boundDB.CreateObjectStore(context.Background(), "plugin_echoext_tasks", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore legacy tasks: %v", err)
	}
	if err := boundDB.ObjectStore("plugin_echoext_tasks").Put(context.Background(), indexeddb.Record{
		"id":    "legacy-task",
		"value": "already-there",
	}); err != nil {
		t.Fatalf("Put legacy task: %v", err)
	}

	prov, err := providers.Get("echoext")
	if err != nil {
		t.Fatalf("providers.Get: %v", err)
	}
	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"store": "tasks",
		"id":    "task-1",
		"value": "ship-it",
	}, ""); err != nil {
		t.Fatalf("Execute indexeddb_roundtrip: %v", err)
	}

	if _, err := boundDB.ObjectStore("plugin_echoext_tasks").Get(context.Background(), "task-1"); err != nil {
		t.Fatalf("legacy backing store should receive new writes: %v", err)
	}
	if _, err := boundDB.ObjectStore("plugin_echoext_tasks").Get(context.Background(), "legacy-task"); err != nil {
		t.Fatalf("legacy backing store should keep old rows: %v", err)
	}
	if _, err := boundDB.ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err == nil {
		t.Fatal("new transport-prefixed store should remain unused while only legacy data exists")
	}
}

func TestPluginIndexedDBBindingsRouteExplicitAndCatchAllStores(t *testing.T) {
	t.Parallel()

	bin := buildEchoPluginBinary(t)
	manifestRoot := writeStaticCatalog(t, &catalog.Catalog{
		Name: "echoext",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "indexeddb_roundtrip",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "binding", Type: "string"},
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
		Providers: config.ProvidersConfig{
			Plugins: map[string]*config.ProviderEntry{
				"echoext": {
					Command:              bin,
					Args:                 []string{"provider"},
					ResolvedManifest:     manifest,
					ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
					IndexedDBSchema:      "roadmap",
					IndexedDBs: []config.PluginIndexedDBBinding{
						{Name: "main", ObjectStores: []string{"tasks"}},
						{Name: "archive"},
					},
				},
			},
		},
	}, NewFactoryRegistry(), Deps{
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

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"binding": "main",
		"store":   "tasks",
		"id":      "task-1",
		"value":   "ship-it",
	}, ""); err != nil {
		t.Fatalf("Execute main tasks roundtrip: %v", err)
	}
	if _, err := boundDBs["main"].ObjectStore("roadmap_tasks").Get(context.Background(), "task-1"); err != nil {
		t.Fatalf("main binding should own tasks store: %v", err)
	}

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"binding": "main",
		"store":   "events",
		"id":      "evt-main",
		"value":   "blocked",
	}, ""); err == nil {
		t.Fatal("main binding should reject stores outside its explicit objectStore allowlist")
	}

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"binding": "archive",
		"store":   "events",
		"id":      "evt-1",
		"value":   "stored",
	}, ""); err != nil {
		t.Fatalf("Execute archive events roundtrip: %v", err)
	}
	if _, err := boundDBs["archive"].ObjectStore("roadmap_events").Get(context.Background(), "evt-1"); err != nil {
		t.Fatalf("archive binding should act as catch-all for unassigned stores: %v", err)
	}

	if _, err := prov.Execute(context.Background(), "indexeddb_roundtrip", map[string]any{
		"binding": "archive",
		"store":   "tasks",
		"id":      "task-archive",
		"value":   "blocked",
	}, ""); err == nil {
		t.Fatal("archive catch-all binding should reject stores explicitly assigned to another binding")
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
