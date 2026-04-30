package invocation

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

const runtimePluginConnectionName = "_plugin"

// ConnectionRuntimeInfo describes deployment-owned connection material that is
// resolved after an operation selects its concrete connection.
type ConnectionRuntimeInfo struct {
	Mode     core.ConnectionMode
	Exposure core.ConnectionExposure
	Token    string
}

// ConnectionRuntimeResolver resolves runtime metadata for a provider
// connection. The connection name is the internal resolved name, such as
// "_plugin" for the plugin connection.
type ConnectionRuntimeResolver func(provider, connection string) (ConnectionRuntimeInfo, bool)

// ConnectionRuntimeMap is a small in-memory resolver built from deployment
// config during bootstrap.
type ConnectionRuntimeMap map[string]map[string]ConnectionRuntimeInfo

func (m ConnectionRuntimeMap) Resolve(provider, connection string) (ConnectionRuntimeInfo, bool) {
	if len(m) == 0 {
		return ConnectionRuntimeInfo{}, false
	}
	connections := m[strings.TrimSpace(provider)]
	if len(connections) == 0 {
		return ConnectionRuntimeInfo{}, false
	}
	connection = strings.TrimSpace(connection)
	if connection == "" {
		connection = runtimePluginConnectionName
	}
	info, ok := connections[connection]
	return info, ok
}
