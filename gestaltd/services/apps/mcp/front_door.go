package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// Workspace MCP handshake tools. Hosts such as Muxy and Cursor require
// tools/list to finish before they mark the server connected, so this set is
// static and does not walk app catalogs.
const (
	SearchToolName   = "gestalt_search"
	DescribeToolName = "gestalt_describe"
	InvokeToolName   = "gestalt_invoke"
)

// workspaceFrontDoorInstructions is returned on initialize so hosts do not
// treat the three handshake tools as an inventory of app operations.
const workspaceFrontDoorInstructions = "tools/list is the connect handshake: gestalt_search, gestalt_describe, and gestalt_invoke. Search to find operations you may use, describe for one operation's schema, then invoke with app, operation, and arguments. App operations are not listed; these three names are reserved for the workspace front door."

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 100
	liveSearchTimeout  = 8 * time.Second
)

const catalogInstanceToolPropertyJSON = `"_instance": {"type": "string", "description": "Optional. Use when the app exposes more than one catalog instance."}`

var (
	searchToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Text to match against app and operation names, titles, and descriptions."
    },
    "app": {
      "type": "string",
      "description": "If set, search only this app and load its current catalog. If omitted, search uses each app's static catalog."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of operations to return. Defaults to 20, max 100."
    },
    ` + catalogInstanceToolPropertyJSON + `
  }
}`)
	describeToolSchema = json.RawMessage(`{
  "type": "object",
  "required": ["app", "operation"],
  "properties": {
    "app": {"type": "string", "description": "App name, for example linear."},
    "operation": {"type": "string", "description": "Operation id, for example search_issues."},
    ` + catalogInstanceToolPropertyJSON + `
  }
}`)
	invokeToolSchema = json.RawMessage(`{
  "type": "object",
  "required": ["app", "operation"],
  "properties": {
    "app": {"type": "string", "description": "App name, for example linear."},
    "operation": {"type": "string", "description": "Operation id, for example search_issues."},
    "arguments": {"type": "object", "description": "Arguments for the operation."},
    ` + catalogInstanceToolPropertyJSON + `
  }
}`)
)

func WorkspaceFrontDoorToolNames() []string {
	return []string{DescribeToolName, InvokeToolName, SearchToolName}
}

func workspaceFrontDoorTools() []mcpgo.Tool {
	return []mcpgo.Tool{
		mcpgo.NewToolWithRawSchema(DescribeToolName, "Return one workspace operation's schema. Use this before gestalt_invoke when you need argument names.", describeToolSchema),
		mcpgo.NewToolWithRawSchema(InvokeToolName, "Invoke a workspace app operation. Pass app, operation, and arguments. Authorization is enforced on this call.", invokeToolSchema),
		mcpgo.NewToolWithRawSchema(SearchToolName, "Search workspace apps and operations the caller may use. Pass query, app, or both. Then call gestalt_describe or gestalt_invoke.", searchToolSchema),
	}
}

type SearchHit struct {
	App             string `json:"app"`
	Operation       string `json:"operation"`
	CatalogInstance string `json:"_instance,omitempty"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
}

type SearchUnavailable struct {
	App   string `json:"app"`
	Error string `json:"error"`
}

type SearchResult struct {
	Results     []SearchHit         `json:"results"`
	Unavailable []SearchUnavailable `json:"unavailable,omitempty"`
	Hint        string              `json:"hint,omitempty"`
	Truncated   bool                `json:"truncated,omitempty"`
}

type searchCandidate struct {
	hit   SearchHit
	query invocation.OperationAccessQuery
}

type describeResult struct {
	App             string          `json:"app"`
	Operation       string          `json:"operation"`
	CatalogInstance string          `json:"_instance,omitempty"`
	Title           string          `json:"title,omitempty"`
	Description     string          `json:"description,omitempty"`
	InputSchema     json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema    json.RawMessage `json:"outputSchema,omitempty"`
}

