package agents

import (
	"testing"
	"time"
)

func TestStructFromMap_NormalizesTimeValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 11, 20, 31, 30, 840502000, time.UTC)
	record := map[string]any{
		"created_at": now,
		"expires_at": &now,
		"nested": map[string]any{
			"updated_at": now,
		},
		"values": []any{now, &now},
	}

	s, err := structFromMap(record)
	if err != nil {
		t.Fatalf("structFromMap: %v", err)
	}

	got := s.AsMap()
	want := now.Format(time.RFC3339Nano)
	if got["created_at"] != want {
		t.Fatalf("created_at = %#v, want %#v", got["created_at"], want)
	}
	if got["expires_at"] != want {
		t.Fatalf("expires_at = %#v, want %#v", got["expires_at"], want)
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok || nested["updated_at"] != want {
		t.Fatalf("nested = %#v, want updated_at=%#v", got["nested"], want)
	}
	values, ok := got["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("values = %#v, want 2 normalized entries", got["values"])
	}
	if values[0] != want || values[1] != want {
		t.Fatalf("values = %#v, want both %#v", values, want)
	}
}
