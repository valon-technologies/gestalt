package bootstrap

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

// ExtensionBoundary owns runtime and binding assembly once provider
// construction has produced the shared invoker/capability surface.
type ExtensionBoundary struct {
	Runtimes *registry.PluginMap[core.Runtime]
	Bindings *registry.PluginMap[core.Binding]
}

func buildExtensions(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*ExtensionBoundary, error) {
	return buildExtensionsWith(ctx, cfg, factories, invoker, lister, audit, egressDeps, buildRuntime)
}

func buildExtensionsWith(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps, buildRuntimeFn runtimeBuilder) (*ExtensionBoundary, error) {
	runtimes, err := buildRuntimesWith(ctx, cfg, factories, invoker, lister, audit, egressDeps, buildRuntimeFn)
	if err != nil {
		return nil, err
	}

	bindings, err := buildBindings(ctx, cfg, factories, invoker, lister, audit, egressDeps)
	if err != nil {
		_ = StopRuntimes(context.Background(), runtimes, runtimeNames(runtimes))
		return nil, err
	}

	return &ExtensionBoundary{
		Runtimes: runtimes,
		Bindings: bindings,
	}, nil
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

func (b *ExtensionBoundary) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}
	return errors.Join(
		CloseBindings(b.Bindings, bindingNames(b.Bindings)),
		StopRuntimes(ctx, b.Runtimes, runtimeNames(b.Runtimes)),
	)
}
