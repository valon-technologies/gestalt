package mcpupstream

import (
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func toolWith(name, title, annTitle string) mcpgo.Tool {
	t := mcpgo.Tool{Name: name, Title: title}
	t.Annotations.Title = annTitle
	return t
}

// The MCP schema defines the order for a Tool in BaseMetadata.title: `title`,
// then `annotations.title`, then the name. Reading only `annotations.title`
// skips the first and highest-priority step.
func TestToolDisplayTitleFollowsSchemaPrecedence(t *testing.T) {
	for name, tc := range map[string]struct {
		tool mcpgo.Tool
		want string
	}{
		"top-level title is preferred": {
			toolWith("resolve-library-id", "Resolve Library ID", ""),
			"Resolve Library ID",
		},
		"top-level title wins over annotations": {
			toolWith("resolve-library-id", "Resolve Library ID", "Legacy Title"),
			"Resolve Library ID",
		},
		"annotations title used when top-level absent": {
			toolWith("read_wiki_contents", "", "Read Wiki Contents"),
			"Read Wiki Contents",
		},
		"blank top-level falls through to annotations": {
			toolWith("read_wiki_contents", "   ", "Read Wiki Contents"),
			"Read Wiki Contents",
		},
		// Empty rather than the name: the consuming surface substitutes its own
		// fallback, and writing the name here would make a real title
		// indistinguishable from a substituted one.
		"neither set yields empty": {
			toolWith("ask_question", "", ""),
			"",
		},
		"whitespace-only annotations yields empty": {
			toolWith("ask_question", "", "  "),
			"",
		},
	} {
		if got := toolDisplayTitle(tc.tool); got != tc.want {
			t.Errorf("%s: toolDisplayTitle() = %q, want %q", name, got, tc.want)
		}
	}
}

// The regression at the level it actually bit: a catalog built from a server
// that sets `title` the way the schema specifies must carry those titles
// through rather than dropping them.
func TestBuildCatalogCarriesUpstreamTitle(t *testing.T) {
	cat := buildCatalog("example", []mcpgo.Tool{
		toolWith("resolve-library-id", "Resolve Library ID", ""),
		toolWith("query-docs", "Query Documentation", ""),
	})

	if len(cat.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(cat.Operations))
	}
	for _, op := range cat.Operations {
		if op.Title == "" {
			t.Errorf("operation %q lost the title its server supplied", op.ID)
		}
		if op.Title == op.ID {
			t.Errorf("operation %q title equals the id, so the field carries no additional signal", op.ID)
		}
	}
}

// A catalog mixing servers that set `title` with servers that set nothing must
// keep exactly the titles that were supplied.
func TestBuildCatalogRetainsTitlesAcrossMixedServers(t *testing.T) {
	cat := buildCatalog("mixed", []mcpgo.Tool{
		toolWith("resolve-library-id", "Resolve Library ID", ""),
		toolWith("search_entries", "Search Entries", ""),
		toolWith("list_entries", "List Entries", ""),
		toolWith("ask_question", "", ""),
		toolWith("read_contents", "", ""),
	})

	titled := 0
	for _, op := range cat.Operations {
		if op.Title != "" {
			titled++
		}
	}
	if titled != 3 {
		t.Errorf("kept %d titles, want 3; reading only annotations.title yields 0", titled)
	}
}
