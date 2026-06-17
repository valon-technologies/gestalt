package invocation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type CatalogSelectorConfig struct {
	Invoker any
	// CatalogConnection selects the API/OAuth surface for REST-visible catalog listing.
	CatalogConnection map[string]string
	// MCPConnection selects the MCP surface for dynamic session operation resolution.
	MCPConnection map[string]string
	// DefaultConnection is the compatibility fallback when no surface-specific map is set.
	DefaultConnection map[string]string
}

// APICatalogConnections returns connection candidates for API catalog listing.
func (cfg CatalogSelectorConfig) APICatalogConnections(providerName string, explicit string) []string {
	if explicit != "" {
		return []string{core.ResolveConnectionAlias(explicit)}
	}

	connections := make([]string, 0, 2)
	if conn := cfg.CatalogConnection[providerName]; conn != "" {
		connections = append(connections, conn)
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

// SessionCatalogConnections returns connection candidates for dynamic MCP/session
// operation resolution. When an MCP surface connection is configured, OAuth/API
// connections are not used as fallbacks.
func (cfg CatalogSelectorConfig) SessionCatalogConnections(providerName string, explicit string) []string {
	if explicit != "" {
		return []string{core.ResolveConnectionAlias(explicit)}
	}

	if conn := cfg.MCPConnection[providerName]; conn != "" {
		return []string{conn}
	}
	if broker, ok := cfg.Invoker.(interface{ MCPConnection(string) string }); ok {
		if conn := broker.MCPConnection(providerName); conn != "" {
			return []string{conn}
		}
	}

	if conn := cfg.DefaultConnection[providerName]; conn != "" {
		return []string{conn}
	}
	return []string{""}
}

func (cfg CatalogSelectorConfig) APICatalogTargets(providerName string, explicit, instance string) []CatalogResolutionTarget {
	connections := cfg.APICatalogConnections(providerName, explicit)
	targets := make([]CatalogResolutionTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, CatalogResolutionTarget{
			Connection: connection,
			Instance:   instance,
			Surface:    core.CatalogSurfaceAPI,
		})
	}
	return targets
}

func (cfg CatalogSelectorConfig) SessionCatalogTargets(providerName string, explicit, instance string) []CatalogResolutionTarget {
	connections := cfg.SessionCatalogConnections(providerName, explicit)
	targets := make([]CatalogResolutionTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, CatalogResolutionTarget{
			Connection: connection,
			Instance:   instance,
			Surface:    core.CatalogSurfaceMCP,
		})
	}
	return targets
}

// HTTPListCatalogTargets returns catalog resolution targets for HTTP operation listing.
// Default listing uses the API/OAuth surface. An explicit MCP connection selects the MCP surface.
func (cfg CatalogSelectorConfig) HTTPListCatalogTargets(providerName string, explicit, instance string) []CatalogResolutionTarget {
	if explicit != "" {
		resolved := core.ResolveConnectionAlias(explicit)
		if mcpConn := cfg.MCPConnection[providerName]; mcpConn != "" && resolved == mcpConn {
			return cfg.SessionCatalogTargets(providerName, explicit, instance)
		}
		if broker, ok := cfg.Invoker.(interface{ MCPConnection(string) string }); ok {
			if mcpConn := broker.MCPConnection(providerName); mcpConn != "" && resolved == mcpConn {
				return cfg.SessionCatalogTargets(providerName, explicit, instance)
			}
		}
	}
	return cfg.APICatalogTargets(providerName, explicit, instance)
}

// ClassifySessionCatalogError maps known MCP/session catalog auth failures to
// client-actionable credential errors instead of generic upstream failures.
func ClassifySessionCatalogError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNoCredential) || errors.Is(err, ErrReconnectRequired) {
		return err
	}
	if sessionCatalogAuthFailure(err) {
		return fmt.Errorf("%w: %v", ErrReconnectRequired, err)
	}
	return err
}

func sessionCatalogAuthFailure(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "(401)") ||
		strings.Contains(msg, " 401 ") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "(403)") ||
		strings.Contains(msg, " 403 ") ||
		strings.Contains(msg, "authorization required")
}
