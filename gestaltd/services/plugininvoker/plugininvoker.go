// Package plugininvoker exposes plugin invocation host-service primitives.
package plugininvoker

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const DefaultSocketEnv = providerhost.DefaultPluginInvokerSocketEnv

type InvocationGrant = providerhost.InvocationGrant
type InvocationGrants = providerhost.InvocationGrants
type InvocationTokenManager = providerhost.InvocationTokenManager

func SocketTokenEnv() string {
	return providerhost.PluginInvokerSocketTokenEnv()
}

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return providerhost.NewInvocationTokenManager(secret)
}

func InvocationDependencyGrants(deps []config.PluginInvocationDependency) InvocationGrants {
	return providerhost.InvocationDependencyGrants(deps)
}

func InvocationTokenFromContext(ctx context.Context) string {
	return providerhost.InvocationTokenFromContext(ctx)
}

func NewServer(pluginName string, deps []config.PluginInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) proto.PluginInvokerServer {
	return providerhost.NewPluginInvokerServer(pluginName, deps, invoker, tokens)
}
