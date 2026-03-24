package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

func TestBundleConfigWritesSelfContainedBundle(t *testing.T) {
	t.Parallel()

	openAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"openapi": "3.0.0",
			"info": map[string]any{
				"title": "REST API",
			},
			"servers": []map[string]any{{"url": "https://api.example.com"}},
			"paths": map[string]any{
				"/users": map[string]any{
					"get": map[string]any{
						"operationId": "listUsers",
						"summary":     "List users",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer openAPIServer.Close()

	graphQLServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"__schema": map[string]any{
					"queryType":    map[string]any{"name": "Query"},
					"mutationType": nil,
					"types": []map[string]any{
						{
							"kind":        "OBJECT",
							"name":        "Query",
							"description": "",
							"fields": []map[string]any{
								{
									"name":        "searchIssues",
									"description": "Search issues",
									"args": []map[string]any{
										{
											"name":         "query",
											"description":  "Search query",
											"type":         map[string]any{"kind": "SCALAR", "name": "String", "ofType": nil},
											"defaultValue": nil,
										},
									},
									"type": map[string]any{"kind": "OBJECT", "name": "IssueConnection", "ofType": nil},
								},
							},
							"inputFields": nil,
							"enumValues":  nil,
						},
						{
							"kind":        "OBJECT",
							"name":        "IssueConnection",
							"description": "",
							"fields":      []map[string]any{},
							"inputFields": nil,
							"enumValues":  nil,
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQLServer.Close()

	dir := t.TempDir()
	iconPath := filepath.Join(dir, "rest.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`), 0644); err != nil {
		t.Fatalf("WriteFile icon: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	outDir := filepath.Join(dir, "bundle")
	cfg := `auth:
  provider: google
  config:
    client_id: ${GOOGLE_CLIENT_ID}
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: ${GESTALT_ENCRYPTION_KEY}
provider_dirs:
  - ./providers-cache
integrations:
  restapi:
    icon_file: ./rest.svg
    upstreams:
      - type: rest
        url: ` + openAPIServer.URL + `
  graphapi:
    upstreams:
      - type: graphql
        url: ` + graphQLServer.URL + `
  combo:
    upstreams:
      - type: graphql
        url: ` + graphQLServer.URL + `
        mcp: true
      - type: mcp
        url: https://example.com/mcp
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if err := bundleConfig(cfgPath, outDir); err != nil {
		t.Fatalf("bundleConfig: %v", err)
	}

	bundledConfigPath := filepath.Join(outDir, bundleConfigName)
	bundledConfigBytes, err := os.ReadFile(bundledConfigPath)
	if err != nil {
		t.Fatalf("ReadFile bundled config: %v", err)
	}
	bundledConfig := string(bundledConfigBytes)
	if !strings.Contains(bundledConfig, "${GOOGLE_CLIENT_ID}") {
		t.Fatalf("bundled config should preserve env placeholders: %s", bundledConfig)
	}
	if strings.Contains(bundledConfig, "provider_dirs:") {
		t.Fatalf("bundled config should not include provider_dirs: %s", bundledConfig)
	}
	if strings.Contains(bundledConfig, openAPIServer.URL) || strings.Contains(bundledConfig, graphQLServer.URL) {
		t.Fatalf("bundled config should not reference source REST/GraphQL URLs: %s", bundledConfig)
	}
	if !strings.Contains(bundledConfig, "provider: providers/restapi.json") {
		t.Fatalf("bundled config missing restapi provider path: %s", bundledConfig)
	}

	restDef, err := provider.LoadFile(filepath.Join(outDir, bundleProvidersDir, "restapi.json"))
	if err != nil {
		t.Fatalf("LoadFile restapi artifact: %v", err)
	}
	if restDef.IconSVG == "" {
		t.Fatal("restapi artifact missing embedded IconSVG")
	}

	openAPIServer.Close()
	graphQLServer.Close()

	loadedCfg, err := config.Load(bundledConfigPath)
	if err != nil {
		t.Fatalf("Load bundled config: %v", err)
	}
	if len(loadedCfg.ProviderDirs) != 0 {
		t.Fatalf("ProviderDirs = %v, want empty", loadedCfg.ProviderDirs)
	}
	if got := loadedCfg.Integrations["restapi"].Upstreams[0].Provider; got != filepath.Join(outDir, bundleProvidersDir, "restapi.json") {
		t.Fatalf("restapi provider path = %q, want %q", got, filepath.Join(outDir, bundleProvidersDir, "restapi.json"))
	}
	if loadedCfg.Integrations["restapi"].IconFile != "" {
		t.Fatalf("restapi IconFile = %q, want empty", loadedCfg.Integrations["restapi"].IconFile)
	}

	if _, hasAPI, err := loadIntegrationAPIDefinition(context.Background(), "restapi", loadedCfg.Integrations["restapi"], loadedCfg.ProviderDirs); err != nil {
		t.Fatalf("loadIntegrationAPIDefinition restapi: %v", err)
	} else if !hasAPI {
		t.Fatal("restapi should still have API definition in bundled config")
	}

	graphDef, hasAPI, err := loadIntegrationAPIDefinition(context.Background(), "graphapi", loadedCfg.Integrations["graphapi"], loadedCfg.ProviderDirs)
	if err != nil {
		t.Fatalf("loadIntegrationAPIDefinition graphapi: %v", err)
	}
	if !hasAPI {
		t.Fatal("graphapi should still have API definition in bundled config")
	}
	if _, ok := graphDef.Operations["searchIssues"]; !ok {
		t.Fatalf("graph artifact missing searchIssues: %+v", graphDef.Operations)
	}
}
