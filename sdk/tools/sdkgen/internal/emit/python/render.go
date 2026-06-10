package python

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// maxImportLine is ruff's default line length; from-imports longer than this
// are wrapped exactly as ruff's isort rule expects.
const maxImportLine = 88

// features tracks which imports a generated file needs; the import header is
// assembled after the body renders.
type features struct {
	dataclass bool
	dcField   bool
	datetime  bool
	anyType   bool
	iterable  bool
	iterator  bool
	grpc      bool
	emptyPb   bool
	structPb  bool
	wire      bool
	wireGrpc  bool
	support   map[string]bool
	cross     map[string]map[string]bool // generated file base -> imported names
}

type renderer struct {
	idx      *index
	base     string // generated file base currently being rendered
	features features
	body     strings.Builder
}

func newRenderer(idx *index, base string) *renderer {
	return &renderer{
		idx:  idx,
		base: base,
		features: features{
			support: map[string]bool{},
			cross:   map[string]map[string]bool{},
		},
	}
}

// use records an import from the shared rpc_support module.
func (r *renderer) use(name string) {
	r.features.support[name] = true
}

// crossRef records an import from another generated file and returns the name
// unchanged. References within the current file are not imports.
func (r *renderer) crossRef(protoFile, name string) string {
	base := generatedFileBase(protoFile)
	if base != r.base {
		if r.features.cross[base] == nil {
			r.features.cross[base] = map[string]bool{}
		}
		r.features.cross[base][name] = true
	}
	return name
}

func (r *renderer) messageType(fullName string) string {
	return r.crossRef(r.idx.messages[fullName].ProtoFile, localName(fullName))
}

func (r *renderer) enumType(fullName string) string {
	return r.crossRef(r.idx.enums[fullName].ProtoFile, localName(fullName))
}

// toWireExpr renders the wire-bound conversion of a singular value.
func (r *renderer) toWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindEnum:
		return expr
	case model.KindJSONNull:
		r.features.structPb = true
		return "_struct.NULL_VALUE"
	case model.KindUnit:
		r.features.emptyPb = true
		return "_empty.Empty()"
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, toWireFunc(ref.Message)) + "(" + expr + ")"
	default:
		fn := "to_wire_" + wellKnownSuffix(ref.Kind)
		r.use(fn)
		return fn + "(" + expr + ")"
	}
}

// fromWireExpr renders the native-bound conversion of a singular wire value.
func (r *renderer) fromWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindEnum:
		return expr
	case model.KindJSONNull, model.KindUnit:
		return "None"
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, fromWireFunc(ref.Message)) + "(" + expr + ")"
	default:
		fn := "from_wire_" + wellKnownSuffix(ref.Kind)
		r.use(fn)
		return fn + "(" + expr + ")"
	}
}

// wellKnownSuffix names the rpc_support converter pair for a well-known type.
func wellKnownSuffix(kind model.SemanticKind) string {
	switch kind {
	case model.KindTimestamp:
		return "timestamp"
	case model.KindDuration:
		return "duration"
	case model.KindJSONStruct:
		return "struct"
	case model.KindJSONValue:
		return "value"
	case model.KindRPCStatus:
		return "status"
	default:
		panic(fmt.Sprintf("python: no converter for kind %d", kind))
	}
}

func identityToWire(ref *model.TypeRef) bool {
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes || ref.Kind == model.KindEnum
}

func identityFromWire(ref *model.TypeRef) bool {
	return identityToWire(ref)
}

// fieldToWire renders the conversion of a whole field value. Wire message
// constructors copy repeated and map arguments, so identity elements pass
// the native container through unchanged.
func (r *renderer) fieldToWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityToWire(f.Elem) {
			return expr
		}
		return "[" + r.toWireExpr(f.Elem, "item") + " for item in " + expr + "]"
	case model.KindMap:
		if identityToWire(f.MapValue) {
			return expr
		}
		return "{key: " + r.toWireExpr(f.MapValue, "item") + " for key, item in " + expr + ".items()}"
	default:
		return r.toWireExpr(fieldRef(f), expr)
	}
}

// fieldFromWire renders the conversion of a whole wire field value into the
// native containers the dataclasses declare.
func (r *renderer) fieldFromWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityFromWire(f.Elem) {
			return "list(" + expr + ")"
		}
		return "[" + r.fromWireExpr(f.Elem, "item") + " for item in " + expr + "]"
	case model.KindMap:
		if identityFromWire(f.MapValue) {
			return "dict(" + expr + ")"
		}
		return "{key: " + r.fromWireExpr(f.MapValue, "item") + " for key, item in " + expr + ".items()}"
	default:
		return r.fromWireExpr(fieldRef(f), expr)
	}
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

