package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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

func TestInlineMCPOAuth_ConnectionAuthWired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

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
	ctx := context.Background()

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

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("vendor")
	if err != nil {
		t.Fatalf("Get vendor provider: %v", err)
	}
	ops := prov.ListOperations()
	if len(ops) == 0 {
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

func TestInlineResponseMapping_AppliedToOpenAPIProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	specSrv := serveOpenAPISpec(t)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"mapped": {
			Plugin: &config.PluginDef{
				OpenAPI: specSrv.URL,
				Auth: &config.ConnectionAuthDef{
					Type: "none",
				},
				ResponseMapping: &config.ResponseMappingDef{
					DataPath: "results",
					Pagination: &config.PaginationMapping{
						HasMorePath: "moreDataAvailable",
						CursorPath:  "nextCursor",
					},
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("mapped")
	if err != nil {
		t.Fatalf("Get mapped provider: %v", err)
	}
	ops := prov.ListOperations()
	if len(ops) == 0 {
		t.Fatal("expected at least one operation from the openapi spec")
	}
	found := false
	for _, op := range ops {
		if op.Name == "list_items" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected list_items operation to be present")
	}
}

func TestInlineResponseMapping_DataPathOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	specSrv := serveOpenAPISpec(t)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"simple": {
			Plugin: &config.PluginDef{
				OpenAPI: specSrv.URL,
				Auth: &config.ConnectionAuthDef{
					Type: "none",
				},
				ResponseMapping: &config.ResponseMappingDef{
					DataPath: "data.items",
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("simple")
	if err != nil {
		t.Fatalf("Get simple provider: %v", err)
	}
	if len(prov.ListOperations()) == 0 {
		t.Fatal("expected at least one operation")
	}
}

func TestInlineResponseMapping_NilDoesNotBreak(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	specSrv := serveOpenAPISpec(t)

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"noop": {
			Plugin: &config.PluginDef{
				OpenAPI: specSrv.URL,
				Auth: &config.ConnectionAuthDef{
					Type: "none",
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("noop")
	if err != nil {
		t.Fatalf("Get noop provider: %v", err)
	}
	if len(prov.ListOperations()) == 0 {
		t.Fatal("expected at least one operation even without response_mapping")
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

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("datadog")
	if err != nil {
		t.Fatalf("Get provider: %v", err)
	}

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
