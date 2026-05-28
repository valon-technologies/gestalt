package gestalt

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	timeType          = reflect.TypeOf(time.Time{})
)

// structFromMap converts a Go map into a protobuf Struct.
func structFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		value = map[string]any{}
	}
	normalized, err := normalizeJSONObject(value, "struct")
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		return nil, nil
	}
	return structpb.NewStruct(normalized)
}

// structFromAny converts a JSON-compatible object into an optional protobuf Struct.
//
// The input may be nil, a string-keyed map, a struct, or a pointer to one of
// those. Struct fields use their exported field names unless a json tag
// supplies a name; json:"-" skips the field and omitempty skips zero values.
// Anonymous embedded fields must have an explicit json tag name; embedded
// fields are not flattened.
func structFromAny(value any) (*structpb.Struct, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeJSONObject(value, "struct")
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		return nil, nil
	}
	return structpb.NewStruct(normalized)
}

// mapFromStruct converts a protobuf Struct into its Go map representation.
func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

// valueFromAny converts a Go JSON-compatible value into a protobuf Value.
func valueFromAny(value any) (*structpb.Value, error) {
	normalized, err := normalizeJSONValue(reflect.ValueOf(value), "value", map[uintptr]string{})
	if err != nil {
		return nil, err
	}
	return structpb.NewValue(normalized)
}

// anyFromValue converts a protobuf Value into its Go representation.
func anyFromValue(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

// valuesFromMap converts a JSON-compatible map into protobuf Value entries.
func valuesFromMap(values map[string]any) (map[string]*structpb.Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]*structpb.Value, len(values))
	for key, value := range values {
		pbValue, err := valueFromAny(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = pbValue
	}
	return out, nil
}

// mapFromValues converts protobuf Value entries into their Go representation.
func mapFromValues(values map[string]*structpb.Value) map[string]any {
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

// timeFromTimestamp converts a protobuf Timestamp into a Go time.
func timeFromTimestamp(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

// timePtrFromTimestamp converts an optional protobuf Timestamp into an optional time.
func timePtrFromTimestamp(value *timestamppb.Timestamp) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if err := value.CheckValid(); err != nil {
		return nil, err
	}
	out := value.AsTime()
	return &out, nil
}

func timestampFromNonZeroTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timestampFromOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamppb.New(*value)
}

func normalizeJSONObject(value any, path string) (map[string]any, error) {
	normalized, err := normalizeJSONValue(reflect.ValueOf(value), path, map[uintptr]string{})
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		return nil, nil
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected JSON object, got %T", path, value)
	}
	return object, nil
}

func normalizeJSONValue(value reflect.Value, path string, seen map[uintptr]string) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		if value.Kind() == reflect.Pointer {
			ptr := value.Pointer()
			if prior, ok := seen[ptr]; ok {
				return nil, fmt.Errorf("%s: contains cycle through %s", path, prior)
			}
			seen[ptr] = path
			defer delete(seen, ptr)
		}
		value = value.Elem()
	}

	if value.Type() == timeType {
		return nil, fmt.Errorf("%s: time.Time is not JSON-compatible; use timestamp helpers", path)
	}
	if implementsJSONMarshaler(value.Type()) {
		return nil, fmt.Errorf("%s: json.Marshaler values are not accepted in protobuf Struct payloads", path)
	}
	if value.Kind() == reflect.Map || value.Kind() == reflect.Slice {
		if value.IsNil() {
			return nil, nil
		}
		ptr := value.Pointer()
		if ptr != 0 {
			if prior, ok := seen[ptr]; ok {
				return nil, fmt.Errorf("%s: contains cycle through %s", path, prior)
			}
			seen[ptr] = path
			defer delete(seen, ptr)
		}
	}

	switch value.Kind() {
	case reflect.Bool:
		return value.Bool(), nil
	case reflect.String:
		return value.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%s: expected finite number", path)
		}
		return number, nil
	case reflect.Map:
		return normalizeJSONMap(value, path, seen)
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil, nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("%s: []byte is not JSON-compatible", path)
		}
		out := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			item, err := normalizeJSONValue(value.Index(index), fmt.Sprintf("%s[%d]", path, index), seen)
			if err != nil {
				return nil, err
			}
			out[index] = item
		}
		return out, nil
	case reflect.Struct:
		return normalizeJSONStruct(value, path, seen)
	default:
		return nil, fmt.Errorf("%s: unsupported JSON value %s", path, value.Type())
	}
}

func implementsJSONMarshaler(valueType reflect.Type) bool {
	if valueType.Implements(jsonMarshalerType) {
		return true
	}
	return valueType.Kind() != reflect.Pointer && reflect.PointerTo(valueType).Implements(jsonMarshalerType)
}

func normalizeJSONMap(value reflect.Value, path string, seen map[uintptr]string) (map[string]any, error) {
	if value.IsNil() {
		return map[string]any{}, nil
	}
	if value.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("%s: map keys must be strings", path)
	}
	out := make(map[string]any, value.Len())
	for _, key := range value.MapKeys() {
		keyString := key.String()
		item, err := normalizeJSONValue(value.MapIndex(key), path+"."+keyString, seen)
		if err != nil {
			return nil, err
		}
		out[keyString] = item
	}
	return out, nil
}

func normalizeJSONStruct(value reflect.Value, path string, seen map[uintptr]string) (map[string]any, error) {
	out := map[string]any{}
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := valueType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip, explicitName := jsonFieldName(field)
		if skip {
			continue
		}
		if field.Anonymous && !explicitName {
			return nil, fmt.Errorf("%s.%s: anonymous embedded fields are not flattened; add a json tag name", path, field.Name)
		}
		fieldValue := value.Field(index)
		if omitEmpty && isJSONEmptyValue(fieldValue) {
			continue
		}
		item, err := normalizeJSONValue(fieldValue, path+"."+name, seen)
		if err != nil {
			return nil, err
		}
		out[name] = item
	}
	return out, nil
}

func isJSONEmptyValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	default:
		return false
	}
}

func jsonFieldName(field reflect.StructField) (name string, omitEmpty bool, skip bool, explicitName bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true, false
	}
	parts := strings.Split(tag, ",")
	name = strings.TrimSpace(parts[0])
	explicitName = name != ""
	if name == "" {
		name = field.Name
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false, explicitName
}
