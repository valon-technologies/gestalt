package providerhost

import (
	"github.com/valon-technologies/gestalt/server/internal/config"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
)

type invocationGrants = plugininvokerservice.InvocationGrants
type InvocationGrant = plugininvokerservice.InvocationGrant
type InvocationGrants = plugininvokerservice.InvocationGrants

func InvocationDependencyGrants(deps []config.PluginInvocationDependency) InvocationGrants {
	return plugininvokerservice.InvocationDependencyGrants(deps)
}
