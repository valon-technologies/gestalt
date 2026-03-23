package chat

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
)

var Factory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
	var cfg bindingConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("chat binding %q: %w", name, err)
	}
	if deps.ChatStore == nil {
		return nil, fmt.Errorf("chat binding %q: chatstore is required", name)
	}
	return New(name, cfg, deps.ChatStore, deps.ChatDispatcher), nil
}
