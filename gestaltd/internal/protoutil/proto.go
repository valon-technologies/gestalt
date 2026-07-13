package protoutil

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

func StructFromMap(values map[string]any) (*structpb.Struct, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized, err := normalizeStructMap(values)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(normalized)
}

func MapFromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func StringSlicesToProto[V ~map[string][]string](values V) map[string]*proto.StringList {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*proto.StringList, len(values))
	for key, item := range values {
		copied := make([]string, len(item))
		copy(copied, item)
		out[key] = &proto.StringList{Values: copied}
	}
	return out
}

func StringListsFromProto[V ~map[string]*proto.StringList](values V) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string][]string, len(values))
	for key, value := range values {
		out[key] = append([]string(nil), value.GetValues()...)
	}
	return out
}

func ValueToAny(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	return v.AsInterface()
}

func ValueFromAny(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeStructValue(value)
	if err != nil {
		return nil, err
	}
	return structpb.NewValue(normalized)
}

// SetProviderNameIfEmpty sets a provider field on a protobuf request when
// absent. It also supports legacy provider_name requests used by other APIs.
func SetProviderNameIfEmpty(req gproto.Message, name string) {
	name = strings.TrimSpace(name)
	if req == nil || name == "" {
		return
	}
	msg := req.ProtoReflect()
	for _, fieldName := range []protoreflect.Name{"provider", "provider_name"} {
		field := msg.Descriptor().Fields().ByName(fieldName)
		if field == nil || field.Kind() != protoreflect.StringKind {
			continue
		}
		if strings.TrimSpace(msg.Get(field).String()) != "" {
			return
		}
		msg.Set(field, protoreflect.ValueOfString(name))
		return
	}
}

func normalizeStructMap(values map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	for key, value := range values {
		out, err := normalizeStructValue(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		normalized[key] = out
	}
	return normalized, nil
}

func normalizeStructValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano), nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return v.UTC().Format(time.RFC3339Nano), nil
	case map[string]any:
		return normalizeStructMap(v)
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, nil
	}

	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return nil, nil
		}
		if rv.Type().Key().Kind() != reflect.String {
			return value, nil
		}
		normalized := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out, err := normalizeStructValue(iter.Value().Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: %w", iter.Key().String(), err)
			}
			normalized[iter.Key().String()] = out
		}
		return normalized, nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil, nil
		}
		normalized := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out, err := normalizeStructValue(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			normalized[i] = out
		}
		return normalized, nil
	default:
		return value, nil
	}
}
