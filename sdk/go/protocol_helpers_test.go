package gestalt

import (
	"bytes"
	"testing"
)

func TestProtoBoundaryHelpers(t *testing.T) {
	msg, err := StructFromAny(map[string]any{"ok": true, "count": 2})
	if err != nil {
		t.Fatalf("StructFromAny: %v", err)
	}

	first, err := MarshalProtoDeterministic(msg)
	if err != nil {
		t.Fatalf("MarshalProtoDeterministic: %v", err)
	}
	second, err := MarshalProtoDeterministic(msg)
	if err != nil {
		t.Fatalf("MarshalProtoDeterministic second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("MarshalProtoDeterministic returned unstable bytes")
	}

	var decoded Struct
	if err := UnmarshalProto(first, &decoded); err != nil {
		t.Fatalf("UnmarshalProto: %v", err)
	}
	if got := MapFromStruct(&decoded); got["ok"] != true || got["count"] != float64(2) {
		t.Fatalf("decoded = %#v", got)
	}

	jsonData, err := MarshalProtoJSON(msg, ProtoJSONMarshalOptions{UseProtoNames: true})
	if err != nil {
		t.Fatalf("MarshalProtoJSON: %v", err)
	}
	var fromJSON Struct
	if err := UnmarshalProtoJSON(jsonData, &fromJSON); err != nil {
		t.Fatalf("UnmarshalProtoJSON: %v", err)
	}
	if got := MapFromStruct(&fromJSON); got["ok"] != true || got["count"] != float64(2) {
		t.Fatalf("fromJSON = %#v", got)
	}
}

func TestMarshalProtoHelpersNilMessage(t *testing.T) {
	detBytes, err := MarshalProtoDeterministic(nil)
	if err != nil {
		t.Fatalf("MarshalProtoDeterministic(nil): %v", err)
	}
	if detBytes != nil {
		t.Fatalf("MarshalProtoDeterministic(nil) bytes = %v, want nil", detBytes)
	}

	jsonBytes, err := MarshalProtoJSON(nil)
	if err != nil {
		t.Fatalf("MarshalProtoJSON(nil): %v", err)
	}
	if jsonBytes != nil {
		t.Fatalf("MarshalProtoJSON(nil) bytes = %v, want nil", jsonBytes)
	}

	jsonBytes, err = MarshalProtoJSON(nil, ProtoJSONMarshalOptions{UseProtoNames: true})
	if err != nil {
		t.Fatalf("MarshalProtoJSON(nil, opts): %v", err)
	}
	if jsonBytes != nil {
		t.Fatalf("MarshalProtoJSON(nil, opts) bytes = %v, want nil", jsonBytes)
	}
}
