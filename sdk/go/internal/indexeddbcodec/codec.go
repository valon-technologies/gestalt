package indexeddbcodec

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Record = map[string]any

type KeyPart struct {
	Scalar  any
	Array   []KeyPart
	IsArray bool
}

var timeType = reflect.TypeOf(time.Time{})

const (
	typedValueNullField   protowire.Number = 1
	typedValueStringField protowire.Number = 2
	typedValueIntField    protowire.Number = 3
	typedValueFloatField  protowire.Number = 4
	typedValueBoolField   protowire.Number = 5
	typedValueTimeField   protowire.Number = 6
	typedValueBytesField  protowire.Number = 7
	typedValueJSONField   protowire.Number = 8

	recordFieldsField  protowire.Number = 1
	mapEntryKeyField   protowire.Number = 1
	mapEntryValueField protowire.Number = 2

	keyValueScalarField protowire.Number = 1
	keyValueArrayField  protowire.Number = 2
	keyArrayElement     protowire.Number = 1
)

func NormalizeValue(v any) (any, error) {
	if v == nil {
		return nil, nil
	}

	switch value := v.(type) {
	case time.Time:
		return normalizeTime(value)
	case *time.Time:
		if value == nil {
			return nil, nil
		}
		return normalizeTime(*value)
	case []byte:
		return append([]byte(nil), value...), nil
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return i, nil
		}
		f, err := value.Float64()
		if err != nil {
			return nil, fmt.Errorf("marshal json.Number %q: %w", value, err)
		}
		return f, nil
	}

	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil, nil
	}
	if rv.Type() == timeType {
		return normalizeTime(rv.Interface().(time.Time))
	}

	switch rv.Kind() {
	case reflect.String:
		return rv.String(), nil
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return nil, fmt.Errorf("marshal unsigned integer %d: overflows int64", u)
		}
		return int64(u), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	}

	jsonValue, err := structpb.NewValue(rv.Interface())
	if err != nil {
		return nil, fmt.Errorf("marshal json value: %w", err)
	}
	return jsonValue.AsInterface(), nil
}

func NormalizeValues(values []any) ([]any, error) {
	out := make([]any, len(values))
	for i, value := range values {
		normalized, err := NormalizeValue(value)
		if err != nil {
			return nil, fmt.Errorf("marshal value %d: %w", i, err)
		}
		out[i] = normalized
	}
	return out, nil
}

func NormalizeRecord(record Record) (Record, error) {
	if record == nil {
		return nil, nil
	}
	out := make(Record, len(record))
	for key, value := range record {
		normalized, err := NormalizeValue(value)
		if err != nil {
			return nil, fmt.Errorf("marshal record field %q: %w", key, err)
		}
		out[key] = normalized
	}
	return out, nil
}

func NormalizeKey(value any) (KeyPart, error) {
	if parts, ok := KeyValueArrayParts(value); ok {
		array := make([]KeyPart, len(parts))
		for i, part := range parts {
			normalized, err := NormalizeKey(part)
			if err != nil {
				return KeyPart{}, err
			}
			array[i] = normalized
		}
		return KeyPart{IsArray: true, Array: array}, nil
	}
	scalar, err := NormalizeValue(value)
	if err != nil {
		return KeyPart{}, err
	}
	return KeyPart{Scalar: scalar}, nil
}

func NormalizeKeyRangeValue(value any) (any, error) {
	return NormalizeValue(value)
}

func CursorKeyParts(key any, indexCursor bool) ([]KeyPart, error) {
	if indexCursor {
		if parts, ok := KeyValueArrayParts(key); ok {
			out := make([]KeyPart, len(parts))
			for i, part := range parts {
				normalized, err := NormalizeKey(part)
				if err != nil {
					return nil, err
				}
				out[i] = normalized
			}
			return out, nil
		}
	}

	normalized, err := NormalizeKey(key)
	if err != nil {
		return nil, err
	}
	return []KeyPart{normalized}, nil
}

