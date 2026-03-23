package agentic

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
)

var RuntimeFactory bootstrap.RuntimeFactory = func(_ context.Context, name string, def config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
	var cfg runtimeConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.PythonCommand == "" {
		cfg.PythonCommand = "python3"
	}
	if cfg.MaxSandboxes == 0 {
		cfg.MaxSandboxes = 5
	}
	if cfg.GRPCPort == 0 {
		cfg.GRPCPort = 50051
	}
	if cfg.StorePath == "" {
		cfg.StorePath = "./chat.db"
	}
	return NewRuntime(name, cfg, deps)
}

var BindingFactory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
	var cfg bindingConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Runtime == "" {
		return nil, fmt.Errorf("agentic binding %q: runtime is required", name)
	}
	if deps.Runtimes == nil {
		return nil, fmt.Errorf("agentic binding %q: no runtimes available", name)
	}
	rt, err := deps.Runtimes.Get(cfg.Runtime)
	if err != nil {
		return nil, fmt.Errorf("agentic binding %q: runtime %q: %w", name, cfg.Runtime, err)
	}
	dispatcher, ok := rt.(Dispatcher)
	if !ok {
		return nil, fmt.Errorf("agentic binding %q: runtime %q does not implement Dispatcher", name, cfg.Runtime)
	}
	sp, ok := rt.(StoreProvider)
	if !ok {
		return nil, fmt.Errorf("agentic binding %q: runtime %q does not implement StoreProvider", name, cfg.Runtime)
	}
	return NewBinding(name, sp.Store(), dispatcher), nil
}
