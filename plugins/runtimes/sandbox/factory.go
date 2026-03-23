package sandbox

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
)

var Factory bootstrap.RuntimeFactory = func(_ context.Context, name string, def config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
	var cfg runtimeConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("sandbox runtime %q: %w", name, err)
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
	if cfg.IdleTimeout == "" {
		cfg.IdleTimeout = "5m"
	}
	if cfg.SandboxScript == "" {
		return nil, fmt.Errorf("sandbox runtime %q: sandbox_script is required", name)
	}
	return New(name, cfg, deps), nil
}