func (h *StatelessHTTPHandler) callSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return mcpgo.NewToolResultError("not authenticated"), nil
	}
	args := req.GetArguments()
	query := stringArg(args, "query")
	appFilter := stringArg(args, "app")
	instance := stringArg(args, "_instance")
	limit := intArg(args, "limit", defaultSearchLimit)
	if limit < 1 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	if instance != "" && appFilter == "" {
		return mcpgo.NewToolResultError("app is required when _instance is set"), nil
	}
	if query == "" && appFilter == "" {
		return toolJSONResult(SearchResult{
			Results: []SearchHit{},
			Hint:    "Pass query or app to search workspace operations.",
		}), nil
	}

	candidates := make([]searchCandidate, 0)
	unavailable := make([]SearchUnavailable, 0)
	for _, provName := range h.providerNames {
		if appFilter != "" && provName != appFilter {
			continue
		}
		if !principal.AllowsProviderPermission(p, provName) {
			continue
		}
		prov, ok := h.provider(ctx, provName)
		if !ok {
			continue
		}
		cat, liveErr := h.catalogForSearch(ctx, provName, prov, instance, appFilter != "")
		if liveErr != nil {
			unavailable = append(unavailable, SearchUnavailable{App: provName, Error: liveErr.Error()})
			continue
		}
		if cat == nil {
			continue
		}
		appMatched := query == "" || searchTextMatches(query, provName, prov.DisplayName(), prov.Description(), cat.DisplayName, cat.Description)
		for i := range cat.Operations {
			op := cat.Operations[i]
			if !catalog.OperationVisibleByDefault(op) || !catalogOperationProjectedToMCP(h.cfg, provName, op) {
				continue
			}
			if !appMatched && !searchTextMatches(query, op.ID, op.Title, op.Description) {
				continue
			}
			candidates = append(candidates, searchCandidate{
				hit: SearchHit{
					App:             provName,
					Operation:       op.ID,
					CatalogInstance: instance,
					Title:           operationTitle(op),
					Description:     op.Description,
				},
				query: invocation.OperationAccessQuery{
					Provider:     provName,
					Operation:    op.ID,
					AllowedRoles: op.AllowedRoles,
				},
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].hit.App != candidates[j].hit.App {
			return candidates[i].hit.App < candidates[j].hit.App
		}
		return candidates[i].hit.Operation < candidates[j].hit.Operation
	})
	allowed, truncated, accessErr := h.pageSearchHits(ctx, p, candidates, limit)
	if accessErr != nil {
		return accessErr, nil
	}
	return toolJSONResult(SearchResult{Results: allowed, Unavailable: unavailable, Truncated: truncated}), nil
}

func (h *StatelessHTTPHandler) pageSearchHits(ctx context.Context, p *principal.Principal, candidates []searchCandidate, limit int) ([]SearchHit, bool, *mcpgo.CallToolResult) {
	hits := make([]SearchHit, 0, min(limit, len(candidates)))
	offset := 0
	for offset < len(candidates) && len(hits) < limit {
		need := limit - len(hits)
		take := min(len(candidates)-offset, invocation.MaxBatchedAccessChecks, max(need, defaultSearchLimit))
		window := candidates[offset : offset+take]
		queries := make([]invocation.OperationAccessQuery, len(window))
		for i := range window {
			queries[i] = window[i].query
		}
		ok, accessErr := h.allowedOperations(ctx, p, queries)
		if accessErr != nil {
			return nil, false, accessErr
		}
		consumed := 0
		for i := range window {
			consumed++
			if ok[i] {
				hits = append(hits, window[i].hit)
				if len(hits) == limit {
					break
				}
			}
		}
		offset += consumed
	}
	return hits, offset < len(candidates), nil
}

// allowedOperations answers the same evaluator questions FilterCatalogForPrincipal
// asks, then maps those answers to booleans so MCP handlers can return tool
// errors instead of JSON-RPC errors.
func (h *StatelessHTTPHandler) allowedOperations(ctx context.Context, p *principal.Principal, queries []invocation.OperationAccessQuery) ([]bool, *mcpgo.CallToolResult) {
	allowed := make([]bool, len(queries))
	if len(queries) == 0 {
		return allowed, nil
	}
	if h.cfg.OperationAccess == nil {
		for i := range allowed {
			allowed[i] = true
		}
		return allowed, nil
	}
	results, checkErr := h.cfg.OperationAccess.CheckOperationAccessMany(ctx, p, queries)
	if checkErr != nil {
		return nil, mcpgo.NewToolResultError("operation access is unavailable: " + checkErr.Error())
	}
	if len(results) != len(queries) {
		return nil, mcpgo.NewToolResultError(fmt.Sprintf("operation access returned %d decisions for %d operations", len(results), len(queries)))
	}
	for i := range results {
		allowed[i] = results[i] == nil
	}
	return allowed, nil
}

