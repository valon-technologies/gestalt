package proxy

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

var Factory bootstrap.BindingFactory = func(_ context.Context, name string, def config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
	var cfg proxyConfig
	if err := def.Config.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(name); err != nil {
		return nil, err
	}
	if len(def.Providers) > 1 {
		return nil, fmt.Errorf("proxy %q: at most one provider may be configured in bindings.%s.providers", name, name)
	}
	var provider string
	if len(def.Providers) == 1 {
		provider = def.Providers[0]
	}
	resolver := egress.Resolver{}
	if deps.Egress.Resolver != nil {
		resolver = *deps.Egress.Resolver
	}
	return New(name, provider, cfg, resolver, nil), nil
}
