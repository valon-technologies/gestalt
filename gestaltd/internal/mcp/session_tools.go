package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	hydrationMarkerPrefix             = "__gestalt_internal_hydrated__:"
	hydrationAttemptMarkerPrefix      = "__gestalt_internal_hydration_attempted__:"
	sessionCatalogOperationMetaPrefix = "__gestalt_internal_catalog_op__:"
	instanceHydratedToolMarkerPrefix  = "__gestalt_internal_instance_tool__:"
)

type sessionCatalogOperationMeta struct {
	AllowedRoles    []string        `json:"allowedRoles,omitempty"`
	Transport       string          `json:"transport,omitempty"`
	Connection      string          `json:"connection,omitempty"`
	Projected       bool            `json:"projected,omitempty"`
	HiddenArguments []string        `json:"hiddenArguments,omitempty"`
	InputSchema     json.RawMessage `json:"inputSchema,omitempty"`
}

func hydrateSessionTools(ctx context.Context, cfg Config, providerNames []string, staticToolNames map[string]struct{}) {
	hydrateSessionToolsForInstance(ctx, cfg, providerNames, staticToolNames, "")
}

func hydrateSessionToolsForInstance(ctx context.Context, cfg Config, providerNames []string, staticToolNames map[string]struct{}, instance string) {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return
	}

	tools := sessionWithTools.GetSessionTools()
	if tools == nil {
		tools = make(map[string]mcpserver.ServerTool)
	}

	changed := false
	for _, provName := range providerNames {
		if sessionProviderHydrated(tools, provName, instance) {
			continue
		}
		if markSessionProviderHydrationAttempted(tools, provName, instance) {
			changed = true
		}

		prov, err := cfg.Providers.Get(provName)
		if err != nil {
			continue
		}

		if !core.SupportsSessionCatalog(prov) {
			continue
		}

		sessionCtx, token, connection, err := resolveSessionToken(ctx, cfg, provName, prov, instance)
		if err != nil {
			continue
		}

		rawCat, _, err := core.CatalogForRequest(sessionCtx, prov, token)
		if err != nil {
			continue
		}
		cat := projectCatalog(cfg, provName, prov, rawCat)
		if deleteSessionProviderHydrationAttempted(tools, provName, instance) {
			changed = true
		}
		if markSessionProviderHydrated(tools, provName, instance) {
			changed = true
		}
		if cat == nil {
			continue
		}
		storeSessionCatalogOperationMetadata(tools, cfg, provName, rawCat, cat, staticToolNames, instance, connection)

		m := buildToolMap(cfg, provName, cat)
		for name := range m {
			if _, exists := staticToolNames[name]; exists {
				continue
			}
			if instance != "" {
				if _, exists := tools[name]; exists {
					continue
				}
				markInstanceHydratedTool(tools, provName, name, instance)
			}
			tools[name] = m[name]
			changed = true
		}
	}

	if changed {
		sessionWithTools.SetSessionTools(tools)
	}
}

func resolveSessionToken(ctx context.Context, cfg Config, provName string, prov core.Provider, instanceOverride string) (context.Context, string, string, error) {
	if prov.ConnectionMode() == core.ConnectionModeNone {
		if cfg.TokenResolver != nil {
			p := principal.FromContext(ctx)
			if p != nil {
				connection, instance := sessionTokenSelectors(cfg, provName, instanceOverride)
				sessionCtx, token, err := cfg.TokenResolver.ResolveToken(ctx, p, provName, connection, instance)
				if err != nil {
					return sessionCtx, token, connection, err
				}
				return withSessionAccessContext(sessionCtx, cfg, provName), token, connection, nil
			}
		}
		return withSessionAccessContext(invocation.WithCredentialContext(ctx, invocation.CredentialContext{Mode: core.ConnectionModeNone}), cfg, provName), "", "", nil
	}
	if cfg.TokenResolver == nil {
		return withSessionAccessContext(ctx, cfg, provName), "", "", nil
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return ctx, "", "", fmt.Errorf("not authenticated")
	}
	connection, instance := sessionTokenSelectors(cfg, provName, instanceOverride)
	sessionCtx, token, err := cfg.TokenResolver.ResolveToken(ctx, p, provName, connection, instance)
	if err != nil {
		return sessionCtx, token, connection, err
	}
	return withSessionAccessContext(sessionCtx, cfg, provName), token, connection, nil
}

func withSessionAccessContext(ctx context.Context, cfg Config, provName string) context.Context {
	if cfg.Authorizer == nil {
		return ctx
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return ctx
	}
	access, allowed := cfg.Authorizer.ResolveAccess(ctx, p, provName)
	if !allowed || (access.Policy == "" && access.Role == "") {
		return ctx
	}
	return invocation.WithAccessContext(ctx, access)
}

func sessionProviderHydrated(tools map[string]mcpserver.ServerTool, provider, instance string) bool {
	_, ok := tools[hydrationMarkerName(provider, instance)]
	return ok
}

