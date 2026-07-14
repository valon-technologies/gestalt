package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/client"
)

var _ client.WorkflowDefinitionSpecSource = (*WorkflowBuilder)(nil)

// ToWorkflowDefinitionSpec lowers the root workflow builder to the generated
// client message without making the generated client import the root package.
func (b *WorkflowBuilder) ToWorkflowDefinitionSpec() (*client.WorkflowDefinitionSpec, error) {
	spec, err := b.ToSpec()
	if err != nil {
		return nil, err
	}
	wire, err := workflowDefinitionSpecToProto(spec)
	if err != nil {
		return nil, err
	}
	return client.FromWireWorkflowDefinitionSpec(wire), nil
}
