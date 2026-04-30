package providerhost

import plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"

const DefaultPluginInvokerSocketEnv = plugininvokerservice.DefaultSocketEnv

func PluginInvokerSocketTokenEnv() string {
	return plugininvokerservice.SocketTokenEnv()
}
