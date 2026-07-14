package workflow_test

import (
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

func TestAppManagedDefinitionID(t *testing.T) {
	t.Parallel()
	got := coreworkflow.AppManagedDefinitionID("dealHub", "extract_row")
	appName, localID, err := coreworkflow.ParseAppManagedDefinitionID(got)
	if err != nil {
		t.Fatalf("ParseAppManagedDefinitionID: %v", err)
	}
	if appName != "dealHub" || localID != "extract_row" {
		t.Fatalf("round trip = (%q, %q), want (dealHub, extract_row)", appName, localID)
	}
}

func TestAppManagedDefinitionIDCollisionSafe(t *testing.T) {
	t.Parallel()
	left := coreworkflow.AppManagedDefinitionID("foo_bar", "baz")
	right := coreworkflow.AppManagedDefinitionID("foo", "bar_baz")
	if left == right {
		t.Fatalf("distinct app/local pairs produced the same stored id: %q", left)
	}
}

func TestValidateLocalDefinitionID(t *testing.T) {
	t.Parallel()
	if err := coreworkflow.ValidateLocalDefinitionID("extract_row"); err != nil {
		t.Fatalf("ValidateLocalDefinitionID: %v", err)
	}
	if err := coreworkflow.ValidateLocalDefinitionID("app_dealHub_extract_row"); err == nil {
		t.Fatal("expected reserved prefix rejection")
	}
}

func TestParseAppManagedDefinitionID(t *testing.T) {
	t.Parallel()
	stored := coreworkflow.AppManagedDefinitionID("foo_bar", "baz")
	appName, localID, err := coreworkflow.ParseAppManagedDefinitionID(stored)
	if err != nil {
		t.Fatalf("ParseAppManagedDefinitionID: %v", err)
	}
	if appName != "foo_bar" || localID != "baz" {
		t.Fatalf("parsed = (%q, %q), want (foo_bar, baz)", appName, localID)
	}
}
