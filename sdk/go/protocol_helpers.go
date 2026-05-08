package gestalt

import (
	"fmt"
	"time"

	gproto "google.golang.org/protobuf/proto"
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

// StructFromAny converts a JSON-compatible map into an optional protobuf Struct.
func StructFromAny(value any) (*structpb.Struct, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return structpb.NewStruct(typed)
	default:
		return nil, fmt.Errorf("expected map[string]any, got %T", value)
	}
}

// MapFromStruct converts a protobuf Struct into its Go map representation.
func MapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

// CloneStruct returns a deep copy of a protobuf Struct.
func CloneStruct(value *structpb.Struct) *structpb.Struct {
	if value == nil {
		return nil
	}
	return gproto.Clone(value).(*structpb.Struct)
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

// ValuesFromMap converts a JSON-compatible map into protobuf Value entries.
func ValuesFromMap(values map[string]any) (map[string]*structpb.Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(values))
	for key, value := range values {
		pbValue, err := structpb.NewValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = pbValue
	}
	return out, nil
}

// MapFromValues converts protobuf Value entries into their Go representation.
func MapFromValues(values map[string]*structpb.Value) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if value == nil {
			out[key] = nil
			continue
		}
		out[key] = value.AsInterface()
	}
	return out
}

// TimestampFromTime converts a Go time into a protobuf Timestamp.
func TimestampFromTime(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value)
}

// TimestampFromTimePtr converts an optional Go time into an optional Timestamp.
func TimestampFromTimePtr(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return TimestampFromTime(*value)
}

// TimeFromTimestamp converts a protobuf Timestamp into a Go time.
func TimeFromTimestamp(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

// TimePtrFromTimestamp converts an optional protobuf Timestamp into an optional time.
func TimePtrFromTimestamp(value *timestamppb.Timestamp) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if err := value.CheckValid(); err != nil {
		return nil, err
	}
	out := value.AsTime()
	return &out, nil
}

// CloneTimestamp returns a deep copy of a protobuf Timestamp.
func CloneTimestamp(value *timestamppb.Timestamp) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return gproto.Clone(value).(*timestamppb.Timestamp)
}
