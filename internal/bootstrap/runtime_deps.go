package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
)

func runtimeDepsForProviders(name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink) RuntimeDeps {
	guarded := guardedInvoker("runtime", name, invoker, lister, providers, audit)
	return RuntimeDeps{
		Invoker:          guarded,
		CapabilityLister: guarded,
	}
}

func bindingDepsForProviders(name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink) BindingDeps {
	guarded := guardedInvoker("binding", name, invoker, lister, providers, audit)
	return BindingDeps{
		Invoker:          guarded,
		CapabilityLister: guarded,
		ProviderLister:   guarded,
	}
}

func guardedInvoker(kind, name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink) *invocation.GuardedInvoker {
	if invoker == nil {
		return nil
	}

	source := fmt.Sprintf("%s:%s", kind, name)
	var opts []invocation.GuardedOption
	if len(providers) > 0 {
		opts = append(opts, invocation.WithAllowedProviders(providers))
	}
	if pl, ok := lister.(invocation.ProviderLister); ok {
		opts = append(opts, invocation.WithProviderLister(pl))
	}
	return invocation.NewGuarded(invoker, lister, source, audit, opts...)
}
