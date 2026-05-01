package gestalt

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func TestTypedValueRoundTrip(t *testing.T) {
	now := time.Date(2026, time.April, 11, 19, 4, 5, 123456000, time.UTC)

	tests := []struct {
		name  string
		input any
		check func(t *testing.T, got any)
	}{
		{
			name:  "null",
			input: nil,
			check: func(t *testing.T, got any) {
				if got != nil {
					t.Fatalf("got %T %#v, want nil", got, got)
				}
			},
		},
		{
			name:  "string",
			input: "hello",
			check: func(t *testing.T, got any) {
				if got != "hello" {
					t.Fatalf("got %v, want hello", got)
				}
			},
		},
		{
			name:  "bool",
			input: true,
			check: func(t *testing.T, got any) {
				if got != true {
					t.Fatalf("got %v, want true", got)
				}
			},
		},
		{
			name:  "int",
			input: int(42),
			check: func(t *testing.T, got any) {
				if got != int64(42) {
					t.Fatalf("got %T %#v, want int64(42)", got, got)
				}
			},
		},
		{
			name:  "float",
			input: 3.5,
			check: func(t *testing.T, got any) {
				if got != 3.5 {
					t.Fatalf("got %v, want 3.5", got)
				}
			},
		},
		{
			name:  "time",
			input: now,
			check: func(t *testing.T, got any) {
				gotTime, ok := got.(time.Time)
				if !ok {
					t.Fatalf("got %T, want time.Time", got)
				}
				if !gotTime.Equal(now) {
					t.Fatalf("got %v, want %v", gotTime, now)
				}
			},
		},
		{
			name:  "bytes",
			input: []byte("hello"),
			check: func(t *testing.T, got any) {
				gotBytes, ok := got.([]byte)
				if !ok {
					t.Fatalf("got %T, want []byte", got)
				}
				if !bytes.Equal(gotBytes, []byte("hello")) {
					t.Fatalf("got %q, want %q", gotBytes, []byte("hello"))
				}
			},
		},
		{
			name:  "json",
			input: map[string]any{"items": []any{"one", float64(2), nil}},
			check: func(t *testing.T, got any) {
				want := map[string]any{"items": []any{"one", float64(2), nil}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %#v, want %#v", got, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pbValue, err := typedValueFromAny(tt.input)
			if err != nil {
				t.Fatalf("typedValueFromAny() error = %v", err)
			}
			got, err := anyFromTypedValue(pbValue)
			if err != nil {
				t.Fatalf("anyFromTypedValue() error = %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestRecordRoundTripPreservesTypes(t *testing.T) {
	now := time.Date(2026, time.April, 11, 19, 4, 5, 123456000, time.UTC)
	record := Record{
		"id":        "conn_123",
		"enabled":   true,
		"attempts":  int32(3),
		"score":     9.25,
		"updatedAt": now,
		"secret":    []byte("token"),
		"meta": map[string]any{
			"team": "platform",
			"tags": []any{"oauth", float64(2)},
		},
	}

	pbRecord, err := recordToProto(record)
	if err != nil {
		t.Fatalf("recordToProto() error = %v", err)
	}
	got, err := recordFromProto(pbRecord)
	if err != nil {
		t.Fatalf("recordFromProto() error = %v", err)
	}

	if got["id"] != "conn_123" {
		t.Fatalf("id = %#v, want conn_123", got["id"])
	}
	if got["enabled"] != true {
		t.Fatalf("enabled = %#v, want true", got["enabled"])
	}
	if got["attempts"] != int64(3) {
		t.Fatalf("attempts = %T %#v, want int64(3)", got["attempts"], got["attempts"])
	}
	if got["score"] != 9.25 {
		t.Fatalf("score = %#v, want 9.25", got["score"])
	}
	gotTime, ok := got["updatedAt"].(time.Time)
	if !ok || !gotTime.Equal(now) {
		t.Fatalf("updatedAt = %#v, want %v", got["updatedAt"], now)
	}
	gotBytes, ok := got["secret"].([]byte)
	if !ok || !bytes.Equal(gotBytes, []byte("token")) {
		t.Fatalf("secret = %#v, want []byte(\"token\")", got["secret"])
	}
	wantMeta := map[string]any{
		"team": "platform",
		"tags": []any{"oauth", float64(2)},
	}
	if !reflect.DeepEqual(got["meta"], wantMeta) {
		t.Fatalf("meta = %#v, want %#v", got["meta"], wantMeta)
	}
}
