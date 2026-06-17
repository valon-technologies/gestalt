package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const postTimeout = 120 * time.Second

type StatelessHTTPHandler struct {
	cfg              Config
	allowedProviders map[string]struct{}
	providerNames    []string
}

type statelessToolRef struct {
	provider  string
	operation string
}

func NewStatelessHTTPHandler(cfg Config) http.Handler {
	var allowed map[string]struct{}
	var names []string
	if cfg.AllowedProviders == nil && cfg.Providers != nil {
		names = append(names, cfg.Providers.List()...)
	} else {
		names = make([]string, 0, len(cfg.AllowedProviders))
		allowed = make(map[string]struct{}, len(cfg.AllowedProviders))
		for _, name := range cfg.AllowedProviders {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			allowed[name] = struct{}{}
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return &StatelessHTTPHandler{
		cfg:              cfg,
		allowedProviders: allowed,
		providerNames:    names,
	}
}

func (h *StatelessHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "MCP only accepts JSON-RPC over POST", http.StatusMethodNotAllowed)
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}
	if !acceptsJSON(r.Header.Get("Accept")) {
		http.Error(w, "Accept must allow application/json", http.StatusBadRequest)
		return
	}
	if version := strings.TrimSpace(r.Header.Get(mcpserver.HeaderKeyProtocolVersion)); version != "" && !slices.Contains(mcpgo.ValidProtocolVersions, version) {
		http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), postTimeout)
	defer cancel()

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeRPCError(w, nil, mcpgo.PARSE_ERROR, "request body is not valid json")
		return
	}

	resp, notification, err := h.handleMessage(ctx, r.Header, raw)
	if err != nil {
		writeRPCMessage(w, err)
		return
	}
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPCMessage(w, resp)
}

func (h *StatelessHTTPHandler) handleMessage(ctx context.Context, headers http.Header, raw json.RawMessage) (any, bool, any) {
	base, err := decodeBaseMessage(raw)
	if err != nil {
		return nil, false, rpcError(nil, mcpgo.PARSE_ERROR, "failed to parse message")
	}
	if base.JSONRPC != mcpgo.JSONRPC_VERSION {
		return nil, false, rpcError(base.id(), mcpgo.INVALID_REQUEST, "invalid JSON-RPC version")
	}
	if base.ID == nil {
		return nil, true, nil
	}
	if base.Result != nil || base.Error != nil {
		return nil, true, nil
	}

	switch base.Method {
	case string(mcpgo.MethodInitialize):
		var req mcpgo.InitializeRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, false, rpcError(base.id(), mcpgo.INVALID_REQUEST, "unparsable initialize request")
		}
		version := req.Params.ProtocolVersion
		if !slices.Contains(mcpgo.ValidProtocolVersions, version) {
			version = mcpgo.LATEST_PROTOCOL_VERSION
		}
		return rpcResponse(base.id(), mcpgo.InitializeResult{
			ProtocolVersion: version,
			ServerInfo: mcpgo.Implementation{
				Name:    serverName,
				Version: serverVersion,
			},
			Capabilities: mcpgo.ServerCapabilities{
				Tools: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{},
			},
		}), false, nil
	case string(mcpgo.MethodPing):
		return rpcResponse(base.id(), mcpgo.EmptyResult{}), false, nil
	case string(mcpgo.MethodToolsList):
		var req mcpgo.ListToolsRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, false, rpcError(base.id(), mcpgo.INVALID_REQUEST, "unparsable tools/list request")
		}
		req.Header = headers
		result, err := h.listTools(ctx, req)
		if err != nil {
			return nil, false, rpcError(base.id(), mcpgo.INVALID_PARAMS, err.Error())
		}
		return rpcResponse(base.id(), result), false, nil
	case string(mcpgo.MethodToolsCall):
		var req mcpgo.CallToolRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, false, rpcError(base.id(), mcpgo.INVALID_REQUEST, "unparsable tools/call request")
		}
		req.Header = headers
		result, err := h.callTool(ctx, req)
		if err != nil {
			if errors.Is(err, invocation.ErrOperationNotFound) || errors.Is(err, core.ErrNotFound) {
				return nil, false, rpcError(base.id(), mcpgo.METHOD_NOT_FOUND, err.Error())
			}
			return nil, false, rpcError(base.id(), mcpgo.INTERNAL_ERROR, err.Error())
		}
		return rpcResponse(base.id(), result), false, nil
	default:
		return nil, false, rpcError(base.id(), mcpgo.METHOD_NOT_FOUND, fmt.Sprintf("method %q is not supported", base.Method))
	}
}

