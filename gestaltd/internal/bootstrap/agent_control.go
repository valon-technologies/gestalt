package bootstrap

import coreagent "github.com/valon-technologies/gestalt/server/core/agent"

// AgentControl exposes the configured agent-provider selection surface that
// higher-level manager and HTTP layers build on.
type AgentControl interface {
	ResolveProvider(name string) (coreagent.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreagent.Provider, err error)
	ProviderNames() []string
}

var _ AgentControl = (*agentRuntime)(nil)
