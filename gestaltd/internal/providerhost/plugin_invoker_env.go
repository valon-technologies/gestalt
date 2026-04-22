package providerhost

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

const (
	DefaultPluginInvokerSocketEnv = proto.EnvPluginInvokerSocket
	defaultPluginInvokerTokenEnv  = DefaultPluginInvokerSocketEnv + "_TOKEN"
)

func PluginInvokerSocketTokenEnv() string {
	return defaultPluginInvokerTokenEnv
}
