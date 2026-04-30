package providerhost

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	authenticationservice "github.com/valon-technologies/gestalt/server/services/authentication"
)

type AuthenticationExecConfig = authenticationservice.ExecConfig

func NewExecutableAuthenticationProvider(ctx context.Context, cfg AuthenticationExecConfig) (core.AuthenticationProvider, error) {
	return authenticationservice.NewExecutable(ctx, cfg)
}
