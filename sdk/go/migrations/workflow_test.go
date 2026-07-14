package migrations_test

import (
	"context"
	"errors"
	"testing"

	sdkclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

type fakeWorkflowClient struct {
	calls int
	err   error
}

func (f *fakeWorkflowClient) ApplyWorkflowMigration(context.Context, string, string, string, *sdkclient.WorkflowDefinitionSpec) error {
	f.calls++
	return f.err
}

func TestWorkflowRevisionAppliesBeforeLedger(t *testing.T) {
	db := newFakeDB()
	workflowClient := &fakeWorkflowClient{}
	_, err := migrations.Run(context.Background(), db, migrations.RunOptions{
		AppName: "dealHub",
		Revisions: []migrations.Revision{{
			ID: "0001_workflow",
			Workflow: &migrations.WorkflowMigration{
				Provider: "temporal",
				Definition: &sdkclient.WorkflowDefinitionSpec{
					Id:    "extract_row",
					RunAs: "service_account:runner",
				},
			},
		}},
		WorkflowClient: workflowClient,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if workflowClient.calls != 1 {
		t.Fatalf("workflow client calls = %d, want 1", workflowClient.calls)
	}
	keys, err := db.ObjectStore("_gestalt_migrations").GetAllKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("ledger keys: %v", err)
	}
	if len(keys) != 1 || keys[0] != "0001_workflow" {
		t.Fatalf("ledger keys = %v, want [0001_workflow]", keys)
	}
}

func TestWorkflowRevisionFailureLeavesLedgerUnchanged(t *testing.T) {
	db := newFakeDB()
	workflowClient := &fakeWorkflowClient{err: errors.New("apply failed")}
	_, err := migrations.Run(context.Background(), db, migrations.RunOptions{
		AppName: "dealHub",
		Revisions: []migrations.Revision{{
			ID: "0001_workflow",
			Workflow: &migrations.WorkflowMigration{
				Provider: "temporal",
				Definition: &sdkclient.WorkflowDefinitionSpec{
					Id: "extract_row",
				},
			},
		}},
		WorkflowClient: workflowClient,
	})
	if err == nil {
		t.Fatal("expected migration failure")
	}
	keys, err := db.ObjectStore("_gestalt_migrations").GetAllKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("ledger keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("ledger keys = %v, want none", keys)
	}
}

func TestValidateRevisionsRejectsMultipleKinds(t *testing.T) {
	_, err := migrations.Run(context.Background(), newFakeDB(), migrations.RunOptions{
		Revisions: []migrations.Revision{{
			ID:     "0001",
			Schema: &migrations.SchemaDeclaration{},
			Workflow: &migrations.WorkflowMigration{
				Provider: "temporal",
			},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// reuse fake db helpers from migrations_test.go in same package
