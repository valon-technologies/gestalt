package invocation

import "github.com/valon-technologies/gestalt/server/core"

type CatalogSelectorConfig struct {
	Invoker           any
	CatalogConnection map[string]string
	DefaultConnection map[string]string
}

func (cfg CatalogSelectorConfig) SessionCatalogConnections(providerName string, explicit string) []string {
	if explicit != "" {
		return []string{core.ResolveConnectionAlias(explicit)}
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

func (cfg CatalogSelectorConfig) SessionCatalogTargets(providerName string, explicit, instance string) []CatalogResolutionTarget {
	connections := cfg.SessionCatalogConnections(providerName, explicit)
	targets := make([]CatalogResolutionTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, CatalogResolutionTarget{
			Connection: connection,
			Instance:   instance,
		})
	}
	return targets
}
