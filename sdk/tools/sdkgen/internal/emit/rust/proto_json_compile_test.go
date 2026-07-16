package rust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func scalarShapesWireMessage() *model.Message {
	oneof := &model.Oneof{Name: "value", FieldNumbers: []int32{1, 2, 3, 4, 5}}
	return &model.Message{
		FullName:  "test.v1.ScalarShapes",
		Name:      "ScalarShapes",
		ProtoFile: "test/v1/scalar_shapes.proto",
		Oneofs:    []*model.Oneof{oneof},
		Fields: []*model.Field{
			{Name: "bool_value", JSONName: "boolValue", Number: 1, Kind: model.KindScalar, Scalar: model.ScalarBool, OneofIndex: 0},
			{Name: "int64_value", JSONName: "int64Value", Number: 2, Kind: model.KindScalar, Scalar: model.ScalarInt64, OneofIndex: 0},
			{Name: "uint64_value", JSONName: "uint64Value", Number: 3, Kind: model.KindScalar, Scalar: model.ScalarUint64, OneofIndex: 0},
			{Name: "float_value", JSONName: "floatValue", Number: 4, Kind: model.KindScalar, Scalar: model.ScalarFloat, OneofIndex: 0},
			{Name: "double_value", JSONName: "doubleValue", Number: 5, Kind: model.KindScalar, Scalar: model.ScalarDouble, OneofIndex: 0},
			{
				Name: "flags", JSONName: "flags", Number: 6, Kind: model.KindMap, OneofIndex: -1,
				MapKey: model.ScalarString, MapValue: &model.TypeRef{Kind: model.KindScalar, Scalar: model.ScalarBool},
			},
			{
				Name: "counts", JSONName: "counts", Number: 7, Kind: model.KindMap, OneofIndex: -1,
				MapKey: model.ScalarString, MapValue: &model.TypeRef{Kind: model.KindScalar, Scalar: model.ScalarInt64},
			},
		},
	}
}

func renderScalarShapesWireJSON(t *testing.T) string {
	t.Helper()

	msg := scalarShapesWireMessage()
	idx := &index{
		messages:     map[string]*model.Message{msg.FullName: msg},
		wireMessages: map[string]*model.Message{msg.FullName: msg},
	}
	r := newRenderer(idx, "codec/scalar_shapes", "codec/scalar_shapes", moduleCodec, true)
	r.renderWireProtoJSON(msg, true, false)
	return r.body.String()
}

func TestWireJSONScalarShapesCompile(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo not available")
	}

	generated := renderScalarShapesWireJSON(t)
	generated = strings.ReplaceAll(generated, "crate::public::proto_json::", "proto_json::")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]
name = "wire_json_scalar_shapes"
version = "0.0.0"
edition = "2024"

[dependencies]
serde_json = "1"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}

	source := wireJSONCompileHarness + "\n" + generated
	if err := os.WriteFile(filepath.Join(src, "lib.rs"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("cargo", "check", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cargo check failed: %v\n%s", err, out)
	}
}

const wireJSONCompileHarness = `
#![allow(dead_code)]

mod proto_json {
    pub fn encode_i64(value: i64) -> serde_json::Value {
        serde_json::Value::String(value.to_string())
    }
    pub fn encode_u64(value: u64) -> serde_json::Value {
        serde_json::Value::String(value.to_string())
    }
    pub fn encode_f32(value: f32) -> serde_json::Value {
        serde_json::json!(f64::from(value))
    }
    pub fn encode_f64(value: f64) -> serde_json::Value {
        serde_json::json!(value)
    }
}

mod v1 {
    use std::collections::BTreeMap;

    pub mod scalar_shapes {
        pub enum Value {
            BoolValue(bool),
            Int64Value(i64),
            Uint64Value(u64),
            FloatValue(f32),
            DoubleValue(f64),
        }
    }

    pub struct ScalarShapes {
        pub value: Option<scalar_shapes::Value>,
        pub flags: BTreeMap<String, bool>,
        pub counts: BTreeMap<String, i64>,
    }
}
`
