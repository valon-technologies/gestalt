package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"gopkg.in/yaml.v3"
)

const (
	testOpenAPIConnectionName = "api"
	testOpenAPIAccessToken    = "api-token"
)

func mustConfigNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func serveMCPOAuthEndpoints(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		writeTestJSON(w, map[string]any{
			"resource":              baseURL + "/mcp",
			"authorization_servers": []string{baseURL},
			"scopes_supported":      []string{"read", "write"},
		})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		writeTestJSON(w, map[string]any{
			"issuer":                                baseURL,
			"authorization_endpoint":                baseURL + "/oauth/authorize",
			"token_endpoint":                        baseURL + "/oauth/token",
			"registration_endpoint":                 baseURL + "/oauth/register",
			"scopes_supported":                      []string{"read", "write"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
		})
	})
	mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id": "dcr-test-client",
		})
	})
	srv := httptest.NewServer(mux)
	testutil.CloseOnCleanup(t, srv)
	return srv
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func serveOpenAPISpec(t *testing.T) *httptest.Server {
	t.Helper()
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Test API"},
		"servers": []any{map[string]string{"url": "https://api.test.example/v1"}},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "list_items",
					"summary":     "List items",
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(spec)
	}))
	testutil.CloseOnCleanup(t, srv)
	return srv
}

func serveOpenAPIBackend(t *testing.T, wantToken string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		writeTestJSON(w, map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Token Test API"},
			"servers": []any{map[string]string{"url": baseURL}},
			"paths": map[string]any{
				"/items": map[string]any{
					"get": map[string]any{
						"operationId": "list_items",
						"summary":     "List items",
					},
				},
			},
		})
	})
	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeTestJSON(w, map[string]any{"items": []string{"alpha"}})
	})

	srv := httptest.NewServer(mux)
	testutil.CloseOnCleanup(t, srv)
	return srv
}

func mustBootstrapResult(t *testing.T, cfg *config.Config, factories *bootstrap.FactoryRegistry) *bootstrap.Result {
	t.Helper()
	if factories == nil {
		factories = validFactories()
	}
	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	return result
}

func mustGetProvider(t *testing.T, result *bootstrap.Result, name string) core.Provider {
	t.Helper()
	prov, err := result.Providers.Get(name)
	if err != nil {
		t.Fatalf("Get %s provider: %v", name, err)
	}
	return prov
}

func bootstrapInlineProvider(t *testing.T, name string, plugin *config.PluginDef) core.Provider {
	t.Helper()
	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		name: {Plugin: plugin},
	}
	return mustGetProvider(t, mustBootstrapResult(t, cfg, nil), name)
}

func serveMCPToolServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := mcpserver.NewMCPServer("test-remote", "1.0.0")
	srv.AddTool(
		mcpgo.NewTool("search", mcpgo.WithDescription("Search workspace")),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("ok"), nil
		},
	)

	httpSrv := mcpserver.NewStreamableHTTPServer(
		srv,
		mcpserver.WithStateLess(true),
	)
	ts := httptest.NewServer(httpSrv)
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func TestInlineMCPOAuth_ConnectionAuthWired(t *testing.T) {
	t.Parallel()

	mcpSrv := serveMCPOAuthEndpoints(t)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"alpha": {
			Plugin: &config.PluginDef{
				MCPURL: mcpSrv.URL + "/mcp",
				Auth: &config.ConnectionAuthDef{
					Type:         "mcp_oauth",
					ClientID:     "test-id",
					ClientSecret: "test-secret",
				},
			},
		},
	}

	result := mustBootstrapResult(t, cfg, nil)

	connAuth := result.ConnectionAuth()
	alphaAuth, ok := connAuth["alpha"]
	if !ok {
		t.Fatal("expected connection auth for alpha integration")
	}
	handler, ok := alphaAuth[config.PluginConnectionName]
	if !ok {
		t.Fatalf("expected handler for connection %q", config.PluginConnectionName)
	}

	authURL, verifier := handler.StartOAuth("test-state", nil)
	if authURL == "" {
		t.Fatal("expected non-empty authorization URL from mcp_oauth handler")
	}
	if verifier == "" {
		t.Fatal("expected non-empty PKCE verifier from mcp_oauth handler")
	}
}

