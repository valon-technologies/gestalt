package mcp

import (
	"context"

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
	ResolveToken(ctx context.Context, p *principal.Principal, providerName string) (string, error)
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
}

func NewServer(cfg Config) *mcpserver.MCPServer {
	srv := mcpserver.NewMCPServer(serverName, serverVersion)

	allowed := make(map[string]struct{}, len(cfg.AllowedProviders))
	for _, p := range cfg.AllowedProviders {
		allowed[p] = struct{}{}
	}

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

	return srv
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

		result, err := invoker.Invoke(ctx, p, provName, opName, req.GetArguments())
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

		token, err := cfg.TokenResolver.ResolveToken(ctx, p, provName)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		ctx = mcpupstream.WithUpstreamToken(ctx, token)
		return caller.CallTool(ctx, opName, req.GetArguments())
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
