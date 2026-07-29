package bootstrap

import (
	"strings"
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

func TestApplyAllowedOperationsRejectsAllUnknown(t *testing.T) {
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

	_, err := applyAllowedOperations("example", map[string]*config.OperationOverride{
		"get_review_ctx": nil,
	}, prov)
	if err == nil {
		t.Fatal("expected error when all allowed operations are unknown")
	}
	if !strings.Contains(err.Error(), "no matching catalog operations") {
		t.Fatalf("error = %q, want no matching catalog operations", err.Error())
	}
}
