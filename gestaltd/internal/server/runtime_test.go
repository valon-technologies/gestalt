package server

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
)

func TestHTTPCatalogConnectionMapUsesAPIConnection(t *testing.T) {
	t.Parallel()

	connMaps := bootstrap.ConnectionMaps{
		DefaultConnection: map[string]string{"notion": "OAuth"},
		APIConnection:     map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}

	got := httpCatalogConnectionMap(connMaps)
	if got["notion"] != "OAuth" {
		t.Fatalf("catalog connection = %q, want %q", got["notion"], "OAuth")
	}
}
