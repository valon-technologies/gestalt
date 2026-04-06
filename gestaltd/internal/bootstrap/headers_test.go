package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

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
				Source:               &config.PluginSourceDef{Ref: manifest.Source, Version: manifest.Version},
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
