package golang

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// wireImport is the wire-stub package generated files convert through,
// aliased proto to match the handwritten sdk/go transport code.
const wireImport = `proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"`

// features tracks which imports a generated file needs; the import header is
// assembled after the body renders.
type features struct {
	context     bool
	errors      bool
	io          bool
	time        bool
	host        bool
	proto       bool
	rpcstatus   bool
	grpc        bool
	durationpb  bool
	emptypb     bool
	structpb    bool
	timestamppb bool
}

type renderer struct {
	idx          *index
	publicClient bool
	features     features
	body         strings.Builder
}

func newRenderer(idx *index) *renderer {
	return &renderer{idx: idx}
}

func newPublicRenderer(idx *index) *renderer {
	return &renderer{idx: idx, publicClient: true}
}

func (r *renderer) messageType(fullName string) string {
	return goTypeName(fullName, r.idx.messages[fullName].ProtoFile)
}

func (r *renderer) enumType(fullName string) string {
	return goTypeName(fullName, r.idx.enums[fullName].ProtoFile)
}

// wireMessage renders the wire-stub type name for a message; renames never
// apply to the wire package.
func wireMessage(fullName string) string {
	return "proto." + localName(fullName)
}

func wireEnum(fullName string) string {
	return "proto." + localName(fullName)
}

// valueType renders the native Go type of a singular value: a repeated
// element, a map value, or a oneof variant payload. Values are always set, so
// nothing is pointer-wrapped except messages.
func (r *renderer) valueType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return goScalarType(ref.Scalar)
	case model.KindBytes:
		return "[]byte"
	case model.KindEnum:
		return r.enumType(ref.Enum)
	case model.KindMessage:
		return "*" + r.messageType(ref.Message)
	case model.KindTimestamp:
		r.features.time = true
		return "time.Time"
	case model.KindDuration:
		r.features.time = true
		return "time.Duration"
	case model.KindJSONStruct:
		return "map[string]any"
	case model.KindJSONValue:
		return "any"
	case model.KindRPCStatus:
		return "*RpcStatus"
	default:
		panic(fmt.Sprintf("golang: no value type for kind %d", ref.Kind))
	}
}

// fieldType renders the native Go type for a non-oneof field, including
// presence: explicit presence renders as a pointer (or as nil for the types
// whose natural Go representation already distinguishes absence).
func (r *renderer) fieldType(f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return "[]" + r.valueType(f.Elem)
	case model.KindMap:
		return "map[" + goScalarType(f.MapKey) + "]" + r.valueType(f.MapValue)
	case model.KindScalar:
		if f.Presence == model.ExplicitPresence {
			return "*" + goScalarType(f.Scalar)
		}
		return goScalarType(f.Scalar)
	case model.KindBytes:
		return "[]byte"
	case model.KindEnum:
		if f.Presence == model.ExplicitPresence {
			return "*" + r.enumType(f.Enum)
		}
		return r.enumType(f.Enum)
	case model.KindMessage:
		return "*" + r.messageType(f.Message)
	case model.KindTimestamp:
		r.features.time = true
		return "*time.Time"
	case model.KindDuration:
		r.features.time = true
		return "*time.Duration"
	case model.KindJSONStruct:
		return "map[string]any"
	case model.KindJSONValue:
		return "any"
	case model.KindRPCStatus:
		return "*RpcStatus"
	default:
		panic(fmt.Sprintf("golang: no field type for kind %d (field %s)", f.Kind, f.Name))
	}
}

// wireValueType renders the wire-stub type of a map value, needed to size
// converted maps.
func (r *renderer) wireValueType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return goScalarType(ref.Scalar)
	case model.KindBytes:
		return "[]byte"
	case model.KindEnum:
		return wireEnum(ref.Enum)
	case model.KindMessage:
		return "*" + wireMessage(ref.Message)
	case model.KindTimestamp:
		r.features.timestamppb = true
		return "*timestamppb.Timestamp"
	case model.KindDuration:
		r.features.durationpb = true
		return "*durationpb.Duration"
	case model.KindJSONStruct:
		r.features.structpb = true
		return "*structpb.Struct"
	case model.KindJSONValue:
		r.features.structpb = true
		return "*structpb.Value"
	case model.KindRPCStatus:
		r.features.rpcstatus = true
		return "*rpcstatus.Status"
	default:
		panic(fmt.Sprintf("golang: no wire value type for kind %d", ref.Kind))
	}
}

// valueToWire renders the wire-bound conversion of a singular set value.
func (r *renderer) valueToWire(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes:
		return expr
	case model.KindEnum:
		return wireEnum(ref.Enum) + "(" + expr + ")"
	case model.KindMessage:
		return toWireFunc(r.messageType(ref.Message)) + "(" + expr + ")"
	case model.KindTimestamp:
		r.features.timestamppb = true
		return "timestamppb.New(" + expr + ")"
	case model.KindDuration:
		r.features.durationpb = true
		return "durationpb.New(" + expr + ")"
	case model.KindJSONStruct:
		return "toWireStruct(" + expr + ")"
	case model.KindJSONValue:
		return "toWireValue(" + expr + ")"
	case model.KindRPCStatus:
		return "toWireStatus(" + expr + ")"
	default:
		panic(fmt.Sprintf("golang: no wire conversion for kind %d", ref.Kind))
	}
}

// valueFromWire renders the native-bound conversion of a singular set wire
// value.
func (r *renderer) valueFromWire(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes:
		return expr
	case model.KindEnum:
		return r.enumType(ref.Enum) + "(" + expr + ")"
	case model.KindMessage:
		return fromWireFunc(r.messageType(ref.Message)) + "(" + expr + ")"
	case model.KindTimestamp:
		return expr + ".AsTime()"
	case model.KindDuration:
		return expr + ".AsDuration()"
	case model.KindJSONStruct:
		return "fromWireStruct(" + expr + ")"
	case model.KindJSONValue:
		return "fromWireValue(" + expr + ")"
	case model.KindRPCStatus:
		return "fromWireStatus(" + expr + ")"
	default:
		panic(fmt.Sprintf("golang: no native conversion for kind %d", ref.Kind))
	}
}

func identityValue(ref *model.TypeRef) bool {
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes
}

// writeDoc renders a normalized proto doc comment as consecutive // lines at
// the given indent. Lines pass through as-is: godoc needs no escaping.
func (r *renderer) writeDoc(indent, doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		if line == "" {
			r.body.WriteString(indent + "//\n")
		} else {
			r.body.WriteString(indent + "// " + line + "\n")
		}
	}
}

