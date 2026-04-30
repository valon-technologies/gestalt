package providerhost

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	secretsservice "github.com/valon-technologies/gestalt/server/services/secrets"
)

type SecretsExecConfig = secretsservice.ExecConfig

func NewExecutableSecretManager(ctx context.Context, cfg SecretsExecConfig) (core.SecretManager, error) {
	return secretsservice.NewExecutable(ctx, cfg)
}