func KeyPartToAny(part KeyPart) (any, error) {
	if part.IsArray {
		out := make([]any, len(part.Array))
		for i, elem := range part.Array {
			value, err := KeyPartToAny(elem)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	}
	return cloneNativeValue(part.Scalar)
}

func KeyPartsToAny(parts []KeyPart) ([]any, error) {
	out := make([]any, len(parts))
	for i, part := range parts {
		value, err := KeyPartToAny(part)
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	return out, nil
}

func EncodeKey(value any) ([]byte, error) {
	normalized, err := NormalizeKey(value)
	if err != nil {
		return nil, err
	}
	return appendKeyValue(nil, normalized)
}

func DecodeKey(data []byte) (any, error) {
	key, err := decodeKeyValue(data)
	if err != nil {
		return nil, err
	}
	return KeyPartToAny(key)
}

func EncodeRecord(record Record) ([]byte, error) {
	normalized, err := NormalizeRecord(record)
	if err != nil {
		return nil, err
	}
	return appendRecord(nil, normalized)
}

func DecodeRecord(data []byte) (Record, error) {
	return decodeRecord(data)
}

func EncodeIndexValues(values []any) ([]byte, error) {
	normalized, err := NormalizeValues(values)
	if err != nil {
		return nil, err
	}
	record := make(Record, len(normalized))
	for i, value := range normalized {
		record[strconv.Itoa(i)] = value
	}
	return appendRecord(nil, record)
}

func DecodeIndexValues(data []byte, keyParts int) ([]any, error) {
	record, err := DecodeRecord(data)
	if err != nil {
		return nil, err
	}

	out := make([]any, keyParts)
	for i := 0; i < keyParts; i++ {
		value, ok := record[strconv.Itoa(i)]
		if !ok {
			return nil, fmt.Errorf("missing index key part %d", i)
		}
		out[i] = value
	}
	return out, nil
}

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

func appendRecord(out []byte, record Record) ([]byte, error) {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		valueBytes, err := appendTypedValue(nil, record[key])
		if err != nil {
			return nil, fmt.Errorf("marshal record field %q: %w", key, err)
		}

		entry := protowire.AppendTag(nil, mapEntryKeyField, protowire.BytesType)
		entry = protowire.AppendString(entry, key)
		entry = protowire.AppendTag(entry, mapEntryValueField, protowire.BytesType)
		entry = protowire.AppendBytes(entry, valueBytes)

		out = protowire.AppendTag(out, recordFieldsField, protowire.BytesType)
		out = protowire.AppendBytes(out, entry)
	}
	return out, nil
}

func decodeRecord(data []byte) (Record, error) {
	out := Record{}
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("unmarshal record: %w", protowire.ParseError(n))
		}
		data = data[n:]

		if num != recordFieldsField {
			n, err := consumeUnknown(num, typ, data)
			if err != nil {
				return nil, fmt.Errorf("unmarshal record: %w", err)
			}
			data = data[n:]
			continue
		}
		if typ != protowire.BytesType {
			return nil, unexpectedWireType("record fields", num, typ)
		}

		entry, n := protowire.ConsumeBytes(data)
		if n < 0 {
			return nil, fmt.Errorf("unmarshal record field: %w", protowire.ParseError(n))
		}
		data = data[n:]

		key, value, err := decodeMapEntry(entry)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func decodeMapEntry(data []byte) (string, any, error) {
	var key string
	var value any
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return "", nil, fmt.Errorf("unmarshal record field: %w", protowire.ParseError(n))
		}
		data = data[n:]

		switch num {
		case mapEntryKeyField:
			if typ != protowire.BytesType {
				return "", nil, unexpectedWireType("record field key", num, typ)
			}
			decoded, n := protowire.ConsumeString(data)
			if n < 0 {
				return "", nil, fmt.Errorf("unmarshal record field key: %w", protowire.ParseError(n))
			}
			key = decoded
			data = data[n:]
		case mapEntryValueField:
			if typ != protowire.BytesType {
				return "", nil, unexpectedWireType("record field value", num, typ)
			}
			encoded, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return "", nil, fmt.Errorf("unmarshal record field value: %w", protowire.ParseError(n))
			}
			decoded, err := decodeTypedValue(encoded)
			if err != nil {
				return "", nil, fmt.Errorf("unmarshal record field %q: %w", key, err)
			}
			value = decoded
			data = data[n:]
		default:
			n, err := consumeUnknown(num, typ, data)
			if err != nil {
				return "", nil, fmt.Errorf("unmarshal record field: %w", err)
			}
			data = data[n:]
		}
	}
	return key, value, nil
}

func appendKeyValue(out []byte, part KeyPart) ([]byte, error) {
	if part.IsArray {
		var array []byte
		for _, elem := range part.Array {
			encoded, err := appendKeyValue(nil, elem)
			if err != nil {
				return nil, err
			}
			array = protowire.AppendTag(array, keyArrayElement, protowire.BytesType)
			array = protowire.AppendBytes(array, encoded)
		}
		out = protowire.AppendTag(out, keyValueArrayField, protowire.BytesType)
		return protowire.AppendBytes(out, array), nil
	}

	encoded, err := appendTypedValue(nil, part.Scalar)
	if err != nil {
		return nil, err
	}
	out = protowire.AppendTag(out, keyValueScalarField, protowire.BytesType)
	return protowire.AppendBytes(out, encoded), nil
}

