package agents

const (
	DefaultHostSocketEnv     = "GESTALT_AGENT_HOST_SOCKET"
	DefaultProviderSocketEnv = "GESTALT_AGENT_PROVIDER_SOCKET"
)

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ProviderSocketTokenEnv() string {
	return DefaultProviderSocketEnv + "_TOKEN"
}
