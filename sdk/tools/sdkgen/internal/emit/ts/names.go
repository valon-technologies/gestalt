package ts

import (
	"fmt"
	"path"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// localName returns the unqualified message or enum name. Names are unique
// within the provider package, so generated identifiers drop the package.
func localName(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

// generatedFileBase derives the generated module base from a proto file path:
// sdk/proto/v1/indexeddb.proto becomes "indexeddb".
func generatedFileBase(protoFile string) string {
	return strings.TrimSuffix(path.Base(protoFile), ".proto")
}

func scalarType(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "boolean"
	case model.ScalarString:
		return "string"
	case model.ScalarInt64, model.ScalarSint64, model.ScalarUint64, model.ScalarSfixed64, model.ScalarFixed64:
		return "bigint"
	default:
		return "number"
	}
}

// mapKeyType renders the TypeScript key type for a proto map key. Proto3 map
// keys are integral, bool, or string; JavaScript object keys make 64-bit and
// bool keys impractical, so they are rendered as strings, matching
// protobuf-es.
func mapKeyType(s model.ScalarType) string {
	switch s {
	case model.ScalarInt32, model.ScalarSint32, model.ScalarUint32, model.ScalarSfixed32, model.ScalarFixed32:
		return "number"
	default:
		return "string"
	}
}

// refType renders the native TypeScript type for a value-type reference.
func (r *renderer) refType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return scalarType(ref.Scalar)
	case model.KindBytes:
		return "Uint8Array"
	case model.KindEnum:
		return r.enumType(ref.Enum)
	case model.KindMessage:
		return r.messageType(ref.Message)
	case model.KindJSONStruct:
		r.features.jsonObject = true
		return "JsonObject"
	case model.KindJSONValue:
		r.features.jsonValue = true
		return "JsonValue"
	case model.KindJSONNull:
		return "null"
	case model.KindTimestamp:
		return "Date"
	case model.KindDuration:
		r.use("DurationMs", true)
		return "DurationMs"
	case model.KindUnit:
		r.use("Unit", true)
		return "Unit"
	case model.KindRPCStatus:
		r.use("RpcStatus", true)
		return "RpcStatus"
	default:
		panic(fmt.Sprintf("ts: no type for kind %d", ref.Kind))
	}
}

func fieldRef(f *model.Field) *model.TypeRef {
	return &model.TypeRef{
		Kind:    f.Kind,
		Scalar:  f.Scalar,
		Message: f.Message,
		Enum:    f.Enum,
	}
}

// fieldType renders the native TypeScript type for a field, excluding
// presence (rendered as an optional property by the caller).
func (r *renderer) fieldType(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return r.refType(f.Elem) + "[]"
	case model.KindMap:
		// An index signature rather than Record<K, V>: proto messages may
		// legitimately be named Record (indexeddb.proto's is), shadowing the
		// TypeScript utility type.
		return "{ [key: " + mapKeyType(f.MapKey) + "]: " + r.refType(f.MapValue) + " }"
	default:
		return r.refType(fieldRef(f))
	}
}

func oneofTypeName(message *model.Message, oneof *model.Oneof) string {
	return localName(message.FullName) + upperFirst(oneof.Name)
}

func toWireFunc(messageFullName string) string {
	return "toWire" + localName(messageFullName)
}

func fromWireFunc(messageFullName string) string {
	return "fromWire" + localName(messageFullName)
}

// tsDefaultExpr renders the proto default value of a non-presence field for
// completing request literals whose signature does not list the field.
func tsDefaultExpr(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return "[]"
	case model.KindMap, model.KindJSONStruct:
		return "{}"
	case model.KindJSONValue:
		return "null"
	case model.KindBytes:
		return "new Uint8Array()"
	case model.KindEnum:
		return "0"
	case model.KindScalar:
		switch f.Scalar {
		case model.ScalarBool:
			return "false"
		case model.ScalarString:
			return `""`
		case model.ScalarInt64, model.ScalarSint64, model.ScalarUint64, model.ScalarSfixed64, model.ScalarFixed64:
			return "0n"
		default:
			return "0"
		}
	default:
		panic(fmt.Sprintf("ts: no default for field kind %d", f.Kind))
	}
}
