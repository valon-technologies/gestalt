package gestalt

import (
	"context"
)

// SecretsProvider serves secret lookups for providers that need host-managed
// secret material.
type SecretsProvider interface {
	PluginProvider
	GetSecret(ctx context.Context, name string) (string, error)
}
