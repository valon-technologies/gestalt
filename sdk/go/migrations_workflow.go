package gestalt

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

func normalizeMigrationRevisions(revisions []migrations.Revision) ([]migrations.Revision, error) {
	if len(revisions) == 0 {
		return revisions, nil
	}
	out := append([]migrations.Revision(nil), revisions...)
	for i := range out {
		if out[i].Workflow == nil {
			continue
		}
		spec, err := clientWorkflowDefinitionSpec(out[i].Workflow.Definition)
		if err != nil {
			return nil, fmt.Errorf("workflow migration %q: %w", out[i].ID, err)
		}
		out[i].Workflow.Definition = spec
	}
	return out, nil
}

func clientWorkflowDefinitionSpec(definition any) (*client.WorkflowDefinitionSpec, error) {
	if spec, ok := definition.(*client.WorkflowDefinitionSpec); ok {
		return spec, nil
	}
	if spec, ok := definition.(client.WorkflowDefinitionSpec); ok {
		return &spec, nil
	}
	resolved, err := ResolveWorkflowDefinitionSpec(definition)
	if err != nil {
		return nil, err
	}
	wire, err := workflowDefinitionSpecToProto(resolved)
	if err != nil {
		return nil, err
	}
	return client.FromWireWorkflowDefinitionSpec(wire), nil
}
