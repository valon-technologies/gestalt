package gestalt

import (
	"context"
)

type SecretsProvider interface {
	RuntimeProvider
	GetSecret(ctx context.Context, name string) (string, error)
}
