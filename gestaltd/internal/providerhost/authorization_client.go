package providerhost

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
)

type AuthorizationExecConfig = authorizationservice.ExecConfig

func NewExecutableAuthorizationProvider(ctx context.Context, cfg AuthorizationExecConfig) (core.AuthorizationProvider, error) {
	return authorizationservice.NewExecutable(ctx, cfg)
}
