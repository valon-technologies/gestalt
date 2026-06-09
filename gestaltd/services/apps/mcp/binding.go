package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const (
	serverName    = "gestalt"
	serverVersion = "0.1.0"
	toolNameSep   = "_"
)

type Config struct {
	Invoker             invocation.Invoker
	TokenResolver       invocation.TokenResolver
	AuditSink           core.AuditSink
	Providers           *registry.ProviderMap[core.Provider]
	AllowedProviders    []string
	ToolPrefixes        map[string]string
	IncludeREST         map[string]bool
	MCPConnection       map[string]string
	CatalogProjection   func(provName string, prov core.Provider, cat *catalog.Catalog) *catalog.Catalog
	InvocationValidator func(ctx context.Context, provName string, prov core.Provider, op catalog.CatalogOperation, params map[string]any, explicitConnection string) error
}
