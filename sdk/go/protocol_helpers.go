package gestalt

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtoMessage is the common interface implemented by generated protobuf messages.
type ProtoMessage = gproto.Message

// Empty aliases google.protobuf.Empty for provider RPC boundary methods.
type Empty = emptypb.Empty

// Struct aliases google.protobuf.Struct for low-level protocol interop.
type Struct = structpb.Struct

// Value aliases google.protobuf.Value for low-level protocol interop.
type Value = structpb.Value

// Timestamp aliases google.protobuf.Timestamp for low-level protocol interop.
type Timestamp = timestamppb.Timestamp

// ProtoJSONMarshalOptions configures MarshalProtoJSON.
type ProtoJSONMarshalOptions struct {
	UseProtoNames     bool
	EmitUnpopulated   bool
	EmitDefaultValues bool
}

// ProtoJSONUnmarshalOptions configures UnmarshalProtoJSON.
type ProtoJSONUnmarshalOptions struct {
	DiscardUnknown bool
}

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	timeType          = reflect.TypeOf(time.Time{})
)

// MarshalProtoDeterministic serializes a protobuf message with deterministic
// map ordering. It is intended for hashes, persistence keys, and wire payloads
// that must remain stable across process runs.
func MarshalProtoDeterministic(msg ProtoMessage) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}
	return gproto.MarshalOptions{Deterministic: true}.Marshal(msg)
}

// UnmarshalProto parses protobuf binary wire data into msg.
func UnmarshalProto(data []byte, msg ProtoMessage) error {
	return gproto.Unmarshal(data, msg)
}

// MarshalProtoJSON serializes a protobuf message using protojson.
//
// Mirrors MarshalProtoDeterministic by returning (nil, nil) for a nil
// message. protojson.MarshalOptions.Marshal would otherwise panic when it
// dereferences the nil interface via m.ProtoReflect().
func MarshalProtoJSON(msg ProtoMessage, options ...ProtoJSONMarshalOptions) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}
	var opt ProtoJSONMarshalOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return (protojson.MarshalOptions{
		UseProtoNames:     opt.UseProtoNames,
		EmitUnpopulated:   opt.EmitUnpopulated,
		EmitDefaultValues: opt.EmitDefaultValues,
	}).Marshal(msg)
}

// UnmarshalProtoJSON parses protojson data into msg.
func UnmarshalProtoJSON(data []byte, msg ProtoMessage, options ...ProtoJSONUnmarshalOptions) error {
	var opt ProtoJSONUnmarshalOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return (protojson.UnmarshalOptions{DiscardUnknown: opt.DiscardUnknown}).Unmarshal(data, msg)
}

// StructFromMap converts a Go map into a protobuf Struct.
func StructFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		value = map[string]any{}
