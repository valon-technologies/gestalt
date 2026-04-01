package bootstrap

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

func buildExtensions(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Runtime], *registry.PluginMap[core.Binding], error) {
	return buildExtensionsWith(ctx, cfg, factories, invoker, lister, audit, egressDeps, buildRuntime)
}

func buildExtensionsWith(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps, buildRuntimeFn runtimeBuilder) (*registry.PluginMap[core.Runtime], *registry.PluginMap[core.Binding], error) {
	runtimes, err := buildRuntimesWith(ctx, cfg, factories, invoker, lister, audit, egressDeps, buildRuntimeFn)
	if err != nil {
		return nil, nil, err
	}

	bindings, err := buildBindings(ctx, cfg, factories, invoker, lister, audit, egressDeps)
	if err != nil {
		_ = StopRuntimes(context.Background(), runtimes, runtimeNames(runtimes))
		return nil, nil, err
	}

	return runtimes, bindings, nil
}

func newGuardedInvoker(kind, name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink) *invocation.GuardedInvoker {
	if invoker == nil {
		return nil
	}

	var opts []invocation.GuardedOption
	if len(providers) > 0 {
		opts = append(opts, invocation.WithAllowedProviders(providers))
	}
	return invocation.NewGuarded(invoker, lister, kind+":"+name, audit, opts...)
}

func shutdownExtensions(ctx context.Context, runtimes *registry.PluginMap[core.Runtime], bindings *registry.PluginMap[core.Binding]) error {
	return errors.Join(
		CloseBindings(bindings, bindingNames(bindings)),
		StopRuntimes(ctx, runtimes, runtimeNames(runtimes)),
	)
}
