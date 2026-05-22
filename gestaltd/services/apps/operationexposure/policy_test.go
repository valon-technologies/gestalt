package operationexposure

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func TestPolicyNewRejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := New(map[string]*OperationOverride{})
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
	}
}

func TestPolicyNewRejectsAliasCollisions(t *testing.T) {
	t.Parallel()

	_, err := New(map[string]*OperationOverride{
		"op_a": {Alias: "shared"},
		"op_b": {Alias: "shared"},
	})
	if err == nil {
		t.Fatal("expected alias collision")
	}
	if !strings.Contains(err.Error(), "alias collisions") {
		t.Fatalf("error = %q, want alias collisions", err)
	}
}

func TestPolicyValidateAndApply(t *testing.T) {
	t.Parallel()

	policy, err := New(map[string]*OperationOverride{
		"list_items": {Alias: "items", Description: "Custom description", AllowedRoles: []string{"admin"}},
		"get_item":   nil,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ops := []core.Operation{
		{Name: "list_items", Description: "List items"},
		{Name: "get_item", Description: "Get item"},
	}
	if err := policy.Validate(ops); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	filteredOps := policy.ApplyOperations(ops)
	if len(filteredOps) != 2 {
		t.Fatalf("got %d operations, want 2", len(filteredOps))
	}
	if filteredOps[0].Name != "items" || filteredOps[0].Description != "Custom description" {
		t.Fatalf("first operation = %+v", filteredOps[0])
	}
	if filteredOps[1].Name != "get_item" {
		t.Fatalf("second operation = %+v", filteredOps[1])
	}

	cat := &catalog.Catalog{
		Name: "example",
		Operations: []catalog.CatalogOperation{
			{ID: "list_items", Description: "List items"},
			{ID: "get_item", Description: "Get item"},
		},
	}
	filteredCat := policy.ApplyCatalog(cat)
	if len(filteredCat.Operations) != 2 {
		t.Fatalf("got %d catalog operations, want 2", len(filteredCat.Operations))
	}
	if filteredCat.Operations[0].ID != "items" || filteredCat.Operations[0].Description != "Custom description" {
		t.Fatalf("first catalog operation = %+v", filteredCat.Operations[0])
	}
	if got := filteredCat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("first catalog AllowedRoles = %#v, want [admin]", got)
	}
}
