package mcp

import "github.com/valon-technologies/gestalt/server/core/catalog"

func connectionForCatalogTransport(cfg Config, providerName, transport string) string {
	if transport == catalog.TransportMCPPassthrough {
		return cfg.MCPConnection[providerName]
	}
	return cfg.APIConnection[providerName]
}