// writeDocPara renders a proto doc comment followed by a blank comment line,
// so generated provenance lines read as a separate godoc paragraph.
// writeIdentDoc renders an identifier-first synthetic doc line and, when the
// proto carries a comment, the proto doc as a following paragraph — so every
// exported symbol's comment satisfies the godoc naming form regardless of
// how the proto comment is phrased.
func (r *renderer) writeIdentDoc(indent, synthetic, doc string) {
	r.writeDoc(indent, synthetic)
	if doc != "" {
		r.body.WriteString(indent + "//\n")
		r.writeDoc(indent, doc)
	}
}

func (r *renderer) writeDocPara(doc string) {
	if doc == "" {
		return
	}
	r.writeDoc("", doc)
	r.body.WriteString("//\n")
}

func (r *renderer) renderEnum(e *model.Enum) {
	name := r.enumType(e.FullName)
	r.writeIdentDoc("", fmt.Sprintf("%s is the %s enum. It is open:\nnumeric values outside the named constants are preserved.", name, e.FullName), e.Doc)
	fmt.Fprintf(&r.body, "type %s int32\n\n", name)
	r.body.WriteString("const (\n")
	for _, v := range e.Values {
		constName := enumValueConst(name, e.Name, v.Name)
		r.writeIdentDoc("\t", fmt.Sprintf("%s is the %s value of %s.", constName, v.Name, name), v.Doc)
		fmt.Fprintf(&r.body, "\t%s %s = %d\n", constName, name, v.Number)
	}
	r.body.WriteString(")\n\n")
}

func (r *renderer) renderMessage(m *model.Message) {
	name := r.messageType(m.FullName)
	for _, o := range m.Oneofs {
		r.renderOneofTypes(m, o)
	}
	hasFields := len(m.Oneofs) > 0
	for _, f := range m.Fields {
		if f.OneofIndex < 0 {
			hasFields = true
		}
	}
	r.writeIdentDoc("", fmt.Sprintf("%s is the native message type for %s.", name, m.FullName), m.Doc)
	if !hasFields {
		fmt.Fprintf(&r.body, "type %s struct{}\n\n", name)
		return
	}
	fmt.Fprintf(&r.body, "type %s struct {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		r.writeDoc("\t", f.Doc)
		fmt.Fprintf(&r.body, "\t%s %s\n", fieldGoName(f), r.fieldType(f))
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "\t%s %s\n", oneofGoName(o), oneofTypeName(name, o))
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderOneofTypes(m *model.Message, o *model.Oneof) {
	name := r.messageType(m.FullName)
	unionName := oneofTypeName(name, o)
	fmt.Fprintf(&r.body, "// %s selects one variant of the %s oneof of %s.\n", unionName, o.Name, name)
	r.body.WriteString("// A nil value means unset.\n")
	fmt.Fprintf(&r.body, "type %s interface {\n\tis%s()\n}\n\n", unionName, unionName)
	for _, f := range oneofFields(m, o) {
		wrapper := variantTypeName(unionName, f)
		switch f.Kind {
		case model.KindJSONNull, model.KindUnit:
			r.writeIdentDoc("", fmt.Sprintf("%s is the %s variant. It carries no payload.", wrapper, f.JSONName), f.Doc)
			fmt.Fprintf(&r.body, "type %s struct{}\n\n", wrapper)
		default:
			r.writeIdentDoc("", fmt.Sprintf("%s is the %s variant.", wrapper, f.JSONName), f.Doc)
			fmt.Fprintf(&r.body, "type %s struct {\n\tValue %s\n}\n\n", wrapper, r.valueType(fieldRef(f)))
		}
		fmt.Fprintf(&r.body, "func (*%s) is%s() {}\n\n", wrapper, unionName)
	}
}

// conversionParts are the rendered pieces of one field's conversion: either a
// struct-literal entry or statements appended after the literal.
type conversionParts struct {
	entry string
	post  []string
}

func (r *renderer) renderConversions(m *model.Message) {
	r.features.proto = true
	name := r.messageType(m.FullName)
	wireName := wireMessage(m.FullName)

	fmt.Fprintf(&r.body, "func %s(value *%s) *%s {\n", toWireFunc(name), name, wireName)
	r.body.WriteString("\tif value == nil {\n\t\treturn nil\n\t}\n")
	var parts []conversionParts
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		parts = append(parts, r.fieldToWire(f))
	}
	r.writeLiteral(wireName, parts)
	for _, o := range m.Oneofs {
		r.writeOneofToWire(m, o)
	}
	r.body.WriteString("\treturn out\n}\n\n")

	fmt.Fprintf(&r.body, "func %s(value *%s) *%s {\n", fromWireFunc(name), wireName, name)
	r.body.WriteString("\tif value == nil {\n\t\treturn nil\n\t}\n")
	parts = parts[:0]
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		parts = append(parts, r.fieldFromWire(f))
	}
	r.writeLiteral(name, parts)
	for _, o := range m.Oneofs {
		r.writeOneofFromWire(m, o)
	}
	r.body.WriteString("\treturn out\n}\n\n")
}

// writeLiteral renders `out := &T{...}` from literal entries, followed by the
// post statements.
func (r *renderer) writeLiteral(typeName string, parts []conversionParts) {
	var entries []string
	var post []string
	for _, p := range parts {
		if p.entry != "" {
			entries = append(entries, p.entry)
		}
		post = append(post, p.post...)
	}
	if len(entries) == 0 {
		fmt.Fprintf(&r.body, "\tout := &%s{}\n", typeName)
	} else {
		fmt.Fprintf(&r.body, "\tout := &%s{\n", typeName)
		for _, entry := range entries {
			fmt.Fprintf(&r.body, "\t\t%s,\n", entry)
		}
		r.body.WriteString("\t}\n")
	}
	for _, line := range post {
		r.body.WriteString("\t" + line + "\n")
	}
}