func sessionProviderHydrationAttempted(tools map[string]mcpserver.ServerTool, provider, instance string) bool {
	_, ok := tools[hydrationAttemptMarkerName(provider, instance)]
	return ok
}

func sessionProviderHydratedFromContext(ctx context.Context, provider, instance string) bool {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return false
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return false
	}
	return sessionProviderHydrated(sessionWithTools.GetSessionTools(), provider, instance)
}

func sessionProviderHydrationAttemptedFromContext(ctx context.Context, provider, instance string) bool {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return false
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return false
	}
	return sessionProviderHydrationAttempted(sessionWithTools.GetSessionTools(), provider, instance)
}

func storeSessionCatalogOperationMetadata(tools map[string]mcpserver.ServerTool, cfg Config, provider string, rawCat *catalog.Catalog, cat *catalog.Catalog, staticToolNames map[string]struct{}, instance string, connection string) {
	rawOps := map[string]catalog.CatalogOperation{}
	if rawCat != nil {
		for i := range rawCat.Operations {
			rawOps[rawCat.Operations[i].ID] = rawCat.Operations[i]
		}
	}
	for i := range cat.Operations {
		op := &cat.Operations[i]
		rawOp := *op
		if candidate, ok := rawOps[op.ID]; ok {
			rawOp = candidate
		}
		payload, err := json.Marshal(sessionCatalogOperationMeta{
			AllowedRoles:    append([]string(nil), op.AllowedRoles...),
			Transport:       op.Transport,
			Connection:      connection,
			Projected:       catalogOperationProjectedToMCP(cfg, provider, *op),
			HiddenArguments: hiddenSessionCatalogArguments(rawOp, *op),
			InputSchema:     append(json.RawMessage(nil), op.InputSchema...),
		})
		if err != nil {
			continue
		}
		name := sessionCatalogOperationMarkerName(provider, op.ID, instance)
		tools[name] = mcpserver.ServerTool{
			Tool:    mcpgo.NewTool(name, mcpgo.WithDescription(string(payload))),
			Handler: internalSessionToolHandler,
		}
	}
}

func sessionCatalogOperationMetaFromContext(ctx context.Context, provider, operation, instance string) (sessionCatalogOperationMeta, bool) {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return sessionCatalogOperationMeta{}, false
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return sessionCatalogOperationMeta{}, false
	}
	tools := sessionWithTools.GetSessionTools()
	if len(tools) == 0 {
		return sessionCatalogOperationMeta{}, false
	}
	tool, ok := tools[sessionCatalogOperationMarkerName(provider, operation, instance)]
	if !ok {
		return sessionCatalogOperationMeta{}, false
	}
	var meta sessionCatalogOperationMeta
	if err := json.Unmarshal([]byte(tool.Tool.Description), &meta); err != nil {
		return sessionCatalogOperationMeta{}, false
	}
	return meta, true
}

func sessionCatalogOperationFromContext(ctx context.Context, provider, operation, instance string) (catalog.CatalogOperation, string, bool) {
	meta, ok := sessionCatalogOperationMetaFromContext(ctx, provider, operation, instance)
	if !ok || !meta.Projected {
		return catalog.CatalogOperation{}, "", false
	}
	return catalog.CatalogOperation{
		ID:           operation,
		AllowedRoles: meta.AllowedRoles,
		Transport:    meta.Transport,
	}, meta.Connection, true
}

func validateSessionCatalogInvocation(ctx context.Context, provider, operation, instance string, args map[string]any) error {
	meta, ok := sessionCatalogOperationMetaFromContext(ctx, provider, operation, instance)
	if !ok || !meta.Projected {
		return nil
	}
	for _, name := range meta.HiddenArguments {
		if _, ok := args[name]; ok {
			return fmt.Errorf("%w: parameter %q is not public", invocation.ErrInvalidInvocation, name)
		}
	}
	enums := sessionCatalogSchemaEnums(meta.InputSchema)
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

func sessionCatalogOperationSuppressedFromContext(ctx context.Context, provider, operation, instance string) bool {
	meta, ok := sessionCatalogOperationMetaFromContext(ctx, provider, operation, instance)
	return ok && !meta.Projected
}

func cleanupSessionToolsForInstance(ctx context.Context, provider, instance string) {
	if instance == "" {
		return
	}

	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return
	}

	tools := sessionWithTools.GetSessionTools()
	if len(tools) == 0 {
		return
	}

	changed := false
	for name := range tools {
		switch {
		case name == hydrationMarkerName(provider, instance):
			delete(tools, name)
			changed = true
		case name == hydrationAttemptMarkerName(provider, instance):
			delete(tools, name)
			changed = true
		case sessionCatalogOperationMarkerForInstance(name, provider, instance):
			delete(tools, name)
			changed = true
		default:
			toolName, ok := instanceHydratedToolName(name, provider, instance)
			if !ok {
				continue
			}
			delete(tools, name)
			delete(tools, toolName)
			changed = true
		}
	}

	if changed {
		sessionWithTools.SetSessionTools(tools)
	}
}