func oneofToWireFunc(m *model.Message, o *model.Oneof) string {
	return "_to_wire_" + snakeCase(oneofTypeName(m, o))
}

func oneofFromWireFunc(m *model.Message, o *model.Oneof) string {
	return "_from_wire_" + snakeCase(oneofTypeName(m, o))
}

func (r *renderer) renderEnum(e *model.Enum) {
	name := localName(e.FullName)
	r.body.WriteString("# Open enum: unknown numeric values are preserved, so the type is int.\n")
	fmt.Fprintf(&r.body, "%s = int\n\n\n", name)
	fmt.Fprintf(&r.body, "class %s:\n", enumValuesClassName(e.FullName))
	fmt.Fprintf(&r.body, "    \"\"\"Named values for the open %s enum.\"\"\"\n\n", name)
	for _, v := range e.Values {
		fmt.Fprintf(&r.body, "    %s: %s = %d\n", v.Name, name, v.Number)
	}
	r.body.WriteString("\n\n")
}

// fieldDecl renders a dataclass field's type and default expression.
func (r *renderer) fieldDecl(f *model.Field) (string, string) {
	if f.Presence == model.ExplicitPresence {
		return r.fieldType(f) + " | None", "None"
	}
	switch f.Kind {
	case model.KindRepeated:
		r.features.dcField = true
		return r.fieldType(f), "field(default_factory=list)"
	case model.KindMap, model.KindJSONStruct:
		r.features.dcField = true
		return r.fieldType(f), "field(default_factory=dict)"
	case model.KindScalar:
		return r.fieldType(f), scalarDefault(f.Scalar)
	case model.KindBytes:
		return r.fieldType(f), `b""`
	case model.KindEnum:
		return r.fieldType(f), "0"
	case model.KindJSONValue:
		// JsonValue includes None: absent and JSON null conflate.
		return r.fieldType(f), "None"
	default:
		// Message-like fields always report explicit presence in proto3;
		// render any other no-presence field as absent-capable anyway.
		return r.fieldType(f) + " | None", "None"
	}
}

func (r *renderer) renderMessage(m *model.Message) {
	name := localName(m.FullName)
	r.features.dataclass = true

	for _, o := range m.Oneofs {
		for _, f := range oneofFields(m, o) {
			fmt.Fprintf(&r.body, "@dataclass(frozen=True, slots=True)\nclass %s:\n", r.variantClassName(m, o, f))
			if f.Kind == model.KindJSONNull || f.Kind == model.KindUnit {
				r.body.WriteString("    pass\n\n\n")
			} else {
				fmt.Fprintf(&r.body, "    value: %s\n\n\n", r.fieldType(f))
			}
		}
		fmt.Fprintf(&r.body, "%s = (\n", oneofTypeName(m, o))
		for i, f := range oneofFields(m, o) {
			if i == 0 {
				fmt.Fprintf(&r.body, "    %s\n", r.variantClassName(m, o, f))
			} else {
				fmt.Fprintf(&r.body, "    | %s\n", r.variantClassName(m, o, f))
			}
		}
		r.body.WriteString("    | None\n)\n\n\n")
	}

	fmt.Fprintf(&r.body, "@dataclass(frozen=True, slots=True)\nclass %s:\n", name)
	if len(m.Fields) == 0 {
		r.body.WriteString("    pass\n")
	}
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		typ, def := r.fieldDecl(f)
		fmt.Fprintf(&r.body, "    %s: %s = %s\n", pyName(f.Name), typ, def)
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "    %s: %s = None\n", pyName(o.Name), oneofTypeName(m, o))
	}
	r.body.WriteString("\n\n")
}

