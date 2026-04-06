package catalog

import "testing"

func TestLoadCatalogYAML(t *testing.T) {
	t.Parallel()

	cat, err := LoadCatalogYAML([]byte(`
name: sample
display_name: Sample Catalog
description: Generic catalog fixture
auth_style: bearer
headers:
  X-Config-Version: "1"
operations:
  - id: get_record
    description: Get a record
  - id: list_records
    provider_id: records.list
    description: List records
    read_only: true
    parameters:
      - name: limit
        type: integer
        description: Maximum number of records
        default: 100
`))
	if err != nil {
		t.Fatalf("LoadCatalogYAML: %v", err)
	}

	if cat.Name != "sample" {
		t.Fatalf("Name = %q, want sample", cat.Name)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(cat.Operations))
	}
	if cat.Operations[0].ID != "get_record" {
		t.Fatalf("operation[0].ID = %q, want get_record", cat.Operations[0].ID)
	}
}

func TestCatalogCloneSortsOperationsByID(t *testing.T) {
	t.Parallel()

	cat := (&Catalog{
		Name: "sample",
		Operations: []CatalogOperation{
			{ID: "zeta", Method: "POST", Path: "/zeta"},
			{ID: "alpha", Method: "GET", Path: "/alpha"},
		},
	}).Clone()

	if len(cat.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(cat.Operations))
	}
	if cat.Operations[0].ID != "alpha" {
		t.Fatalf("operation[0].ID = %q, want alpha", cat.Operations[0].ID)
	}
	if cat.Operations[1].ID != "zeta" {
		t.Fatalf("operation[1].ID = %q, want zeta", cat.Operations[1].ID)
	}
}

func TestLoadCatalogYAMLRejectsDuplicateOperationIDs(t *testing.T) {
	t.Parallel()

	_, err := LoadCatalogYAML([]byte(`
name: broken
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
