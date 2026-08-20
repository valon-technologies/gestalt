package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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

const (
	defaultSearchLimit  = 20
	maxSearchLimit      = 100
	maxSearchCandidates = 1000
	liveSearchTimeout   = 8 * time.Second
)

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
      "description": "If set, search only this app and include its live catalog."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of operations to return. Defaults to 20, max 100."
    }
  }
}`)
	describeToolSchema = json.RawMessage(`{
  "type": "object",
  "required": ["app", "operation"],
  "properties": {
    "app": {"type": "string", "description": "App name, for example linear."},
    "operation": {"type": "string", "description": "Operation id, for example search_issues."}
  }
}`)
	invokeToolSchema = json.RawMessage(`{
  "type": "object",
  "required": ["app", "operation"],
  "properties": {
    "app": {"type": "string", "description": "App name, for example linear."},
    "operation": {"type": "string", "description": "Operation id, for example search_issues."},
    "arguments": {"type": "object", "description": "Arguments for the operation."},
    "_instance": {"type": "string", "description": "Optional catalog instance."}
  }
}`)
)

func workspaceFrontDoorTools() []mcpgo.Tool {
	return []mcpgo.Tool{
		mcpgo.NewToolWithRawSchema(DescribeToolName, "Return one workspace operation's schema. Use this before gestalt_invoke when you need argument names.", describeToolSchema),
		mcpgo.NewToolWithRawSchema(InvokeToolName, "Invoke a workspace app operation. Pass app, operation, and arguments. Authorization is enforced on this call.", invokeToolSchema),
		mcpgo.NewToolWithRawSchema(SearchToolName, "Search workspace apps and operations the caller may use. Pass query and optionally app. Then call gestalt_describe or gestalt_invoke.", searchToolSchema),
	}
}

type searchHit struct {
	App         string `json:"app"`
	Operation   string `json:"operation"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MCPName     string `json:"mcpName"`
}

type searchUnavailable struct {
	App   string `json:"app"`
	Error string `json:"error"`
}

type searchResult struct {
	Results     []searchHit         `json:"results"`
	Unavailable []searchUnavailable `json:"unavailable,omitempty"`
}

type searchCandidate struct {
	hit   searchHit
	query invocation.OperationAccessQuery
}

type describeResult struct {
	App          string          `json:"app"`
	Operation    string          `json:"operation"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	MCPName      string          `json:"mcpName"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

func (h *StatelessHTTPHandler) callSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return mcpgo.NewToolResultError("not authenticated"), nil
	}
	args := req.GetArguments()
	query := stringArg(args, "query")
	appFilter := stringArg(args, "app")
	limit := intArg(args, "limit", defaultSearchLimit)
	if limit < 1 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	if query == "" && appFilter == "" {
		return toolJSONResult(searchResult{Results: []searchHit{}}), nil
	}

	candidates := make([]searchCandidate, 0)
	unavailable := make([]searchUnavailable, 0)
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
		live := appFilter != "" || queryMentionsProvider(query, provName, prov)
		cat, liveErr := h.catalogForSearch(ctx, provName, prov, live)
		if liveErr != nil {
			unavailable = append(unavailable, searchUnavailable{App: provName, Error: liveErr.Error()})
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
				hit: searchHit{
					App:         provName,
					Operation:   op.ID,
					Title:       operationTitle(op),
					Description: op.Description,
					MCPName:     toolName(h.cfg.ToolPrefixes, provName, op.ID),
				},
				query: invocation.OperationAccessQuery{
					Provider:     provName,
					Operation:    op.ID,
					AllowedRoles: op.AllowedRoles,
				},
			})
			if len(candidates) >= maxSearchCandidates {
				break
			}
		}
		if len(candidates) >= maxSearchCandidates {
			break
		}
	}

	allowed, err := h.filterSearchHits(ctx, p, candidates)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	if len(allowed) > limit {
		allowed = allowed[:limit]
	}
	return toolJSONResult(searchResult{Results: allowed, Unavailable: unavailable}), nil
}

