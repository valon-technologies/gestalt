package integration

import (
	"encoding/json"
	"testing"

	"github.com/valon-technologies/gestalt/core/catalog"
)

func TestCompileSchemasSynthesizesSchemaAndMethodHints(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "generic",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "list_records",
				Method: "GET",
				Path:   "/records",
				Parameters: []catalog.CatalogParameter{
					{Name: "cursor", Type: "string", Description: "Page cursor", Required: true},
					{Name: "limit", Type: "int", Default: 50},
				},
			},
			{ID: "replace_record", Method: "PUT", Path: "/records/{id}"},
			{ID: "delete_record", Method: "DELETE", Path: "/records/{id}"},
		},
	}

	CompileSchemas(cat)

	listOp := cat.Operations[0]
	if listOp.InputSchema == nil {
		t.Fatal("CompileSchemas should synthesize InputSchema from parameters")
	}

	var schema map[string]any
	if err := json.Unmarshal(listOp.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal InputSchema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties has type %T, want map[string]any", schema["properties"])
	}
	if len(props) != 2 {
		t.Fatalf("got %d properties, want 2", len(props))
	}
	if props["limit"].(map[string]any)["type"] != "integer" {
		t.Fatalf("limit type = %v, want integer", props["limit"].(map[string]any)["type"])
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "cursor" {
		t.Fatalf("required = %v, want [cursor]", required)
	}
	if listOp.Annotations.OpenWorldHint == nil || !*listOp.Annotations.OpenWorldHint {
		t.Fatal("GET operation should receive openWorldHint")
	}
	if listOp.Annotations.ReadOnlyHint == nil || !*listOp.Annotations.ReadOnlyHint {
		t.Fatal("GET operation should receive readOnlyHint")
	}

	replaceOp := cat.Operations[1]
	if replaceOp.Annotations.IdempotentHint == nil || !*replaceOp.Annotations.IdempotentHint {
		t.Fatal("PUT operation should receive idempotentHint")
	}

	deleteOp := cat.Operations[2]
	if deleteOp.Annotations.DestructiveHint == nil || !*deleteOp.Annotations.DestructiveHint {
		t.Fatal("DELETE operation should receive destructiveHint")
	}
}

func TestCompileSchemasPreservesExistingSchemaAndAnnotations(t *testing.T) {
	t.Parallel()

	existingSchema := json.RawMessage(`{"type":"object","properties":{"custom":{"type":"string"}}}`)
	cat := &catalog.Catalog{
		Name: "generic",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "create_record",
				Method:      "POST",
				Path:        "/records",
				InputSchema: existingSchema,
				Parameters: []catalog.CatalogParameter{
					{Name: "ignored", Type: "string"},
				},
				Annotations: catalog.OperationAnnotations{
					OpenWorldHint: boolPtr(false),
				},
			},
			{
				ID:     "read_record",
				Method: "GET",
				Path:   "/records/{id}",
				Annotations: catalog.OperationAnnotations{
					ReadOnlyHint:  boolPtr(false),
					OpenWorldHint: boolPtr(false),
				},
			},
		},
	}

	CompileSchemas(cat)

	if string(cat.Operations[0].InputSchema) != string(existingSchema) {
		t.Fatalf("CompileSchemas overwrote existing InputSchema: got %s", cat.Operations[0].InputSchema)
	}
	if cat.Operations[0].Annotations.OpenWorldHint == nil || *cat.Operations[0].Annotations.OpenWorldHint {
		t.Fatal("should preserve existing openWorldHint=false")
	}

	a := cat.Operations[1].Annotations
	if a.ReadOnlyHint == nil || *a.ReadOnlyHint {
		t.Error("should preserve existing readOnlyHint=false")
	}
	if a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Error("should preserve existing openWorldHint=false")
	}
}