func TestInlineMCPOAuth_SpecLoadedOpenAPI(t *testing.T) {
	t.Parallel()

	mcpSrv := serveMCPOAuthEndpoints(t)
	specSrv := serveOpenAPISpec(t)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"vendor": {
			Plugin: &config.PluginDef{
				OpenAPI: specSrv.URL,
				MCPURL:  mcpSrv.URL + "/mcp",
				Auth: &config.ConnectionAuthDef{
					Type:         "mcp_oauth",
					ClientID:     "vendor-id",
					ClientSecret: "vendor-secret",
				},
			},
		},
	}

	result := mustBootstrapResult(t, cfg, nil)
	prov := mustGetProvider(t, result, "vendor")
	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) == 0 {
		t.Fatal("expected at least one operation from the openapi spec")
	}

	connAuth := result.ConnectionAuth()
	vendorAuth, ok := connAuth["vendor"]
	if !ok {
		t.Fatal("expected connection auth for vendor integration")
	}
	handler, ok := vendorAuth[config.PluginConnectionName]
	if !ok {
		t.Fatalf("expected handler for connection %q", config.PluginConnectionName)
	}
	authURL, _ := handler.StartOAuth("s1", nil)
	if authURL == "" {
		t.Fatal("expected non-empty authorization URL from mcp_oauth handler for spec-loaded provider")
	}
}

