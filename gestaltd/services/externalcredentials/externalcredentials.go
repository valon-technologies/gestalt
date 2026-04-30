// Package externalcredentials exposes external credential provider transport primitives.
package externalcredentials

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultSocketEnv = providerhost.DefaultExternalCredentialSocketEnv

type ExecConfig = providerhost.ExternalCredentialsExecConfig

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.ExternalCredentialProvider, error) {
	return providerhost.NewExecutableExternalCredentialProvider(ctx, cfg)
}

func NewProviderServer(provider core.ExternalCredentialProvider) proto.ExternalCredentialProviderServer {
	return providerhost.NewExternalCredentialProviderServer(provider)
}
