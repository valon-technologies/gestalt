package invocationconfig

import (
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func PluginInvocationDependencies(deps []config.PluginInvocationDependency) []invocation.PluginInvocationDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]invocation.PluginInvocationDependency, 0, len(deps))
	for _, dep := range deps {
		out = append(out, invocation.PluginInvocationDependency{
			Plugin:                dep.Plugin,
			Operation:             dep.Operation,
			Surface:               dep.Surface,
			CredentialMode:        core.ConnectionMode(dep.CredentialMode),
			RunAs:                 dep.RunAsSubject(),
			RunAsExternalIdentity: dep.RunAsExternalIdentity(),
		})
	}
	return out
}