// fieldToWire renders the conversion of one non-oneof field into the wire
// message under construction.
func (r *renderer) fieldToWire(f *model.Field) conversionParts {
	name := fieldGoName(f)
	expr := "value." + name
	switch f.Kind {
	case model.KindRepeated:
		if identityValue(f.Elem) {
			return conversionParts{entry: name + ": " + expr}
		}
		return conversionParts{post: []string{
			"for _, item := range " + expr + " {",
			"\tout." + name + " = append(out." + name + ", " + r.valueToWire(f.Elem, "item") + ")",
			"}",
		}}
	case model.KindMap:
		if identityValue(f.MapValue) {
			return conversionParts{entry: name + ": " + expr}
		}
		return conversionParts{post: []string{
			"if " + expr + " != nil {",
			"\tout." + name + " = make(map[" + goScalarType(f.MapKey) + "]" + r.wireValueType(f.MapValue) + ", len(" + expr + "))",
			"\tfor key, item := range " + expr + " {",
			"\t\tout." + name + "[key] = " + r.valueToWire(f.MapValue, "item"),
			"\t}",
			"}",
		}}
	case model.KindScalar, model.KindBytes:
		// Explicit presence is a pointer on both sides; copy either way.
		return conversionParts{entry: name + ": " + expr}
	case model.KindEnum:
		if f.Presence == model.ExplicitPresence {
			return conversionParts{post: []string{
				"if " + expr + " != nil {",
				"\twireValue := " + wireEnum(f.Enum) + "(*" + expr + ")",
				"\tout." + name + " = &wireValue",
				"}",
			}}
		}
		return conversionParts{entry: name + ": " + wireEnum(f.Enum) + "(" + expr + ")"}
	case model.KindMessage, model.KindTimestamp, model.KindDuration, model.KindJSONStruct, model.KindJSONValue, model.KindRPCStatus:
		// The rpc_support converters and generated message converters are
		// nil-safe, so absence flows through unconditionally.
		return conversionParts{entry: name + ": " + r.fieldConvToWire(f, expr)}
	default:
		panic(fmt.Sprintf("golang: unsupported singular field kind %d (field %s)", f.Kind, f.Name))
	}
}

// fieldConvToWire renders the nil-safe converter call for a presence-carrying
// singular field. These fields are pointers (or nil-able values) natively, so
// the converter signatures differ from the always-set value conversions.
func (r *renderer) fieldConvToWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindMessage:
		return toWireFunc(r.messageType(f.Message)) + "(" + expr + ")"
	case model.KindTimestamp:
		return "toWireTimestamp(" + expr + ")"
	case model.KindDuration:
		return "toWireDuration(" + expr + ")"
	case model.KindJSONStruct:
		return "toWireStruct(" + expr + ")"
	case model.KindJSONValue:
		return "toWireValue(" + expr + ")"
	case model.KindRPCStatus:
		return "toWireStatus(" + expr + ")"
	default:
		panic(fmt.Sprintf("golang: no field converter for kind %d", f.Kind))
	}
}

func (r *renderer) fieldFromWire(f *model.Field) conversionParts {
	name := fieldGoName(f)
	expr := "value." + name
	switch f.Kind {
	case model.KindRepeated:
		if identityValue(f.Elem) {
			return conversionParts{entry: name + ": " + expr}
		}
		return conversionParts{post: []string{
			"for _, item := range " + expr + " {",
			"\tout." + name + " = append(out." + name + ", " + r.valueFromWire(f.Elem, "item") + ")",
			"}",
		}}
	case model.KindMap:
		if identityValue(f.MapValue) {
			return conversionParts{entry: name + ": " + expr}
		}
		return conversionParts{post: []string{
			"if " + expr + " != nil {",
			"\tout." + name + " = make(map[" + goScalarType(f.MapKey) + "]" + r.valueType(f.MapValue) + ", len(" + expr + "))",
			"\tfor key, item := range " + expr + " {",
			"\t\tout." + name + "[key] = " + r.valueFromWire(f.MapValue, "item"),
			"\t}",
			"}",
		}}
	case model.KindScalar, model.KindBytes:
		return conversionParts{entry: name + ": " + expr}
	case model.KindEnum:
		if f.Presence == model.ExplicitPresence {
			return conversionParts{post: []string{
				"if " + expr + " != nil {",
				"\tnativeValue := " + r.enumType(f.Enum) + "(*" + expr + ")",
				"\tout." + name + " = &nativeValue",
				"}",
			}}
		}
		return conversionParts{entry: name + ": " + r.enumType(f.Enum) + "(" + expr + ")"}
	case model.KindMessage:
		return conversionParts{entry: name + ": " + fromWireFunc(r.messageType(f.Message)) + "(" + expr + ")"}
	case model.KindTimestamp:
		return conversionParts{entry: name + ": fromWireTimestamp(" + expr + ")"}
	case model.KindDuration:
		return conversionParts{entry: name + ": fromWireDuration(" + expr + ")"}
	case model.KindJSONStruct:
		return conversionParts{entry: name + ": fromWireStruct(" + expr + ")"}
	case model.KindJSONValue:
		return conversionParts{entry: name + ": fromWireValue(" + expr + ")"}
	case model.KindRPCStatus:
		return conversionParts{entry: name + ": fromWireStatus(" + expr + ")"}
	default:
		panic(fmt.Sprintf("golang: unsupported singular field kind %d (field %s)", f.Kind, f.Name))
	}
}

func (r *renderer) writeOneofToWire(m *model.Message, o *model.Oneof) {
	name := r.messageType(m.FullName)
	unionName := oneofTypeName(name, o)
	prop := oneofGoName(o)
	fmt.Fprintf(&r.body, "\tswitch variant := value.%s.(type) {\n", prop)
	for _, f := range oneofFields(m, o) {
		wireWrapper := wireMessage(m.FullName) + "_" + upperFirst(f.JSONName)
		wireField := upperFirst(f.JSONName)
		var payload string
		switch f.Kind {
		case model.KindJSONNull:
			r.features.structpb = true
			payload = "structpb.NullValue_NULL_VALUE"
		case model.KindUnit:
			r.features.emptypb = true
			payload = "&emptypb.Empty{}"
		default:
			payload = r.valueToWire(fieldRef(f), "variant.Value")
		}
		fmt.Fprintf(&r.body, "\tcase *%s:\n", variantTypeName(unionName, f))
		fmt.Fprintf(&r.body, "\t\tout.%s = &%s{%s: %s}\n", prop, wireWrapper, wireField, payload)
	}
	r.body.WriteString("\t}\n")
}

