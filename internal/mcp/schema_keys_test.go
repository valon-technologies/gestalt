package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/core/integration"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestNewServer_FlatToolsSanitizeInvalidParameterNames(t *testing.T) {
	t.Parallel()

	var gotParams map[string]any

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "datadog"},
		ops: []core.Operation{
			{
				Name:        "search_logs",
				Description: "Search logs",
				Method:      http.MethodGet,
				Parameters: []core.Parameter{
					{Name: "filter[from]", Type: "string", Required: true},
					{Name: "page[size]", Type: "integer"},
				},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, p *principal.Principal, providerName, _, operation string, params map[string]any) (*core.OperationResult, error) {
				if p == nil || p.UserID == "" {
					t.Fatal("expected authenticated principal")
				}
				if providerName != "datadog" {
					t.Fatalf("expected provider datadog, got %q", providerName)
				}
				if operation != "search_logs" {
					t.Fatalf("expected operation search_logs, got %q", operation)
				}
				gotParams = params
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		Providers: providers,
	})

	schema := toolInputSchema(t, srv.ListTools()["datadog_search_logs"].Tool)
	assertSchemaKeys(t, schema, []string{"filter.from", "page.size"}, []string{"filter.from"})

	tool := srv.GetTool("datadog_search_logs")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "datadog_search_logs"
	req.Params.Arguments = map[string]any{
		"filter.from": "now-15m",
		"page.size":   25,
	}

	result, err := tool.Handler(ctxWithPrincipal(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if gotParams["filter[from]"] != "now-15m" {
		t.Fatalf("expected raw filter[from] param, got %v", gotParams)
	}
	if gotParams["page[size]"] != 25 {
		t.Fatalf("expected raw page[size] param, got %v", gotParams)
	}
}

func TestNewServer_CatalogToolsSanitizeInvalidSchemaPropertyNames(t *testing.T) {
	t.Parallel()

	var gotParams map[string]any

	cat := &catalog.Catalog{
		Name: "datadog",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "search_logs",
				Method:      http.MethodPost,
				Path:        "/logs/events/search",
				Description: "Search logs",
				InputSchema: json.RawMessage(`{
					"type":"object",
					"properties":{
						"filter[from]":{"type":"string"},
						"filter[to]":{"type":"string"},
						"page[size]":{"type":"integer"}
					},
					"required":["filter[from]"]
				}`),
			},
		},
	}

	prov := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "datadog"},
		ops:             coreintegration.OperationsList(cat),
		catalog:         cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, _ *principal.Principal, _, _, _ string, params map[string]any) (*core.OperationResult, error) {
				gotParams = params
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		Providers: providers,
	})

	schema := toolInputSchema(t, srv.ListTools()["datadog_search_logs"].Tool)
	assertSchemaKeys(t, schema, []string{"filter.from", "filter.to", "page.size"}, []string{"filter.from"})

	tool := srv.GetTool("datadog_search_logs")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "datadog_search_logs"
	req.Params.Arguments = map[string]any{
		"filter.from": "now-15m",
		"filter.to":   "now",
		"page.size":   50,
	}

	result, err := tool.Handler(ctxWithPrincipal(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if gotParams["filter[from]"] != "now-15m" || gotParams["filter[to]"] != "now" || gotParams["page[size]"] != 50 {
		t.Fatalf("expected remapped raw params, got %v", gotParams)
	}
}

func TestNewServer_MCPPassthroughSanitizesArrayItemSchemaKeys(t *testing.T) {
	t.Parallel()

	var gotArgs map[string]any

	cat := &catalog.Catalog{
		Name: "svc",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "search_logs",
				Description: "Search logs",
				Transport:   catalog.TransportMCPPassthrough,
				InputSchema: json.RawMessage(`{
					"type":"object",
					"properties":{
						"filters":{
							"type":"array",
							"items":{
								"type":"object",
								"properties":{
									"filter[from]":{"type":"string"},
									"filter[to]":{"type":"string"}
								},
								"required":["filter[from]"]
							}
						}
					},
					"required":["filters"]
				}`),
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "svc"},
		ops:             []core.Operation{{Name: "search_logs", Description: "Search logs"}},
		cat:             cat,
		callFn: func(_ context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			if name != "search_logs" {
				t.Fatalf("expected upstream name search_logs, got %q", name)
			}
			gotArgs = args
			return &mcpgo.CallToolResult{
				Content:           []mcpgo.Content{mcpgo.NewTextContent(`{"ok":true}`)},
				StructuredContent: map[string]any{"ok": true},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "test-token"},
		Providers:     providers,
	})

	schema := toolInputSchema(t, srv.ListTools()["svc_search_logs"].Tool)
	filters := nestedMap(t, schema, "properties", "filters")
	items := nestedMap(t, filters, "items")
	assertSchemaKeys(t, items, []string{"filter.from", "filter.to"}, []string{"filter.from"})

	tool := srv.GetTool("svc_search_logs")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "svc_search_logs"
	req.Params.Arguments = map[string]any{
		"filters": []any{
			map[string]any{
				"filter.from": "now-1h",
				"filter.to":   "now",
			},
		},
	}

	result, err := tool.Handler(ctxWithPrincipal(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	filtersArg, ok := gotArgs["filters"].([]any)
	if !ok || len(filtersArg) != 1 {
		t.Fatalf("expected one upstream filter item, got %v", gotArgs["filters"])
	}
	filterItem, ok := filtersArg[0].(map[string]any)
	if !ok {
		t.Fatalf("expected upstream filter item map, got %T", filtersArg[0])
	}
	if filterItem["filter[from]"] != "now-1h" || filterItem["filter[to]"] != "now" {
		t.Fatalf("expected remapped array item args, got %v", filterItem)
	}
}

func toolInputSchema(t *testing.T, tool mcpgo.Tool) map[string]any {
	t.Helper()

	raw, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal tool: %v", err)
	}

	schema, ok := doc["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("tool missing inputSchema: %v", doc)
	}
	return schema
}

func nestedMap(t *testing.T, root map[string]any, keys ...string) map[string]any {
	t.Helper()

	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("expected map at key %q, got %T", key, current[key])
		}
		current = next
	}
	return current
}

func assertSchemaKeys(t *testing.T, schema map[string]any, wantProps, wantRequired []string) {
	t.Helper()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing properties: %v", schema)
	}

	gotProps := make([]string, 0, len(props))
	for key := range props {
		gotProps = append(gotProps, key)
	}
	sort.Strings(gotProps)
	sort.Strings(wantProps)
	if !slices.Equal(gotProps, wantProps) {
		t.Fatalf("unexpected properties: got %v want %v", gotProps, wantProps)
	}

	gotRequired := stringSlice(schema["required"])
	sort.Strings(gotRequired)
	sort.Strings(wantRequired)
	if !slices.Equal(gotRequired, wantRequired) {
		t.Fatalf("unexpected required fields: got %v want %v", gotRequired, wantRequired)
	}
}

func stringSlice(v any) []string {
	switch vals := v.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), vals...)
	case []any:
		out := make([]string, 0, len(vals))
		for _, item := range vals {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

