package gestalt

import proto "github.com/valon-technologies/gestalt/internal/gen/v1"

// AgentProvider is implemented by providers that serve the agent base
// primitive over gRPC.
type AgentProvider interface {
	Provider
	proto.AgentProviderServer
}
