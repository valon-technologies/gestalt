package mcp

import (
	"context"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/catalog"
	ci "github.com/valon-technologies/toolshed/core/integration"
	"github.com/valon-technologies/toolshed/internal/invocation"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/registry"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "toolshed"
	serverVersion = "0.1.0"
	toolNameSep   = "_"
	httpErrorMin  = 400
)

type Config struct {
	Broker           *invocation.Broker
	Providers        *registry.PluginMap[core.Provider]
	AllowedProviders []string
	ToolNamePrefix   string
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
				addCatalogTools(srv, cfg, provName, cat)
				continue
			}
		}

		addFlatTools(srv, cfg, provName, prov)
	}

	return srv
}

func addCatalogTools(srv *mcpserver.MCPServer, cfg Config, provName string, cat *catalog.Catalog) {
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if op.Visible != nil && !*op.Visible {
			continue
		}

		name := toolName(cfg.ToolNamePrefix, provName, op.ID)

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

		handler := makeHandler(cfg.Broker, provName, op.ID)
		srv.AddTool(tool, handler)
	}
}

func addFlatTools(srv *mcpserver.MCPServer, cfg Config, provName string, prov core.Provider) {
	for _, op := range prov.ListOperations() {
		name := toolName(cfg.ToolNamePrefix, provName, op.Name)

		opts := []mcpgo.ToolOption{mcpgo.WithDescription(op.Description)}
		annot := mapAnnotations(ci.AnnotationsFromMethod(op.Method))
		annot.Title = op.Name
		opts = append(opts, mcpgo.WithToolAnnotation(annot))

		for _, param := range op.Parameters {
			opts = append(opts, paramToOption(param))
		}

		tool := mcpgo.NewTool(name, opts...)
		handler := makeHandler(cfg.Broker, provName, op.Name)
		srv.AddTool(tool, handler)
	}
}

func makeHandler(broker *invocation.Broker, provName, opName string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}

		result, err := broker.Invoke(ctx, p, provName, opName, req.GetArguments())
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if result.Status >= httpErrorMin {
			return mcpgo.NewToolResultError(result.Body), nil
		}

		return mcpgo.NewToolResultText(result.Body), nil
	}
}

func toolName(prefix, provider, operation string) string {
	return prefix + provider + toolNameSep + operation
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
