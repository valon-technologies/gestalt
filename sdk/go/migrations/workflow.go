package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/encoding/protojson"
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
	definition := revision.Workflow.Definition
	if definition == nil {
		return fmt.Errorf("workflow migration %q requires definition spec", revision.ID)
	}
	return workflowClient.ApplyWorkflowMigration(ctx, revision.ID, provider, "", definition)
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

func (c *hostWorkflowMigrationClient) ApplyWorkflowMigration(ctx context.Context, revisionID, provider, idempotencyKey string, definition map[string]any) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("migrations host service client is not configured")
	}
	spec, err := workflowDefinitionSpecFromMap(definition)
	if err != nil {
		return err
	}
	_, err = c.client.ApplyWorkflowMigration(ctx, &client.ApplyWorkflowMigrationRequest{
		Provider:       strings.TrimSpace(provider),
		RevisionId:     strings.TrimSpace(revisionID),
		Spec:           spec,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
	return err
}

func workflowDefinitionSpecFromMap(raw map[string]any) (*client.WorkflowDefinitionSpec, error) {
	wire, err := workflowDefinitionSpecProtoFromMap(raw)
	if err != nil {
		return nil, err
	}
	return client.FromWireWorkflowDefinitionSpec(wire), nil
}

func workflowDefinitionSpecProtoFromMap(raw map[string]any) (*proto.WorkflowDefinitionSpec, error) {
	if raw == nil {
		return nil, fmt.Errorf("workflow definition spec is required")
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode workflow definition spec: %w", err)
	}
	spec := &proto.WorkflowDefinitionSpec{}
	if err := protojson.Unmarshal(payload, spec); err != nil {
		return nil, fmt.Errorf("decode workflow definition spec: %w", err)
	}
	return spec, nil
}
