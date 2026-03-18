package integration

import (
	"testing"
)

func TestLoadCatalogYAML(t *testing.T) {
	t.Parallel()

	catalog, err := LoadCatalogYAML([]byte(`
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

	if catalog.Name != "example" {
		t.Fatalf("Name = %q, want example", catalog.Name)
	}
	if len(catalog.Operations) != 1 {
		t.Fatalf("len(Operations) = %d, want 1", len(catalog.Operations))
	}
	if catalog.Operations[0].ID != "list_items" {
		t.Fatalf("operation id = %q, want list_items", catalog.Operations[0].ID)
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

func TestBaseFromCatalog(t *testing.T) {
	t.Parallel()

	catalog := MustLoadCatalogYAML([]byte(`
name: example
display_name: Example
description: Example integration
base_url: https://api.example.com
auth_style: raw
headers:
  X-Base: catalog
operations:
  - id: create_item
    method: POST
    path: /items
    description: Create an item
    parameters:
      - name: name
        type: string
        required: true
`))

	base, err := BaseFromCatalog(catalog, Base{
		BaseURL: "https://override.example.com",
		Headers: map[string]string{"X-Override": "runtime"},
	})
	if err != nil {
		t.Fatalf("BaseFromCatalog: %v", err)
	}

	if base.Name() != "example" {
		t.Fatalf("Name() = %q, want example", base.Name())
	}
	if base.DisplayName() != "Example" {
		t.Fatalf("DisplayName() = %q, want Example", base.DisplayName())
	}
	if base.Description() != "Example integration" {
		t.Fatalf("Description() = %q, want Example integration", base.Description())
	}
	if base.BaseURL != "https://override.example.com" {
		t.Fatalf("BaseURL = %q, want override URL", base.BaseURL)
	}
	if base.AuthStyle != AuthStyleRaw {
		t.Fatalf("AuthStyle = %v, want %v", base.AuthStyle, AuthStyleRaw)
	}
	if got := base.Headers["X-Base"]; got != "catalog" {
		t.Fatalf("X-Base header = %q, want catalog", got)
	}
	if got := base.Headers["X-Override"]; got != "runtime" {
		t.Fatalf("X-Override header = %q, want runtime", got)
	}
	if len(base.Operations) != 1 || base.Operations[0].Name != "create_item" {
		t.Fatalf("operations = %#v, want create_item", base.Operations)
	}
	if endpoint := base.Endpoints["create_item"]; endpoint.Path != "/items" || endpoint.Method != "POST" {
		t.Fatalf("endpoint = %#v, want POST /items", endpoint)
	}
}
