package rust

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

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

// heckSnake converts CamelCase to snake_case the way prost's heck dependency
// does: word boundaries fall before an uppercase rune that follows a
// lowercase rune or digit, and before the last rune of an all-caps run that
// is followed by a lowercase rune. Wire oneof modules, tonic client modules,
// and tonic method names all use this scheme (TypedValue's oneof lives in
// typed_value, S3ObjectAccess's client in s3_object_access_client), so
// generated references must match it exactly.
func heckSnake(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			capRun := unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || capRun {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// heckUpperCamel converts any proto name to UpperCamelCase the way prost's
// heck dependency does, lowercasing the tail of each word: prost renders
// HTTPSubjectRequest as HttpSubjectRequest and AppInvokeGraphQLRequest as
// AppInvokeGraphQlRequest, so every wire reference must match it exactly.
func heckUpperCamel(s string) string {
	return upperCamel(heckSnake(s))
}

// upperCamel converts a snake_case proto name to UpperCamelCase the way prost
// names oneof variant idents.
func upperCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// rustKeywords are the reserved words that cannot be used as plain
// identifiers; generated field and method names escape them as raw
// identifiers, matching prost (r#ref, r#type).
var rustKeywords = map[string]bool{
	"as": true, "async": true, "await": true, "break": true, "const": true,
	"continue": true, "dyn": true, "else": true, "enum": true, "extern": true,
	"false": true, "fn": true, "for": true, "if": true, "impl": true,
	"in": true, "let": true, "loop": true, "match": true, "mod": true,
	"move": true, "mut": true, "pub": true, "ref": true, "return": true,
	"static": true, "struct": true, "trait": true, "true": true, "type": true,
	"unsafe": true, "use": true, "where": true, "while": true,
}

func escapeIdent(name string) string {
	if rustKeywords[name] {
		return "r#" + name
	}
	return name
}

func scalarType(s model.ScalarType) string {
	switch s {
	case model.ScalarBool:
		return "bool"
	case model.ScalarString:
		return "String"
	case model.ScalarInt32, model.ScalarSint32, model.ScalarSfixed32:
		return "i32"
	case model.ScalarUint32, model.ScalarFixed32:
		return "u32"
	case model.ScalarInt64, model.ScalarSint64, model.ScalarSfixed64:
		return "i64"
	case model.ScalarUint64, model.ScalarFixed64:
		return "u64"
	case model.ScalarFloat:
		return "f32"
	case model.ScalarDouble:
		return "f64"
	default:
		panic(fmt.Sprintf("rust: no type for scalar %d", s))
	}
}

// refType renders the native Rust type for a value-type reference.
func (r *renderer) refType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return scalarType(ref.Scalar)
	case model.KindBytes:
		return "Vec<u8>"
	case model.KindEnum:
		return r.enumType(ref.Enum)
	case model.KindMessage:
		return r.messageType(ref.Message)
	case model.KindJSONStruct:
		return "serde_json::Map<String, serde_json::Value>"
	case model.KindJSONValue:
		return "serde_json::Value"
	case model.KindJSONNull, model.KindUnit:
		return "()"
	case model.KindTimestamp:
		return "std::time::SystemTime"
	case model.KindDuration:
		return "std::time::Duration"
	case model.KindRPCStatus:
		r.useType("RpcStatus")
		return "RpcStatus"
	default:
		panic(fmt.Sprintf("rust: no type for kind %d", ref.Kind))
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

// fieldType renders the native Rust type for a field, excluding presence
// (rendered as Option by the caller).
func (r *renderer) fieldType(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return "Vec<" + r.refType(f.Elem) + ">"
	case model.KindMap:
		return "std::collections::BTreeMap<" + scalarType(f.MapKey) + ", " + r.refType(f.MapValue) + ">"
	default:
		return r.refType(fieldRef(f))
	}
}

func oneofTypeName(message *model.Message, oneof *model.Oneof) string {
	return localName(message.FullName) + upperCamel(oneof.Name)
}

// publicSnake names generated Rust surfaces: heck's scheme with compound
// acronyms kept whole, so InvokeGraphQL becomes invoke_graphql. Wire
// references must keep heckSnake, which matches prost exactly.
func publicSnake(s string) string {
	return heckSnake(strings.ReplaceAll(s, "GraphQL", "Graphql"))
}

func methodConstSuffix(name string) string {
	name = strings.ReplaceAll(name, "GraphQL", "Graphql")
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(name[i-1])
			if prev >= 'a' && prev <= 'z' {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func screamingSnake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return strings.ToUpper(b.String())
}

func toWireFunc(messageFullName string) string {
	return "to_wire_" + publicSnake(localName(messageFullName))
}

func fromWireFunc(messageFullName string) string {
	return "from_wire_" + publicSnake(localName(messageFullName))
}

func oneofToWireFunc(m *model.Message, o *model.Oneof) string {
	return "to_wire_" + heckSnake(oneofTypeName(m, o))
}

func oneofFromWireFunc(m *model.Message, o *model.Oneof) string {
	return "from_wire_" + heckSnake(oneofTypeName(m, o))
}

// wireTypeName returns the prost ident of a message in the v1 wire module,
// e.g. HttpSubjectRequest for HTTPSubjectRequest.
func wireTypeName(fullName string) string {
	return heckUpperCamel(localName(fullName))
}

// wireOneofKind returns the wire path of a oneof's prost enum, e.g.
// v1::typed_value::Kind.
func wireOneofKind(m *model.Message, o *model.Oneof) string {
	return "v1::" + heckSnake(localName(m.FullName)) + "::" + heckUpperCamel(o.Name)
}

// wireClientModule returns the tonic client module for a service, e.g.
// s3_object_access_client. Tonic snake-cases service modules and method
// names with heck, just like prost.
func wireClientModule(svc *model.Service) string {
	return heckSnake(localName(svc.FullName)) + "_client"
}

// wireClientType returns the tonic client type inside wireClientModule, e.g.
// IndexedDbClient.
func wireClientType(svc *model.Service) string {
	return heckUpperCamel(localName(svc.FullName)) + "Client"
}

// wireFieldDefault renders the prost wire default for a field omitted from the
// public native request type.
func wireFieldDefault(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return "Vec::new()"
	case model.KindMap:
		return "Default::default()"
	default:
		ref := fieldRef(f)
		if f.Presence == model.ExplicitPresence {
			return "None"
		}
		switch ref.Kind {
		case model.KindMessage:
			return "None"
		case model.KindEnum:
			return "0"
		case model.KindBytes:
			return "Vec::new()"
		case model.KindScalar:
			switch ref.Scalar {
			case model.ScalarBool:
				return "false"
			case model.ScalarString:
				return "String::new()"
			case model.ScalarFloat, model.ScalarDouble:
				return "0.0"
			default:
				return "0"
			}
		default:
			return "Default::default()"
		}
	}
}

func rustStrSlice(values []string) string {
	if len(values) == 0 {
		return "&[]"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "&[" + strings.Join(parts, ", ") + "]"
}
