package gestalt

import (
	"context"
)

// SecretsProvider serves secret lookups for providers that need host-managed
// secret material.
type SecretsProvider interface {
	Provider
	GetSecret(ctx context.Context, name string) (string, error)
}
