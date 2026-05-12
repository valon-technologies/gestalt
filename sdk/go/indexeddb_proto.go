package gestalt

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func typedValueFromAny(v any) (*proto.TypedValue, error) {
	normalized, err := indexeddbcodec.NormalizeValue(v)
	if err != nil {
		return nil, err
	}
	return typedValueFromNormalized(normalized)
}

func typedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	normalized, err := indexeddbcodec.NormalizeValues(values)
	if err != nil {
		return nil, err
	}

	out := make([]*proto.TypedValue, len(normalized))
	for i, value := range normalized {
		pbValue, err := typedValueFromNormalized(value)
		if err != nil {
			return nil, fmt.Errorf("marshal value %d: %w", i, err)
		}
		out[i] = pbValue
	}
	return out, nil
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
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

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	out := make([]any, len(values))
	for i, value := range values {
		goValue, err := anyFromTypedValue(value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal value %d: %w", i, err)
		}
		out[i] = goValue
	}
	return out, nil
}

func recordToProto(record Record) (*proto.Record, error) {
	normalized, err := indexeddbcodec.NormalizeRecord(record)
	if err != nil {
		return nil, err
	}

	fields := make(map[string]*proto.TypedValue, len(normalized))
	for key, value := range normalized {
		pbValue, err := typedValueFromNormalized(value)
		if err != nil {
			return nil, fmt.Errorf("marshal record field %q: %w", key, err)
		}
		fields[key] = pbValue
	}
	return &proto.Record{Fields: fields}, nil
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	out := make([]*proto.Record, len(records))
	for i, record := range records {
		pbRecord, err := recordToProto(record)
		if err != nil {
			return nil, fmt.Errorf("marshal record %d: %w", i, err)
		}
		out[i] = pbRecord
	}
	return out, nil
}

func recordFromProto(record *proto.Record) (Record, error) {
	if record == nil {
		return nil, nil
	}
	fields := record.GetFields()
	out := make(Record, len(fields))
	for key, value := range fields {
		goValue, err := anyFromTypedValue(value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal record field %q: %w", key, err)
		}
		out[key] = goValue
	}
	return out, nil
}

func recordsFromProto(records []*proto.Record) ([]Record, error) {
	out := make([]Record, len(records))
	for i, record := range records {
		goRecord, err := recordFromProto(record)
		if err != nil {
			return nil, fmt.Errorf("unmarshal record %d: %w", i, err)
		}
		out[i] = goRecord
	}
	return out, nil
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	parts := make([]indexeddbcodec.KeyPart, len(kvs))
	for i, kv := range kvs {
		part, err := keyPartFromProto(kv)
		if err != nil {
			return nil, err
		}
		parts[i] = part
	}
	return indexeddbcodec.KeyPartsToAny(parts)
}

func keyValueToAny(kv *proto.KeyValue) (any, error) {
	part, err := keyPartFromProto(kv)
	if err != nil {
		return nil, err
	}
	return indexeddbcodec.KeyPartToAny(part)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	part, err := indexeddbcodec.NormalizeKey(v)
	if err != nil {
		return nil, err
	}
	return keyPartToProto(part)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	parts, err := indexeddbcodec.CursorKeyParts(key, indexCursor)
	if err != nil {
		return nil, err
	}

	out := make([]*proto.KeyValue, len(parts))
	for i, part := range parts {
		pbPart, err := keyPartToProto(part)
		if err != nil {
			return nil, err
		}
		out[i] = pbPart
	}
	return out, nil
}

func typedValueFromNormalized(v any) (*proto.TypedValue, error) {
	switch value := v.(type) {
	case nil:
		return &proto.TypedValue{Kind: &proto.TypedValue_NullValue{NullValue: structpb.NullValue_NULL_VALUE}}, nil
	case string:
		return &proto.TypedValue{Kind: &proto.TypedValue_StringValue{StringValue: value}}, nil
	case int64:
		return &proto.TypedValue{Kind: &proto.TypedValue_IntValue{IntValue: value}}, nil
	case float64:
		return &proto.TypedValue{Kind: &proto.TypedValue_FloatValue{FloatValue: value}}, nil
	case bool:
		return &proto.TypedValue{Kind: &proto.TypedValue_BoolValue{BoolValue: value}}, nil
	case time.Time:
		timestamp := timestamppb.New(value)
		if err := timestamp.CheckValid(); err != nil {
			return nil, fmt.Errorf("marshal timestamp: %w", err)
		}
		return &proto.TypedValue{Kind: &proto.TypedValue_TimeValue{TimeValue: timestamp}}, nil
	case []byte:
		return &proto.TypedValue{Kind: &proto.TypedValue_BytesValue{BytesValue: append([]byte(nil), value...)}}, nil
	default:
		jsonValue, err := structpb.NewValue(value)
		if err != nil {
			return nil, fmt.Errorf("marshal json value: %w", err)
		}
		return &proto.TypedValue{Kind: &proto.TypedValue_JsonValue{JsonValue: jsonValue}}, nil
	}
}

func keyPartToProto(part indexeddbcodec.KeyPart) (*proto.KeyValue, error) {
	if part.IsArray {
		elements := make([]*proto.KeyValue, len(part.Array))
		for i, elem := range part.Array {
			pbElem, err := keyPartToProto(elem)
			if err != nil {
				return nil, err
			}
			elements[i] = pbElem
		}
		return &proto.KeyValue{Kind: &proto.KeyValue_Array{Array: &proto.KeyValueArray{Elements: elements}}}, nil
	}

	scalar, err := typedValueFromNormalized(part.Scalar)
	if err != nil {
		return nil, err
	}
	return &proto.KeyValue{Kind: &proto.KeyValue_Scalar{Scalar: scalar}}, nil
}

func keyPartFromProto(kv *proto.KeyValue) (indexeddbcodec.KeyPart, error) {
	if kv == nil || kv.GetKind() == nil {
		return indexeddbcodec.KeyPart{}, fmt.Errorf("indexeddb: unsupported key value kind <nil>")
	}

	switch value := kv.GetKind().(type) {
	case *proto.KeyValue_Scalar:
		scalar, err := anyFromTypedValue(value.Scalar)
		if err != nil {
			return indexeddbcodec.KeyPart{}, err
		}
		return indexeddbcodec.KeyPart{Scalar: scalar}, nil
	case *proto.KeyValue_Array:
		elements := value.Array.GetElements()
		parts := make([]indexeddbcodec.KeyPart, len(elements))
		for i, elem := range elements {
			part, err := keyPartFromProto(elem)
			if err != nil {
				return indexeddbcodec.KeyPart{}, err
			}
			parts[i] = part
		}
		return indexeddbcodec.KeyPart{IsArray: true, Array: parts}, nil
	default:
		return indexeddbcodec.KeyPart{}, fmt.Errorf("indexeddb: unsupported key value kind %T", value)
	}
}
