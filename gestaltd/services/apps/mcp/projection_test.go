package mcp

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func TestCatalogOperationProjectedToMCPRespectsOperationExposure(t *testing.T) {
	t.Parallel()

	disabled := false
	if catalogOperationProjectedToMCP(Config{}, "example", catalog.CatalogOperation{ID: "hidden", MCP: &disabled}) {
		t.Fatal("MCP-disabled operation was projected to MCP")
	}
	if !catalogOperationProjectedToMCP(Config{}, "example", catalog.CatalogOperation{ID: "default"}) {
		t.Fatal("operation without an MCP override should be projected")
	}
}
