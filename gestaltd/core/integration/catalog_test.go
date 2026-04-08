package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func TestCompileSchemasPreservesExistingInputSchema(t *testing.T) {
	t.Parallel()

	existing := json.RawMessage(`{"type":"object","properties":{"custom":{"type":"string"}}}`)
	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "op1",
				Method:      http.MethodPost,
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
			{ID: "read", Method: http.MethodGet, Path: "/read"},
			{ID: "write", Method: http.MethodPost, Path: "/write"},
			{ID: "remove", Method: http.MethodDelete, Path: "/remove"},
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
				Method: http.MethodGet,
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