func decodeKeyValue(data []byte) (KeyPart, error) {
	var (
		out  KeyPart
		seen bool
	)
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return KeyPart{}, fmt.Errorf("unmarshal key value: %w", protowire.ParseError(n))
		}
		data = data[n:]

		switch num {
		case keyValueScalarField:
			if typ != protowire.BytesType {
				return KeyPart{}, unexpectedWireType("key scalar", num, typ)
			}
			encoded, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return KeyPart{}, fmt.Errorf("unmarshal key scalar: %w", protowire.ParseError(n))
			}
			scalar, err := decodeTypedValue(encoded)
			if err != nil {
				return KeyPart{}, fmt.Errorf("unmarshal key scalar: %w", err)
			}
			out = KeyPart{Scalar: scalar}
			seen = true
			data = data[n:]
		case keyValueArrayField:
			if typ != protowire.BytesType {
				return KeyPart{}, unexpectedWireType("key array", num, typ)
			}
			encoded, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return KeyPart{}, fmt.Errorf("unmarshal key array: %w", protowire.ParseError(n))
			}
			array, err := decodeKeyValueArray(encoded)
			if err != nil {
				return KeyPart{}, err
			}
			out = KeyPart{IsArray: true, Array: array}
			seen = true
			data = data[n:]
		default:
			n, err := consumeUnknown(num, typ, data)
			if err != nil {
				return KeyPart{}, fmt.Errorf("unmarshal key value: %w", err)
			}
			data = data[n:]
		}
	}
	if !seen {
		return KeyPart{}, fmt.Errorf("indexeddb: unsupported key value kind <nil>")
	}
	return out, nil
}

func decodeKeyValueArray(data []byte) ([]KeyPart, error) {
	var out []KeyPart
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("unmarshal key array: %w", protowire.ParseError(n))
		}
		data = data[n:]

		if num != keyArrayElement {
			n, err := consumeUnknown(num, typ, data)
			if err != nil {
				return nil, fmt.Errorf("unmarshal key array: %w", err)
			}
			data = data[n:]
			continue
		}
		if typ != protowire.BytesType {
			return nil, unexpectedWireType("key array element", num, typ)
		}

		encoded, n := protowire.ConsumeBytes(data)
		if n < 0 {
			return nil, fmt.Errorf("unmarshal key array element: %w", protowire.ParseError(n))
		}
		part, err := decodeKeyValue(encoded)
		if err != nil {
			return nil, fmt.Errorf("unmarshal key array element: %w", err)
		}
		out = append(out, part)
		data = data[n:]
	}
	return out, nil
}

func appendTypedValue(out []byte, value any) ([]byte, error) {
	normalized, err := NormalizeValue(value)
	if err != nil {
		return nil, err
	}

	switch v := normalized.(type) {
	case nil:
		out = protowire.AppendTag(out, typedValueNullField, protowire.VarintType)
		return protowire.AppendVarint(out, 0), nil
	case string:
		out = protowire.AppendTag(out, typedValueStringField, protowire.BytesType)
		return protowire.AppendString(out, v), nil
	case int64:
		out = protowire.AppendTag(out, typedValueIntField, protowire.VarintType)
		return protowire.AppendVarint(out, uint64(v)), nil
	case float64:
		out = protowire.AppendTag(out, typedValueFloatField, protowire.Fixed64Type)
		return protowire.AppendFixed64(out, math.Float64bits(v)), nil
	case bool:
		out = protowire.AppendTag(out, typedValueBoolField, protowire.VarintType)
		return protowire.AppendVarint(out, protowire.EncodeBool(v)), nil
	case time.Time:
		timestamp := timestamppb.New(v)
		if err := timestamp.CheckValid(); err != nil {
			return nil, fmt.Errorf("marshal timestamp: %w", err)
		}
		encoded, err := proto.Marshal(timestamp)
		if err != nil {
			return nil, fmt.Errorf("marshal timestamp: %w", err)
		}
		out = protowire.AppendTag(out, typedValueTimeField, protowire.BytesType)
		return protowire.AppendBytes(out, encoded), nil
	case []byte:
		out = protowire.AppendTag(out, typedValueBytesField, protowire.BytesType)
		return protowire.AppendBytes(out, v), nil
	default:
		jsonValue, err := structpb.NewValue(v)
		if err != nil {
			return nil, fmt.Errorf("marshal json value: %w", err)
		}
		encoded, err := proto.Marshal(jsonValue)
		if err != nil {
			return nil, fmt.Errorf("marshal json value: %w", err)
		}
		out = protowire.AppendTag(out, typedValueJSONField, protowire.BytesType)
		return protowire.AppendBytes(out, encoded), nil
	}
}

