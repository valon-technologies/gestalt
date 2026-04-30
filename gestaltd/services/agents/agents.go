// Package agents exposes agent provider transport primitives.
package agents

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultHostSocketEnv = providerhost.DefaultAgentHostSocketEnv
const DefaultManagerSocketEnv = providerhost.DefaultAgentManagerSocketEnv

type ExecConfig = providerhost.AgentExecConfig
type RemoteConfig = providerhost.RemoteAgentConfig
type InvocationTokenManager = providerhost.InvocationTokenManager
type ManagerService = providerhost.AgentManagerService

func HostSocketTokenEnv() string {
	return DefaultHostSocketEnv + "_TOKEN"
}

func ManagerSocketTokenEnv() string {
	return providerhost.AgentManagerSocketTokenEnv()
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreagent.Provider, error) {
	return providerhost.NewExecutableAgent(ctx, cfg)
}

func NewRemote(ctx context.Context, cfg RemoteConfig) (coreagent.Provider, error) {
	return providerhost.NewRemoteAgent(ctx, cfg)
}

func NewHostServer(
	providerName string,
	searchTools func(context.Context, coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error),
	executeTool func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error),
) proto.AgentHostServer {
	return providerhost.NewAgentHostServer(providerName, searchTools, executeTool)
}

func NewManagerServer(pluginName string, manager ManagerService, tokens *InvocationTokenManager) proto.AgentManagerHostServer {
	return providerhost.NewAgentManagerServer(pluginName, manager, tokens)
}
