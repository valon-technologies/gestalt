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
// sdk/proto/v1/datastore.proto becomes "datastore".
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

// fieldGoName renders the exported Go field name for a proto field.
func fieldGoName(f *model.Field) string {
	return upperFirst(f.JSONName)
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
	return oneofUnionName + upperFirst(f.JSONName)
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