func (h *StatelessHTTPHandler) listTools(ctx context.Context, req mcpgo.ListToolsRequest) (mcpgo.ListToolsResult, error) {
	if strings.TrimSpace(string(req.Params.Cursor)) != "" {
		return mcpgo.ListToolsResult{}, fmt.Errorf("tools/list cursor is not supported")
	}
	p := principal.FromContext(ctx)
	tools := make([]mcpgo.Tool, 0)
	for _, provName := range h.providerNames {
		prov, ok := h.provider(provName)
		if !ok {
			continue
		}
		rawCat, err := h.resolveCatalog(ctx, provName, prov, "", true)
		if err != nil {
			continue
		}
		cat := projectCatalog(h.cfg, provName, prov, rawCat)
		if cat == nil {
			continue
		}
		toolMap, _ := statelessToolMap(h.cfg, provName, cat)
		names := make([]string, 0, len(toolMap))
		for name := range toolMap {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if p != nil && !principal.AllowsProviderPermission(p, provName) {
				continue
			}
			tools = append(tools, toolMap[name])
		}
	}
	return mcpgo.ListToolsResult{Tools: tools}, nil
}

func (h *StatelessHTTPHandler) callTool(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return mcpgo.NewToolResultError("not authenticated"), nil
	}
	provName := providerNameForTool(h.cfg.ToolPrefixes, h.providerNames, req.Params.Name)
	if provName == "" {
		return nil, fmt.Errorf("%w: %q", invocation.ErrOperationNotFound, req.Params.Name)
	}
	prov, ok := h.provider(provName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", core.ErrNotFound, provName)
	}

	rawArgs := req.GetArguments()
	instance := normalizedSessionCatalogInstance(rawArgs["_instance"])
	args := make(map[string]any, len(rawArgs))
	for key, value := range rawArgs {
		if key == "_instance" {
			continue
		}
		args[key] = value
	}

	rawCat, err := h.resolveCatalog(ctx, provName, prov, instance, true)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	projectedCat := projectCatalog(h.cfg, provName, prov, rawCat)
	projectedTools, projectedRefs := statelessToolMap(h.cfg, provName, projectedCat)
	if _, ok := projectedTools[req.Params.Name]; !ok {
		return nil, fmt.Errorf("%w: %q", invocation.ErrOperationNotFound, req.Params.Name)
	}
	ref, ok := projectedRefs[req.Params.Name]
	if !ok || ref.provider != provName {
		return nil, fmt.Errorf("%w: %q", invocation.ErrOperationNotFound, req.Params.Name)
	}
	rawOp, ok := invocation.CatalogOperation(rawCat, ref.operation)
	if !ok {
		return nil, fmt.Errorf("%w: %q", invocation.ErrOperationNotFound, ref.operation)
	}
	projectedOp, ok := invocation.CatalogOperation(projectedCat, ref.operation)
	if !ok || !catalogOperationProjectedToMCP(h.cfg, provName, projectedOp) {
		return mcpgo.NewToolResultError("requested instance is unavailable for this tool"), nil
	}
	if err := validateProjectedCatalogInvocation(rawOp, projectedOp, args); err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	sessionConnections := []string{h.cfg.MCPConnection[provName]}
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, provName, h.cfg.TokenResolver, p, ref.operation, sessionConnections, instance)
	if err != nil {
		return nil, err
	}
	ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceMCP)
	ctx = invocation.WithCatalogOperation(ctx, provName, opMeta)
	if resolvedConnection != "" {
		ctx = invocation.WithConnection(ctx, resolvedConnection)
	}
	if err := validateToolInvocation(ctx, h.cfg, provName, ref.operation, projectedOp, true, args); err != nil {
		if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrScopeDenied) {
			return mcpgo.NewToolResultError("operation access denied"), nil
		}
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	ctx = mcpupstream.WithCallToolMeta(ctx, req.Params.Meta)
	result, err := h.cfg.Invoker.Invoke(ctx, p, provName, instance, ref.operation, args)
	if err != nil {
		if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrScopeDenied) {
			return mcpgo.NewToolResultError("operation access denied"), nil
		}
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	return operationResultToMCP(result), nil
}

