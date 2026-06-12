package golang

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

// localName returns the unqualified message or enum name. Names are unique
// within the provider package, so generated identifiers drop the package.
func localName(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

// generatedFileBase derives the generated file base from a proto file path:
// sdk/proto/v1/indexeddb.proto becomes "indexeddb".
func generatedFileBase(protoFile string) string {
	return strings.TrimSuffix(path.Base(protoFile), ".proto")
}

// goTypeName renders the Go identifier for a generated message or enum.
// Generated code lives in its own client package, so proto-local names (which
// are unique within the provider package) map through directly.
func goTypeName(fullName, protoFile string) string {
	_ = protoFile
	return localName(fullName)
}

func goScalarType(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "bool"
	case model.ScalarString:
		return "string"
	case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
		return "int32"
	case model.ScalarUint32, model.ScalarFixed32:
		return "uint32"
	case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
		return "int64"
	case model.ScalarUint64, model.ScalarFixed64:
		return "uint64"
	case model.ScalarFloat:
		return "float32"
	case model.ScalarDouble:
		return "float64"
	default:
		panic(fmt.Sprintf("golang: no type for scalar %d", s))
	}
}

// pascalCase converts a snake_case or SCREAMING_SNAKE name to PascalCase.
func pascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		parts[i] = upperFirst(strings.ToLower(part))
	}
	return strings.Join(parts, "")
}

// screamingSnake converts a PascalCase name to SCREAMING_SNAKE, the prefix
// convention buf lint enforces on enum value names.
func screamingSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// enumValueConst renders the Go constant name for one enum value, stripping
// the conventional enum-name prefix from the proto value name when present.
func enumValueConst(goEnumName, protoEnumName, valueName string) string {
	trimmed := strings.TrimPrefix(valueName, screamingSnake(protoEnumName)+"_")
	return goEnumName + pascalCase(trimmed)
}

// goInitialisms maps camel words whose Go-idiomatic spelling is fully
// capitalized, matching the handwritten sdk/go surface (DefinitionID,
// HostBaseURL, MetadataJSON).
var goInitialisms = map[string]string{
	"Api": "API", "Http": "HTTP", "Https": "HTTPS", "Id": "ID", "Ids": "IDs",
	"Json": "JSON", "Pid": "PID", "Sql": "SQL", "Tls": "TLS", "Ttl": "TTL",
	"Uri": "URI", "Url": "URL", "Uuid": "UUID",
}

// camelWords splits a PascalCase identifier at its word boundaries; digits
// bind to the preceding word.
func camelWords(s string) []string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	return append(words, s[start:])
}

// exportedName renders the exported Go identifier for a camelCase proto JSON
// name, applying the initialism spellings the handwritten SDK uses. Wire-stub
// names are protoc-gen-go's business and never pass through here.
func exportedName(jsonName string) string {
	words := camelWords(upperFirst(jsonName))
	for i, w := range words {
		if mapped, ok := goInitialisms[w]; ok {
			words[i] = mapped
		}
	}
	return strings.Join(words, "")
}

// fieldGoName renders the exported Go field name for a proto field.
func fieldGoName(f *model.Field) string {
	return exportedName(f.JSONName)
}

// wireFieldName renders the protoc-gen-go field name on the wire struct,
// which never applies the SDK initialism table.
func wireFieldName(f *model.Field) string {
	return upperFirst(f.JSONName)
}

// reservedParamNames are identifiers a flattened signature parameter must not
// shadow: Go keywords, the predeclared identifiers the generated bodies rely
// on, and the locals those bodies declare.
var reservedParamNames = map[string]bool{
	// Go keywords.
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	// Predeclared identifiers used by generated bodies.
	"nil": true, "true": true, "false": true, "len": true, "make": true,
	"append": true, "error": true, "string": true, "bool": true, "byte": true,
	// Locals declared by generated method bodies.
	"c": true, "ctx": true, "request": true, "response": true, "err": true,
	"out": true, "entry": true, "frames": true, "frame": true, "ok": true,
	"recvErr": true, "opts": true,
}

// goParamName renders a flattened signature parameter name from a field's
// JSON name, escaping names that would shadow an identifier the generated
// body depends on.
func goParamName(jsonName string) string {
	if reservedParamNames[jsonName] {
		return jsonName + "_"
	}
	return jsonName
}

// zeroScalar renders the zero-value literal of a proto scalar's Go type.
func zeroScalar(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "false"
	case model.ScalarString:
		return `""`
	default:
		return "0"
	}
}

// zeroValueRef renders the zero-value literal of a singular value type, as
// rendered by valueType.
func zeroValueRef(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return zeroScalar(ref.Scalar)
	case model.KindEnum, model.KindDuration:
		return "0"
	case model.KindTimestamp:
		return "time.Time{}"
	default:
		return "nil"
	}
}

// zeroValue renders the zero-value literal of a field's native type, as
// rendered by fieldType: explicit presence renders as a pointer, so its zero
// is nil.
func zeroValue(f *model.Field) string {
	switch f.Kind {
	case model.KindScalar:
		if f.Presence == model.ExplicitPresence {
			return "nil"
		}
		return zeroScalar(f.Scalar)
	case model.KindEnum:
		if f.Presence == model.ExplicitPresence {
			return "nil"
		}
		return "0"
	case model.KindRepeated, model.KindMap, model.KindBytes, model.KindMessage,
		model.KindTimestamp, model.KindDuration, model.KindJSONStruct,
		model.KindJSONValue, model.KindRPCStatus:
		return "nil"
	default:
		panic(fmt.Sprintf("golang: no zero value for kind %d (field %s)", f.Kind, f.Name))
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

// oneofGoName renders the exported Go field name for a oneof.
func oneofGoName(o *model.Oneof) string {
	return pascalCase(o.Name)
}

func oneofTypeName(messageGoName string, o *model.Oneof) string {
	return messageGoName + oneofGoName(o)
}

// variantTypeName renders the wrapper struct name for one oneof variant.
func variantTypeName(oneofUnionName string, f *model.Field) string {
	return oneofUnionName + exportedName(f.JSONName)
}

func oneofFields(m *model.Message, o *model.Oneof) []*model.Field {
	var out []*model.Field
	for _, number := range o.FieldNumbers {
		for _, f := range m.Fields {
			if f.Number == number {
				out = append(out, f)
			}
		}
	}
	return out
}

func toWireFunc(messageGoName string) string {
	return "toWire" + messageGoName
}

func fromWireFunc(messageGoName string) string {
	return "fromWire" + messageGoName
}