func (r *renderer) writeOneofFromWire(m *model.Message, o *model.Oneof) {
	name := r.messageType(m.FullName)
	unionName := oneofTypeName(name, o)
	prop := oneofGoName(o)
	fmt.Fprintf(&r.body, "\tswitch variant := value.%s.(type) {\n", prop)
	for _, f := range oneofFields(m, o) {
		wireWrapper := wireMessage(m.FullName) + "_" + upperFirst(f.JSONName)
		wireField := upperFirst(f.JSONName)
		fmt.Fprintf(&r.body, "\tcase *%s:\n", wireWrapper)
		switch f.Kind {
		case model.KindJSONNull, model.KindUnit:
			fmt.Fprintf(&r.body, "\t\tout.%s = &%s{}\n", prop, variantTypeName(unionName, f))
		default:
			payload := r.valueFromWire(fieldRef(f), "variant."+wireField)
			fmt.Fprintf(&r.body, "\t\tout.%s = &%s{Value: %s}\n", prop, variantTypeName(unionName, f), payload)
		}
	}
	r.body.WriteString("\t}\n")
}

func (r *renderer) renderClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName
	r.features.context = true
	r.features.grpc = true
	r.features.proto = true

	ctxField := contextFieldOf(svc)
	doc := fmt.Sprintf("%s is the generated client for %s.\nEvery transport error is converted to *GestaltError.", name, svc.FullName)
	r.writeIdentDoc("", doc, svc.Doc)
	if ctxField != nil {
		ctxType := r.fieldType(ctxField)
		fmt.Fprintf(&r.body, "type %s struct {\n\tclient proto.%sClient\n\tcontext %s\n}\n\n", name, wireName, ctxType)
		fmt.Fprintf(&r.body, "// New%s creates a %s client over an injected gRPC connection. A\n", name, name)
		r.body.WriteString("// WithRequestContext option sets a default request context, injected into\n// outgoing requests that do not carry one.\n")
		fmt.Fprintf(&r.body, "func New%s(conn grpc.ClientConnInterface, opts ...ClientOption) *%s {\n", name, name)
		r.body.WriteString("\toptions := applyClientOptions(opts)\n")
		fmt.Fprintf(&r.body, "\treturn &%s{client: proto.New%sClient(conn), context: options.requestContext}\n}\n\n", name, wireName)
	} else {
		fmt.Fprintf(&r.body, "type %s struct {\n\tclient proto.%sClient\n}\n\n", name, wireName)
		fmt.Fprintf(&r.body, "// New%s creates a %s client over an injected gRPC connection.\n", name, name)
		fmt.Fprintf(&r.body, "func New%s(conn grpc.ClientConnInterface) *%s {\n", name, name)
		fmt.Fprintf(&r.body, "\treturn &%s{client: proto.New%sClient(conn)}\n}\n\n", name, wireName)
	}

	if svc.HostBinding != "" {
		r.renderConnect(name, svc.HostBinding, ctxField != nil)
	}

	for _, method := range svc.Methods {
		r.renderMethod(name, method)
	}
	for _, method := range svc.Methods {
		if method.Stream != model.Unary {
			r.renderStreamWrapper(name, method)
		}
		if method.Initial != nil {
			r.renderFramedStreamWrapper(name, method)
		}
	}
}

// renderConnect renders the Connect<Service> constructor for a service with
// the host_binding annotation. Connect+service name is the one exported
// pattern: it dials the host-service target advertised through the
// GESTALT_HOST_SERVICE_* environment, exactly like the handwritten sdk/go
// clients, via the module-internal host package.
func (r *renderer) renderConnect(svcName, binding string, withOptions bool) {
	r.features.host = true
	params, forward := "", ""
	if withOptions {
		params, forward = ", opts ...ClientOption", ", opts..."
	}
	fmt.Fprintf(&r.body, "var connect%sConns host.ConnPool\n\n", svcName)
	fmt.Fprintf(&r.body, "// Connect%s dials the %q host service advertised through the\n", svcName, binding)
	r.body.WriteString("// GESTALT_HOST_SERVICE_SOCKET environment and returns a connected client.\n")
	r.body.WriteString("// name selects a named binding; the empty string selects the default\n// binding. Connections are pooled per binding and shared across clients for\n// the life of the process. The first dial blocks until the connection is\n// ready or ctx is done.\n")
	fmt.Fprintf(&r.body, "func Connect%s(ctx context.Context, name string%s) (*%s, error) {\n", svcName, params, svcName)
	fmt.Fprintf(&r.body, "\ttarget, token, err := host.Target(%q)\n", binding)
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\tconn, err := connect%sConns.Conn(ctx, %q, target, token, name)\n", svcName, binding)
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn New%s(conn%s), nil\n}\n\n", svcName, forward)
}

// methodRequest renders the request parameter, the wire argument, and the
// default-context injection lines of a method that sends a single request
// message.
func (r *renderer) methodRequest(m *model.Method) (param, arg string, prep []string) {
	if m.InputIsEmpty {
		r.features.emptypb = true
		return "", "&emptypb.Empty{}", nil
	}
	requestType := r.messageType(m.Input.FullName)
	if !r.publicClient && findField(m.Input, "context") != nil {
		prep = []string{
			"\tif request.Context == nil && c.context != nil {",
			"\t\tshallow := *request",
			"\t\tshallow.Context = c.context",
			"\t\trequest = &shallow",
			"\t}",
		}
	}
	return ", request *" + requestType, toWireFunc(requestType) + "(request)", prep
}

// contextFieldOf returns a service's first request context field, which
// determines whether the client carries a default RequestContext.
func contextFieldOf(svc *model.Service) *model.Field {
	for _, m := range svc.Methods {
		if m.Input == nil {
			continue
		}
		if f := findField(m.Input, "context"); f != nil {
			return f
		}
	}
	return nil
}

func findField(m *model.Message, protoName string) *model.Field {
	for _, f := range m.Fields {
		if f.Name == protoName {
			return f
		}
	}
	return nil
}

// collapsed describes how an annotated response collapses at the API
// boundary: the ergonomic value results (before the trailing error), the
// matching zero-value literals returned alongside a non-nil error, and the
// statements that derive the return values from the wire response variable
// `response`.
type collapsed struct {
	types []string
	zero  []string
	doc   string // one generated doc line describing the collapse
	lines []string
}