func TestBootstrap_SpecLoadedManifestCombinesOpenAPIAndMCP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mcpSrv := serveMCPToolServer(t)

	pluginDir := t.TempDir()
	openapiPath := filepath.Join(pluginDir, "openapi.json")
	if err := os.WriteFile(openapiPath, []byte(`{
  "openapi": "3.0.0",
  "info": { "title": "Hybrid Test API" },
  "servers": [{ "url": "https://api.test.example" }],
  "paths": {
    "/items": {
      "get": {
        "operationId": "api_list_items",
        "summary": "List items"
      }
    },
    "/items/{item_id}": {
      "get": {
        "operationId": "api_get_item",
        "summary": "Get item"
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.json): %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/acme/plugins/hybrid",
		Version:     "0.1.0",
		DisplayName: "Hybrid",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI:       "openapi.json",
			MCPURL:        mcpSrv.URL,
			MCPConnection: "MCP",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"api_get_item": {
					Alias: "get_item",
				},
			},
			Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
				"MCP": {
					Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeMCPOAuth},
				},
				"manifest-api": {
					Mode: "user",
				},
				"manifest-default": {
					Mode: "user",
				},
			},
			OpenAPIConnection: "manifest-api",
			DefaultConnection: "manifest-default",
		},
	}
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"hybrid": {
			Plugin: &config.PluginDef{
				Source:               &config.PluginSourceDef{Ref: manifest.Source, Version: manifest.Version},
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				ResolvedManifest:     manifest,
				AllowedOperations: map[string]*config.OperationOverride{
					"api_list_items": {
						Alias: "list_items",
					},
				},
				OpenAPIConnection: "local-api",
				DefaultConnection: "local-default",
				Connections: map[string]*config.ConnectionDef{
					"local-api": {
						Mode: "user",
					},
					"local-default": {
						Mode: "user",
					},
				},
			},
		},
	}

	result := mustBootstrapResult(t, cfg, nil)
	prov := mustGetProvider(t, result, "hybrid")
	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}

	staticOps := make(map[string]catalog.CatalogOperation, len(cat.Operations))
	staticIDs := make([]string, 0, len(cat.Operations))
	for _, op := range cat.Operations {
		staticOps[op.ID] = op
		staticIDs = append(staticIDs, fmt.Sprintf("%s:%s", op.ID, op.Transport))
	}
	if _, ok := staticOps["list_items"]; !ok {
		t.Fatalf("expected REST operation from packaged openapi spec, got %v", staticIDs)
	}
	if _, ok := staticOps["api_get_item"]; ok {
		t.Fatalf("expected local allowed_operations to replace manifest allowlist, got %v", staticIDs)
	}

	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatalf("provider does not implement SessionCatalogProvider: %T", prov)
	}
	sessionCat, err := scp.CatalogForRequest(ctx, "")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if sessionCat == nil {
		t.Fatal("expected non-nil session catalog")
	}
	sessionOps := make(map[string]catalog.CatalogOperation, len(sessionCat.Operations))
	sessionIDs := make([]string, 0, len(sessionCat.Operations))
	for _, op := range sessionCat.Operations {
		sessionOps[op.ID] = op
		sessionIDs = append(sessionIDs, fmt.Sprintf("%s:%s", op.ID, op.Transport))
	}
	if op, ok := sessionOps["search"]; !ok {
		t.Fatalf("expected MCP operation from upstream mcp server, got %v", sessionIDs)
	} else if op.Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("search transport = %q, want %q", op.Transport, catalog.TransportMCPPassthrough)
	}

	maps, err := bootstrap.BuildConnectionMaps(cfg)
	if err != nil {
		t.Fatalf("BuildConnectionMaps: %v", err)
	}
	if got := maps.DefaultConnection["hybrid"]; got != "local-default" {
		t.Fatalf("default connection = %q, want %q", got, "local-default")
	}
	if got := maps.APIConnection["hybrid"]; got != "local-api" {
		t.Fatalf("api connection = %q, want %q", got, "local-api")
	}
	if got := maps.MCPConnection["hybrid"]; got != "MCP" {
		t.Fatalf("mcp connection = %q, want %q", got, "MCP")
	}
}

func TestInlineOAuth_NamedOpenAPIConnectionAuthWired(t *testing.T) {
	t.Parallel()

	t.Run("named connection override", func(t *testing.T) {
		t.Parallel()

		specSrv := serveOpenAPISpec(t)
		cfg := validConfig()
		cfg.Server.BaseURL = "https://gestalt.example.com"
		cfg.Integrations = map[string]config.IntegrationDef{
			"sample": {
				Plugin: &config.PluginDef{
					OpenAPI:           specSrv.URL,
					OpenAPIConnection: testOpenAPIConnectionName,
					Config: mustConfigNode(t, map[string]any{
						"client_id":     "sample-client-id",
						"client_secret": "sample-client-secret",
					}),
					Connections: map[string]*config.ConnectionDef{
						testOpenAPIConnectionName: {
							Auth: config.ConnectionAuthDef{
								Type:             pluginmanifestv1.AuthTypeOAuth2,
								AuthorizationURL: "https://example.com/authorize",
								TokenURL:         "https://example.com/token",
							},
						},
					},
				},
			},
		}

		result := mustBootstrapResult(t, cfg, nil)
		connAuth := result.ConnectionAuth()
		sampleAuth, ok := connAuth["sample"]
		if !ok {
			t.Fatal("expected connection auth for sample integration")
		}
		handler, ok := sampleAuth[testOpenAPIConnectionName]
		if !ok {
			t.Fatalf("expected handler for connection %q", testOpenAPIConnectionName)
		}
		if handler.AuthorizationBaseURL() != "https://example.com/authorize" {
			t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/authorize")
		}
		if handler.TokenURL() != "https://example.com/token" {
			t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/token")
		}
	})

	t.Run("spec security scheme fallback", func(t *testing.T) {
		t.Parallel()

		specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"openapi": "3.2.0",
				"info":    map[string]string{"title": "Secured API"},
				"servers": []any{map[string]string{"url": "https://api.example.com/v1"}},
				"security": []any{
					map[string]any{"oauth_auth": []string{"read"}},
				},
				"components": map[string]any{
					"securitySchemes": map[string]any{
						"oauth_auth": map[string]any{
							"type":              "oauth2",
							"oauth2MetadataUrl": "https://example.com/.well-known/oauth-authorization-server",
							"flows": map[string]any{
								"authorizationCode": map[string]any{
									"authorizationUrl": "https://example.com/oauth/authorize",
									"tokenUrl":         "https://example.com/oauth/token",
									"scopes": map[string]string{
										"read": "Read data",
									},
								},
							},
						},
					},
				},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
				},
			})
		}))
		testutil.CloseOnCleanup(t, specSrv)

		cfg := validConfig()
		cfg.Server.BaseURL = "https://gestalt.example.com"
		cfg.Integrations = map[string]config.IntegrationDef{
			"sample": {
				Plugin: &config.PluginDef{
					OpenAPI: specSrv.URL,
					Config: mustConfigNode(t, map[string]any{
						"client_id":     "sample-client-id",
						"client_secret": "sample-client-secret",
					}),
				},
			},
		}

		result := mustBootstrapResult(t, cfg, nil)
		connAuth := result.ConnectionAuth()
		sampleAuth, ok := connAuth["sample"]
		if !ok {
			t.Fatal("expected connection auth for sample integration")
		}
		handler, ok := sampleAuth[config.PluginConnectionName]
		if !ok {
			t.Fatalf("expected handler for connection %q", config.PluginConnectionName)
		}
		if handler.AuthorizationBaseURL() != "https://example.com/oauth/authorize" {
			t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/oauth/authorize")
		}
		if handler.TokenURL() != "https://example.com/oauth/token" {
			t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/oauth/token")
		}

		authURL, _ := handler.StartOAuth("state-123", nil)
		if authURL == "" {
			t.Fatal("expected non-empty authorization URL")
		}
		if !strings.Contains(authURL, "client_id=sample-client-id") {
			t.Fatalf("auth url missing client_id: %q", authURL)
		}
		if !strings.Contains(authURL, "scope=read") {
			t.Fatalf("auth url missing scope: %q", authURL)
		}
		if !strings.Contains(authURL, "redirect_uri=https%3A%2F%2Fgestalt.example.com%2Fapi%2Fv1%2Fauth%2Fcallback") {
			t.Fatalf("auth url missing redirect_uri: %q", authURL)
		}
	})
}

func invokeListItemsConnection(t *testing.T, buildPlugin func(apiBase string) *config.PluginDef) string {
	t.Helper()

	ctx := context.Background()
	apiSrv := serveOpenAPIBackend(t, testOpenAPIAccessToken)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"vendor": {Plugin: buildPlugin(apiSrv.URL)},
	}

	var gotConnection string
	factories := validFactories()
	factories.Datastores["test-store"] = func(yaml.Node, bootstrap.Deps) (core.Datastore, error) {
		return &coretesting.StubDatastore{
			TokenFn: func(_ context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
				gotConnection = connection
				return &core.IntegrationToken{
					UserID:      userID,
					Integration: integration,
					Connection:  connection,
					Instance:    instance,
					AccessToken: testOpenAPIAccessToken,
				}, nil
			},
		}, nil
	}

	result := mustBootstrapResult(t, cfg, factories)

	resp, err := result.Invoker.Invoke(ctx, &principal.Principal{UserID: "u1"}, "vendor", "", "list_items", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Status, http.StatusOK)
	}
	return gotConnection
}

func TestBootstrapInvoke_ConnectionSelection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		wantConnection string
		buildPlugin    func(apiBase string) *config.PluginDef
	}{
		{
			name:           "uses named openapi connection",
			wantConnection: testOpenAPIConnectionName,
			buildPlugin: func(apiBase string) *config.PluginDef {
				return &config.PluginDef{
					OpenAPI:           apiBase + "/openapi.json",
					OpenAPIConnection: testOpenAPIConnectionName,
					Connections: map[string]*config.ConnectionDef{
						testOpenAPIConnectionName: {
							Auth: config.ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeManual},
						},
					},
				}
			},
		},
		{
			name:           "uses explicit default named connection without base auth",
			wantConnection: testOpenAPIConnectionName,
			buildPlugin: func(apiBase string) *config.PluginDef {
				return &config.PluginDef{
					BaseURL:           apiBase,
					DefaultConnection: testOpenAPIConnectionName,
					Connections: map[string]*config.ConnectionDef{
						testOpenAPIConnectionName: {
							Auth: config.ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeManual},
						},
					},
					Operations: []config.InlineOperationDef{
						{Name: "list_items", Method: http.MethodGet, Path: "/items"},
					},
				}
			},
		},
		{
			name:           "uses explicit plugin default when plugin and named connections both exist",
			wantConnection: config.PluginConnectionName,
			buildPlugin: func(apiBase string) *config.PluginDef {
				return &config.PluginDef{
					BaseURL:           apiBase,
					DefaultConnection: config.PluginConnectionAlias,
					Auth: &config.ConnectionAuthDef{
						Type: pluginmanifestv1.AuthTypeManual,
					},
					Connections: map[string]*config.ConnectionDef{
						testOpenAPIConnectionName: {
							Auth: config.ConnectionAuthDef{Type: pluginmanifestv1.AuthTypeManual},
						},
					},
					Operations: []config.InlineOperationDef{
						{Name: "list_items", Method: http.MethodGet, Path: "/items"},
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := invokeListItemsConnection(t, tc.buildPlugin); got != tc.wantConnection {
				t.Fatalf("connection = %q, want %q", got, tc.wantConnection)
			}
		})
	}
}

func TestInlineResponseMapping(t *testing.T) {
	t.Parallel()

	specSrv := serveOpenAPISpec(t)
	testCases := []struct {
		name            string
		integrationName string
		responseMapping *config.ResponseMappingDef
		wantOperationID string
	}{
		{
			name:            "applied to openapi provider",
			integrationName: "mapped",
			responseMapping: &config.ResponseMappingDef{
				DataPath: "results",
				Pagination: &config.PaginationMapping{
					HasMorePath: "moreDataAvailable",
					CursorPath:  "nextCursor",
				},
			},
			wantOperationID: "list_items",
		},
		{
			name:            "data path only",
			integrationName: "simple",
			responseMapping: &config.ResponseMappingDef{
				DataPath: "data.items",
			},
		},
		{
			name:            "nil does not break",
			integrationName: "noop",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prov := bootstrapInlineProvider(t, tc.integrationName, &config.PluginDef{
				OpenAPI: specSrv.URL,
				Auth: &config.ConnectionAuthDef{
					Type: "none",
				},
				ResponseMapping: tc.responseMapping,
			})
			cat := prov.Catalog()
			if cat == nil || len(cat.Operations) == 0 {
				t.Fatal("expected at least one operation")
			}
			if tc.wantOperationID == "" {
				return
			}
			for _, op := range cat.Operations {
				if op.ID == tc.wantOperationID {
					return
				}
			}
			t.Fatalf("expected %q operation to be present", tc.wantOperationID)
		})
	}
}

func TestInlinePagination(t *testing.T) {
	t.Parallel()

	pageCount := 0
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			writeTestJSON(w, map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": "Paginated API"},
				"servers": []any{map[string]string{"url": "http://" + r.Host}},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
					"/items/{id}": map[string]any{
						"get": map[string]any{
							"operationId": "get_item",
							"summary":     "Get item",
						},
					},
				},
			})
		case "/items":
			pageCount++
			cursor := r.URL.Query().Get("cursor")
			switch cursor {
			case "":
				writeTestJSON(w, map[string]any{
					"data":       []string{"a", "b"},
					"nextCursor": "page2",
				})
			case "page2":
				writeTestJSON(w, map[string]any{
					"data":       []string{"c"},
					"nextCursor": nil,
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	prov := bootstrapInlineProvider(t, "paginated", &config.PluginDef{
		OpenAPI: apiSrv.URL + "/openapi.json",
		Auth:    &config.ConnectionAuthDef{Type: "none"},
		Pagination: &config.PaginationConfigDef{
			Style:       "cursor",
			CursorParam: "cursor",
			CursorPath:  "nextCursor",
			ResultsPath: "data",
			LimitParam:  "limit",
		},
		AllowedOperations: map[string]*config.OperationOverride{
			"list_items": {Paginate: true},
			"get_item":   {},
		},
	})

	ctx := context.Background()

	result, err := prov.Execute(ctx, "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute list_items: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", result.Status, result.Body)
	}

	var items []string
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("unmarshal: %v (body = %s)", err, result.Body)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items %v, want 3 (auto-pagination should combine pages)", len(items), items)
	}
	if pageCount != 2 {
		t.Fatalf("expected 2 page fetches, got %d", pageCount)
	}
}

func TestInlineOpenAPI_NamedConnectionAuthMapping(t *testing.T) {
	t.Parallel()

	t.Run("configured mapping", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeTestJSON(w, map[string]string{
				"primary_token":   r.Header.Get("X-Primary-Token"),
				"secondary_token": r.Header.Get("X-Secondary-Token"),
			})
		}))
		testutil.CloseOnCleanup(t, apiSrv)

		specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": "Test API"},
				"servers": []any{map[string]string{"url": apiSrv.URL}},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
				},
			})
		}))
		testutil.CloseOnCleanup(t, specSrv)

		const connName = "api"
		cfg := validConfig()
		cfg.Integrations = map[string]config.IntegrationDef{
			"sample": {
				Plugin: &config.PluginDef{
					OpenAPI:           specSrv.URL,
					OpenAPIConnection: connName,
					Connections: map[string]*config.ConnectionDef{
						connName: {
							Auth: config.ConnectionAuthDef{
								Type: pluginmanifestv1.AuthTypeManual,
								AuthMapping: &config.AuthMappingDef{
									Headers: map[string]string{
										"X-Primary-Token":   "primary_token",
										"X-Secondary-Token": "secondary_token",
									},
								},
							},
						},
					},
				},
			},
		}

		result := mustBootstrapResult(t, cfg, nil)
		prov := mustGetProvider(t, result, "sample")

		token := `{"primary_token":"k1","secondary_token":"k2"}`
		opResult, err := prov.Execute(ctx, "list_items", nil, token)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var resp map[string]string
		if err := json.Unmarshal([]byte(opResult.Body), &resp); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if resp["primary_token"] != "k1" {
			t.Errorf("X-Primary-Token = %q, want %q", resp["primary_token"], "k1")
		}
		if resp["secondary_token"] != "k2" {
			t.Errorf("X-Secondary-Token = %q, want %q", resp["secondary_token"], "k2")
		}
	})

	t.Run("security scheme mapping", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeTestJSON(w, map[string]string{
				"primary_token":   r.Header.Get("X-Primary-Token"),
				"secondary_token": r.Header.Get("X-Secondary-Token"),
			})
		}))
		testutil.CloseOnCleanup(t, apiSrv)

		specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"openapi": "3.2.0",
				"info":    map[string]string{"title": "Secured API"},
				"servers": []any{map[string]string{"url": apiSrv.URL}},
				"security": []any{
					map[string]any{
						"primary_token":   []any{},
						"secondary_token": []any{},
					},
				},
				"components": map[string]any{
					"securitySchemes": map[string]any{
						"primary_token": map[string]any{
							"type":        "apiKey",
							"in":          "header",
							"name":        "X-Primary-Token",
							"description": "Primary token",
						},
						"secondary_token": map[string]any{
							"type":        "apiKey",
							"in":          "header",
							"name":        "X-Secondary-Token",
							"description": "Secondary token",
						},
					},
				},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
				},
			})
		}))
		testutil.CloseOnCleanup(t, specSrv)

		cfg := validConfig()
		cfg.Integrations = map[string]config.IntegrationDef{
			"sample": {
				Plugin: &config.PluginDef{
					OpenAPI: specSrv.URL,
				},
			},
		}

		result := mustBootstrapResult(t, cfg, nil)
		prov := mustGetProvider(t, result, "sample")

		token := `{"primary_token":"k1","secondary_token":"k2"}`
		opResult, err := prov.Execute(ctx, "list_items", nil, token)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var resp map[string]string
		if err := json.Unmarshal([]byte(opResult.Body), &resp); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if resp["primary_token"] != "k1" {
			t.Errorf("X-Primary-Token = %q, want %q", resp["primary_token"], "k1")
		}
		if resp["secondary_token"] != "k2" {
			t.Errorf("X-Secondary-Token = %q, want %q", resp["secondary_token"], "k2")
		}

		cfp, ok := prov.(core.CredentialFieldsProvider)
		if !ok {
			t.Fatalf("provider does not expose credential fields: %T", prov)
		}
		fields := cfp.CredentialFields()
		if len(fields) != 2 {
			t.Fatalf("credential fields = %d, want 2", len(fields))
		}
		if fields[0].Name != "primary_token" || fields[0].Label != "Primary Token" {
			t.Fatalf("first credential field = %+v", fields[0])
		}
		if fields[1].Name != "secondary_token" || fields[1].Label != "Secondary Token" {
			t.Fatalf("second credential field = %+v", fields[1])
		}
	})
}

func TestInlineDeclarative_ConfigDisplayOverridesAppliedAfterRestriction(t *testing.T) {
	t.Parallel()

	const iconSVG = `<svg viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`
	iconPath := filepath.Join(t.TempDir(), "icon.svg")
	if err := os.WriteFile(iconPath, []byte(iconSVG), 0o644); err != nil {
		t.Fatalf("Write icon: %v", err)
	}

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"alpha": {
			DisplayName: "Alpha Display",
			Description: "Alpha Description",
			IconFile:    iconPath,
			Plugin: &config.PluginDef{
				Auth: &config.ConnectionAuthDef{Type: "none"},
				Operations: []config.InlineOperationDef{
					{Name: "do_thing", Method: http.MethodPost, Path: "/things"},
				},
				AllowedOperations: map[string]*config.OperationOverride{
					"do_thing": nil,
				},
			},
		},
	}

	result := mustBootstrapResult(t, cfg, nil)
	prov := mustGetProvider(t, result, "alpha")
	if prov.DisplayName() != "Alpha Display" {
		t.Fatalf("DisplayName = %q, want %q", prov.DisplayName(), "Alpha Display")
	}
	if prov.Description() != "Alpha Description" {
		t.Fatalf("Description = %q, want %q", prov.Description(), "Alpha Description")
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if cat.DisplayName != "Alpha Display" {
		t.Fatalf("catalog DisplayName = %q, want %q", cat.DisplayName, "Alpha Display")
	}
	if cat.Description != "Alpha Description" {
		t.Fatalf("catalog Description = %q, want %q", cat.Description, "Alpha Description")
	}
	if cat.IconSVG != iconSVG {
		t.Fatalf("catalog IconSVG = %q, want %q", cat.IconSVG, iconSVG)
	}
}
