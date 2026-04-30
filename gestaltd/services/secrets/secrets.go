// Package secrets exposes secret manager provider transport primitives.
package secrets

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

type ExecConfig = providerhost.SecretsExecConfig

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.SecretManager, error) {
	return providerhost.NewExecutableSecretManager(ctx, cfg)
}
