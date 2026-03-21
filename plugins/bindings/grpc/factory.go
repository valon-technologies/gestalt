package grpc

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
)

const defaultPort = 50051

var Factory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
	var cfg grpcConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("grpc %q: %w", name, err)
	}

	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("grpc %q: port must be between 0 and 65535", name)
	}

	if deps.Invoker == nil {
		return nil, fmt.Errorf("grpc %q: missing invoker", name)
	}

	return New(name, cfg, deps.Invoker, deps.CapabilityLister, deps.ProviderLister), nil
}