// collapseOutput returns the response collapse for a method, or nil when the
// faithful response type is returned.
func (r *renderer) collapseOutput(m *model.Method) *collapsed {
	if m.Output == nil {
		return nil
	}
	fromWire := fromWireFunc(r.messageType(m.Output.FullName))
	// json_result decodes the HTTP-shaped result's JSON envelope: the method
	// returns the decoded payload and surfaces envelope failures as
	// *InvokeError beside transport *GestaltError.
	if jr := m.JsonResult; jr != nil {
		status := findField(m.Output, jr.Status)
		body := findField(m.Output, jr.Body)
		return &collapsed{
			types: []string{"any"},
			zero:  []string{"nil"},
			doc:   "The result decodes with the standard JSON operation envelope semantics;\n// envelope failures return *InvokeError.",
			lines: []string{
				"out := " + fromWire + "(response)",
				fmt.Sprintf("return DecodeAppResult(%s, %s, out.%s, out.%s)",
					jsonResultContext(m, "app"), jsonResultContext(m, "operation"),
					fieldGoName(status), fieldGoName(body)),
			},
		}
	}
	// optional_result collapses to Go's comma-ok form: (value T, ok bool,
	// err error). The value is meaningful only when ok is true.
	if or := m.Output.OptionalResult; or != nil {
		guard := findField(m.Output, or.Guard)
		value := findField(m.Output, or.Value)
		valueZero := zeroValue(value)
		return &collapsed{
			types: []string{r.fieldType(value), "bool"},
			zero:  []string{valueZero, "false"},
			doc:   "The response collapses to a comma-ok pair: the value is meaningful only when ok is true.",
			lines: []string{
				"out := " + fromWire + "(response)",
				"if !out." + fieldGoName(guard) + " {",
				"\treturn " + valueZero + ", false, nil",
				"}",
				"return out." + fieldGoName(value) + ", true, nil",
			},
		}
	}
	if k := m.Output.Keyed; k != nil {
		entries := findField(m.Output, k.Entries)
		entry := r.idx.messages[entries.Elem.Message]
		key := findField(entry, k.Key)
		present := findField(entry, k.Present)
		value := findField(entry, k.Value)
		mapType := "map[" + goScalarType(key.Scalar) + "]" + r.fieldType(value)
		return &collapsed{
			types: []string{mapType},
			zero:  []string{"nil"},
			doc: fmt.Sprintf("The response collapses to a map keyed by %s; entries whose %s is false are omitted.",
				key.JSONName, present.JSONName),
			lines: []string{
				"out := make(" + mapType + ")",
				"for _, entry := range " + fromWire + "(response)." + fieldGoName(entries) + " {",
				"\tif entry." + fieldGoName(present) + " {",
				"\t\tout[entry." + fieldGoName(key) + "] = entry." + fieldGoName(value),
				"\t}",
				"}",
				"return out, nil",
			},
		}
	}
	if m.Output.Unwrap != "" {
		field := findField(m.Output, m.Output.Unwrap)
		return &collapsed{
			types: []string{r.fieldType(field)},
			zero:  []string{zeroValue(field)},
			doc:   fmt.Sprintf("The response collapses to its %s field.", field.JSONName),
			lines: []string{
				"return " + fromWire + "(response)." + fieldGoName(field) + ", nil",
			},
		}
	}
	return nil
}

// jsonResultContext renders the expression carrying a request field into the
// decode call's error context: the request variable's field when the request
// declares it, an empty string otherwise.
func jsonResultContext(m *model.Method, name string) string {
	if m.Input != nil {
		if f := findField(m.Input, name); f != nil {
			return "request." + fieldGoName(f)
		}
	}
	return `""`
}

// renderMethod renders the public surface of one method. Annotated methods
// render an ergonomic form under the natural name plus the faithful form
// under a Raw suffix; everything else renders the faithful form only.
func (r *renderer) renderMethod(svcName string, m *model.Method) {
	switch {
	case m.Initial != nil && m.Stream == model.ServerStream:
		r.renderFramedRead(svcName, m)
		r.renderFaithfulMethod(svcName, m, true)
	case m.Initial != nil && m.Stream == model.ClientStream:
		r.renderFramedWrite(svcName, m)
		r.renderFaithfulMethod(svcName, m, true)
	case m.Stream == model.Unary && (len(m.Signature) > 0 || len(m.OptionalSignature) > 0 || r.collapseOutput(m) != nil):
		r.renderErgonomicUnary(svcName, m)
		r.renderFaithfulMethod(svcName, m, true)
	default:
		r.renderFaithfulMethod(svcName, m, false)
	}
}

// renderOptionsStruct renders the per-method options struct carrying a
// method's optional_signature fields, adjacent to the method that takes it.
func (r *renderer) renderOptionsStruct(svcName string, m *model.Method) string {
	name := svcName + m.Name + "Options"
	fmt.Fprintf(&r.body, "// %s carries the optional parameters of [%s.%s].\n", name, svcName, m.Name)
	r.body.WriteString("// A nil options value is equivalent to the zero value.\n")
	fmt.Fprintf(&r.body, "type %s struct {\n", name)
	omitted := map[string]bool{}
	if r.publicClient {
		omitted = publicsurface.OmittedFields(m)
	}
	for _, fieldName := range m.OptionalSignature {
		if omitted[fieldName] {
			continue
		}
		f := findField(m.Input, fieldName)
		r.writeDoc("\t", f.Doc)
		fmt.Fprintf(&r.body, "\t%s %s\n", fieldGoName(f), r.fieldType(f))
	}
	r.body.WriteString("}\n\n")
	return name
}