func (r *renderer) renderConversions(m *model.Message) {
	name := localName(m.FullName)
	r.features.anyType = true
	r.features.wire = true

	// vulture reports unused parameters, so empty messages take an
	// underscore-prefixed (ignored) parameter.
	param := "value"
	if len(m.Fields) == 0 {
		param = "_value"
	}

	fmt.Fprintf(&r.body, "def %s(%s: %s) -> Any:\n", toWireFunc(m.FullName), param, name)
	if len(m.Fields) == 0 {
		fmt.Fprintf(&r.body, "    return _wire.%s()\n\n\n", name)
	} else {
		fmt.Fprintf(&r.body, "    return _wire.%s(\n", name)
		for _, f := range m.Fields {
			if f.OneofIndex >= 0 {
				continue
			}
			expr := "value." + pyName(f.Name)
			conv := r.fieldToWire(f, expr)
			if f.Presence == model.ExplicitPresence && conv != expr {
				conv = "None if " + expr + " is None else " + conv
			}
			if pythonKeywords[f.Name] {
				// pb2 constructors cannot take a keyword-named kwarg directly.
				fmt.Fprintf(&r.body, "        **{%q: %s},\n", f.Name, conv)
			} else {
				fmt.Fprintf(&r.body, "        %s=%s,\n", f.Name, conv)
			}
		}
		for _, o := range m.Oneofs {
			fmt.Fprintf(&r.body, "        **%s(value.%s),\n", oneofToWireFunc(m, o), pyName(o.Name))
		}
		r.body.WriteString("    )\n\n\n")
	}

	fmt.Fprintf(&r.body, "def %s(%s: Any) -> %s:\n", fromWireFunc(m.FullName), param, name)
	if len(m.Fields) == 0 {
		fmt.Fprintf(&r.body, "    return %s()\n\n\n", name)
	} else {
		fmt.Fprintf(&r.body, "    return %s(\n", name)
		for _, f := range m.Fields {
			if f.OneofIndex >= 0 {
				continue
			}
			conv := r.fieldFromWire(f, wireFieldExpr(f.Name))
			if f.Presence == model.ExplicitPresence {
				fmt.Fprintf(&r.body, "        %s=%s if value.HasField(%q) else None,\n", pyName(f.Name), conv, f.Name)
			} else {
				fmt.Fprintf(&r.body, "        %s=%s,\n", pyName(f.Name), conv)
			}
		}
		for _, o := range m.Oneofs {
			fmt.Fprintf(&r.body, "        %s=%s(value),\n", pyName(o.Name), oneofFromWireFunc(m, o))
		}
		r.body.WriteString("    )\n\n\n")
	}

	for _, o := range m.Oneofs {
		r.renderOneofConverters(m, o)
	}
}

func (r *renderer) renderOneofConverters(m *model.Message, o *model.Oneof) {
	unionName := oneofTypeName(m, o)
	r.features.anyType = true

	fmt.Fprintf(&r.body, "def %s(value: %s) -> dict[str, Any]:\n", oneofToWireFunc(m, o), unionName)
	for _, f := range oneofFields(m, o) {
		fmt.Fprintf(&r.body, "    if isinstance(value, %s):\n", r.variantClassName(m, o, f))
		switch f.Kind {
		case model.KindJSONNull, model.KindUnit:
			fmt.Fprintf(&r.body, "        return {%q: %s}\n", f.Name, r.toWireExpr(fieldRef(f), ""))
		default:
			fmt.Fprintf(&r.body, "        return {%q: %s}\n", f.Name, r.toWireExpr(fieldRef(f), "value.value"))
		}
	}
	r.body.WriteString("    return {}\n\n\n")

	fmt.Fprintf(&r.body, "def %s(value: Any) -> %s:\n", oneofFromWireFunc(m, o), unionName)
	fmt.Fprintf(&r.body, "    case = value.WhichOneof(%q)\n", o.Name)
	for _, f := range oneofFields(m, o) {
		fmt.Fprintf(&r.body, "    if case == %q:\n", f.Name)
		switch f.Kind {
		case model.KindJSONNull, model.KindUnit:
			fmt.Fprintf(&r.body, "        return %s()\n", r.variantClassName(m, o, f))
		default:
			fmt.Fprintf(&r.body, "        return %s(value=%s)\n", r.variantClassName(m, o, f), r.fromWireExpr(fieldRef(f), wireFieldExpr(f.Name)))
		}
	}
	r.body.WriteString("    return None\n\n\n")
}

func (r *renderer) renderClient(svc *model.Service) {
	name := localName(svc.FullName)
	r.features.grpc = true
	r.features.wireGrpc = true
	r.features.anyType = true

	fmt.Fprintf(&r.body, "class %s:\n", name)
	fmt.Fprintf(&r.body, "    \"\"\"Client for the %s service.\"\"\"\n\n", svc.FullName)
	r.body.WriteString("    def __init__(self, channel: grpc.Channel) -> None:\n")
	fmt.Fprintf(&r.body, "        self._stub = _wire_grpc.%sStub(channel)\n\n", name)
	for _, method := range svc.Methods {
		r.renderMethod(method)
	}
	r.body.WriteString("\n")
}

