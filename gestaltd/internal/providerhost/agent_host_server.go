package providerhost

import agentservice "github.com/valon-technologies/gestalt/server/services/agents"

type agentExecuteToolFunc = agentservice.ExecuteToolFunc
type agentSearchToolsFunc = agentservice.SearchToolsFunc
type AgentHostServer = agentservice.HostServer

func NewAgentHostServer(providerName string, searchTools agentSearchToolsFunc, executeTool agentExecuteToolFunc) *AgentHostServer {
	return agentservice.NewHostServer(providerName, searchTools, executeTool)
}