// renderErgonomicUnary renders the annotated surface of a unary method:
// flattened parameters from the signature annotation (presence fields are
// pointer parameters, already placed last by validation), a trailing nilable
// options struct from the optional_signature annotation, and a collapsed
// return from the response annotations. Empty-input methods take ctx only.
func (r *renderer) renderErgonomicUnary(svcName string, m *model.Method) {
	params, arg, prep := r.methodRequest(m)
	var requestLines []string
	if len(m.Signature) > 0 || len(m.OptionalSignature) > 0 {
		requestType := r.messageType(m.Input.FullName)
		omitted := map[string]bool{}
		if r.publicClient {
			omitted = publicsurface.OmittedFields(m)
		}
		var decls, fields []string
		inSignature := false
		for _, name := range m.Signature {
			if omitted[name] {
				continue
			}
			inSignature = inSignature || name == "context"
			f := findField(m.Input, name)
			param := goParamName(f.JSONName)
			decls = append(decls, param+" "+r.fieldType(f))
			fields = append(fields, fieldGoName(f)+": "+param)
		}
		if len(m.OptionalSignature) > 0 {
			optionsType := r.renderOptionsStruct(svcName, m)
			decls = append(decls, "opts *"+optionsType)
			requestLines = append(requestLines, fmt.Sprintf("\tif opts == nil {\n\t\topts = &%s{}\n\t}\n", optionsType))
			for _, name := range m.OptionalSignature {
				if omitted[name] {
					continue
				}
				inSignature = inSignature || name == "context"
				f := findField(m.Input, name)
				fields = append(fields, fieldGoName(f)+": opts."+fieldGoName(f))
			}
		}
		// The flattened form has no context parameter, so the literal takes
		// the client default directly when the field is part of the public API.
		if ctxF := findField(m.Input, "context"); ctxF != nil && !inSignature && !omitted["context"] {
			fields = append(fields, fieldGoName(ctxF)+": c.context")
			prep = nil
		}
		params = ", " + strings.Join(decls, ", ")
		requestLines = append(requestLines, fmt.Sprintf("\trequest := &%s{%s}\n", requestType, strings.Join(fields, ", ")))
		arg = toWireFunc(requestType) + "(request)"
	}
	for _, line := range prep {
		requestLines = append(requestLines, line+"\n")
	}
	collapse := r.collapseOutput(m)

	synthetic := fmt.Sprintf("%s is the ergonomic form of [%s.%sRaw].", m.Name, svcName, m.Name)
	if collapse != nil {
		synthetic += "\n" + collapse.doc
	}
	r.writeIdentDoc("", synthetic, m.Doc)
	recv := fmt.Sprintf("func (c *%s) %s", svcName, m.Name)
	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "%s(ctx context.Context%s) error {\n", recv, params)
		for _, line := range requestLines {
			r.body.WriteString(line)
		}
		fmt.Fprintf(&r.body, "\tif _, err := c.client.%s(ctx, %s); err != nil {\n", m.Name, arg)
		r.body.WriteString("\t\treturn toGestaltError(err)\n\t}\n\treturn nil\n}\n\n")
	case collapse != nil:
		fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (%s, error) {\n", recv, params, strings.Join(collapse.types, ", "))
		for _, line := range requestLines {
			r.body.WriteString(line)
		}
		fmt.Fprintf(&r.body, "\tresponse, err := c.client.%s(ctx, %s)\n", m.Name, arg)
		fmt.Fprintf(&r.body, "\tif err != nil {\n\t\treturn %s, toGestaltError(err)\n\t}\n", strings.Join(collapse.zero, ", "))
		for _, line := range collapse.lines {
			r.body.WriteString("\t" + line + "\n")
		}
		r.body.WriteString("}\n\n")
	default:
		responseType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (*%s, error) {\n", recv, params, responseType)
		for _, line := range requestLines {
			r.body.WriteString(line)
		}
		fmt.Fprintf(&r.body, "\tresponse, err := c.client.%s(ctx, %s)\n", m.Name, arg)
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn %s(response), nil\n}\n\n", fromWireFunc(responseType))
	}
}

// renderFramedRead renders a server-streaming method with the framing
// annotation: the leading header frame is consumed and returned beside a
// typed payload stream. A missing or mistyped first frame is a clear
// *GestaltError rather than a surprising zero header.
func (r *renderer) renderFramedRead(svcName string, m *model.Method) {
	frames := m.Output
	header := findField(frames, m.Initial.HeaderField)
	oneof := frames.Oneofs[header.OneofIndex]
	unionName := oneofTypeName(r.messageType(frames.FullName), oneof)
	dataStream := svcName + m.Name + "DataStream"
	r.features.errors = true
	r.features.io = true

	param := ""
	callArgs := "ctx"
	if !m.InputIsEmpty {
		param = ", request *" + r.messageType(m.Input.FullName)
		callArgs = "ctx, request"
	}

	r.writeIdentDoc("", fmt.Sprintf("%s is the ergonomic form of [%s.%sRaw]: it consumes the\nleading %s header frame and returns it beside the %s payload\nstream.", m.Name, svcName, m.Name, header.JSONName, m.Initial.ChunkField), m.Doc)
	headerZero := zeroValueRef(fieldRef(header))
	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context%s) (%s, *%s, error) {\n",
		svcName, m.Name, param, r.valueType(fieldRef(header)), dataStream)
	fmt.Fprintf(&r.body, "\tframes, err := c.%sRaw(%s)\n", m.Name, callArgs)
	fmt.Fprintf(&r.body, "\tif err != nil {\n\t\treturn %s, nil, err\n\t}\n", headerZero)
	r.body.WriteString("\tframe, err := frames.Recv()\n")
	fmt.Fprintf(&r.body, "\tif err != nil && !errors.Is(err, io.EOF) {\n\t\treturn %s, nil, err\n\t}\n", headerZero)
	r.body.WriteString("\tif err != nil {\n")
	fmt.Fprintf(&r.body, "\t\treturn %s, nil, &GestaltError{Code: GestaltErrorCodeInternal, Message: \"stream did not begin with the expected header frame\"}\n\t}\n", headerZero)
	fmt.Fprintf(&r.body, "\theader, ok := frame.%s.(*%s)\n", oneofGoName(oneof), variantTypeName(unionName, header))
	r.body.WriteString("\tif !ok {\n")
	fmt.Fprintf(&r.body, "\t\treturn %s, nil, &GestaltError{Code: GestaltErrorCodeInternal, Message: \"stream did not begin with the expected header frame\"}\n\t}\n", headerZero)
	fmt.Fprintf(&r.body, "\treturn header.Value, &%s{frames: frames}, nil\n}\n\n", dataStream)
}

// renderFramedWrite renders a client-streaming method with the framing
// annotation: the header frame is sent before the typed payload stream is
// handed back.
func (r *renderer) renderFramedWrite(svcName string, m *model.Method) {
	frames := m.Input
	header := findField(frames, m.Initial.HeaderField)
	oneof := frames.Oneofs[header.OneofIndex]
	frameType := r.messageType(frames.FullName)
	unionName := oneofTypeName(frameType, oneof)
	dataStream := svcName + m.Name + "DataStream"
	headerParam := goParamName(header.JSONName)

	r.writeIdentDoc("", fmt.Sprintf("%s is the ergonomic form of [%s.%sRaw]: it sends the %s\nheader frame and returns the %s payload stream.", m.Name, svcName, m.Name, header.JSONName, m.Initial.ChunkField), m.Doc)
	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, %s %s) (*%s, error) {\n",
		svcName, m.Name, headerParam, r.valueType(fieldRef(header)), dataStream)
	fmt.Fprintf(&r.body, "\tframes, err := c.%sRaw(ctx)\n", m.Name)
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(&r.body, "\tif err := frames.Send(&%s{%s: &%s{Value: %s}}); err != nil {\n",
		frameType, oneofGoName(oneof), variantTypeName(unionName, header), headerParam)
	r.body.WriteString("\t\t// A failed header send reports a broken stream; surface the cause.\n")
	r.body.WriteString("\t\tif _, recvErr := frames.CloseAndRecv(); recvErr != nil {\n\t\t\treturn nil, recvErr\n\t\t}\n")
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn &%s{frames: frames}, nil\n}\n\n", dataStream)
}

