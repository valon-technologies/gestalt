package providerhost

const (
	DefaultAgentHostSocketEnv    = "GESTALT_AGENT_HOST_SOCKET"
	DefaultAgentManagerSocketEnv = "GESTALT_AGENT_MANAGER_SOCKET"
)

func AgentManagerSocketTokenEnv() string {
	return DefaultAgentManagerSocketEnv + "_TOKEN"
}
