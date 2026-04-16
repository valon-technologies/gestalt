package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"

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
	Operation  catalog.CatalogOperation `json:"operation"`
	Connection string                   `json:"connection,omitempty"`
	Projected  bool                     `json:"projected,omitempty"`
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
		scp, ok := prov.(core.SessionCatalogProvider)
		if !ok {
			continue
		}
		sessionCtx, token, connection, err := resolveSessionToken(ctx, cfg, provName, prov, instance)
		if err != nil {
			continue
		}
		cat, err := scp.CatalogForRequest(sessionCtx, token)
		if err != nil {
			continue
		}
		if deleteSessionProviderHydrationAttempted(tools, provName, instance) {
			changed = true
		}
		if markSessionProviderHydrated(tools, provName, instance) {
			changed = true
		}
		if cat == nil {
			continue
		}
		effectiveCat := invocation.HydrateSessionCatalog(prov.Catalog(), cat)
		coreintegration.CompileSchemas(effectiveCat)
		storeSessionCatalogOperationMetadata(tools, cfg, provName, effectiveCat, instance, connection)
		m := buildToolMap(cfg, provName, effectiveCat)
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
				connection, instance := sessionTokenSelectors(cfg, p, provName, instanceOverride)
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
	connection, instance := sessionTokenSelectors(cfg, p, provName, instanceOverride)
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
	if p == nil || cfg.Authorizer.IsWorkload(p) {
		return ctx
	}
	access, allowed := cfg.Authorizer.ResolveAccess(p, provName)
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
func storeSessionCatalogOperationMetadata(tools map[string]mcpserver.ServerTool, cfg Config, provider string, cat *catalog.Catalog, instance string, connection string) {
	for i := range cat.Operations {
		op := &cat.Operations[i]
		payload, err := json.Marshal(sessionCatalogOperationMeta{
			Operation:  *op,
			Connection: connection,
			Projected:  catalogOperationProjectedToMCP(cfg, provider, *op),
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
	return meta.Operation, meta.Connection, true
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

func sessionTokenSelectors(cfg Config, p *principal.Principal, provName, instanceOverride string) (string, string) {
	connection := cfg.MCPConnection[provName]
	instance := normalizedSessionCatalogInstance(instanceOverride)
	if cfg.Authorizer == nil || !cfg.Authorizer.IsWorkload(p) {
		return connection, instance
	}
	if binding, ok := cfg.Authorizer.Binding(p, provName); ok {
		return binding.Connection, binding.Instance
	}
	return connection, ""
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

func workloadInstanceOverrideRequested(authz *authorization.Authorizer, p *principal.Principal, instance string) bool {
	return authz != nil && p != nil && authz.IsWorkload(p) && instance != ""
}
