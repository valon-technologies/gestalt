package client

import (
	"reflect"
	"testing"
)

func TestToWireStructNormalizesTypedContainers(t *testing.T) {
	in := map[string]any{
		"kind": "steps",
		"steps": []map[string]any{
			{"id": "generateTeam", "kind": "app", "app": "delta", "operation": "generate.teamWorkflow"},
		},
	}
	out := fromWireStruct(toWireStruct(in))
	want := map[string]any{
		"kind": "steps",
		"steps": []any{
			map[string]any{"id": "generateTeam", "kind": "app", "app": "delta", "operation": "generate.teamWorkflow"},
		},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("round-trip mismatch:\ngot  %#v\nwant %#v", out, want)
	}
}
