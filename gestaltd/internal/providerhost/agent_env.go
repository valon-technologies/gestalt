package providerhost

import agentservice "github.com/valon-technologies/gestalt/server/services/agents"

const (
	DefaultAgentHostSocketEnv    = agentservice.DefaultHostSocketEnv
	DefaultAgentManagerSocketEnv = agentservice.DefaultManagerSocketEnv
)

func AgentManagerSocketTokenEnv() string {
	return agentservice.ManagerSocketTokenEnv()
}
