package secrets

import "context"

// Client resolves secret values by name.
type Client interface {
	GetSecret(ctx context.Context, name string) (string, error)
}