func (h *StatelessHTTPHandler) filterSearchHits(ctx context.Context, p *principal.Principal, candidates []searchCandidate) ([]searchHit, error) {
	if len(candidates) == 0 {
		return []searchHit{}, nil
	}
	if h.cfg.OperationAccess == nil {
		hits := make([]searchHit, len(candidates))
		for i := range candidates {
			hits[i] = candidates[i].hit
		}
		return hits, nil
	}
	queries := make([]invocation.OperationAccessQuery, len(candidates))
	for i := range candidates {
		queries[i] = candidates[i].query
	}
	results, err := h.cfg.OperationAccess.CheckOperationAccessMany(ctx, p, queries)
	if err != nil {
		return nil, fmt.Errorf("search authorization is unavailable: %w", err)
	}
	if len(results) != len(candidates) {
		return nil, fmt.Errorf("search authorization returned %d decisions for %d operations", len(results), len(candidates))
	}
	hits := make([]searchHit, 0, len(candidates))
	for i := range candidates {
		if results[i] == nil {
			hits = append(hits, candidates[i].hit)
		}
	}
	return hits, nil
}

func (h *StatelessHTTPHandler) catalogForSearch(ctx context.Context, provName string, prov core.Provider, live bool) (*catalog.Catalog, error) {
	if live {
		liveCtx, cancel := context.WithTimeout(ctx, liveSearchTimeout)
		defer cancel()
		raw, err := h.resolveCatalog(liveCtx, provName, prov, "", false)
		if err == nil {
			return projectCatalog(h.cfg, provName, prov, raw), nil
		}
		if static := projectCatalog(h.cfg, provName, prov, prov.Catalog()); static != nil {
			return static, nil
		}
		return nil, err
	}
	return projectCatalog(h.cfg, provName, prov, prov.Catalog()), nil
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
	rawCat, err := h.resolveCatalog(ctx, app, prov, normalizedSessionCatalogInstance(args["_instance"]), true)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	cat := projectCatalog(h.cfg, app, prov, rawCat)
	op, found := invocation.CatalogOperation(cat, operation)
	if !found || !catalogOperationProjectedToMCP(h.cfg, app, op) {
		return mcpgo.NewToolResultError(fmt.Sprintf("operation %q is not available on app %q", operation, app)), nil
	}
	if h.cfg.OperationAccess != nil {
		results, err := h.cfg.OperationAccess.CheckOperationAccessMany(ctx, p, []invocation.OperationAccessQuery{{
			Provider:     app,
			Operation:    operation,
			AllowedRoles: op.AllowedRoles,
		}})
		if err != nil {
			return mcpgo.NewToolResultError("operation access is unavailable: " + err.Error()), nil
		}
		if len(results) != 1 || results[0] != nil {
			return mcpgo.NewToolResultError("operation access denied"), nil
		}
	}
	return toolJSONResult(describeResult{
		App:          app,
		Operation:    operation,
		Title:        operationTitle(op),
		Description:  op.Description,
		MCPName:      toolName(h.cfg.ToolPrefixes, app, operation),
		InputSchema:  op.InputSchema,
		OutputSchema: op.OutputSchema,
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
	inner.Params.Name = toolName(h.cfg.ToolPrefixes, app, operation)
	inner.Params.Arguments = nested
	return h.callAppTool(ctx, inner)
}

func operationTitle(op catalog.CatalogOperation) string {
	if strings.TrimSpace(op.Title) != "" {
		return op.Title
	}
	return op.ID
}

func queryMentionsProvider(query, provName string, prov core.Provider) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	names := []string{provName}
	if prov != nil {
		names = append(names, prov.DisplayName(), prov.Name())
	}
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && strings.Contains(q, name) {
			return true
		}
	}
	return false
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
