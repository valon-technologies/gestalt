package gestalt

import (
	"reflect"
	"testing"
)

func TestExportedStructHelpers(t *testing.T) {
	t.Parallel()

	type payload struct {
		ID      string `json:"id"`
		Skipped string `json:"-"`
		Empty   string `json:"empty,omitempty"`
		Count   int    `json:"count"`
	}

	pb, err := StructFromAny(payload{
		ID:      "abc",
		Skipped: "ignored",
		Count:   2,
	})
	if err != nil {
		t.Fatalf("StructFromAny: %v", err)
	}

	got := MapFromStruct(pb)
	want := map[string]any{
		"id":    "abc",
		"count": float64(2),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapFromStruct(StructFromAny(payload)) = %#v, want %#v", got, want)
	}

	pb, err = StructFromMap(map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("StructFromMap: %v", err)
	}
	if got := MapFromStruct(pb); !reflect.DeepEqual(got, map[string]any{"ok": true}) {
		t.Fatalf("MapFromStruct(StructFromMap(map)) = %#v", got)
	}
}
