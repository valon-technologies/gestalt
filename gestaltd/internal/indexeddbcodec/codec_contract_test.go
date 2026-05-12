package indexeddbcodec

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestStableKeyEncodingMatchesLegacyWire(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		encoded string
		want    any
	}{
		{
			name:    "string",
			input:   "alpha",
			encoded: "0a071205616c706861",
			want:    "alpha",
		},
		{
			name:    "compound",
			input:   []any{"by_type", int64(7), []any{"nested", int64(9)}},
			encoded: "12290a0b0a09120762795f747970650a040a0218070a1412120a0a0a0812066e65737465640a040a021809",
			want:    []any{"by_type", int64(7), []any{"nested", int64(9)}},
		},
		{
			name:    "bytes",
			input:   []byte{0, 1, 255},
			encoded: "0a053a030001ff",
			want:    []byte{0, 1, 255},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantBytes := mustDecodeHex(t, tt.encoded)
			gotBytes, err := EncodeKey(tt.input)
			if err != nil {
				t.Fatalf("EncodeKey: %v", err)
			}
			if !reflect.DeepEqual(gotBytes, wantBytes) {
				t.Fatalf("EncodeKey bytes = %x, want %x", gotBytes, wantBytes)
			}

			gotValue, err := DecodeKey(wantBytes)
			if err != nil {
				t.Fatalf("DecodeKey legacy bytes: %v", err)
			}
			if !reflect.DeepEqual(gotValue, tt.want) {
				t.Fatalf("DecodeKey legacy bytes = %#v, want %#v", gotValue, tt.want)
			}
		})
	}
}

func TestStableIndexValuesEncodingMatchesLegacyWire(t *testing.T) {
	t.Parallel()

	values := []any{"by_type", int64(7), true}
	wantBytes := mustDecodeHex(t, "0a0e0a01301209120762795f747970650a070a0131120218070a070a013212022801")

	gotBytes, err := EncodeIndexValues(values)
	if err != nil {
		t.Fatalf("EncodeIndexValues: %v", err)
	}
	if !reflect.DeepEqual(gotBytes, wantBytes) {
		t.Fatalf("EncodeIndexValues bytes = %x, want %x", gotBytes, wantBytes)
	}

	gotValues, err := DecodeIndexValues(wantBytes, len(values))
	if err != nil {
		t.Fatalf("DecodeIndexValues legacy bytes: %v", err)
	}
	if !reflect.DeepEqual(gotValues, values) {
		t.Fatalf("DecodeIndexValues legacy bytes = %#v, want %#v", gotValues, values)
	}
}

func TestRecordDecodesLegacyWire(t *testing.T) {
	t.Parallel()

	legacyBytes := mustDecodeHex(t, "0a0b0a05636f756e74120218030a0a0a0269641204120272310a2c0a046a736f6e122442222a200a080a026f6b120220010a140a0474616773120c320a0a031a01610a031a0162")
	want := Record{
		"count": int64(3),
		"id":    "r1",
		"json": map[string]any{
			"ok":   true,
			"tags": []any{"a", "b"},
		},
	}

	got, err := DecodeRecord(legacyBytes)
	if err != nil {
		t.Fatalf("DecodeRecord legacy bytes: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DecodeRecord legacy bytes = %#v, want %#v", got, want)
	}

	encoded, err := EncodeRecord(want)
	if err != nil {
		t.Fatalf("EncodeRecord: %v", err)
	}
	roundTrip, err := DecodeRecord(encoded)
	if err != nil {
		t.Fatalf("DecodeRecord new bytes: %v", err)
	}
	if !reflect.DeepEqual(roundTrip, want) {
		t.Fatalf("DecodeRecord new bytes = %#v, want %#v", roundTrip, want)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()

	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hex %q: %v", value, err)
	}
	return data
}
