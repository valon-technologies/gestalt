package protoutil

import (
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
	return structpb.NewStruct(normalizeStructMap(values))
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
	return v.AsInterface()
}

func ValueFromAny(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	return structpb.NewValue(normalizeStructValue(value))
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

func normalizeStructMap(values map[string]any) map[string]any {
	normalized := make(map[string]any, len(values))
	for key, value := range values {
		normalized[key] = normalizeStructValue(value)
	}
	return normalized
}

func normalizeStructValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if v == nil {
			return nil
		}
		return v.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		return normalizeStructMap(v)
	}

	rv := reflect.ValueOf(value)

	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		if rv.Type().Key().Kind() != reflect.String {
			return value
		}
		normalized := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			normalized[iter.Key().String()] = normalizeStructValue(iter.Value().Interface())
		}
		return normalized
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil
		}
		normalized := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			normalized[i] = normalizeStructValue(rv.Index(i).Interface())
		}
		return normalized
	default:
		return value
	}
}
