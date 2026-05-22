package workflows

const DefaultProviderSocketEnv = "GESTALT_WORKFLOW_PROVIDER_SOCKET"

func ProviderSocketTokenEnv() string {
	return DefaultProviderSocketEnv + "_TOKEN"
}
