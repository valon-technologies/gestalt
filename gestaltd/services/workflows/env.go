package workflows

const (
	DefaultHostSocketEnv     = "GESTALT_WORKFLOW_HOST_SOCKET"
	DefaultProviderSocketEnv = "GESTALT_WORKFLOW_PROVIDER_SOCKET"
)

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ProviderSocketTokenEnv() string {
	return DefaultProviderSocketEnv + "_TOKEN"
}
