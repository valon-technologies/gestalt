package echo

import (
	"context"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
)

var Factory bootstrap.RuntimeFactory = func(_ context.Context, name string, _ config.RuntimeDef, broker core.Broker) (core.Runtime, error) {
	return New(name, broker), nil
}
