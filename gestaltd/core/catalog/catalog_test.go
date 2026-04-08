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
  - id: list_records
    provider_id: records.list
    description: List records
    read_only: true
    parameters:
      - name: limit
        type: integer
        description: Maximum number of records
        default: 100
  - id: get_record
    description: Get a record
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
	if cat.Operations[0].ID != "list_records" {
		t.Fatalf("operation[0].ID = %q, want list_records", cat.Operations[0].ID)
	}
}

func TestLoadCatalogYAMLRejectsMalformedOperationIDs(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"chat..post",
		".chat",
		"chat.",
		"-foo",
		"foo-",
		"foo.-bar",
		"foo.bar-",
		"foo bar",
		"foo@bar",
		"foo/bar",
	}
	for _, id := range invalid {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			_, err := LoadCatalogYAML([]byte("name: bad\noperations:\n  - id: \"" + id + "\"\n    method: GET\n    path: /test\n"))
			if err == nil {
				t.Fatalf("expected error for operation id %q", id)
			}
		})
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
