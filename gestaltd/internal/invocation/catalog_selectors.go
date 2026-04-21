package invocation

import (
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type CatalogSelectorConfig struct {
	Authorizer        authorization.RuntimeAuthorizer
	Invoker           any
	CatalogConnection map[string]string
	DefaultConnection map[string]string
}

func (cfg CatalogSelectorConfig) SessionCatalogConnections(providerName string, p *principal.Principal, explicit string) []string {
	if explicit != "" {
		return []string{config.ResolveConnectionAlias(explicit)}
	}
	if cfg.isWorkload(p) {
		return []string{""}
	}

	connections := make([]string, 0, 2)
	if conn := cfg.CatalogConnection[providerName]; conn != "" {
		connections = append(connections, conn)
	} else if broker, ok := cfg.Invoker.(interface{ MCPConnection(string) string }); ok {
		if conn := broker.MCPConnection(providerName); conn != "" {
			connections = append(connections, conn)
		}
	} else if conn := cfg.DefaultConnection[providerName]; conn != "" {
		connections = append(connections, conn)
	}
	if conn := cfg.DefaultConnection[providerName]; conn != "" && (len(connections) == 0 || connections[0] != conn) && cfg.CatalogConnection[providerName] == "" {
		connections = append(connections, conn)
	}
	if len(connections) == 0 {
		return []string{""}
	}
	return connections
}

func (cfg CatalogSelectorConfig) BoundSessionCatalogConnections(providerName string, p *principal.Principal, explicit, instance string) ([]string, string) {
	connections := cfg.SessionCatalogConnections(providerName, p, explicit)
	boundConnections := make([]string, 0, len(connections))
	boundInstance := instance
	for _, connection := range connections {
		boundConnection, nextInstance := cfg.WorkloadBindingSelectors(p, providerName, connection, instance)
		boundConnections = append(boundConnections, boundConnection)
		boundInstance = nextInstance
	}
	return boundConnections, boundInstance
}

func (cfg CatalogSelectorConfig) BoundSessionCatalogTargets(providerName string, p *principal.Principal, explicit, instance string) []CatalogResolutionTarget {
	connections := cfg.SessionCatalogConnections(providerName, p, explicit)
	targets := make([]CatalogResolutionTarget, 0, len(connections))
	for _, connection := range connections {
		boundConnection, boundInstance := cfg.WorkloadBindingSelectors(p, providerName, connection, instance)
		targets = append(targets, CatalogResolutionTarget{
			Connection: boundConnection,
			Instance:   boundInstance,
		})
	}
	return targets
}

func (cfg CatalogSelectorConfig) WorkloadBindingSelectors(p *principal.Principal, provider, connection, instance string) (string, string) {
	resolveConnection := func(connection string) string {
		connections := cfg.SessionCatalogConnections(provider, nil, connection)
		if len(connections) == 0 {
			return ""
		}
		return connections[0]
	}
	if !cfg.isWorkload(p) || cfg.Authorizer == nil {
		return resolveConnection(connection), instance
	}
	binding, ok := cfg.Authorizer.Binding(p, provider)
	if !ok {
		return resolveConnection(connection), instance
	}
	if connection == "" {
		connection = binding.Connection
	}
	if instance == "" {
		instance = binding.Instance
	}
	return resolveConnection(connection), instance
}

func (cfg CatalogSelectorConfig) isWorkload(p *principal.Principal) bool {
	return cfg.Authorizer != nil && cfg.Authorizer.IsWorkload(p)
}
