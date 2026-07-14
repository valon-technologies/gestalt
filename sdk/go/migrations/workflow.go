package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

func applyWorkflowRevision(ctx context.Context, opts RunOptions, revision Revision) error {
	if revision.Workflow == nil {
		return fmt.Errorf("workflow revision %q is missing workflow payload", revision.ID)
	}
	if strings.TrimSpace(opts.AppName) == "" {
		return fmt.Errorf("workflow migration requires app name")
	}
	provider := strings.TrimSpace(revision.Workflow.Provider)
	spec, err := workflowDefinitionSpecForMigration(revision.Workflow.Definition)
	if err != nil {
		return fmt.Errorf("workflow migration %q: %w", revision.ID, err)
	}
	workflowClient, err := client.ConnectWorkflow(ctx, "")
	if err != nil {
		return fmt.Errorf("connect workflow host service: %w", err)
	}
	idempotencyKey := WorkflowMigrationIdempotencyKey(revision.ID, provider, spec.Id)
	_, err = workflowClient.ApplyDefinition(ctx, provider, idempotencyKey, spec)
	return err
}

// WorkflowMigrationIdempotencyKey returns the stable idempotency key for a workflow revision.
func WorkflowMigrationIdempotencyKey(revisionID, provider, localDefinitionID string) string {
	parts := []string{
		"workflow-migration",
		strings.TrimSpace(revisionID),
		strings.TrimSpace(provider),
		strings.TrimSpace(localDefinitionID),
	}
	return strings.Join(parts, "/")
}

func workflowDefinitionSpecForMigration(definition any) (*client.WorkflowDefinitionSpec, error) {
	switch typed := definition.(type) {
	case *client.WorkflowDefinitionSpec:
		return typed, nil
	case client.WorkflowDefinitionSpec:
		return &typed, nil
	default:
		return nil, fmt.Errorf("unsupported workflow definition spec %T", definition)
	}
}
