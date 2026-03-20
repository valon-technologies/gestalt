package echo

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
)

var Factory bootstrap.RuntimeFactory = func(_ context.Context, name string, _ config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
	return New(name, deps), nil
}
