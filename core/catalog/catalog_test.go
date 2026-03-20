package catalog

import "testing"

func TestLoadCatalogYAML(t *testing.T) {
	t.Parallel()

	cat, err := LoadCatalogYAML([]byte(`
name: example
display_name: Example
description: Example integration
base_url: https://api.example.com
auth_style: bearer
headers:
  X-API-Version: "2026-03-17"
operations:
  - id: list_items
    provider_id: items.list
    method: GET
    path: /items
    description: List items
    read_only: true
    parameters:
      - name: limit
        type: integer
        description: Maximum items to return
        default: 100
`))
	if err != nil {
		t.Fatalf("LoadCatalogYAML: %v", err)
	}

	if cat.Name != "example" {
		t.Fatalf("Name = %q, want example", cat.Name)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("len(Operations) = %d, want 1", len(cat.Operations))
	}
	if cat.Operations[0].ID != "list_items" {
		t.Fatalf("operation id = %q, want list_items", cat.Operations[0].ID)
	}
}

func TestLoadCatalogYAMLRejectsInvalidCatalog(t *testing.T) {
	t.Parallel()

	_, err := LoadCatalogYAML([]byte(`
name: invalid
operations:
  - id: duplicate
    method: GET
    path: /one
  - id: duplicate
    method: POST
    path: /two
`))
	if err == nil {
		t.Fatal("expected duplicate operation error")
	}
}
