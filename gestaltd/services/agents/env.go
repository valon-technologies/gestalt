package agents

const DefaultProviderSocketEnv = "GESTALT_AGENT_PROVIDER_SOCKET"

func ProviderSocketTokenEnv() string {
	return DefaultProviderSocketEnv + "_TOKEN"
}
