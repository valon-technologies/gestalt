package gestalt

import (
	"context"
)

type SecretsProvider interface {
	PluginProvider
	GetSecret(ctx context.Context, name string) (string, error)
}
