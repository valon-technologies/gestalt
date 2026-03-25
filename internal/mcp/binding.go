package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	ci "github.com/valon-technologies/gestalt/core/integration"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "gestalt"
	serverVersion = "0.1.0"
	toolNameSep   = "_"
	httpErrorMin  = 400
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, instance string) (string, error)
}

type directToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

type Config struct {
	Invoker          invocation.Invoker
	TokenResolver    TokenResolver
	Providers        *registry.PluginMap[core.Provider]
	AllowedProviders []string
	ToolPrefixes     map[string]string
	IncludeHTTP      map[string]bool
}

func NewServer(cfg Config) *mcpserver.MCPServer {
	hooks := &mcpserver.Hooks{}
	srv := mcpserver.NewMCPServer(
		serverName,
		serverVersion,
		mcpserver.WithHooks(hooks),
		mcpserver.WithToolCapabilities(true),
	)

	allowed := make(map[string]struct{}, len(cfg.AllowedProviders))
	for _, p := range cfg.AllowedProviders {
		allowed[p] = struct{}{}
	}

	var dynamicProviders []string
	for _, provName := range cfg.Providers.List() {
		if cfg.AllowedProviders != nil {
			if _, ok := allowed[provName]; !ok {
				continue
			}
		}

		prov, err := cfg.Providers.Get(provName)
		if err != nil {
			continue
		}

		if _, ok := prov.(core.SessionCatalogProvider); ok {
			dynamicProviders = append(dynamicProviders, provName)
		}

		if cp, ok := prov.(core.CatalogProvider); ok {
			if cat := cp.Catalog(); cat != nil {
				addCatalogTools(srv, cfg, provName, cat, prov)
				continue
			}
		}

		addFlatTools(srv, cfg, provName, prov)
	}

	if len(dynamicProviders) > 0 {
		hooks.AddBeforeListTools(func(ctx context.Context, _ any, _ *mcpgo.ListToolsRequest) {
			hydrateSessionTools(ctx, cfg, dynamicProviders)
		})
		hooks.AddBeforeCallTool(func(ctx context.Context, _ any, req *mcpgo.CallToolRequest) {
			if provName := providerNameForTool(cfg.ToolPrefixes, dynamicProviders, req.Params.Name); provName != "" {
				hydrateSessionTools(ctx, cfg, []string{provName})
			}
		})
	}

	return srv
}

func addCatalogTools(srv *mcpserver.MCPServer, cfg Config, provName string, cat *catalog.Catalog, prov core.Provider) {
	m := buildToolMap(cfg, provName, prov, cat)
	for name := range m {
		srv.AddTool(m[name].Tool, m[name].Handler)
	}
}

func addFlatTools(srv *mcpserver.MCPServer, cfg Config, provName string, prov core.Provider) {
	for _, op := range prov.ListOperations() {
		name := toolName(cfg.ToolPrefixes, provName, op.Name)

		opts := []mcpgo.ToolOption{mcpgo.WithDescription(op.Description)}
		annot := mapAnnotations(ci.AnnotationsFromMethod(op.Method))
		annot.Title = op.Name
		opts = append(opts, mcpgo.WithToolAnnotation(annot))

		for _, param := range op.Parameters {
			opts = append(opts, paramToOption(param))
		}

		tool := mcpgo.NewTool(name, opts...)
		handler := makeHandler(cfg.Invoker, provName, op.Name)
		srv.AddTool(tool, handler)
	}
}

func makeHandler(invoker invocation.Invoker, provName, opName string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}

		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		result, err := invoker.Invoke(ctx, p, provName, instance, opName, args)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if result.Status >= httpErrorMin {
			return mcpgo.NewToolResultError(result.Body), nil
		}

		return mcpgo.NewToolResultText(result.Body), nil
	}
}

func makeDirectHandler(cfg Config, provName, opName string, caller directToolCaller) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}
		ctx = attachEgressSubject(ctx, p)

		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		token, err := cfg.TokenResolver.ResolveToken(ctx, p, provName, instance)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		ctx = mcpupstream.WithUpstreamToken(ctx, token)
		return caller.CallTool(ctx, opName, args)
	}
}

