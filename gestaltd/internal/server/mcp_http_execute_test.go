package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestExecuteOperation_AllowsMCPHTTPPassthrough(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test-int",
		Operations: []catalog.CatalogOperation{
			{
				ID:           "run_query",
				Description:  "Run a query",
				InputSchema:  json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
				OutputSchema: json.RawMessage(`{"type":"object","properties":{"rows":{"type":"integer"}}}`),
				Transport:    catalog.TransportMCPPassthrough,
			},
			{
				ID:          "reserved",
				Description: "Uses a reserved REST control name",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"_connection":{"type":"string"}}}`),
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "reserved_param",
				Description: "Uses reserved REST control metadata",
				Parameters: []catalog.CatalogParameter{{
					Name: "_instance",
					Type: "string",
				}},
				Transport: catalog.TransportMCPPassthrough,
			},
			{
				ID:          "reserved_ref",
				Description: "Uses reserved REST control name through a JSON Schema ref",
				InputSchema: json.RawMessage(`{"$defs":{"Args":{"type":"object","properties":{"_connection":{"type":"string"}}}},"$ref":"#/$defs/Args"}`),
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "reserved_allof",
				Description: "Uses reserved REST control name through JSON Schema composition",
				InputSchema: json.RawMessage(`{"allOf":[{"type":"object","properties":{"_instance":{"type":"string"}}}]}`),
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "external_ref",
				Description: "Uses an unresolved external JSON Schema ref",
				InputSchema: json.RawMessage(`{"$ref":"https://example.test/schema.json"}`),
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "invalid_schema",
				Description: "Uses malformed JSON Schema",
				InputSchema: json.RawMessage(`{`),
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	var calledName string
	var calledArgs map[string]any
	prov := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test-int", ConnMode: core.ConnectionModeNone},
		},
		catalog: cat,
		callFn: func(_ context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			calledArgs = args
			return &mcpgo.CallToolResult{
				Content:           []mcpgo.Content{mcpgo.NewTextContent("query executed")},
				StructuredContent: map[string]any{"rows": 1},
			}, nil
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"test-int": {
				Surfaces: &config.ProviderSurfaceOverrides{
					MCP: &config.ProviderMCPSurfaceOverride{
						URL: "https://mcp.example.test/mcp",
					},
				},
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	opsResp, err := http.Get(ts.URL + "/api/v1/apps/test-int/operations")
	if err != nil {
		t.Fatalf("GET operations: %v", err)
	}
	defer func() { _ = opsResp.Body.Close() }()
	if opsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(opsResp.Body)
		t.Fatalf("GET operations expected 200, got %d: %s", opsResp.StatusCode, body)
	}
	var ops []catalog.CatalogOperation
	if err := json.NewDecoder(opsResp.Body).Decode(&ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("operations length = %d, want 1: %#v", len(ops), ops)
	}
	if ops[0].ID != "run_query" {
		t.Fatalf("operation ID = %q, want run_query", ops[0].ID)
	}
	if ops[0].Method != http.MethodPost {
		t.Fatalf("operation method = %q, want POST", ops[0].Method)
	}
	if ops[0].Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("operation transport = %q, want %q", ops[0].Transport, catalog.TransportMCPPassthrough)
	}
	var outputSchema map[string]any
	if err := json.Unmarshal(ops[0].OutputSchema, &outputSchema); err != nil {
		t.Fatalf("unmarshal output schema: %v", err)
	}
	properties, ok := outputSchema["properties"].(map[string]any)
	if !ok || properties["structuredContent"] == nil || properties["isError"] == nil {
		t.Fatalf("output schema missing MCP envelope properties: %#v", outputSchema)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/test-int/run_query", nil)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET run_query: %v", err)
	}
	_ = getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET run_query status = %d, want 405", getResp.StatusCode)
	}
	if got := getResp.Header.Get("Allow"); got != http.MethodPost {
		t.Fatalf("GET run_query Allow = %q, want POST", got)
	}

	postReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/test-int/run_query", bytes.NewBufferString(`{"sql":"SELECT 1"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST run_query: %v", err)
	}
	defer func() { _ = postResp.Body.Close() }()
	if postResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(postResp.Body)
		t.Fatalf("POST run_query expected 200, got %d: %s", postResp.StatusCode, body)
	}
	var result map[string]any
	if err := json.NewDecoder(postResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode run_query result: %v", err)
	}
	if result["isError"] != false {
		t.Fatalf("isError = %v, want false", result["isError"])
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["rows"].(float64) != 1 {
		t.Fatalf("structuredContent = %#v, want rows=1", result["structuredContent"])
	}
	if calledName != "run_query" {
		t.Fatalf("calledName = %q, want run_query", calledName)
	}
	if calledArgs["sql"] != "SELECT 1" {
		t.Fatalf("called sql arg = %#v, want SELECT 1", calledArgs["sql"])
	}

	prov.callFn = func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
		return &mcpgo.CallToolResult{
			Content:           []mcpgo.Content{mcpgo.NewTextContent("query failed")},
			StructuredContent: map[string]any{"code": "bad_query"},
			IsError:           true,
		}, nil
	}
	errorResp, err := http.Post(ts.URL+"/api/v1/test-int/run_query", "application/json", strings.NewReader(`{"sql":"broken"}`))
	if err != nil {
		t.Fatalf("POST run_query error result: %v", err)
	}
	defer func() { _ = errorResp.Body.Close() }()
	if errorResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(errorResp.Body)
		t.Fatalf("POST run_query error expected 200, got %d: %s", errorResp.StatusCode, body)
	}
	result = nil
	if err := json.NewDecoder(errorResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error result: %v", err)
	}
	if result["isError"] != true {
		t.Fatalf("isError = %v, want true", result["isError"])
	}

	assertHiddenMCPHTTP := func(operation, body string) {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/v1/test-int/"+operation, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", operation, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("POST %s expected 404, got %d: %s", operation, resp.StatusCode, payload)
		}
	}
	assertHiddenMCPHTTP("reserved", `{"_connection":"tool-value"}`)
	assertHiddenMCPHTTP("reserved_param", `{"_instance":"tool-value"}`)
	assertHiddenMCPHTTP("reserved_ref", `{"_connection":"tool-value"}`)
	assertHiddenMCPHTTP("reserved_allof", `{"_instance":"tool-value"}`)
	assertHiddenMCPHTTP("external_ref", `{}`)
	assertHiddenMCPHTTP("invalid_schema", `{}`)
}
