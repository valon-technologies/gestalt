package indexeddbproto

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RecordToProto(record indexeddbcodec.Record) (*proto.Record, error) {
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

func RecordFromProto(record *proto.Record) (indexeddbcodec.Record, error) {
	if record == nil {
		return nil, nil
	}
	fields := record.GetFields()
	out := make(indexeddbcodec.Record, len(fields))
	for key, value := range fields {
		goValue, err := anyFromTypedValue(value)
		if err != nil {
			return nil, fmt.Errorf("unmarshal record field %q: %w", key, err)
		}
		out[key] = goValue
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
