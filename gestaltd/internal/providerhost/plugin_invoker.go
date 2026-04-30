package providerhost

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
)

type PluginInvokerServer = plugininvokerservice.PluginInvokerServer

func NewPluginInvokerServer(pluginName string, deps []config.PluginInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) *PluginInvokerServer {
	return plugininvokerservice.NewPluginInvokerServer(pluginName, deps, invoker, tokens)
}

var _ proto.PluginInvokerServer = (*PluginInvokerServer)(nil)
