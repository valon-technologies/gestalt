package providerutil

import (
	"io"

	"github.com/valon-technologies/gestalt/server/core"
)

// Close closes a provider when it implements io.Closer.
func Close(provider any) error {
	if closer, ok := provider.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ResolveConnection uses a provider's dynamic resolver when available.
func ResolveConnection(provider core.Provider, operation string, params map[string]any) (string, error) {
	if resolver, ok := provider.(core.OperationConnectionResolver); ok {
		return resolver.ResolveConnectionForOperation(operation, params)
	}
	return provider.ConnectionForOperation(operation), nil
}

// ConnectionOverrideAllowed reports the provider's optional override policy.
func ConnectionOverrideAllowed(provider core.Provider, operation string, params map[string]any) bool {
	if policy, ok := provider.(core.OperationConnectionOverridePolicy); ok {
		return policy.OperationConnectionOverrideAllowed(operation, params)
	}
	return false
}
