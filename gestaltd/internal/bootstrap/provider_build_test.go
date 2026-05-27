package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
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
	manifestApp := &providermanifestv1.Spec{
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

	applyProviderPagination(def, manifestApp, allowedOperations)

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

func TestMCPAllowedOperationsForSpecCompositeFiltersOnlyWhenAPIIsPresent(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"lookup": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: "query { lookup { nodes { id } } }",
			},
		},
		"mcp_lookup": {Description: "MCP lookup"},
		"list_notes": {Alias: "listNotes"},
	}
	apiCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "listNotes"},
	}}
	mcpCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "lookup"},
		{ID: "mcp_lookup"},
	}}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, mcpCatalog)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for matching static MCP catalog")
	}
	if len(filtered) != 1 || filtered["mcp_lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only mcp_lookup", filtered)
	}
	if _, ok := filtered["lookup"]; ok {
		t.Fatal("GraphQL-tagged operation should not be passed to MCP filter when API surface exists")
	}

	unfiltered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, false, nil, mcpCatalog)
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for MCP-only provider")
	}
	if len(unfiltered) != len(allowedOperations) {
		t.Fatalf("unfiltered allowedOperations = %#v, want all operations for MCP-only provider", unfiltered)
	}
}

func TestMCPAllowedOperationsForSpecCompositePreservesDynamicAllowlist(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: "query { searchIssues { nodes { id } } }",
			},
		},
		"lookup":     {Description: "MCP lookup"},
		"list_notes": {Alias: "listNotes"},
	}
	apiCatalog := &catalog.Catalog{Operations: []catalog.CatalogOperation{
		{ID: "listNotes"},
	}}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, apiCatalog, &catalog.Catalog{})
	if !includeMCP {
		t.Fatal("includeMCP = false, want true for dynamic MCP allowlist")
	}
	if len(filtered) != 1 || filtered["lookup"] == nil {
		t.Fatalf("filtered allowedOperations = %#v, want only dynamic MCP operation", filtered)
	}
}

func TestMCPAllowedOperationsForSpecCompositeOmitsMCPWhenNoMCPAllowlistEntries(t *testing.T) {
	t.Parallel()

	allowedOperations := map[string]*config.OperationOverride{
		"searchIssues": {
			Description: "GraphQL search",
			GraphQL: &providermanifestv1.ManifestGraphQLOperation{
				Document: "query { searchIssues { nodes { id } } }",
			},
		},
	}

	filtered, includeMCP := mcpAllowedOperationsForSpecComposite(allowedOperations, true, nil, &catalog.Catalog{})
	if includeMCP {
		t.Fatalf("includeMCP = true with filtered allowedOperations %#v, want false", filtered)
	}
	if filtered != nil {
		t.Fatalf("filtered allowedOperations = %#v, want nil", filtered)
	}
}
