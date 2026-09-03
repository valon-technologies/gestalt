package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestApplyAllowedOperationsIgnoresUnknownOperations(t *testing.T) {
	t.Parallel()

	prov := &coretesting.StubIntegration{
		N: "example",
		CatalogVal: &catalog.Catalog{
			Name: "example",
			Operations: []catalog.CatalogOperation{
				{ID: "get_item"},
			},
		},
	}

	wrapped, err := applyAllowedOperations("example", map[string]*config.OperationOverride{
		"get_item":       nil,
		"get_review_ctx": nil,
	}, prov)
	if err != nil {
		t.Fatalf("applyAllowedOperations: %v", err)
	}

	filtered := wrapped.Catalog()
	if len(filtered.Operations) != 1 || filtered.Operations[0].ID != "get_item" {
		t.Fatalf("catalog operations = %#v, want only get_item", filtered.Operations)
	}
}

func TestApplyAllowedOperationsSkipsRestrictionWhenNoMatches(t *testing.T) {
	t.Parallel()

	prov := &coretesting.StubIntegration{
		N: "example",
		CatalogVal: &catalog.Catalog{
			Name: "example",
			Operations: []catalog.CatalogOperation{
				{ID: "get_item"},
			},
		},
	}

	wrapped, err := applyAllowedOperations("example", map[string]*config.OperationOverride{
		"get_review_ctx": nil,
	}, prov)
	if err != nil {
		t.Fatalf("applyAllowedOperations: %v", err)
	}
	if wrapped != prov {
		t.Fatal("expected provider to remain unwrapped when no allowed operations match")
	}

	filtered := wrapped.Catalog()
	if len(filtered.Operations) != 1 || filtered.Operations[0].ID != "get_item" {
		t.Fatalf("catalog operations = %#v, want unrestricted get_item", filtered.Operations)
	}
}

func TestApplyAllowedOperationsWrapsEmptyAllowlist(t *testing.T) {
	t.Parallel()

	prov := &coretesting.StubIntegration{
		N: "example",
		CatalogVal: &catalog.Catalog{
			Name: "example",
			Operations: []catalog.CatalogOperation{
				{ID: "get_item"},
			},
		},
	}

	wrapped, err := applyAllowedOperations("example", map[string]*config.OperationOverride{}, prov)
	if err != nil {
		t.Fatalf("applyAllowedOperations: %v", err)
	}
	if wrapped == prov {
		t.Fatal("expected provider to be wrapped for explicit empty allowlist")
	}

	filtered := wrapped.Catalog()
	if len(filtered.Operations) != 0 {
		t.Fatalf("catalog operations = %#v, want no operations", filtered.Operations)
	}
}