func statelessToolMap(cfg Config, provName string, cat *catalog.Catalog) (map[string]mcpgo.Tool, map[string]statelessToolRef) {
	if cat == nil {
		return map[string]mcpgo.Tool{}, map[string]statelessToolRef{}
	}
	tools := make(map[string]mcpgo.Tool, len(cat.Operations))
	refs := make(map[string]statelessToolRef, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if !catalogOperationProjectedToMCP(cfg, provName, *op) {
			continue
		}

		name := toolName(cfg.ToolPrefixes, provName, op.ID)
		var tool mcpgo.Tool
		if len(op.InputSchema) > 0 {
			tool = mcpgo.NewToolWithRawSchema(name, op.Description, op.InputSchema)
		} else {
			tool = mcpgo.NewTool(name, mcpgo.WithDescription(op.Description))
		}

		tool.Annotations = mapAnnotations(op.Annotations)
		if op.Title != "" {
			tool.Annotations.Title = op.Title
		} else {
			tool.Annotations.Title = op.ID
		}

		if len(op.OutputSchema) > 0 {
			tool.RawOutputSchema = op.OutputSchema
		}

		tools[name] = tool
		refs[name] = statelessToolRef{provider: provName, operation: op.ID}
	}
	return tools, refs
}

func (h *StatelessHTTPHandler) provider(provName string) (core.Provider, bool) {
	if h.cfg.Providers == nil {
		return nil, false
	}
	if h.allowedProviders != nil {
		if _, ok := h.allowedProviders[provName]; !ok {
			return nil, false
		}
	}
	prov, err := h.cfg.Providers.Get(provName)
	return prov, err == nil && prov != nil
}

func (h *StatelessHTTPHandler) resolveCatalog(ctx context.Context, provName string, prov core.Provider, instance string, strict bool) (*catalog.Catalog, error) {
	targets := []invocation.CatalogResolutionTarget{{
		Connection: h.cfg.MCPConnection[provName],
		Instance:   instance,
		Surface:    core.CatalogSurfaceMCP,
	}}
	cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(ctx, prov, provName, h.cfg.TokenResolver, principal.FromContext(ctx), targets, strict)
	return cat, err
}

func validateProjectedCatalogInvocation(rawOp, projectedOp catalog.CatalogOperation, args map[string]any) error {
	for _, name := range hiddenSessionCatalogArguments(rawOp, projectedOp) {
		if _, ok := args[name]; ok {
			return fmt.Errorf("%w: parameter %q is not public", invocation.ErrInvalidInvocation, name)
		}
	}
	enums := sessionCatalogSchemaEnums(projectedOp.InputSchema)
	for name, values := range enums {
		raw, ok := args[name]
		if !ok || raw == nil {
			continue
		}
		if _, allowed := values[fmt.Sprint(raw)]; !allowed {
			return fmt.Errorf("%w: parameter %q value %q is not public", invocation.ErrInvalidInvocation, name, fmt.Sprint(raw))
		}
	}
	return nil
}