func decodeTypedValue(data []byte) (any, error) {
	var out any
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("unmarshal typed value: %w", protowire.ParseError(n))
		}
		data = data[n:]

		switch num {
		case typedValueNullField:
			if typ != protowire.VarintType {
				return nil, unexpectedWireType("typed null", num, typ)
			}
			_, n := protowire.ConsumeVarint(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed null: %w", protowire.ParseError(n))
			}
			out = nil
			data = data[n:]
		case typedValueStringField:
			if typ != protowire.BytesType {
				return nil, unexpectedWireType("typed string", num, typ)
			}
			value, n := protowire.ConsumeString(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed string: %w", protowire.ParseError(n))
			}
			out = value
			data = data[n:]
		case typedValueIntField:
			if typ != protowire.VarintType {
				return nil, unexpectedWireType("typed int", num, typ)
			}
			value, n := protowire.ConsumeVarint(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed int: %w", protowire.ParseError(n))
			}
			out = int64(value)
			data = data[n:]
		case typedValueFloatField:
			if typ != protowire.Fixed64Type {
				return nil, unexpectedWireType("typed float", num, typ)
			}
			value, n := protowire.ConsumeFixed64(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed float: %w", protowire.ParseError(n))
			}
			out = math.Float64frombits(value)
			data = data[n:]
		case typedValueBoolField:
			if typ != protowire.VarintType {
				return nil, unexpectedWireType("typed bool", num, typ)
			}
			value, n := protowire.ConsumeVarint(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed bool: %w", protowire.ParseError(n))
			}
			out = protowire.DecodeBool(value)
			data = data[n:]
		case typedValueTimeField:
			if typ != protowire.BytesType {
				return nil, unexpectedWireType("typed timestamp", num, typ)
			}
			encoded, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal timestamp: %w", protowire.ParseError(n))
			}
			timestamp := &timestamppb.Timestamp{}
			if err := proto.Unmarshal(encoded, timestamp); err != nil {
				return nil, fmt.Errorf("unmarshal timestamp: %w", err)
			}
			if err := timestamp.CheckValid(); err != nil {
				return nil, fmt.Errorf("unmarshal timestamp: %w", err)
			}
			out = timestamp.AsTime()
			data = data[n:]
		case typedValueBytesField:
			if typ != protowire.BytesType {
				return nil, unexpectedWireType("typed bytes", num, typ)
			}
			value, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal typed bytes: %w", protowire.ParseError(n))
			}
			out = append([]byte(nil), value...)
			data = data[n:]
		case typedValueJSONField:
			if typ != protowire.BytesType {
				return nil, unexpectedWireType("typed json", num, typ)
			}
			encoded, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil, fmt.Errorf("unmarshal json value: %w", protowire.ParseError(n))
			}
			jsonValue := &structpb.Value{}
			if err := proto.Unmarshal(encoded, jsonValue); err != nil {
				return nil, fmt.Errorf("unmarshal json value: %w", err)
			}
			out = jsonValue.AsInterface()
			data = data[n:]
		default:
			n, err := consumeUnknown(num, typ, data)
			if err != nil {
				return nil, fmt.Errorf("unmarshal typed value: %w", err)
			}
			data = data[n:]
		}
	}
	return out, nil
}

func normalizeTime(value time.Time) (time.Time, error) {
	timestamp := timestamppb.New(value)
	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("marshal timestamp: %w", err)
	}
	return timestamp.AsTime(), nil
}

func cloneNativeValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return append([]byte(nil), v...), nil
	default:
		return NormalizeValue(v)
	}
}

func consumeUnknown(num protowire.Number, typ protowire.Type, data []byte) (int, error) {
	n := protowire.ConsumeFieldValue(num, typ, data)
	if n < 0 {
		return 0, protowire.ParseError(n)
	}
	return n, nil
}

func unexpectedWireType(context string, num protowire.Number, typ protowire.Type) error {
	return fmt.Errorf("unmarshal %s: field %d has wire type %d", context, num, typ)
}
