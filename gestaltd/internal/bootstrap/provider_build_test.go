package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
)

func TestApplyProviderPaginationUsesExposedAlias(t *testing.T) {
	t.Parallel()

	def := &declarative.Definition{
		Operations: map[string]declarative.OperationDef{
			"listNotes": {
				Method: "GET",
				Path:   "/v1/notes",
			},
			"getNote": {
				Method: "GET",
				Path:   "/v1/notes/{note_id}",
			},
		},
	}
	manifestPlugin := &providermanifestv1.Spec{
		Pagination: &providermanifestv1.ManifestPaginationConfig{
			Style:        providermanifestv1.PaginationStyleCursor,
			CursorParam:  "cursor",
			LimitParam:   "page_size",
			DefaultLimit: 10,
			ResultsPath:  "notes",
		},
	}
	allowedOperations := map[string]*config.OperationOverride{
		"list_notes": {
			Alias:    "listNotes",
			Paginate: true,
		},
		"mcp_only": {
			Paginate: true,
		},
	}

	applyProviderPagination(def, manifestPlugin, allowedOperations)

	listOp := def.Operations["listNotes"]
	if listOp.Pagination == nil {
		t.Fatal("listNotes pagination = nil, want pagination on exposed alias")
	}
	if listOp.Pagination.CursorParam != "cursor" {
		t.Fatalf("CursorParam = %q, want cursor", listOp.Pagination.CursorParam)
	}
	if listOp.Pagination.LimitParam != "page_size" {
		t.Fatalf("LimitParam = %q, want page_size", listOp.Pagination.LimitParam)
	}
	if listOp.Pagination.DefaultLimit != 10 {
		t.Fatalf("DefaultLimit = %d, want 10", listOp.Pagination.DefaultLimit)
	}
	if listOp.Pagination.ResultsPath != "notes" {
		t.Fatalf("ResultsPath = %q, want notes", listOp.Pagination.ResultsPath)
	}
	if _, ok := def.Operations["list_notes"]; ok {
		t.Fatal("applyProviderPagination created original list_notes operation; want only exposed alias")
	}
	if _, ok := def.Operations["mcp_only"]; ok {
		t.Fatal("applyProviderPagination created absent mcp_only operation")
	}
}
