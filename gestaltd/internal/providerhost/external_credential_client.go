package providerhost

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
)

type ExternalCredentialsExecConfig = externalcredentialsservice.ExecConfig

func NewExecutableExternalCredentialProvider(ctx context.Context, cfg ExternalCredentialsExecConfig) (core.ExternalCredentialProvider, error) {
	return externalcredentialsservice.NewExecutable(ctx, cfg)
}
