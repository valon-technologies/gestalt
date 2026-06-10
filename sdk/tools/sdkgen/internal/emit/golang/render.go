package golang

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// wireImport is the wire-stub package generated files convert through,
// aliased proto to match the handwritten sdk/go transport code.
const wireImport = `proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"`

// features tracks which imports a generated file needs; the import header is
// assembled after the body renders.
type features struct {
	context     bool
	time        bool
	proto       bool
	rpcstatus   bool
	grpc        bool
	durationpb  bool
	emptypb     bool
	structpb    bool
	timestamppb bool
}

type renderer struct {
	idx      *index
	features features
	body     strings.Builder
}

func newRenderer(idx *index) *renderer {
	return &renderer{idx: idx}
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

func (r *renderer) renderEnum(e *model.Enum) {
	name := r.enumType(e.FullName)
	fmt.Fprintf(&r.body, "// %s is the %s enum. It is open:\n", name, e.FullName)
	r.body.WriteString("// numeric values outside the named constants are preserved.\n")
	fmt.Fprintf(&r.body, "type %s int32\n\n", name)
	r.body.WriteString("const (\n")
	for _, v := range e.Values {
		fmt.Fprintf(&r.body, "\t%s %s = %d\n", enumValueConst(name, e.Name, v.Name), name, v.Number)
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
	if !hasFields {
		fmt.Fprintf(&r.body, "type %s struct{}\n\n", name)
		return
	}
	fmt.Fprintf(&r.body, "type %s struct {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
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
			fmt.Fprintf(&r.body, "// %s is the %s variant. It carries no payload.\n", wrapper, f.JSONName)
			fmt.Fprintf(&r.body, "type %s struct{}\n\n", wrapper)
		default:
			fmt.Fprintf(&r.body, "// %s is the %s variant.\n", wrapper, f.JSONName)
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
	name := localName(svc.FullName)
	r.features.context = true
	r.features.grpc = true
	r.features.proto = true

	fmt.Fprintf(&r.body, "// %s is the generated client for %s.\n", name, svc.FullName)
	r.body.WriteString("// Every transport error is converted to *GestaltError.\n")
	fmt.Fprintf(&r.body, "type %s struct {\n\tclient proto.%sClient\n}\n\n", name, name)
	fmt.Fprintf(&r.body, "// New%s creates a %s client over an injected gRPC connection.\n", name, name)
	fmt.Fprintf(&r.body, "func New%s(conn grpc.ClientConnInterface) *%s {\n", name, name)
	fmt.Fprintf(&r.body, "\treturn &%s{client: proto.New%sClient(conn)}\n}\n\n", name, name)

	for _, method := range svc.Methods {
		r.renderMethod(name, method)
	}
	for _, method := range svc.Methods {
		if method.Stream != model.Unary {
			r.renderStreamWrapper(name, method)
		}
	}
}

// methodRequest renders the request parameter and wire argument of a method
// that sends a single request message.
func (r *renderer) methodRequest(m *model.Method) (param, arg string) {
	if m.InputIsEmpty {
		r.features.emptypb = true
		return "", "&emptypb.Empty{}"
	}
	requestType := r.messageType(m.Input.FullName)
	return ", request *" + requestType, toWireFunc(requestType) + "(request)"
}

func (r *renderer) renderMethod(svcName string, m *model.Method) {
	recv := fmt.Sprintf("func (c *%s) %s", svcName, m.Name)
	streamType := svcName + m.Name + "Stream"
	switch m.Stream {
	case model.ServerStream:
		param, arg := r.methodRequest(m)
		fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (*%s, error) {\n", recv, param, streamType)
		fmt.Fprintf(&r.body, "\tstream, err := c.client.%s(ctx, %s)\n", m.Name, arg)
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn &%s{stream: stream}, nil\n}\n\n", streamType)
	case model.ClientStream, model.Bidi:
		fmt.Fprintf(&r.body, "%s(ctx context.Context) (*%s, error) {\n", recv, streamType)
		fmt.Fprintf(&r.body, "\tstream, err := c.client.%s(ctx)\n", m.Name)
		r.body.WriteString("\tif err != nil {\n\t\treturn nil, toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\treturn &%s{stream: stream}, nil\n}\n\n", streamType)
	default:
		param, arg := r.methodRequest(m)
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "%s(ctx context.Context%s) error {\n", recv, param)
			fmt.Fprintf(&r.body, "\tif _, err := c.client.%s(ctx, %s); err != nil {\n", m.Name, arg)
			r.body.WriteString("\t\treturn toGestaltError(err)\n\t}\n\treturn nil\n}\n\n")
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "%s(ctx context.Context%s) (*%s, error) {\n", recv, param, responseType)
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

	switch m.Stream {
	case model.ServerStream:
		fmt.Fprintf(&r.body, "// %s is the server stream of %s frames\n", streamType, responseType)
		fmt.Fprintf(&r.body, "// returned by %s.%s.\n", svcName, m.Name)
	case model.ClientStream:
		fmt.Fprintf(&r.body, "// %s is the client stream of %s frames\n", streamType, requestType)
		fmt.Fprintf(&r.body, "// accepted by %s.%s.\n", svcName, m.Name)
	default:
		fmt.Fprintf(&r.body, "// %s is the bidirectional stream opened by\n", streamType)
		fmt.Fprintf(&r.body, "// %s.%s.\n", svcName, m.Name)
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

// assemble prepends the package clause and import header derived from the
// rendered body.
func (r *renderer) assemble() string {
	var b strings.Builder
	b.WriteString("package client\n\n")
	var std, ext []string
	if r.features.context {
		std = append(std, `"context"`)
	}
	if r.features.time {
		std = append(std, `"time"`)
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
	if len(std)+len(ext) > 0 {
		b.WriteString("import (\n")
		for _, imp := range std {
			b.WriteString("\t" + imp + "\n")
		}
		if len(std) > 0 && len(ext) > 0 {
			b.WriteString("\n")
		}
		for _, imp := range ext {
			b.WriteString("\t" + imp + "\n")
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(r.body.String())
	return strings.TrimRight(b.String(), "\n") + "\n"
}
