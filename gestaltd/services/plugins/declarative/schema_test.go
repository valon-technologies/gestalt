package declarative

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func TestSynthesizeInputSchemaBasic(t *testing.T) {
	t.Parallel()

	params := []catalog.CatalogParameter{
		{Name: "channel", Type: "string", Description: "Channel ID", Required: true},
		{Name: "limit", Type: "integer", Description: "Max items", Default: 100},
	}

	raw := SynthesizeInputSchema(params)
	if raw == nil {
		t.Fatal("expected non-nil schema")
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if schema["type"] != "object" {
		t.Errorf("type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties not a map: %T", schema["properties"])
	}
	if len(props) != 2 {
		t.Errorf("got %d properties, want 2", len(props))
	}

	channelProp := props["channel"].(map[string]any)
	if channelProp["type"] != "string" {
		t.Errorf("channel type = %v", channelProp["type"])
	}
	if channelProp["description"] != "Channel ID" {
		t.Errorf("channel description = %v", channelProp["description"])
	}

	limitProp := props["limit"].(map[string]any)
	if limitProp["type"] != "integer" {
		t.Errorf("limit type = %v", limitProp["type"])
	}
	if limitProp["default"].(float64) != 100 {
		t.Errorf("limit default = %v", limitProp["default"])
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("required not an array: %T", schema["required"])
	}
	if len(required) != 1 || required[0] != "channel" {
		t.Errorf("required = %v, want [channel]", required)
	}
}

func TestSynthesizeInputSchemaEmpty(t *testing.T) {
	t.Parallel()

	if got := SynthesizeInputSchema(nil); got != nil {
		t.Errorf("expected nil for empty params, got %s", got)
	}
	if got := SynthesizeInputSchema([]catalog.CatalogParameter{}); got != nil {
		t.Errorf("expected nil for zero-length params, got %s", got)
	}
}

func TestSynthesizeInputSchemaNoRequired(t *testing.T) {
	t.Parallel()

	params := []catalog.CatalogParameter{
		{Name: "q", Type: "string", Description: "Search query"},
	}

	raw := SynthesizeInputSchema(params)
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := schema["required"]; ok {
		t.Error("should not have required key when no params are required")
	}
}

func TestSynthesizeInputSchemaNormalizesTypes(t *testing.T) {
	t.Parallel()

	params := []catalog.CatalogParameter{
		{Name: "flag", Type: "bool"},
		{Name: "count", Type: "int"},
		{Name: "ratio", Type: "float"},
		{Name: "tags", Type: "array"},
	}

	raw := SynthesizeInputSchema(params)
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	props := schema["properties"].(map[string]any)
	cases := map[string]string{
		"flag":  "boolean",
		"count": "integer",
		"ratio": "number",
		"tags":  "array",
	}
	for name, want := range cases {
		got := props[name].(map[string]any)["type"]
		if got != want {
			t.Errorf("%s type = %v, want %v", name, got, want)
		}
	}
}

func TestAnnotationsFromMethod(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method          string
		wantReadOnly    *bool
		wantIdempotent  *bool
		wantDestructive *bool
	}{
		{http.MethodGet, boolPtr(true), nil, nil},
		{http.MethodHead, boolPtr(true), nil, nil},
		{http.MethodPost, nil, nil, nil},
		{http.MethodPut, nil, boolPtr(true), nil},
		{http.MethodDelete, nil, nil, boolPtr(true)},
		{http.MethodPatch, nil, nil, nil},
	}

	for _, tc := range cases {
		a := AnnotationsFromMethod(tc.method)

		if a.OpenWorldHint == nil || !*a.OpenWorldHint {
			t.Errorf("%s: openWorldHint should be true", tc.method)
		}
		if !ptrEqual(a.ReadOnlyHint, tc.wantReadOnly) {
			t.Errorf("%s: readOnlyHint = %v, want %v", tc.method, ptrStr(a.ReadOnlyHint), ptrStr(tc.wantReadOnly))
		}
		if !ptrEqual(a.IdempotentHint, tc.wantIdempotent) {
			t.Errorf("%s: idempotentHint = %v, want %v", tc.method, ptrStr(a.IdempotentHint), ptrStr(tc.wantIdempotent))
		}
		if !ptrEqual(a.DestructiveHint, tc.wantDestructive) {
			t.Errorf("%s: destructiveHint = %v, want %v", tc.method, ptrStr(a.DestructiveHint), ptrStr(tc.wantDestructive))
		}
	}
}

func ptrEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrStr(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	if *p {
		return "true"
	}
	return "false"
}
