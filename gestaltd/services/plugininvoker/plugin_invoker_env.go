package plugininvoker

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

const (
	DefaultPluginInvokerSocketEnv = proto.EnvPluginInvokerSocket
	DefaultSocketEnv              = DefaultPluginInvokerSocketEnv
	defaultPluginInvokerTokenEnv  = DefaultPluginInvokerSocketEnv + "_TOKEN"
)

func PluginInvokerSocketTokenEnv() string {
	return defaultPluginInvokerTokenEnv
}

func SocketTokenEnv() string {
	return PluginInvokerSocketTokenEnv()
}
