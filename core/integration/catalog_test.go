package integration

import (
	"encoding/json"
	"testing"

	"github.com/valon-technologies/gestalt/core/catalog"
)

func TestLoadCatalogYAML(t *testing.T) {
	t.Parallel()

	cat, err := LoadCatalogYAML([]byte(`
name: example
display_name: Example
description: Example integration
icon_svg: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>'
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
	if cat.IconSVG != `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>` {
		t.Fatalf("IconSVG = %q, want svg string", cat.IconSVG)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("len(Operations) = %d, want 1", len(cat.Operations))
	}
	if cat.Operations[0].ID != "list_items" {
		t.Fatalf("operation id = %q, want list_items", cat.Operations[0].ID)
	}
}

func TestLoadCatalogYAMLWithoutIcon(t *testing.T) {
	t.Parallel()

	cat, err := LoadCatalogYAML([]byte(`
name: no_icon
operations:
  - id: op
    method: GET
    path: /op
`))
	if err != nil {
		t.Fatalf("LoadCatalogYAML: %v", err)
	}
	if cat.IconSVG != "" {
		t.Fatalf("IconSVG = %q, want empty string", cat.IconSVG)
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

	cat := MustLoadCatalogYAML([]byte(`
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

	base, err := BaseFromCatalog(cat, Base{
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

	storedCat := base.Catalog()
	if storedCat == nil {
		t.Fatal("BaseFromCatalog should store catalog on base")
	}
	if storedCat.Name != "example" {
		t.Errorf("stored catalog name = %q", storedCat.Name)
	}
}

func TestCompileSchemasFillsInputSchema(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "op1",
				Method: "GET",
				Path:   "/test",
				Parameters: []catalog.CatalogParameter{
					{Name: "q", Type: "string", Description: "Query", Required: true},
					{Name: "limit", Type: "integer", Default: 50},
				},
			},
		},
	}

	CompileSchemas(cat)

	op := cat.Operations[0]
	if op.InputSchema == nil {
		t.Fatal("CompileSchemas should synthesize InputSchema from Parameters")
	}

	var schema map[string]any
	if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal InputSchema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v", schema["type"])
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("got %d properties, want 2", len(props))
	}
}

func TestCompileSchemasPreservesExistingInputSchema(t *testing.T) {
	t.Parallel()

	existing := json.RawMessage(`{"type":"object","properties":{"custom":{"type":"string"}}}`)
	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "op1",
				Method:      "POST",
				Path:        "/test",
				InputSchema: existing,
				Parameters: []catalog.CatalogParameter{
					{Name: "ignored", Type: "string"},
				},
			},
		},
	}

	CompileSchemas(cat)

	if string(cat.Operations[0].InputSchema) != string(existing) {
		t.Errorf("CompileSchemas overwrote existing InputSchema: got %s", cat.Operations[0].InputSchema)
	}
}

func TestCompileSchemasFillsAnnotations(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{ID: "read", Method: "GET", Path: "/read"},
			{ID: "write", Method: "POST", Path: "/write"},
			{ID: "remove", Method: "DELETE", Path: "/remove"},
		},
	}

	CompileSchemas(cat)

	if cat.Operations[0].Annotations.ReadOnlyHint == nil || !*cat.Operations[0].Annotations.ReadOnlyHint {
		t.Error("GET should have readOnlyHint=true")
	}
	if cat.Operations[1].Annotations.OpenWorldHint == nil || !*cat.Operations[1].Annotations.OpenWorldHint {
		t.Error("POST should have openWorldHint=true")
	}
	if cat.Operations[2].Annotations.DestructiveHint == nil || !*cat.Operations[2].Annotations.DestructiveHint {
		t.Error("DELETE should have destructiveHint=true")
	}
}

func TestCompileSchemasPreservesExistingAnnotations(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "op1",
				Method: "GET",
				Path:   "/test",
				Annotations: catalog.OperationAnnotations{
					ReadOnlyHint:  boolPtr(false),
					OpenWorldHint: boolPtr(false),
				},
			},
		},
	}

	CompileSchemas(cat)

	a := cat.Operations[0].Annotations
	if a.ReadOnlyHint == nil || *a.ReadOnlyHint {
		t.Error("should preserve existing readOnlyHint=false")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Error("should preserve existing openWorldHint=false")
	}
}
