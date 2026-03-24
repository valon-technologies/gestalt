package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/internal/provider"
)

func TestCompileProvidersWritesArtifacts(t *testing.T) {
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
	cfgPath := filepath.Join(dir, "config.yaml")
	outDir := filepath.Join(dir, "providers")

	providerDir := filepath.Join(dir, "cached-providers")
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fromdirDef := &provider.Definition{
		Provider: "fromdir",
		Auth:     provider.AuthDef{Type: "manual"},
		Operations: map[string]provider.OperationDef{
			"findItems": {Description: "Find items", Transport: "graphql", Query: "query FindItems { findItems { id } }"},
		},
	}
	fromdirData, err := json.MarshalIndent(fromdirDef, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	fromdirData = append(fromdirData, '\n')
	if err := os.WriteFile(filepath.Join(providerDir, "fromdir.json"), fromdirData, 0644); err != nil {
		t.Fatalf("WriteFile fromdir: %v", err)
	}

	iconPath := filepath.Join(dir, "rest.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`), 0644); err != nil {
		t.Fatalf("WriteFile icon: %v", err)
	}
	cfg := `auth:
  provider: google
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: test-key
integrations:
  restapi:
    display_name: REST API
    icon_file: ` + iconPath + `
    upstreams:
      - type: rest
        url: ` + openAPIServer.URL + `
  graphapi:
    upstreams:
      - type: graphql
        url: ` + graphQLServer.URL + `
  mcp_only:
    upstreams:
      - type: mcp
        url: https://example.com/mcp
  fromdir:
    upstreams:
      - type: graphql
provider_dirs:
  - ` + providerDir + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := compileProviders(cfgPath, outDir); err != nil {
		t.Fatalf("compileProviders: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "mcp_only.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no artifact for mcp_only, stat err=%v", err)
	}

	fromdirDef, err = provider.LoadFile(filepath.Join(outDir, "fromdir.json"))
	if err != nil {
		t.Fatalf("LoadFile fromdir: %v", err)
	}
	if _, ok := fromdirDef.Operations["findItems"]; !ok {
		t.Fatalf("fromdir artifact missing findItems: %+v", fromdirDef.Operations)
	}

	restDef, err := provider.LoadFile(filepath.Join(outDir, "restapi.json"))
	if err != nil {
		t.Fatalf("LoadFile restapi: %v", err)
	}
	if _, ok := restDef.Operations["listUsers"]; !ok {
		t.Fatalf("rest artifact missing listUsers: %+v", restDef.Operations)
	}
	if restDef.DisplayName != "REST API" {
		t.Fatalf("rest artifact DisplayName = %q, want REST API", restDef.DisplayName)
	}
	if restDef.IconSVG == "" {
		t.Fatal("rest artifact missing embedded IconSVG")
	}

	graphDef, err := provider.LoadFile(filepath.Join(outDir, "graphapi.json"))
	if err != nil {
		t.Fatalf("LoadFile graphapi: %v", err)
	}
	op, ok := graphDef.Operations["searchIssues"]
	if !ok {
		t.Fatalf("graph artifact missing searchIssues: %+v", graphDef.Operations)
	}
	if op.Transport != "graphql" {
		t.Fatalf("searchIssues transport = %q, want graphql", op.Transport)
	}
	if op.InputSchema != nil {
		t.Fatalf("searchIssues InputSchema = %s, want nil", op.InputSchema)
	}
}