// renderFramedStreamWrapper renders the typed payload stream behind a framed
// method, wrapping the frame-level Raw stream.
func (r *renderer) renderFramedStreamWrapper(svcName string, m *model.Method) {
	dataStream := svcName + m.Name + "DataStream"
	rawStream := svcName + m.Name + "Stream"
	switch m.Stream {
	case model.ServerStream:
		frames := m.Output
		chunk := findField(frames, m.Initial.ChunkField)
		oneof := frames.Oneofs[chunk.OneofIndex]
		unionName := oneofTypeName(r.messageType(frames.FullName), oneof)
		fmt.Fprintf(&r.body, "// %s is the stream of %s payloads returned by\n", dataStream, chunk.JSONName)
		fmt.Fprintf(&r.body, "// %s.%s after the header frame.\n", svcName, m.Name)
		fmt.Fprintf(&r.body, "type %s struct {\n\tframes *%s\n}\n\n", dataStream, rawStream)
		fmt.Fprintf(&r.body, "// Recv receives the next %s payload. io.EOF reports the normal end of\n", chunk.JSONName)
		r.body.WriteString("// the stream; any other frame variant or transport failure is a\n// *GestaltError.\n")
		fmt.Fprintf(&r.body, "func (s *%s) Recv() (%s, error) {\n", dataStream, r.valueType(fieldRef(chunk)))
		r.body.WriteString("\tframe, err := s.frames.Recv()\n")
		fmt.Fprintf(&r.body, "\tif err != nil {\n\t\treturn %s, err\n\t}\n", zeroValueRef(fieldRef(chunk)))
		fmt.Fprintf(&r.body, "\tchunk, ok := frame.%s.(*%s)\n", oneofGoName(oneof), variantTypeName(unionName, chunk))
		fmt.Fprintf(&r.body, "\tif !ok {\n\t\treturn %s, &GestaltError{Code: GestaltErrorCodeInternal, Message: \"unexpected frame in payload stream\"}\n\t}\n", zeroValueRef(fieldRef(chunk)))
		r.body.WriteString("\treturn chunk.Value, nil\n}\n\n")
		// Byte payload streams gain an io.ReadAll-style buffering helper.
		if chunk.Kind == model.KindBytes {
			r.features.errors = true
			r.features.io = true
			fmt.Fprintf(&r.body, "// ReadAll buffers the remaining %s payload into one byte slice, like\n", chunk.JSONName)
			r.body.WriteString("// io.ReadAll over the stream.\n")
			fmt.Fprintf(&r.body, "func (s *%s) ReadAll() ([]byte, error) {\n", dataStream)
			r.body.WriteString("\tvar out []byte\n\tfor {\n\t\tchunk, err := s.Recv()\n")
			r.body.WriteString("\t\tif errors.Is(err, io.EOF) {\n\t\t\treturn out, nil\n\t\t}\n")
			r.body.WriteString("\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n")
			r.body.WriteString("\t\tout = append(out, chunk...)\n\t}\n}\n\n")
		}
	case model.ClientStream:
		frames := m.Input
		chunk := findField(frames, m.Initial.ChunkField)
		oneof := frames.Oneofs[chunk.OneofIndex]
		frameType := r.messageType(frames.FullName)
		unionName := oneofTypeName(frameType, oneof)
		chunkParam := goParamName(chunk.JSONName)
		responseType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "// %s is the stream of %s payloads accepted by\n", dataStream, chunk.JSONName)
		fmt.Fprintf(&r.body, "// %s.%s after the header frame.\n", svcName, m.Name)
		fmt.Fprintf(&r.body, "type %s struct {\n\tframes *%s\n}\n\n", dataStream, rawStream)
		fmt.Fprintf(&r.body, "// Send sends one %s payload. io.EOF reports a broken stream; the cause\n// surfaces on CloseAndRecv.\n", chunk.JSONName)
		fmt.Fprintf(&r.body, "func (s *%s) Send(%s %s) error {\n", dataStream, chunkParam, r.valueType(fieldRef(chunk)))
		fmt.Fprintf(&r.body, "\treturn s.frames.Send(&%s{%s: &%s{Value: %s}})\n}\n\n",
			frameType, oneofGoName(oneof), variantTypeName(unionName, chunk), chunkParam)
		r.body.WriteString("// CloseAndRecv closes the send side and receives the response.\n")
		fmt.Fprintf(&r.body, "func (s *%s) CloseAndRecv() (*%s, error) {\n\treturn s.frames.CloseAndRecv()\n}\n\n", dataStream, responseType)
	}
}

// renderFaithfulMethod renders the descriptor-faithful method. When an
// annotated surface owns the natural name, the faithful variant keeps a Raw
// suffix so both remain available.
func (r *renderer) renderFaithfulMethod(svcName string, m *model.Method, rawSuffix bool) {
	methodName := m.Name
	if rawSuffix {
		methodName += "Raw"
		r.writeIdentDoc("", fmt.Sprintf("%s is the faithful form of [%s.%s].", methodName, svcName, m.Name), m.Doc)
	} else {
		r.writeIdentDoc("", fmt.Sprintf("%s calls the %s RPC of %s.", methodName, m.Name, svcName), m.Doc)
	}
	recv := fmt.Sprintf("func (c *%s) %s", svcName, methodName)
	streamType := svcName + m.Name + "Stream"
	switch m.Stream {
	case model.ServerStream:
		param, arg, prep := r.methodRequest(m)
		fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (*%s, error) {\n", recv, param, streamType)
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "\tstream, err := c.client.%s(ctx, %s)\n", m.Name, arg)
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn &%s{stream: stream}, nil\n}\n\n", streamType)
	case model.ClientStream, model.Bidi:
		fmt.Fprintf(&r.body, "%s(ctx context.Context) (*%s, error) {\n", recv, streamType)
		fmt.Fprintf(&r.body, "\tstream, err := c.client.%s(ctx)\n", m.Name)
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn &%s{stream: stream}, nil\n}\n\n", streamType)
	default:
		param, arg, prep := r.methodRequest(m)
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "%s(ctx context.Context%s) error {\n", recv, param)
			for _, line := range prep {
				r.body.WriteString(line + "\n")
			}
			fmt.Fprintf(&r.body, "\tif _, err := c.client.%s(ctx, %s); err != nil {\n", m.Name, arg)
			r.body.WriteString("\t\treturn toGestaltError(err)\n\t}\n\treturn nil\n}\n\n")
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (*%s, error) {\n", recv, param, responseType)
			for _, line := range prep {
				r.body.WriteString(line + "\n")
			}
			fmt.Fprintf(&r.body, "\tresponse, err := c.client.%s(ctx, %s)\n", m.Name, arg)
			r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
			fmt.Fprintf(&r.body, "\treturn %s(response), nil\n}\n\n", fromWireFunc(responseType))
		}
	}
}

