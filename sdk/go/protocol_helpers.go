package gestalt

import (
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StructFromMap converts a Go map into a protobuf Struct.
func StructFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		value = map[string]any{}
	}
	return structpb.NewStruct(value)
}

// MapFromStruct converts a protobuf Struct into its Go map representation.
func MapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

// ValueFromAny converts a Go JSON-compatible value into a protobuf Value.
func ValueFromAny(value any) (*structpb.Value, error) {
	return structpb.NewValue(value)
}

// AnyFromValue converts a protobuf Value into its Go representation.
func AnyFromValue(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

// TimestampFromTime converts a Go time into a protobuf Timestamp.
func TimestampFromTime(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value)
}

// TimeFromTimestamp converts a protobuf Timestamp into a Go time.
func TimeFromTimestamp(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}
