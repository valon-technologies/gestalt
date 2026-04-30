package providerhost

import (
	"context"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
)

type AgentExecConfig = agentservice.ExecConfig
type RemoteAgentConfig = agentservice.RemoteConfig

func NewExecutableAgent(ctx context.Context, cfg AgentExecConfig) (coreagent.Provider, error) {
	return agentservice.NewExecutable(ctx, cfg)
}

func NewRemoteAgent(ctx context.Context, cfg RemoteAgentConfig) (coreagent.Provider, error) {
	return agentservice.NewRemote(ctx, cfg)
}
