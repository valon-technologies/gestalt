package bootstrap

import (
	"context"
	"fmt"
	"log"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
	"github.com/valon-technologies/gestalt/internal/registry"
)

type RuntimeDeps struct {
	Invoker          invocation.Invoker
	CapabilityLister invocation.CapabilityLister
	Egress           EgressDeps
}

type RuntimeFactory func(ctx context.Context, name string, cfg config.RuntimeDef, deps RuntimeDeps) (core.Runtime, error)

type runtimeBuilder func(context.Context, string, config.RuntimeDef, *FactoryRegistry, RuntimeDeps) (core.Runtime, error)

func buildRuntimes(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Runtime], error) {
	return buildRuntimesWith(ctx, cfg, factories, invoker, lister, audit, egressDeps, buildRuntime)
}

func buildRuntimesWith(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps, buildRuntimeFn runtimeBuilder) (*registry.PluginMap[core.Runtime], error) {
	if len(cfg.Runtimes) == 0 {
		return nil, nil
	}

	runtimes := registry.NewRuntimeMap()

	for name := range cfg.Runtimes {
		def := cfg.Runtimes[name]
		deps := runtimeDepsForProviders(name, invoker, lister, def.Providers, audit, egressDeps)
		rt, err := buildRuntimeFn(ctx, name, def, factories, deps)
		if err != nil {
			_ = StopRuntimes(context.Background(), runtimes, runtimeNames(runtimes))
			return nil, fmt.Errorf("bootstrap: runtime %q: %w", name, err)
		}

		if err := runtimes.Register(name, rt); err != nil {
			_ = rt.Stop(context.Background())
			_ = StopRuntimes(context.Background(), runtimes, runtimeNames(runtimes))
			return nil, fmt.Errorf("bootstrap: registering runtime %q: %w", name, err)
		}
		log.Printf("loaded runtime %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return runtimes, nil
}

func buildRuntime(ctx context.Context, name string, cfg config.RuntimeDef, factories *FactoryRegistry, deps RuntimeDeps) (core.Runtime, error) {
	if cfg.Plugin != nil {
		m, err := config.NodeToMap(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("decode runtime plugin config for %q: %w", name, err)
		}
		return pluginapi.NewExecutableRuntime(ctx, name, pluginapi.ExecConfig{
			Command: cfg.Plugin.Command,
			Args:    cfg.Plugin.Args,
			Env:     cfg.Plugin.Env,
			Name:    name,
			Config:  m,
			Mode:    cfg.Plugin.Mode,
		}, deps.Invoker, deps.CapabilityLister)
	}

	factory, ok := factories.Runtimes[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown runtime type %q", cfg.Type)
	}
	return factory(ctx, name, cfg, deps)
}

func runtimeNames(runtimes *registry.PluginMap[core.Runtime]) []string {
	if runtimes == nil {
		return nil
	}
	return runtimes.List()
}
