package gestalt

import proto "github.com/valon-technologies/gestalt/internal/gen/v1"

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive over gRPC.
type WorkflowProvider interface {
	Provider
	proto.WorkflowProviderServer
}