func (h *StatelessHTTPHandler) catalogForSearch(ctx context.Context, provName string, prov core.Provider, instance string, live bool) (*catalog.Catalog, error) {
	if !live {
		return projectCatalog(h.cfg, provName, prov, prov.Catalog()), nil
	}
	liveCtx, cancel := context.WithTimeout(ctx, liveSearchTimeout)
	defer cancel()
	raw, err := h.resolveCatalog(liveCtx, provName, prov, instance, true)
	if err != nil {
		return nil, err
	}
	return projectCatalog(h.cfg, provName, prov, raw), nil
}

func (h *StatelessHTTPHandler) callDescribe(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return mcpgo.NewToolResultError("not authenticated"), nil
	}
	args := req.GetArguments()
	app := stringArg(args, "app")
	operation := stringArg(args, "operation")
	if app == "" || operation == "" {
		return mcpgo.NewToolResultError("app and operation are required"), nil
	}
	if !principal.AllowsProviderPermission(p, app) {
		return mcpgo.NewToolResultError("operation access denied"), nil
	}
	prov, ok := h.provider(ctx, app)
	if !ok {
		return mcpgo.NewToolResultError(fmt.Sprintf("app %q is not available", app)), nil
	}
	instance := normalizedSessionCatalogInstance(args["_instance"])
	rawCat, err := h.resolveCatalog(ctx, app, prov, instance, true)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	cat := projectCatalog(h.cfg, app, prov, rawCat)
	op, found := invocation.CatalogOperation(cat, operation)
	if !found || !catalogOperationProjectedToMCP(h.cfg, app, op) {
		return mcpgo.NewToolResultError(fmt.Sprintf("operation %q is not available on app %q", operation, app)), nil
	}
	allowed, accessErr := h.allowedOperations(ctx, p, []invocation.OperationAccessQuery{{
		Provider:     app,
		Operation:    operation,
		AllowedRoles: op.AllowedRoles,
	}})
	if accessErr != nil {
		return accessErr, nil
	}
	if len(allowed) != 1 || !allowed[0] {
		return mcpgo.NewToolResultError("operation access denied"), nil
	}
	return toolJSONResult(describeResult{
		App:             app,
		Operation:       operation,
		CatalogInstance: instance,
		Title:           operationTitle(op),
		Description:     op.Description,
		InputSchema:     op.InputSchema,
		OutputSchema:    op.OutputSchema,
	}), nil
}

func (h *StatelessHTTPHandler) callInvoke(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	args := req.GetArguments()
	app := stringArg(args, "app")
	operation := stringArg(args, "operation")
	if app == "" || operation == "" {
		return mcpgo.NewToolResultError("app and operation are required"), nil
	}
	nested := map[string]any{}
	if raw, ok := args["arguments"].(map[string]any); ok {
		for key, value := range raw {
			nested[key] = value
		}
	}
	if inst, ok := args["_instance"]; ok {
		nested["_instance"] = inst
	}
	inner := req
	inner.Params.Arguments = nested
	result, err := h.callResolvedAppTool(ctx, inner, statelessToolRef{provider: app, operation: operation})
	if errors.Is(err, core.ErrNotFound) || errors.Is(err, invocation.ErrOperationNotFound) {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	return result, err
}

func operationTitle(op catalog.CatalogOperation) string {
	if strings.TrimSpace(op.Title) != "" {
		return op.Title
	}
	return op.ID
}

func searchTextMatches(query string, parts ...string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(part), q) {
			return true
		}
	}
	return false
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func intArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch value := args[key].(type) {
	case nil:
		return fallback
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, err := value.Int64()
		if err != nil {
			return fallback
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

func toolJSONResult(v any) *mcpgo.CallToolResult {
	raw, err := json.Marshal(v)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error())
	}
	return mcpgo.NewToolResultText(string(raw))
}

// DecodeSearchToolResult reads gestalt_search's JSON body from a tools/call
// envelope. Tests and clients use this so they do not each invent a decoder.
func DecodeSearchToolResult(envelope map[string]any) (SearchResult, error) {
	if envelope == nil {
		return SearchResult{}, fmt.Errorf("missing MCP envelope")
	}
	if rpcErr, ok := envelope["error"]; ok {
		return SearchResult{}, fmt.Errorf("gestalt_search rpc error: %v", rpcErr)
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return SearchResult{}, fmt.Errorf("gestalt_search missing result")
	}
	if isError, _ := result["isError"].(bool); isError {
		return SearchResult{}, fmt.Errorf("gestalt_search tool error: %v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return SearchResult{Results: []SearchHit{}}, nil
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	var body SearchResult
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return SearchResult{}, fmt.Errorf("decode gestalt_search: %w", err)
	}
	if body.Results == nil {
		body.Results = []SearchHit{}
	}
	return body, nil
}
