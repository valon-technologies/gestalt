package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive over gRPC.
type WorkflowProvider interface {
	Provider
	proto.WorkflowProviderServer
}
