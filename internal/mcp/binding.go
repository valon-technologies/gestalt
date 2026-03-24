package mcp

import (
	"context"
	"log"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	ci "github.com/valon-technologies/gestalt/core/integration"
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

type DeferredUpstream interface {
	IsDeferred() bool
	EnsureInitialized(ctx context.Context) (bool, error)
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
	allowed := make(map[string]struct{}, len(cfg.AllowedProviders))
	for _, p := range cfg.AllowedProviders {
		allowed[p] = struct{}{}
	}

	var deferred []deferredInfo
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
		if du := extractDeferredUpstream(prov); du != nil && du.IsDeferred() {
			deferred = append(deferred, deferredInfo{provName: provName, upstream: du, prov: prov})
		}
	}

	hooks := &mcpserver.Hooks{}
	opts := []mcpserver.ServerOption{mcpserver.WithHooks(hooks)}
	if len(deferred) > 0 {
		// AddTool enables tool capabilities implicitly, but with only
		// deferred providers the capability is never set.
		opts = append(opts, mcpserver.WithToolCapabilities(true))
	}
	srv := mcpserver.NewMCPServer(serverName, serverVersion, opts...)

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

		if cp, ok := prov.(core.CatalogProvider); ok {
			if cat := cp.Catalog(); cat != nil {
				addCatalogTools(srv, cfg, provName, cat, prov)
				continue
			}
		}

		addFlatTools(srv, cfg, provName, prov)
	}

	if len(deferred) > 0 {
		hooks.AddBeforeListTools(func(ctx context.Context, _ any, _ *mcpgo.ListToolsRequest) {
			tryInitDeferred(ctx, srv, cfg, deferred)
		})
	}

	return srv
}

type deferredInfo struct {
	provName string
	upstream DeferredUpstream
	prov     core.Provider
}

func tryInitDeferred(ctx context.Context, srv *mcpserver.MCPServer, cfg Config, deferred []deferredInfo) {
	p := principal.FromContext(ctx)
	if p == nil || cfg.TokenResolver == nil {
		return
	}
	for _, d := range deferred {
		if !d.upstream.IsDeferred() {
			continue
		}
		token, err := cfg.TokenResolver.ResolveToken(ctx, p, d.provName, "")
		if err != nil {
			continue
		}
		initCtx := mcpupstream.WithUpstreamToken(ctx, token)
		initialized, err := d.upstream.EnsureInitialized(initCtx)
		if err != nil {
			log.Printf("WARNING: deferred init %q: %v", d.provName, err)
			continue
		}
		if !initialized {
			continue
		}
		cp, ok := d.prov.(core.CatalogProvider)
		if !ok {
			continue
		}
		cat := cp.Catalog()
		if cat == nil {
			continue
		}
		addCatalogTools(srv, cfg, d.provName, cat, d.prov)
		log.Printf("deferred MCP upstream %q initialized, registered tools", d.provName)
	}
}

func extractDeferredUpstream(prov core.Provider) DeferredUpstream {
	if du, ok := prov.(DeferredUpstream); ok {
		return du
	}
	return nil
}

func addCatalogTools(srv *mcpserver.MCPServer, cfg Config, provName string, cat *catalog.Catalog, prov core.Provider) {
	caller, isDirect := unwrapDirectCaller(prov)
	if isDirect && cfg.TokenResolver == nil {
		isDirect = false
	}

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
		if isDirect && op.Transport != catalog.TransportHTTP {
			handler = makeDirectHandler(cfg, provName, op.ID, caller)
		} else {
			handler = makeHandler(cfg.Invoker, provName, op.ID)
		}
		srv.AddTool(tool, handler)
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
