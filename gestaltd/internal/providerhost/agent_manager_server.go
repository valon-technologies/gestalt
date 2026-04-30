package providerhost

import agentservice "github.com/valon-technologies/gestalt/server/services/agents"

type AgentManagerService = agentservice.ManagerService
type AgentManagerServer = agentservice.ManagerServer

func NewAgentManagerServer(pluginName string, manager AgentManagerService, tokens *InvocationTokenManager) *AgentManagerServer {
	return agentservice.NewManagerServer(pluginName, manager, tokens)
}
