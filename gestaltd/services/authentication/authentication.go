// Package authentication exposes authentication provider transport primitives.
package authentication

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

type ExecConfig = providerhost.AuthenticationExecConfig

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.AuthenticationProvider, error) {
	return providerhost.NewExecutableAuthenticationProvider(ctx, cfg)
}