func hiddenSessionCatalogArguments(rawOp catalog.CatalogOperation, projectedOp catalog.CatalogOperation) []string {
	projected := map[string]struct{}{}
	for _, param := range projectedOp.Parameters {
		projected[param.Name] = struct{}{}
	}
	for name := range schemaPropertyNames(projectedOp.InputSchema) {
		projected[name] = struct{}{}
	}

	hidden := map[string]struct{}{}
	for _, param := range rawOp.Parameters {
		if _, ok := projected[param.Name]; !ok {
			hidden[param.Name] = struct{}{}
		}
	}
	for name := range schemaPropertyNames(rawOp.InputSchema) {
		if _, ok := projected[name]; !ok {
			hidden[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(hidden))
	for name := range hidden {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schemaPropertyNames(raw json.RawMessage) map[string]struct{} {
	props := schemaProperties(raw)
	if len(props) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(props))
	for name := range props {
		names[name] = struct{}{}
	}
	return names
}

func sessionCatalogSchemaEnums(raw json.RawMessage) map[string]map[string]struct{} {
	props := schemaProperties(raw)
	if len(props) == 0 {
		return nil
	}
	out := map[string]map[string]struct{}{}
	for name, prop := range props {
		rawEnum, _ := prop["enum"].([]any)
		if len(rawEnum) == 0 {
			continue
		}
		values := make(map[string]struct{}, len(rawEnum))
		for _, value := range rawEnum {
			values[fmt.Sprint(value)] = struct{}{}
		}
		out[name] = values
	}
	return out
}

func schemaProperties(raw json.RawMessage) map[string]map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	rawProps, _ := schema["properties"].(map[string]any)
	if len(rawProps) == 0 {
		return nil
	}
	props := make(map[string]map[string]any, len(rawProps))
	for name, rawProp := range rawProps {
		prop, _ := rawProp.(map[string]any)
		if prop == nil {
			continue
		}
		props[name] = prop
	}
	return props
}

func normalizedSessionCatalogInstance(value any) string {
	raw := strings.TrimSpace(fmt.Sprint(value))
	if value == nil || raw == "" || raw == "<nil>" {
		return ""
	}
	return raw
}

func operationResultToMCP(result *core.OperationResult) *mcpgo.CallToolResult {
	if result == nil {
		return mcpgo.NewToolResultText("{}")
	}
	if orig, ok := result.MCPResult.(*mcpgo.CallToolResult); ok && orig != nil {
		return orig
	}
	body := string(result.Body)
	if result.Status >= http.StatusBadRequest {
		return mcpgo.NewToolResultError(body)
	}
	return mcpgo.NewToolResultText(body)
}

type baseRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *mcpgo.RequestId `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

func (m baseRPCMessage) id() *mcpgo.RequestId {
	return m.ID
}

func decodeBaseMessage(raw json.RawMessage) (baseRPCMessage, error) {
	var msg baseRPCMessage
	err := json.Unmarshal(raw, &msg)
	return msg, err
}

func rpcResponse(id *mcpgo.RequestId, result any) mcpgo.JSONRPCResponse {
	reqID := mcpgo.NewRequestId(nil)
	if id != nil {
		reqID = *id
	}
	return mcpgo.JSONRPCResponse{JSONRPC: mcpgo.JSONRPC_VERSION, ID: reqID, Result: result}
}

func rpcError(id *mcpgo.RequestId, code int, message string) mcpgo.JSONRPCError {
	reqID := mcpgo.NewRequestId(nil)
	if id != nil {
		reqID = *id
	}
	return mcpgo.JSONRPCError{
		JSONRPC: mcpgo.JSONRPC_VERSION,
		ID:      reqID,
		Error:   mcpgo.NewJSONRPCErrorDetails(code, message, nil),
	}
}

func writeRPCMessage(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCError(w http.ResponseWriter, id *mcpgo.RequestId, code int, message string) {
	writeRPCMessage(w, rpcError(id, code, message))
}

func isJSONContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && mediaType == "application/json"
}

func acceptsJSON(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	for _, part := range strings.Split(raw, ",") {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if mediaType == "application/json" || mediaType == "application/*" || mediaType == "*/*" {
			return true
		}
	}
	return false
}
