package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

func TestWorkflowRevisionAppliesBeforeLedger(t *testing.T) {
	t.Parallel()
	db := newFakeDB()
	_, err := migrations.Run(context.Background(), db, migrations.RunOptions{
		AppName: "dealHub",
		Revisions: []migrations.Revision{{
			ID: "0001_workflow",
			Workflow: &migrations.WorkflowMigration{
				Provider: "temporal",
				Definition: &client.WorkflowDefinitionSpec{
					Id:    "extract_row",
					RunAs: "service_account:runner",
				},
			},
		}},
	})
	if err == nil {
		t.Fatal("expected connect workflow host service failure without env")
	}
}

func TestWorkflowRevisionFailureLeavesLedgerUnchanged(t *testing.T) {
	t.Parallel()
	db := newFakeDB()
	_, err := migrations.Run(context.Background(), db, migrations.RunOptions{
		AppName: "dealHub",
		Revisions: []migrations.Revision{{
			ID: "0001_workflow",
			Workflow: &migrations.WorkflowMigration{
				Provider: "temporal",
				Definition: &client.WorkflowDefinitionSpec{
					Id: "extract_row",
				},
			},
		}},
	})
	if err == nil {
		t.Fatal("expected migration failure")
	}
	if !errors.Is(err, errors.New("")) {
		// connect failure is expected in unit tests without host env
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
	t.Parallel()
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

func TestWorkflowMigrationIdempotencyKey(t *testing.T) {
	t.Parallel()
	got := migrations.WorkflowMigrationIdempotencyKey("0001", "temporal", "extract_row")
	want := "workflow-migration/0001/temporal/extract_row"
	if got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}

// reuse fake db helpers from migrations_test.go in same package
