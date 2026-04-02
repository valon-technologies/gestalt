package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

func mustConfigNode(t *testing.T, value any) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		t.Fatalf("node.Encode: %v", err)
	}
	return node
}

func TestBootstrap_ConfigHeadersOverrideManifestHeaders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		headerName    = "X-Static-Version"
		manifestValue = "from-manifest"
		configValue   = "from-config"
	)

	gotHeader := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/sample",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: apiSrv.URL,
			Headers: map[string]string{
				"x-static-version": manifestValue,
			},
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   "list_items",
					Method: http.MethodGet,
					Path:   "/items",
				},
			},
		},
	}
	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"sample": {
			Plugin: &config.PluginDef{
				IsDeclarative:    true,
				ResolvedManifest: manifest,
				Headers: map[string]string{
					headerName: configValue,
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	execResult, err := prov.Execute(ctx, "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != configValue {
			t.Fatalf("%s = %q, want %q", headerName, got, configValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestBootstrap_ConfigBaseURLOverridesManifestBaseURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	gotPath := make(chan string, 1)
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- "manifest:" + r.URL.Path
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, manifestSrv)

	overrideSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- "override:" + r.URL.Path
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, overrideSrv)

	pluginDir := t.TempDir()
	openapiPath := filepath.Join(pluginDir, "openapi.json")
	if err := os.WriteFile(openapiPath, []byte(`{
  "openapi": "3.0.0",
  "info": { "title": "Base URL Override Test API" },
  "servers": [{ "url": "`+manifestSrv.URL+`" }],
  "paths": {
    "/items": {
      "get": {
        "operationId": "list_items",
        "summary": "List items",
        "responses": {
          "200": { "description": "OK" }
        }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.json): %v", err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/sample",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "openapi.json",
		},
	}
	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"sample": {
			Plugin: &config.PluginDef{
				Source:               manifest.Source,
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				ResolvedManifest:     manifest,
				BaseURL:              overrideSrv.URL,
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	execResult, err := prov.Execute(ctx, "list_items", nil, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotPath:
		if got != "override:/items" {
			t.Fatalf("upstream request = %q, want %q", got, "override:/items")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestBootstrap_ManagedParametersInjectHeadersAndHideOpenAPIParams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		headerName  = "Intercom-Version"
		headerValue = "2.11"
	)

	gotHeader := make(chan string, 1)
	gotPageSize := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get(headerName)
		gotPageSize <- r.URL.Query().Get("page_size")
		writeTestJSON(w, map[string]any{"items": []any{}})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Managed Parameters Test API"},
			"servers": []any{map[string]string{"url": apiSrv.URL}},
			"paths": map[string]any{
				"/items": map[string]any{
					"get": map[string]any{
						"operationId": "list_items",
						"summary":     "List items",
						"parameters": []any{
							map[string]any{
								"name":     headerName,
								"in":       "header",
								"required": true,
								"schema":   map[string]any{"type": "string"},
							},
							map[string]any{
								"name":   "page_size",
								"in":     "query",
								"schema": map[string]any{"type": "integer"},
							},
						},
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
				ManagedParameters: []config.ManagedParameterDef{
					{
						In:    "header",
						Name:  "intercom-version",
						Value: headerValue,
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

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	params := cat.Operations[0].Parameters
	if len(params) != 1 || params[0].Name != "page_size" {
		t.Fatalf("catalog params = %+v, want only page_size", params)
	}

	execResult, err := prov.Execute(ctx, "list_items", map[string]any{"page_size": 25}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeader:
		if got != headerValue {
			t.Fatalf("%s = %q, want %q", headerName, got, headerValue)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream header")
	}

	select {
	case got := <-gotPageSize:
		if got != "25" {
			t.Fatalf("page_size = %q, want %q", got, "25")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream query param")
	}
}

func TestBootstrap_OpenAPISecuritySchemesBuildManualCredentialFieldsAndHeaderMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type requestHeaders struct {
		apiKey         string
		applicationKey string
	}

	gotHeaders := make(chan requestHeaders, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders <- requestHeaders{
			apiKey:         r.Header.Get("DD-API-KEY"),
			applicationKey: r.Header.Get("DD-APPLICATION-KEY"),
		}
		writeTestJSON(w, map[string]any{"ok": true})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	pluginDir := t.TempDir()
	openapiPath := filepath.Join(pluginDir, "openapi.json")
	if err := os.WriteFile(openapiPath, []byte(`{
  "openapi": "3.2.0",
  "info": { "title": "Datadog Test API" },
  "servers": [{ "url": "`+apiSrv.URL+`" }],
  "security": [{ "api_key": [], "application_key": [] }],
  "components": {
    "securitySchemes": {
      "api_key": {
        "type": "apiKey",
        "in": "header",
        "name": "DD-API-KEY"
      },
      "application_key": {
        "type": "apiKey",
        "in": "header",
        "name": "DD-APPLICATION-KEY"
      }
    }
  },
  "paths": {
    "/items": {
      "get": {
        "operationId": "list_items",
        "responses": {
          "200": { "description": "OK" }
        }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.json): %v", err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/datadog",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "openapi.json",
		},
	}
	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"datadog": {
			Plugin: &config.PluginDef{
				Source:               manifest.Source,
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				ResolvedManifest:     manifest,
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
		t.Fatalf("Providers.Get: %v", err)
	}

	cfp, ok := prov.(core.CredentialFieldsProvider)
	if !ok {
		t.Fatalf("provider does not expose credential fields: %T", prov)
	}
	fields := cfp.CredentialFields()
	if len(fields) != 2 {
		t.Fatalf("credential fields = %d, want 2", len(fields))
	}
	if fields[0].Name != "api_key" || fields[0].Label != "API Key" {
		t.Fatalf("first credential field = %+v", fields[0])
	}
	if fields[1].Name != "application_key" || fields[1].Label != "Application Key" {
		t.Fatalf("second credential field = %+v", fields[1])
	}

	token := `{"api_key":"api-secret","application_key":"app-secret"}`
	execResult, err := prov.Execute(ctx, "list_items", nil, token)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotHeaders:
		if got.apiKey != "api-secret" || got.applicationKey != "app-secret" {
			t.Fatalf("headers = %+v, want api-secret/app-secret", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func TestBootstrap_OpenAPISecuritySchemesBuildOAuthConnectionHandlerWithoutManifestAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pluginDir := t.TempDir()
	openapiPath := filepath.Join(pluginDir, "openapi.json")
	if err := os.WriteFile(openapiPath, []byte(`{
  "openapi": "3.2.0",
  "info": { "title": "GitLab Test API" },
  "servers": [{ "url": "https://gitlab.example.com/api/v4" }],
  "security": [{ "gitlab_oauth": ["api"] }],
  "components": {
    "securitySchemes": {
      "gitlab_oauth": {
        "type": "oauth2",
        "oauth2MetadataUrl": "https://gitlab.example.com/.well-known/oauth-authorization-server",
        "flows": {
          "authorizationCode": {
            "authorizationUrl": "https://gitlab.example.com/oauth/authorize",
            "tokenUrl": "https://gitlab.example.com/oauth/token",
            "scopes": {
              "api": "Full API access"
            }
          }
        }
      }
    }
  },
  "paths": {
    "/projects": {
      "get": {
        "operationId": "list_projects",
        "responses": {
          "200": { "description": "OK" }
        }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(openapi.json): %v", err)
	}
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/gitlab",
		Version: "1.0.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "openapi.json",
		},
	}
	cfg := validConfig()
	cfg.Server.BaseURL = "https://gestalt.example.com"
	cfg.Integrations = map[string]config.IntegrationDef{
		"gitlab": {
			Plugin: &config.PluginDef{
				Source:               manifest.Source,
				IsDeclarative:        true,
				ResolvedManifestPath: manifestPath,
				ResolvedManifest:     manifest,
				Config: mustConfigNode(t, map[string]any{
					"client_id":     "gitlab-client-id",
					"client_secret": "gitlab-client-secret",
				}),
			},
		},
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	handler := result.ConnectionAuth()["gitlab"][config.PluginConnectionName]
	if handler == nil {
		t.Fatal("expected oauth handler for plugin connection")
	}
	if handler.AuthorizationBaseURL() != "https://gitlab.example.com/oauth/authorize" {
		t.Fatalf("AuthorizationBaseURL = %q", handler.AuthorizationBaseURL())
	}
	if handler.TokenURL() != "https://gitlab.example.com/oauth/token" {
		t.Fatalf("TokenURL = %q", handler.TokenURL())
	}

	authURL, _ := handler.StartOAuth("state-123", nil)
	if !strings.HasPrefix(authURL, "https://gitlab.example.com/oauth/authorize?") {
		t.Fatalf("auth url = %q", authURL)
	}
	if !strings.Contains(authURL, "client_id=gitlab-client-id") {
		t.Fatalf("auth url missing client_id: %q", authURL)
	}
	if !strings.Contains(authURL, "scope=api") {
		t.Fatalf("auth url missing scope: %q", authURL)
	}
	if !strings.Contains(authURL, "redirect_uri=https%3A%2F%2Fgestalt.example.com%2Fapi%2Fv1%2Fauth%2Fcallback") {
		t.Fatalf("auth url missing redirect_uri: %q", authURL)
	}
}

func TestBootstrap_ManagedParametersRewritePathParamsAndHideOpenAPIParams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const managedAccountID = "acct-managed"

	gotPath := make(chan string, 1)
	gotPageSize := make(chan string, 1)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotPageSize <- r.URL.Query().Get("page_size")
		writeTestJSON(w, map[string]any{"messages": []any{}})
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]string{"title": "Managed Path Parameters Test API"},
			"servers": []any{map[string]string{"url": apiSrv.URL}},
			"paths": map[string]any{
				"/accounts/{account_id}/items": map[string]any{
					"get": map[string]any{
						"operationId": "list_items",
						"summary":     "List items",
						"parameters": []any{
							map[string]any{
								"name":     "account_id",
								"in":       "path",
								"required": true,
								"schema":   map[string]any{"type": "string"},
							},
							map[string]any{
								"name":   "page_size",
								"in":     "query",
								"schema": map[string]any{"type": "integer"},
							},
						},
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
				ManagedParameters: []config.ManagedParameterDef{
					{
						In:    "path",
						Name:  "account_id",
						Value: managedAccountID,
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

	prov, err := result.Providers.Get("sample")
	if err != nil {
		t.Fatalf("Providers.Get: %v", err)
	}

	cat := prov.Catalog()
	if cat == nil || len(cat.Operations) != 1 {
		t.Fatalf("unexpected catalog: %+v", cat)
	}
	if got := cat.Operations[0].Path; got != "/accounts/acct-managed/items" {
		t.Fatalf("catalog path = %q, want %q", got, "/accounts/acct-managed/items")
	}
	params := cat.Operations[0].Parameters
	if len(params) != 1 || params[0].Name != "page_size" {
		t.Fatalf("catalog params = %+v, want only page_size", params)
	}

	execResult, err := prov.Execute(ctx, "list_items", map[string]any{"page_size": 25}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", execResult.Status, http.StatusOK)
	}

	select {
	case got := <-gotPath:
		if got != "/accounts/acct-managed/items" {
			t.Fatalf("path = %q, want %q", got, "/accounts/acct-managed/items")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream path")
	}

	select {
	case got := <-gotPageSize:
		if got != "25" {
			t.Fatalf("page_size = %q, want %q", got, "25")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream query param")
	}
}
