package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestWireJSONContainerTimestampUsesWKTHelpers(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{}, "codec/app", "codec/app", modulePublic, true)
	ref := &model.TypeRef{Kind: model.KindTimestamp}

	encode := r.wireJSONEncodeValue(ref, "item", wireJSONScalarBorrowed)
	if strings.Contains(encode, "serde_json::json!") {
		t.Fatalf("timestamp encode must not use serde_json::json!: %q", encode)
	}
	if !strings.Contains(encode, "encode_timestamp") {
		t.Fatalf("timestamp encode missing encode_timestamp: %q", encode)
	}

	decode := r.wireJSONDecodeElemResult(ref, "item")
	if !strings.Contains(decode, "decode_timestamp") {
		t.Fatalf("timestamp decode missing decode_timestamp: %q", decode)
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

func TestWireJSONContainerDurationUsesWKTHelpers(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{}, "codec/app", "codec/app", modulePublic, true)
	ref := &model.TypeRef{Kind: model.KindDuration}

	encode := r.wireJSONEncodeValue(ref, "item", wireJSONScalarBorrowed)
	if strings.Contains(encode, "serde_json::json!") {
		t.Fatalf("duration encode must not use serde_json::json!: %q", encode)
	}
	if !strings.Contains(encode, "encode_duration") {
		t.Fatalf("duration encode missing encode_duration: %q", encode)
	}

	decode := r.wireJSONDecodeElemResult(ref, "item")
	if !strings.Contains(decode, "decode_duration") {
		t.Fatalf("duration decode missing decode_duration: %q", decode)
	}
}
