package appinvoker

import proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"

const (
	DefaultAppInvokerSocketEnv = proto.EnvAppInvokerSocket
	DefaultSocketEnv           = DefaultAppInvokerSocketEnv
	defaultAppInvokerTokenEnv  = DefaultAppInvokerSocketEnv + "_TOKEN"
)

func AppInvokerSocketTokenEnv() string {
	return defaultAppInvokerTokenEnv
}

func SocketTokenEnv() string {
	return AppInvokerSocketTokenEnv()
}
