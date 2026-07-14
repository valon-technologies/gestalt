package client

import (
	"context"
	"fmt"
	"strings"
)

// WorkflowDefinitionSpecSource is implemented by handwritten workflow
// builders that can lower themselves to the generated client spec.
type WorkflowDefinitionSpecSource interface {
	ToWorkflowDefinitionSpec() (*WorkflowDefinitionSpec, error)
}

// ApplyDefinitionFrom applies a generated workflow definition spec produced by
// a handwritten source such as the root package's WorkflowBuilder.
func (c *Workflow) ApplyDefinitionFrom(ctx context.Context, provider, idempotencyKey string, source WorkflowDefinitionSpecSource) (*WorkflowDefinition, error) {
	spec, err := resolveWorkflowDefinitionSpecSource(source)
	if err != nil {
		return nil, err
	}
	return c.ApplyDefinition(ctx, provider, idempotencyKey, spec)
}

func resolveWorkflowDefinitionSpecSource(source WorkflowDefinitionSpecSource) (*WorkflowDefinitionSpec, error) {
	if source == nil {
		return nil, fmt.Errorf("workflow definition spec source is nil")
	}
	spec, err := source.ToWorkflowDefinitionSpec()
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, fmt.Errorf("workflow definition spec is nil")
	}
	if strings.TrimSpace(spec.Id) == "" {
		return nil, fmt.Errorf("workflow definition requires ID")
	}
	if strings.TrimSpace(spec.RunAs) == "" {
		return nil, fmt.Errorf("workflow definition requires RunAs")
	}
	return spec, nil
}