func markSessionProviderHydrated(tools map[string]mcpserver.ServerTool, provider, instance string) bool {
	name := hydrationMarkerName(provider, instance)
	if _, ok := tools[name]; ok {
		return false
	}
	tools[name] = mcpserver.ServerTool{
		Tool:    mcpgo.NewTool(name, mcpgo.WithDescription("gestalt internal hydration marker")),
		Handler: internalSessionToolHandler,
	}
	return true
}

func markSessionProviderHydrationAttempted(tools map[string]mcpserver.ServerTool, provider, instance string) bool {
	name := hydrationAttemptMarkerName(provider, instance)
	if _, ok := tools[name]; ok {
		return false
	}
	tools[name] = mcpserver.ServerTool{
		Tool:    mcpgo.NewTool(name, mcpgo.WithDescription("gestalt internal hydration attempt marker")),
		Handler: internalSessionToolHandler,
	}
	return true
}

func deleteSessionProviderHydrationAttempted(tools map[string]mcpserver.ServerTool, provider, instance string) bool {
	name := hydrationAttemptMarkerName(provider, instance)
	if _, ok := tools[name]; !ok {
		return false
	}
	delete(tools, name)
	return true
}

func markInstanceHydratedTool(tools map[string]mcpserver.ServerTool, provider, toolName, instance string) bool {
	if instance == "" {
		return false
	}
	name := instanceHydratedToolMarkerName(provider, toolName, instance)
	if _, ok := tools[name]; ok {
		return false
	}
	tools[name] = mcpserver.ServerTool{
		Tool:    mcpgo.NewTool(name, mcpgo.WithDescription("gestalt internal instance tool marker")),
		Handler: internalSessionToolHandler,
	}
	return true
}

func hydrationMarkerName(provider, instance string) string {
	if instance == "" {
		return hydrationMarkerPrefix + provider
	}
	return hydrationMarkerPrefix + provider + ":" + encodeSessionCatalogMarkerSegment(instance)
}

func hydrationAttemptMarkerName(provider, instance string) string {
	if instance == "" {
		return hydrationAttemptMarkerPrefix + provider
	}
	return hydrationAttemptMarkerPrefix + provider + ":" + encodeSessionCatalogMarkerSegment(instance)
}

func isHydrationMarkerTool(name string) bool {
	return len(name) > len(hydrationMarkerPrefix) && name[:len(hydrationMarkerPrefix)] == hydrationMarkerPrefix
}

func isHydrationAttemptMarkerTool(name string) bool {
	return len(name) > len(hydrationAttemptMarkerPrefix) && name[:len(hydrationAttemptMarkerPrefix)] == hydrationAttemptMarkerPrefix
}

func sessionCatalogOperationMarkerName(provider, operation, instance string) string {
	if instance == "" {
		return sessionCatalogOperationMetaPrefix + provider + ":" + operation
	}
	return sessionCatalogOperationMetaPrefix + provider + ":" + operation + ":" + encodeSessionCatalogMarkerSegment(instance)
}

func isSessionCatalogOperationMarkerTool(name string) bool {
	return len(name) > len(sessionCatalogOperationMetaPrefix) && name[:len(sessionCatalogOperationMetaPrefix)] == sessionCatalogOperationMetaPrefix
}

func sessionCatalogOperationMarkerForInstance(name, provider, instance string) bool {
	return strings.HasPrefix(name, sessionCatalogOperationMetaPrefix+provider+":") &&
		strings.HasSuffix(name, ":"+encodeSessionCatalogMarkerSegment(instance))
}

func internalSessionToolHandler(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultError("tool not found"), nil
}

func sessionTokenSelectors(cfg Config, provName, instanceOverride string) (string, string) {
	connection := cfg.MCPConnection[provName]
	instance := normalizedSessionCatalogInstance(instanceOverride)
	return connection, instance
}

func encodeSessionCatalogMarkerSegment(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeSessionCatalogMarkerSegment(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func instanceHydratedToolMarkerName(provider, toolName, instance string) string {
	return instanceHydratedToolMarkerPrefix + provider + ":" + encodeSessionCatalogMarkerSegment(toolName) + ":" + encodeSessionCatalogMarkerSegment(instance)
}

func instanceHydratedToolName(name, provider, instance string) (string, bool) {
	prefix := instanceHydratedToolMarkerPrefix + provider + ":"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	toolToken, instanceToken, ok := strings.Cut(rest, ":")
	if !ok || instanceToken != encodeSessionCatalogMarkerSegment(instance) {
		return "", false
	}
	toolName, err := decodeSessionCatalogMarkerSegment(toolToken)
	if err != nil {
		return "", false
	}
	return toolName, true
}

func normalizedSessionCatalogInstance(value any) string {
	instance, _ := value.(string)
	return strings.TrimSpace(instance)
}
