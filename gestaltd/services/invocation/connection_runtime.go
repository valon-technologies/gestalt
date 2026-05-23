package invocation

import (
	"context"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type ConnectionRuntimeCredential struct {
	Token     string
	ExpiresAt *time.Time
}

type ConnectionRuntimeCredentialSource interface {
	ResolveConnectionCredential(context.Context) (ConnectionRuntimeCredential, error)
}

// ConnectionRuntimeInfo describes connection material that is resolved after an
// operation selects its concrete connection.
type ConnectionRuntimeInfo struct {
	ConnectionID      string
	Mode              core.ConnectionMode
	AuthType          providermanifestv1.AuthType
	AuthConfig        core.ExternalCredentialAuthConfig
	TokenSource       ConnectionRuntimeCredentialSource
	AuthMapping       *providermanifestv1.AuthMapping
	Params            map[string]string
	CredentialRefresh *providermanifestv1.CredentialRefreshConfig
}

// ConnectionRuntimeResolver resolves runtime metadata for a provider
// connection. The connection name is the internal resolved name, such as
// "_plugin" for the app connection.
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
		connection = core.AppConnectionName
	}
	info, ok := connections[connection]
	return info, ok
}
