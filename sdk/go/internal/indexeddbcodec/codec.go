package indexeddbcodec

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Record is the JSON-like value stored in an object store row.
type Record = map[string]any

var timeType = reflect.TypeOf(time.Time{})

// TypedValueFromAny converts a native value to its wire TypedValue.
func TypedValueFromAny(v any) (*proto.TypedValue, error) {
	if v == nil {
		return &proto.TypedValue{
			Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE},
		}, nil
	}

	switch value := v.(type) {
	case time.Time:
		return timestampToTypedValue(value)
	case *time.Time:
		if value == nil {
			return &proto.TypedValue{
				Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE},
			}, nil
		}
		return timestampToTypedValue(*value)
	case []byte:
		return &proto.TypedValue{
			Kind: &proto.TypedValue_BytesValue{BytesValue: append([]byte(nil), value...)},
		}, nil
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return &proto.TypedValue{Kind: &proto.TypedValue_IntValue{IntValue: i}}, nil
		}
		f, err := value.Float64()
		if err != nil {
			return nil, fmt.Errorf("marshal json.Number %q: %w", value, err)
		}
		return &proto.TypedValue{Kind: &proto.TypedValue_FloatValue{FloatValue: f}}, nil
	}

	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return &proto.TypedValue{
				Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE},
			}, nil
		}
		if rv.Type() == reflect.TypeOf(&time.Time{}) {
			ts := rv.Interface().(*time.Time)
			return timestampToTypedValue(*ts)
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return &proto.TypedValue{
			Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE},
		}, nil
	}
	if rv.Type() == timeType {
		return timestampToTypedValue(rv.Interface().(time.Time))
	}

	switch rv.Kind() {
	case reflect.String:
		return &proto.TypedValue{Kind: &proto.TypedValue_StringValue{StringValue: rv.String()}}, nil
	case reflect.Bool:
		return &proto.TypedValue{Kind: &proto.TypedValue_BoolValue{BoolValue: rv.Bool()}}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &proto.TypedValue{Kind: &proto.TypedValue_IntValue{IntValue: rv.Int()}}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return nil, fmt.Errorf("marshal unsigned integer %d: overflows int64", u)
		}
		return &proto.TypedValue{Kind: &proto.TypedValue_IntValue{IntValue: int64(u)}}, nil
	case reflect.Float32, reflect.Float64:
		return &proto.TypedValue{Kind: &proto.TypedValue_FloatValue{FloatValue: rv.Float()}}, nil
	}

	jsonValue, err := structpb.NewValue(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json value: %w", err)
	}
	return &proto.TypedValue{Kind: &proto.TypedValue_JsonValue{JsonValue: jsonValue}}, nil
}

// AnyFromTypedValue converts a wire TypedValue to its native value.
func AnyFromTypedValue(v *proto.TypedValue) (any, error) {
	if v == nil || v.GetKind() == nil {
		return nil, nil
	}

	switch kind := v.GetKind().(type) {
	case *proto.TypedValue_NullValue:
		return nil, nil
	case *proto.TypedValue_StringValue:
		return kind.StringValue, nil
	case *proto.TypedValue_IntValue:
		return kind.IntValue, nil
	case *proto.TypedValue_FloatValue:
		return kind.FloatValue, nil
	case *proto.TypedValue_BoolValue:
		return kind.BoolValue, nil
	case *proto.TypedValue_TimeValue:
		if kind.TimeValue == nil {
			return nil, nil
		}
		if err := kind.TimeValue.CheckValid(); err != nil {
			return nil, fmt.Errorf("unmarshal timestamp: %w", err)
		}
		return kind.TimeValue.AsTime(), nil
	case *proto.TypedValue_BytesValue:
		return append([]byte(nil), kind.BytesValue...), nil
	case *proto.TypedValue_JsonValue:
		if kind.JsonValue == nil {
			return nil, nil
		}
		return kind.JsonValue.AsInterface(), nil
	default:
		return nil, fmt.Errorf("unmarshal typed value: unsupported kind %T", kind)
	}
}

// AnyFromTypedValues converts wire TypedValues to native values.
func AnyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	out := make([]any, len(values))
	for i, value := range values {
		goValue, err := AnyFromTypedValue(value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal value %d: %w", i, err)
		}
		out[i] = goValue
	}
	return out, nil
}

// RecordToProto converts a native record to its wire form.
func RecordToProto(record Record) (*proto.Record, error) {
	fields := make(map[string]*proto.TypedValue, len(record))
	for key, value := range record {
		pbValue, err := TypedValueFromAny(value)
		if err != nil {
			return nil, fmt.Errorf("marshal record field %q: %w", key, err)
		}
		fields[key] = pbValue
	}
	return &proto.Record{Fields: fields}, nil
}

