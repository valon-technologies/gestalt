// Package authorization exposes authorization provider transport primitives.
package authorization

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultSocketEnv = providerhost.DefaultAuthorizationSocketEnv

type ExecConfig = providerhost.AuthorizationExecConfig

func SocketTokenEnv() string {
	return providerhost.AuthorizationSocketTokenEnv()
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthorizationProvider, error) {
	return providerhost.NewExecutableAuthorizationProvider(ctx, cfg)
}

func NewProviderServer(provider core.AuthorizationProvider) proto.AuthorizationProviderServer {
	return providerhost.NewAuthorizationProviderServer(provider)
}