func (r *renderer) renderMethod(m *model.Method) {
	methodName := pyName(snakeCase(m.Name))
	requestParam := ""
	requestArg := "_empty.Empty()"
	if m.InputIsEmpty {
		r.features.emptyPb = true
	} else {
		requestType := r.messageType(m.Input.FullName)
		switch m.Stream {
		case model.ClientStream, model.Bidi:
			r.features.iterable = true
			r.use("map_send")
			requestParam = ", requests: Iterable[" + requestType + "]"
			requestArg = "map_send(requests, " + r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + ")"
		default:
			requestParam = ", request: " + requestType
			requestArg = r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		}
	}

	stubCall := fmt.Sprintf("self._stub.%s(%s)", m.Name, requestArg)
	switch m.Stream {
	case model.ServerStream, model.Bidi:
		responseType := r.messageType(m.Output.FullName)
		r.features.iterator = true
		r.use("map_recv")
		fmt.Fprintf(&r.body, "    def %s(self%s) -> Iterator[%s]:\n", methodName, requestParam, responseType)
		fmt.Fprintf(&r.body, "        return map_recv(%s, %s)\n\n", stubCall, r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	default:
		r.use("call_unary")
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "    def %s(self%s) -> None:\n", methodName, requestParam)
			fmt.Fprintf(&r.body, "        call_unary(lambda: %s)\n\n", stubCall)
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, requestParam, responseType)
			fmt.Fprintf(&r.body, "        response = call_unary(lambda: %s)\n", stubCall)
			fmt.Fprintf(&r.body, "        return %s(response)\n\n", r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
		}
	}
}

// fromImport renders one from-import, wrapping exactly as ruff's isort rule
// formats lines longer than the default line length.
func fromImport(module string, names []string) string {
	single := "from " + module + " import " + strings.Join(names, ", ")
	if len(single) <= maxImportLine {
		return single + "\n"
	}
	var b strings.Builder
	b.WriteString("from " + module + " import (\n")
	for _, name := range names {
		b.WriteString("    " + name + ",\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// assemble prepends the docstring, import header, and wire-module aliases
// derived from the rendered body.
func (r *renderer) assemble() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"Generated provider SDK surface for %s.proto.\"\"\"\n\n", r.base)
	b.WriteString("from __future__ import annotations\n")

	var stdlib []string
	if r.features.datetime {
		stdlib = append(stdlib, "import datetime\n")
	}
	if r.features.dataclass {
		names := []string{"dataclass"}
		if r.features.dcField {
			names = append(names, "field")
		}
		stdlib = append(stdlib, fromImport("dataclasses", names))
	}
	var typingNames []string
	if r.features.anyType {
		typingNames = append(typingNames, "Any")
	}
	if r.features.iterable {
		typingNames = append(typingNames, "Iterable")
	}
	if r.features.iterator {
		typingNames = append(typingNames, "Iterator")
	}
	if len(typingNames) > 0 {
		stdlib = append(stdlib, fromImport("typing", typingNames))
	}
	if len(stdlib) > 0 {
		b.WriteString("\n")
		for _, line := range stdlib {
			b.WriteString(line)
		}
	}

	var thirdParty []string
	if r.features.grpc {
		thirdParty = append(thirdParty, "import grpc\n")
	}
	if r.features.emptyPb {
		thirdParty = append(thirdParty, "from google.protobuf import empty_pb2 as _empty_pb2\n")
	}
	if r.features.structPb {
		thirdParty = append(thirdParty, "from google.protobuf import struct_pb2 as _struct_pb2\n")
	}
	if len(thirdParty) > 0 {
		b.WriteString("\n")
		for _, line := range thirdParty {
			b.WriteString(line)
		}
	}

	b.WriteString("\n")
	if r.features.wire {
		fmt.Fprintf(&b, "from ._gen.v1 import %s_pb2 as _%s_pb2\n", r.base, r.base)
	}
	if r.features.wireGrpc {
		fmt.Fprintf(&b, "from ._gen.v1 import %s_pb2_grpc as _%s_pb2_grpc\n", r.base, r.base)
	}
	type localImport struct {
		module string
		names  []string
	}
	var locals []localImport
	for base, names := range r.features.cross {
		locals = append(locals, localImport{module: "." + base, names: sortedKeys(names)})
	}
	if len(r.features.support) > 0 {
		locals = append(locals, localImport{module: ".rpc_support", names: sortedKeys(r.features.support)})
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].module < locals[j].module })
	for _, imp := range locals {
		b.WriteString(fromImport(imp.module, imp.names))
	}

	var aliases []string
	if r.features.emptyPb {
		aliases = append(aliases, "_empty: Any = _empty_pb2\n")
	}
	if r.features.structPb {
		aliases = append(aliases, "_struct: Any = _struct_pb2\n")
	}
	if r.features.wire {
		aliases = append(aliases, fmt.Sprintf("_wire: Any = _%s_pb2\n", r.base))
	}
	if r.features.wireGrpc {
		aliases = append(aliases, fmt.Sprintf("_wire_grpc: Any = _%s_pb2_grpc\n", r.base))
	}
	if len(aliases) > 0 {
		b.WriteString("\n")
		for _, line := range aliases {
			b.WriteString(line)
		}
	}

	b.WriteString("\n\n")
	b.WriteString(r.body.String())
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
