package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestWireJSONContainerUsesWKTHelpers(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{}, "codec/app", "codec/app", modulePublic, true)
	cases := []struct {
		name    string
		ref     *model.TypeRef
		encode  string
		decode  string
	}{
		{
			name:   "timestamp",
			ref:    &model.TypeRef{Kind: model.KindTimestamp},
			encode: "encode_timestamp",
			decode: "decode_timestamp",
		},
		{
			name:   "duration",
			ref:    &model.TypeRef{Kind: model.KindDuration},
			encode: "encode_duration",
			decode: "decode_duration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encode := r.wireJSONEncodeValue(tc.ref, "item", wireJSONScalarBorrowed)
			if strings.Contains(encode, "serde_json::json!") {
				t.Fatalf("%s encode must not use serde_json::json!: %q", tc.name, encode)
			}
			if !strings.Contains(encode, tc.encode) {
				t.Fatalf("%s encode missing %s: %q", tc.name, tc.encode, encode)
			}
			decode := r.wireJSONDecodeElemResult(tc.ref, "item")
			if !strings.Contains(decode, tc.decode) {
				t.Fatalf("%s decode missing %s: %q", tc.name, tc.decode, decode)
			}
		})
	}
}

func TestWireJSONEncodeEnumUsesNumericFallback(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{
		enums: map[string]*model.Enum{
			"test.v1.Status": {
				FullName: "test.v1.Status",
				Values:   []model.EnumValue{{Name: "OK", Number: 1}},
			},
		},
	}, "codec/app", "codec/app", modulePublic, true)
	ref := &model.TypeRef{Kind: model.KindEnum, Enum: "test.v1.Status"}

	encode := r.wireJSONEncodeValue(ref, "item", wireJSONScalarBorrowed)
	if strings.Contains(encode, "Value::Null") {
		t.Fatalf("unknown enum must encode as number, not null: %q", encode)
	}
	if !strings.Contains(encode, "serde_json::json!(v)") {
		t.Fatalf("enum encode missing numeric fallback: %q", encode)
	}
}