// renderStreamWrapper renders the typed wrapper over one method's wire-level
// gRPC stream, converting every frame and every error.
func (r *renderer) renderStreamWrapper(svcName string, m *model.Method) {
	// Streamed frames are always real messages: an Empty frame type has no
	// native representation worth generating.
	if m.OutputIsEmpty || (m.InputIsEmpty && m.Stream != model.ServerStream) {
		panic(fmt.Sprintf("golang: google.protobuf.Empty stream frames are not supported (%s.%s)", svcName, m.Name))
	}
	streamType := svcName + m.Name + "Stream"
	wireStream := fmt.Sprintf("proto.%s_%sClient", svcName, m.Name)
	responseType := r.messageType(m.Output.FullName)
	requestType := ""
	if !m.InputIsEmpty {
		requestType = r.messageType(m.Input.FullName)
	}
	methodName := m.Name
	if m.Initial != nil {
		methodName += "Raw"
	}

	r.writeDocPara(m.Doc)
	switch m.Stream {
	case model.ServerStream:
		fmt.Fprintf(&r.body, "// %s is the server stream of %s frames\n", streamType, responseType)
		fmt.Fprintf(&r.body, "// returned by %s.%s.\n", svcName, methodName)
	case model.ClientStream:
		fmt.Fprintf(&r.body, "// %s is the client stream of %s frames\n", streamType, requestType)
		fmt.Fprintf(&r.body, "// accepted by %s.%s.\n", svcName, methodName)
	default:
		fmt.Fprintf(&r.body, "// %s is the bidirectional stream opened by\n", streamType)
		fmt.Fprintf(&r.body, "// %s.%s.\n", svcName, methodName)
	}
	fmt.Fprintf(&r.body, "type %s struct {\n\tstream %s\n}\n\n", streamType, wireStream)

	if m.Stream == model.ClientStream || m.Stream == model.Bidi {
		fmt.Fprintf(&r.body, "// Send sends one frame. io.EOF reports a broken stream; the cause\n// surfaces on the terminating call.\n")
		fmt.Fprintf(&r.body, "func (s *%s) Send(request *%s) error {\n", streamType, requestType)
		fmt.Fprintf(&r.body, "\treturn streamError(s.stream.Send(%s(request)))\n}\n\n", toWireFunc(requestType))
	}
	if m.Stream == model.ServerStream || m.Stream == model.Bidi {
		fmt.Fprintf(&r.body, "// Recv receives the next frame. io.EOF reports the normal end of the\n// stream; every other error is a *GestaltError.\n")
		fmt.Fprintf(&r.body, "func (s *%s) Recv() (*%s, error) {\n", streamType, responseType)
		fmt.Fprintf(&r.body, "\tframe, err := s.stream.Recv()\n")
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, streamError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn %s(frame), nil\n}\n\n", fromWireFunc(responseType))
	}
	if m.Stream == model.ClientStream {
		fmt.Fprintf(&r.body, "// CloseAndRecv closes the send side and receives the response.\n")
		fmt.Fprintf(&r.body, "func (s *%s) CloseAndRecv() (*%s, error) {\n", streamType, responseType)
		fmt.Fprintf(&r.body, "\tresponse, err := s.stream.CloseAndRecv()\n")
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn %s(response), nil\n}\n\n", fromWireFunc(responseType))
	}
	if m.Stream == model.Bidi {
		fmt.Fprintf(&r.body, "// CloseSend closes the send side of the stream.\n")
		fmt.Fprintf(&r.body, "func (s *%s) CloseSend() error {\n", streamType)
		r.body.WriteString("\treturn streamError(s.stream.CloseSend())\n}\n\n")
	}
}

// assembleGenerated wraps the renderer body in the generated package.
func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) importHeader() string {
	var b strings.Builder
	var std, ext []string
	if r.features.context {
		std = append(std, `"context"`)
	}
	if r.features.errors {
		std = append(std, `"errors"`)
	}
	if r.features.io {
		std = append(std, `"io"`)
	}
	if r.features.time {
		std = append(std, `"time"`)
	}
	if r.features.host && !r.publicClient {
		ext = append(ext, `"github.com/valon-technologies/gestalt/sdk/go/internal/host"`)
	}
	if r.features.proto {
		ext = append(ext, wireImport)
	}
	if r.features.rpcstatus {
		ext = append(ext, `rpcstatus "google.golang.org/genproto/googleapis/rpc/status"`)
	}
	if r.features.grpc {
		ext = append(ext, `"google.golang.org/grpc"`)
	}
	if r.features.durationpb {
		ext = append(ext, `"google.golang.org/protobuf/types/known/durationpb"`)
	}
	if r.features.emptypb {
		ext = append(ext, `"google.golang.org/protobuf/types/known/emptypb"`)
	}
	if r.features.structpb {
		ext = append(ext, `"google.golang.org/protobuf/types/known/structpb"`)
	}
	if r.features.timestamppb {
		ext = append(ext, `"google.golang.org/protobuf/types/known/timestamppb"`)
	}
	if len(std) == 0 && len(ext) == 0 {
		return ""
	}
	b.WriteString("import (\n")
	for _, imp := range std {
		fmt.Fprintf(&b, "\t%s\n", imp)
	}
	if len(std) > 0 && len(ext) > 0 {
		b.WriteString("\n")
	}
	for _, imp := range ext {
		fmt.Fprintf(&b, "\t%s\n", imp)
	}
	b.WriteString(")\n\n")
	return b.String()
}

// assemble prepends the package clause and import header derived from the
// rendered body.
func (r *renderer) assemble() string {
	pkg := "client"
	if r.publicClient {
		pkg = "generated"
	}
	out := "package " + pkg + "\n\n" + r.importHeader() + r.body.String()
	if r.publicClient {
		return out
	}
	return strings.TrimRight(out, "\n") + "\n"
}
