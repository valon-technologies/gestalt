package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
				BaseURL: "https://api.test.example",
				MCPURL:  mcpSrv.URL + "/mcp",
				Auth: &config.ConnectionAuthDef{
					Type:         "mcp_oauth",
					ClientID:     "test-id",
					ClientSecret: "test-secret",
				},
				Operations: []config.InlineOperationDef{
					{Name: "do_thing", Method: "POST", Path: "/things"},
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
			Connections: map[string]*pluginmanifestv1.ManifestConnectionDef{
				"MCP": {
					Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeMCPOAuth},
				},
			},
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
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				ResolvedManifest:     manifest,
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
	if _, ok := staticOps["api_list_items"]; !ok {
		t.Fatalf("expected REST operation from packaged openapi spec, got %v", staticIDs)
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
	if got := maps.APIConnection["hybrid"]; got != config.PluginConnectionName {
		t.Fatalf("api connection = %q, want %q", got, config.PluginConnectionName)
	}
	if got := maps.MCPConnection["hybrid"]; got != "MCP" {
		t.Fatalf("mcp connection = %q, want %q", got, "MCP")
	}
}

func TestInlineOAuth_NamedOpenAPIConnectionAuthWired(t *testing.T) {
	t.Parallel()

	specSrv := serveOpenAPISpec(t)
	var pluginConfig yaml.Node
	if err := pluginConfig.Encode(map[string]any{
		"client_id":     "vendor-id",
		"client_secret": "vendor-secret",
	}); err != nil {
		t.Fatalf("pluginConfig.Encode: %v", err)
	}

	cfg := validConfig()
	cfg.Server.BaseURL = "https://gestalt.example.com"
	cfg.Integrations = map[string]config.IntegrationDef{
		"vendor": {
			Plugin: &config.PluginDef{
				OpenAPI:           specSrv.URL,
				OpenAPIConnection: testOpenAPIConnectionName,
				Config:            pluginConfig,
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
	vendorAuth, ok := connAuth["vendor"]
	if !ok {
		t.Fatal("expected connection auth for vendor integration")
	}
	handler, ok := vendorAuth[testOpenAPIConnectionName]
	if !ok {
		t.Fatalf("expected handler for connection %q", testOpenAPIConnectionName)
	}
	if handler.AuthorizationBaseURL() != "https://example.com/authorize" {
		t.Fatalf("authorization URL = %q, want %q", handler.AuthorizationBaseURL(), "https://example.com/authorize")
	}
	if handler.TokenURL() != "https://example.com/token" {
		t.Fatalf("token URL = %q, want %q", handler.TokenURL(), "https://example.com/token")
	}
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

func TestInlineOpenAPI_NamedConnectionAuthMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]string{
			"api_key": r.Header.Get("DD-API-KEY"),
			"app_key": r.Header.Get("DD-APPLICATION-KEY"),
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
		"datadog": {
			Plugin: &config.PluginDef{
				OpenAPI:           specSrv.URL,
				OpenAPIConnection: connName,
				Connections: map[string]*config.ConnectionDef{
					connName: {
						Auth: config.ConnectionAuthDef{
							Type: pluginmanifestv1.AuthTypeManual,
							AuthMapping: &config.AuthMappingDef{
								Headers: map[string]string{
									"DD-API-KEY":         "api_key",
									"DD-APPLICATION-KEY": "app_key",
								},
							},
						},
					},
				},
			},
		},
	}

	result := mustBootstrapResult(t, cfg, nil)
	prov := mustGetProvider(t, result, "datadog")

	token := `{"api_key":"k1","app_key":"k2"}`
	opResult, err := prov.Execute(ctx, "list_items", nil, token)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal([]byte(opResult.Body), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp["api_key"] != "k1" {
		t.Errorf("DD-API-KEY = %q, want %q", resp["api_key"], "k1")
	}
	if resp["app_key"] != "k2" {
		t.Errorf("DD-APPLICATION-KEY = %q, want %q", resp["app_key"], "k2")
	}
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