func hydrateSessionTools(ctx context.Context, cfg Config, providerNames []string) {
	if p := principal.FromContext(ctx); p != nil {
		ctx = attachEgressSubject(ctx, p)
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
	if tools == nil {
		tools = make(map[string]mcpserver.ServerTool)
	}

	changed := false
	for _, provName := range providerNames {
		prefix := toolName(cfg.ToolPrefixes, provName, "")
		if hasToolsWithPrefix(tools, prefix) {
			continue
		}

		prov, err := cfg.Providers.Get(provName)
		if err != nil {
			continue
		}

		scp, ok := prov.(core.SessionCatalogProvider)
		if !ok {
			continue
		}

		token, err := resolveSessionToken(ctx, cfg, provName, prov)
		if err != nil {
			continue
		}

		cat, err := scp.CatalogForRequest(ctx, token)
		if err != nil || cat == nil {
			continue
		}

		m := buildToolMap(cfg, provName, prov, cat)
		for name := range m {
			tools[name] = m[name]
		}
		changed = true
	}

	if changed {
		sessionWithTools.SetSessionTools(tools)
	}
}

func attachEgressSubject(ctx context.Context, p *principal.Principal) context.Context {
	if _, ok := egress.SubjectFromContext(ctx); ok {
		return ctx
	}
	if p == nil || p.UserID == "" {
		return ctx
	}
	return egress.WithSubject(ctx, egress.Subject{
		Kind: egress.SubjectUser,
		ID:   p.UserID,
	})
}

func hasToolsWithPrefix(tools map[string]mcpserver.ServerTool, prefix string) bool {
	for name := range tools {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func buildToolMap(cfg Config, provName string, prov core.Provider, cat *catalog.Catalog) map[string]mcpserver.ServerTool {
	caller, isDirect := unwrapDirectCaller(prov)
	if isDirect && cfg.TokenResolver == nil {
		isDirect = false
	}

	tools := make(map[string]mcpserver.ServerTool, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if op.Visible != nil && !*op.Visible {
			continue
		}
		if cfg.IncludeHTTP != nil && op.Transport == catalog.TransportHTTP && !cfg.IncludeHTTP[provName] {
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

		var handler mcpserver.ToolHandlerFunc
		if isDirect && op.Transport != catalog.TransportHTTP && op.Transport != catalog.TransportPlugin {
			handler = makeDirectHandler(cfg, provName, op.ID, caller)
		} else {
			handler = makeHandler(cfg.Invoker, provName, op.ID)
		}
		tools[name] = mcpserver.ServerTool{Tool: tool, Handler: handler}
	}
	return tools
}

func resolveSessionToken(ctx context.Context, cfg Config, provName string, prov core.Provider) (string, error) {
	if cfg.TokenResolver == nil || prov.ConnectionMode() == core.ConnectionModeNone {
		return "", nil
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return "", fmt.Errorf("not authenticated")
	}
	return cfg.TokenResolver.ResolveToken(ctx, p, provName, "")
}

func unwrapDirectCaller(prov core.Provider) (directToolCaller, bool) {
	if c, ok := prov.(directToolCaller); ok {
		return c, true
	}
	type inner interface{ Inner() core.Provider }
	if r, ok := prov.(inner); ok {
		c, ok := r.Inner().(directToolCaller)
		return c, ok
	}
	return nil, false
}

func providerNameForTool(prefixes map[string]string, providers []string, tool string) string {
	var best string
	bestLen := -1
	for _, prov := range providers {
		prefix := toolName(prefixes, prov, "")
		if !strings.HasPrefix(tool, prefix) {
			continue
		}
		if len(prefix) > bestLen {
			best = prov
			bestLen = len(prefix)
		}
	}
	return best
}

func toolName(prefixes map[string]string, provider, operation string) string {
	return prefixes[provider] + provider + toolNameSep + operation
}

func mapAnnotations(a catalog.OperationAnnotations) mcpgo.ToolAnnotation {
	return mcpgo.ToolAnnotation{
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
}

func buildPropertyOpts(param core.Parameter) []mcpgo.PropertyOption {
	opts := []mcpgo.PropertyOption{mcpgo.Description(param.Description)}
	if param.Required {
		opts = append(opts, mcpgo.Required())
	}
	return opts
}

func paramToOption(param core.Parameter) mcpgo.ToolOption {
	switch ci.NormalizeType(param.Type) {
	case "integer", "number":
		return mcpgo.WithNumber(param.Name, buildPropertyOpts(param)...)
	case "boolean":
		return mcpgo.WithBoolean(param.Name, buildPropertyOpts(param)...)
	case "array":
		return mcpgo.WithArray(param.Name, buildPropertyOpts(param)...)
	case "object":
		return mcpgo.WithObject(param.Name, buildPropertyOpts(param)...)
	default:
		return mcpgo.WithString(param.Name, buildPropertyOpts(param)...)
	}
}
