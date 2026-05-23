package invocation

import "github.com/valon-technologies/gestalt/server/core"

// AppInvocationDependency describes one plugin-to-plugin invocation grant.
// It is the service-level form of the config "invokes" entry.
type AppInvocationDependency struct {
	App                   string
	Operation             string
	Surface               string
	CredentialMode        core.ConnectionMode
	RunAs                 *core.RunAsSubject
	RunAsExternalIdentity *core.ExternalIdentityRef
	RunAsExplicitOnly     bool
}

func CloneAppInvocationDependencies(src []AppInvocationDependency) []AppInvocationDependency {
	if len(src) == 0 {
		return nil
	}
	out := make([]AppInvocationDependency, len(src))
	for i := range src {
		out[i] = src[i]
		if src[i].RunAs != nil {
			runAs := *src[i].RunAs
			out[i].RunAs = &runAs
		}
		if src[i].RunAsExternalIdentity != nil {
			identity := *src[i].RunAsExternalIdentity
			out[i].RunAsExternalIdentity = &identity
		}
	}
	return out
}

func CloneAppInvocationDependencyMap(src map[string][]AppInvocationDependency) map[string][]AppInvocationDependency {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]AppInvocationDependency, len(src))
	for pluginName, deps := range src {
		out[pluginName] = CloneAppInvocationDependencies(deps)
	}
	return out
}
