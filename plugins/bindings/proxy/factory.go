package proxy

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/egress"
)

var Factory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
	var cfg proxyConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(name); err != nil {
		return nil, err
	}
	resolver := egress.Resolver{}
	if deps.Egress.Resolver != nil {
		resolver = *deps.Egress.Resolver
	}
	return New(name, cfg, resolver), nil
}
