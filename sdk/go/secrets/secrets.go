package secrets

import "context"

// Secrets resolves secret values by name.
type Secrets interface {
	GetSecret(ctx context.Context, name string) (string, error)
}
