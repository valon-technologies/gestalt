package rust

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestWireJSONContainerUsesWKTHelpers(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{}, "codec/app", "codec/app", modulePublic, true, nil, nil)
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

func TestWireJSONEncodeExplicitPresenceScalarKeepsZero(t *testing.T) {
	t.Parallel()

	r := newRenderer(&index{}, "codec/app", "codec/app", modulePublic, true, nil, nil)
	r.body.Reset()
	r.renderWireJSONEncodeField(&model.Field{
		Name:     "total_count",
		JSONName: "totalCount",
		Kind:     model.KindScalar,
		Scalar:   model.ScalarInt64,
		Presence: model.ExplicitPresence,
	}, "value")
	out := r.body.String()
	if !strings.Contains(out, "if let Some(inner) = &value.total_count") {
		t.Fatalf("expected Option unwrap, got:\n%s", out)
	}
	if strings.Contains(out, "!= 0") {
		t.Fatalf("explicit presence scalar must not skip zero values:\n%s", out)
	}
	if !strings.Contains(out, `object.insert("totalCount".into()`) {
		t.Fatalf("expected unconditional insert for present scalar:\n%s", out)
	}
}
