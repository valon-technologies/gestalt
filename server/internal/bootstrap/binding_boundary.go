package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

type BindingDeps struct {
	Invoker invocation.Invoker
	Egress  EgressDeps
}

type BindingFactory func(ctx context.Context, name string, cfg config.BindingDef, deps BindingDeps) (core.Binding, error)

func buildBindings(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Binding], error) {
	if len(cfg.Bindings) == 0 {
		return nil, nil
	}

	bindings := registry.NewBindingMap()

	for name := range cfg.Bindings {
		def := cfg.Bindings[name]
		factory, ok := factories.Bindings[def.Type]
		if !ok {
			_ = CloseBindings(bindings, bindingNames(bindings))
			return nil, fmt.Errorf("bootstrap: unknown binding type %q for binding %q", def.Type, name)
		}

		deps := bindingDepsForProviders(name, invoker, lister, def.Providers, audit, egressDeps)
		binding, err := factory(ctx, name, def, deps)
		if err != nil {
			_ = CloseBindings(bindings, bindingNames(bindings))
			return nil, fmt.Errorf("bootstrap: binding %q: %w", name, err)
		}

		if err := bindings.Register(name, binding); err != nil {
			_ = binding.Close()
			_ = CloseBindings(bindings, bindingNames(bindings))
			return nil, fmt.Errorf("bootstrap: registering binding %q: %w", name, err)
		}
		slog.Info("loaded binding", "binding", name, "type", def.Type, "providers", def.Providers)
	}

	return bindings, nil
}

func bindingNames(bindings *registry.PluginMap[core.Binding]) []string {
	if bindings == nil {
		return nil
	}
	return bindings.List()
}
