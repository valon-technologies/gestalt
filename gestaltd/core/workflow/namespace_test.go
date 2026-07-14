package workflow_test

import (
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func TestAppManagedDefinitionID(t *testing.T) {
	t.Parallel()
	got := coreworkflow.AppManagedDefinitionID("dealHub", "extract_row")
	want := "app_dealHub_extract_row"
	if got != want {
		t.Fatalf("AppManagedDefinitionID() = %q, want %q", got, want)
	}
}

func TestValidateAppManagedDefinitionID(t *testing.T) {
	t.Parallel()
	if err := coreworkflow.ValidateAppManagedDefinitionID("dealHub", "app_dealHub_extract_row"); err != nil {
		t.Fatalf("ValidateAppManagedDefinitionID: %v", err)
	}
	if err := coreworkflow.ValidateAppManagedDefinitionID("dealHub", "app_other_extract_row"); err == nil {
		t.Fatal("expected namespace mismatch error")
	}
}
