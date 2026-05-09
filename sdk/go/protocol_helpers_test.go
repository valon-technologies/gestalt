package gestalt

import (
	"bytes"
	"testing"
)

func TestProtoBoundaryHelpers(t *testing.T) {
	var _ ProtoMessage = (*Empty)(nil)

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
