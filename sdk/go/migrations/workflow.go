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
	workflowClient := opts.WorkflowClient
	if workflowClient == nil {
		var err error
		workflowClient, err = defaultWorkflowMigrationClient(ctx)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(opts.AppName) == "" {
		return fmt.Errorf("workflow migration requires app name")
	}
	provider := strings.TrimSpace(revision.Workflow.Provider)
	spec, err := workflowDefinitionSpecForMigration(revision.Workflow.Definition)
	if err != nil {
		return fmt.Errorf("workflow migration %q: %w", revision.ID, err)
	}
	return workflowClient.ApplyWorkflowMigration(ctx, revision.ID, provider, "", spec)
}

func defaultWorkflowMigrationClient(ctx context.Context) (WorkflowMigrationClient, error) {
	migrationClient, err := client.ConnectMigration(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("connect migrations host service: %w", err)
	}
	return &hostWorkflowMigrationClient{client: migrationClient}, nil
}

type hostWorkflowMigrationClient struct {
	client *client.Migration
}

func (c *hostWorkflowMigrationClient) ApplyWorkflowMigration(ctx context.Context, revisionID, provider, idempotencyKey string, spec *client.WorkflowDefinitionSpec) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("migrations host service client is not configured")
	}
	if spec == nil {
		return fmt.Errorf("workflow definition spec is required")
	}
	_, err := c.client.ApplyWorkflowMigration(ctx, &client.ApplyWorkflowMigrationRequest{
		Provider:       strings.TrimSpace(provider),
		RevisionId:     strings.TrimSpace(revisionID),
		Spec:           spec,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
	return err
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
