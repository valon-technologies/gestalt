package python

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// pythonKeywords mirrors the stdlib keyword.kwlist for Python 3.10+. Proto
// field, oneof, or method names matching a keyword cannot be Python
// identifiers and are renamed with a trailing underscore per PEP 8.
var pythonKeywords = map[string]bool{
	"False": true, "None": true, "True": true,
	"and": true, "as": true, "assert": true, "async": true, "await": true,
	"break": true, "class": true, "continue": true, "def": true, "del": true,
	"elif": true, "else": true, "except": true, "finally": true, "for": true,
	"from": true, "global": true, "if": true, "import": true, "in": true,
	"is": true, "lambda": true, "nonlocal": true, "not": true, "or": true,
	"pass": true, "raise": true, "return": true, "try": true, "while": true,
	"with": true, "yield": true,
}

// pyName returns the generated Python identifier for a proto name, renaming
// keywords with a trailing underscore: "from" becomes "from_". Conversions
// keep addressing the wire field by its true proto name.
func pyName(name string) string {
	if pythonKeywords[name] {
		return name + "_"
	}
	return name
}

// wireFieldExpr renders access to a wire message field. Keyword-named fields
// cannot be attribute expressions, so they go through getattr.
func wireFieldExpr(name string) string {
	if pythonKeywords[name] {
		return fmt.Sprintf("getattr(value, %q)", name)
	}
	return "value." + name
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

// snakeCase converts a PascalCase or camelCase identifier to snake_case,
// keeping acronym runs together: "GetAllKeys" becomes "get_all_keys" and
// "CreateObjectAccessURL" becomes "create_object_access_url". Compound
// acronyms stay one word: "InvokeGraphQL" becomes "invoke_graphql".
func snakeCase(s string) string {
	s = strings.ReplaceAll(s, "GraphQL", "Graphql")
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prevLower := !unicode.IsUpper(runes[i-1]) && runes[i-1] != '_'
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func screamingSnake(s string) string {
	return strings.ToUpper(snakeCase(s))
}

// pascalCase converts a snake_case identifier to PascalCase: "json_value"
// becomes "JsonValue".
func pascalCase(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

func scalarType(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "bool"
	case model.ScalarString:
		return "str"
	case model.ScalarFloat, model.ScalarDouble:
		return "float"
	default:
		return "int"
	}
}

func scalarDefault(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "False"
	case model.ScalarString:
		return `""`
	case model.ScalarFloat, model.ScalarDouble:
		return "0.0"
	default:
		return "0"
	}
}

// mapKeyType renders the Python key type for a proto map key. Proto3 map
// keys are integral, bool, or string, all of which dict keys represent
// natively.
func mapKeyType(s model.ScalarType) string {
	return scalarType(s)
}

// refType renders the native Python type for a value-type reference.
func (r *renderer) refType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return scalarType(ref.Scalar)
	case model.KindBytes:
		return "bytes"
	case model.KindEnum:
		return r.enumType(ref.Enum)
	case model.KindMessage:
		return r.messageType(ref.Message)
	case model.KindJSONStruct:
		r.useType("JsonValue")
		return "dict[str, JsonValue]"
	case model.KindJSONValue:
		r.useType("JsonValue")
		return "JsonValue"
	case model.KindJSONNull:
		return "None"
	case model.KindTimestamp:
		r.features.datetime = true
		return "datetime.datetime"
	case model.KindDuration:
		r.features.datetime = true
		return "datetime.timedelta"
	case model.KindUnit:
		return "None"
	case model.KindRPCStatus:
		r.useType("RpcStatus")
		return "RpcStatus"
	default:
		panic(fmt.Sprintf("python: no type for kind %d", ref.Kind))
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

// fieldType renders the native Python type for a field, excluding presence
// (rendered as a `| None` union by the caller).
func (r *renderer) fieldType(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return "list[" + r.refType(f.Elem) + "]"
	case model.KindMap:
		return "dict[" + mapKeyType(f.MapKey) + ", " + r.refType(f.MapValue) + "]"
	default:
		return r.refType(fieldRef(f))
	}
}

func oneofTypeName(message *model.Message, oneof *model.Oneof) string {
	return localName(message.FullName) + pascalCase(oneof.Name)
}

// variantClassName names the dataclass for one oneof variant:
// TypedValue.string_value becomes TypedValueStringValue. When any variant in
// the oneof would collide with another generated type (KeyValue.kind's array
// variant vs the KeyValueArray message), every variant in the oneof includes
// the oneof name: KeyValueKindArray.
func (r *renderer) variantClassName(m *model.Message, o *model.Oneof, f *model.Field) string {
	if r.oneofQualified(m, o) {
		return oneofTypeName(m, o) + pascalCase(f.Name)
	}
	return localName(m.FullName) + pascalCase(f.Name)
}

func (r *renderer) oneofQualified(m *model.Message, o *model.Oneof) bool {
	for _, f := range oneofFields(m, o) {
		if r.idx.taken[localName(m.FullName)+pascalCase(f.Name)] {
			return true
		}
	}
	return false
}

func enumValuesClassName(fullName string) string {
	return localName(fullName) + "Values"
}

// Converter functions live in the generated _codec package, which is already
// internal by the SDK's underscore convention (like _gen), so their names
// carry no leading underscore.
func toWireFunc(messageFullName string) string {
	return "to_wire_" + snakeCase(localName(messageFullName))
}

func fromWireFunc(messageFullName string) string {
	return "from_wire_" + snakeCase(localName(messageFullName))
}

func pythonStringTuple(values []string) string {
	if len(values) == 0 {
		return "()"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "(" + strings.Join(parts, ", ") + ",)"
}