// RecordFromProto converts a wire record to its native form.
func RecordFromProto(record *proto.Record) (Record, error) {
	if record == nil {
		return nil, nil
	}
	fields := record.GetFields()
	out := make(Record, len(fields))
	for key, value := range fields {
		goValue, err := AnyFromTypedValue(value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal record field %q: %w", key, err)
		}
		out[key] = goValue
	}
	return out, nil
}

// RecordsFromProto converts wire records to native records.
func RecordsFromProto(records []*proto.Record) ([]Record, error) {
	out := make([]Record, len(records))
	for i, record := range records {
		goRecord, err := RecordFromProto(record)
		if err != nil {
			return nil, fmt.Errorf("unmarshal record %d: %w", i, err)
		}
		out[i] = goRecord
	}
	return out, nil
}

// RecordsToProto converts native records to wire records.
func RecordsToProto(records []Record) ([]*proto.Record, error) {
	out := make([]*proto.Record, len(records))
	for i, record := range records {
		pbRecord, err := RecordToProto(record)
		if err != nil {
			return nil, fmt.Errorf("marshal record %d: %w", i, err)
		}
		out[i] = pbRecord
	}
	return out, nil
}

// KeyValuesToAny converts wire key values to native key parts.
func KeyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	parts := make([]any, len(kvs))
	for i, kv := range kvs {
		part, err := KeyValueToAny(kv)
		if err != nil {
			return nil, err
		}
		parts[i] = part
	}
	return parts, nil
}

// KeyValueToAny converts one wire key value to its native key part.
func KeyValueToAny(kv *proto.KeyValue) (any, error) {
	switch v := kv.GetKind().(type) {
	case *proto.KeyValue_Scalar:
		return AnyFromTypedValue(v.Scalar)
	case *proto.KeyValue_Array:
		return KeyValuesToAny(v.Array.GetElements())
	default:
		return nil, fmt.Errorf("indexeddb: unsupported key value kind %T", v)
	}
}

// AnyToKeyValue converts one native key part to its wire key value.
func AnyToKeyValue(v any) (*proto.KeyValue, error) {
	if arr, ok := KeyValueArrayParts(v); ok {
		elems := make([]*proto.KeyValue, len(arr))
		for i, elem := range arr {
			kv, err := AnyToKeyValue(elem)
			if err != nil {
				return nil, err
			}
			elems[i] = kv
		}
		return &proto.KeyValue{Kind: &proto.KeyValue_Array{Array: &proto.KeyValueArray{Elements: elems}}}, nil
	}
	tv, err := TypedValueFromAny(v)
	if err != nil {
		return nil, err
	}
	return &proto.KeyValue{Kind: &proto.KeyValue_Scalar{Scalar: tv}}, nil
}

// CursorKeyToProto converts a native cursor key to its wire KeyValue.
func CursorKeyToProto(key any) (*proto.KeyValue, error) {
	return AnyToKeyValue(key)
}

// EncodeKey serializes a native key for storage.
func EncodeKey(value any) ([]byte, error) {
	kv, err := AnyToKeyValue(value)
	if err != nil {
		return nil, err
	}
	return gproto.Marshal(kv)
}

// DecodeKey deserializes a stored key to its native value.
func DecodeKey(data []byte) (any, error) {
	kv := &proto.KeyValue{}
	if err := gproto.Unmarshal(data, kv); err != nil {
		return nil, err
	}
	return KeyValueToAny(kv)
}

// EncodeRecord serializes a native record for storage.
func EncodeRecord(record Record) ([]byte, error) {
	pbRecord, err := RecordToProto(record)
	if err != nil {
		return nil, err
	}
	return gproto.Marshal(pbRecord)
}

// DecodeRecord deserializes a stored record to its native form.
func DecodeRecord(data []byte) (Record, error) {
	pbRecord := &proto.Record{}
	if err := gproto.Unmarshal(data, pbRecord); err != nil {
		return nil, err
	}
	return RecordFromProto(pbRecord)
}

// KeyValueArrayParts reports a native composite key's parts when the value
// is an array-shaped key.
func KeyValueArrayParts(v any) ([]any, bool) {
	if arr, ok := v.([]any); ok {
		return append([]any(nil), arr...), true
	}
	if _, ok := v.([]byte); ok {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, false
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return nil, false
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	parts := make([]any, rv.Len())
	for i := range parts {
		parts[i] = rv.Index(i).Interface()
	}
	return parts, true
}

func timestampToTypedValue(value time.Time) (*proto.TypedValue, error) {
	timestamp := timestamppb.New(value)
	if err := timestamp.CheckValid(); err != nil {
		return nil, fmt.Errorf("marshal timestamp: %w", err)
	}
	return &proto.TypedValue{Kind: &proto.TypedValue_TimeValue{TimeValue: timestamp}}, nil
}
