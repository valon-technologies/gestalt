package providerpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStaticCatalogParsesYAMLSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := []byte(`
name: provider
displayName: Demo Provider
operations:
  - id: echo
    method: POST
    inputSchema:
      type: object
      properties:
        message:
          type: string
      required:
        - message
    outputSchema:
      type: object
      properties:
        echoed:
          type: string
`)
	if err := os.WriteFile(filepath.Join(root, StaticCatalogFile), data, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}

	cat, err := ReadStaticCatalog(root, "")
	if err != nil {
		t.Fatalf("ReadStaticCatalog: %v", err)
	}
	if cat == nil {
		t.Fatal("expected catalog")
	}
	if cat.DisplayName != "Demo Provider" {
		t.Fatalf("DisplayName = %q, want %q", cat.DisplayName, "Demo Provider")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("operation count = %d, want 1", len(cat.Operations))
	}
	if len(cat.Operations[0].InputSchema) == 0 {
		t.Fatal("expected inputSchema to be parsed")
	}
	if len(cat.Operations[0].OutputSchema) == 0 {
		t.Fatal("expected outputSchema to be parsed")
	}

	var input map[string]any
	if err := json.Unmarshal(cat.Operations[0].InputSchema, &input); err != nil {
		t.Fatalf("Unmarshal(inputSchema): %v", err)
	}
	if input["type"] != "object" {
		t.Fatalf("inputSchema.type = %v, want object", input["type"])
	}

	var output map[string]any
	if err := json.Unmarshal(cat.Operations[0].OutputSchema, &output); err != nil {
		t.Fatalf("Unmarshal(outputSchema): %v", err)
	}
	if output["type"] != "object" {
		t.Fatalf("outputSchema.type = %v, want object", output["type"])
	}
}

func TestReadStaticCatalogRejectsBlankAllowedRoles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := []byte(`
name: provider
operations:
  - id: echo
    method: POST
    allowedRoles:
      - "   "
`)
	if err := os.WriteFile(filepath.Join(root, StaticCatalogFile), data, 0o644); err != nil {
		t.Fatalf("WriteFile(catalog.yaml): %v", err)
	}

	_, err := ReadStaticCatalog(root, "")
	if err == nil || err.Error() == "" || !strings.Contains(err.Error(), "allowedRoles entry with empty value") {
		t.Fatalf("ReadStaticCatalog error = %v, want blank allowedRoles error", err)
	}
}
