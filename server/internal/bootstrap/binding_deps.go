package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
)

func bindingDepsForProviders(name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink, egressDeps EgressDeps) BindingDeps {
	return BindingDeps{
		Invoker: newGuardedInvoker("binding", name, invoker, lister, providers, audit),
		Egress:  egressDeps,
	}
}

func newGuardedInvoker(kind, name string, invoker invocation.Invoker, lister invocation.CapabilityLister, providers []string, audit core.AuditSink) *invocation.GuardedInvoker {
	if invoker == nil {
		return nil
	}

	source := fmt.Sprintf("%s:%s", kind, name)
	var opts []invocation.GuardedOption
	if len(providers) > 0 {
		opts = append(opts, invocation.WithAllowedProviders(providers))
	}
	return invocation.NewGuarded(invoker, lister, source, audit, opts...)
}
