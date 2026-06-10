package rust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// features tracks which imports a generated file needs; the use header is
// assembled after the body renders.
type features struct {
	v1        bool // the crate::generated::v1 wire alias
	streamExt bool // tokio_stream::StreamExt for stream mapping
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

// fnToWire returns the converter function for reference kinds converted by a
// unary function, or "" when conversion is identity or inline.
func (r *renderer) fnToWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, toWireFunc(ref.Message))
	case model.KindTimestamp:
		r.use("to_wire_timestamp")
		return "to_wire_timestamp"
	case model.KindDuration:
		r.use("to_wire_duration")
		return "to_wire_duration"
	case model.KindJSONStruct:
		r.use("to_wire_struct")
		return "to_wire_struct"
	case model.KindJSONValue:
		r.use("to_wire_value")
		return "to_wire_value"
	case model.KindRPCStatus:
		r.use("to_wire_status")
		return "to_wire_status"
	default:
		return ""
	}
}

func (r *renderer) fnFromWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, fromWireFunc(ref.Message))
	case model.KindTimestamp:
		r.use("from_wire_timestamp")
		return "from_wire_timestamp"
	case model.KindDuration:
		r.use("from_wire_duration")
		return "from_wire_duration"
	case model.KindJSONStruct:
		r.use("from_wire_struct")
		return "from_wire_struct"
	case model.KindJSONValue:
		r.use("from_wire_value")
		return "from_wire_value"
	case model.KindRPCStatus:
		r.use("from_wire_status")
		return "from_wire_status"
	default:
		return ""
	}
}

// toWireExpr renders the wire-bound conversion of a singular value. Scalars,
// bytes, and enums convert by identity: open enums are i32 on both sides.
func (r *renderer) toWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindEnum:
		return expr
	case model.KindJSONNull:
		return "prost_types::NullValue::NullValue as i32"
	case model.KindUnit:
		return "()"
	default:
		return r.fnToWire(ref) + "(" + expr + ")"
	}
}

// fromWireExpr renders the native-bound conversion of a singular wire value.
func (r *renderer) fromWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindEnum:
		return expr
	case model.KindJSONNull, model.KindUnit:
		return "()"
	default:
		return r.fnFromWire(ref) + "(" + expr + ")"
	}
}

// identityConv reports whether a reference converts by identity in both
// directions: native and wire types are the same Rust type.
func identityConv(ref *model.TypeRef) bool {
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes || ref.Kind == model.KindEnum
}

// fieldToWire renders the conversion of a whole field value.
func (r *renderer) fieldToWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityConv(f.Elem) {
			return expr
		}
		if fn := r.fnToWire(f.Elem); fn != "" {
			return expr + ".into_iter().map(" + fn + ").collect()"
		}
		return expr + ".into_iter().map(|item| " + r.toWireExpr(f.Elem, "item") + ").collect()"
	case model.KindMap:
		if identityConv(f.MapValue) {
			return expr
		}
		return expr + ".into_iter().map(|(key, item)| (key, " + r.toWireExpr(f.MapValue, "item") + ")).collect()"
	default:
		ref := fieldRef(f)
		if f.Presence == model.ExplicitPresence {
			if identityConv(ref) {
				return expr
			}
			if fn := r.fnToWire(ref); fn != "" {
				return expr + ".map(" + fn + ")"
			}
			return expr + ".map(|item| " + r.toWireExpr(ref, "item") + ")"
		}
		return r.toWireExpr(ref, expr)
	}
}

func (r *renderer) fieldFromWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityConv(f.Elem) {
			return expr
		}
		if fn := r.fnFromWire(f.Elem); fn != "" {
			return expr + ".into_iter().map(" + fn + ").collect()"
		}
		return expr + ".into_iter().map(|item| " + r.fromWireExpr(f.Elem, "item") + ").collect()"
	case model.KindMap:
		if identityConv(f.MapValue) {
			return expr
		}
		return expr + ".into_iter().map(|(key, item)| (key, " + r.fromWireExpr(f.MapValue, "item") + ")).collect()"
	default:
		ref := fieldRef(f)
		if f.Presence == model.ExplicitPresence {
			if identityConv(ref) {
				return expr
			}
			if fn := r.fnFromWire(ref); fn != "" {
				return expr + ".map(" + fn + ")"
			}
			return expr + ".map(|item| " + r.fromWireExpr(ref, "item") + ")"
		}
		return r.fromWireExpr(ref, expr)
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

// isUnitVariant reports whether a oneof member carries no payload in the
// native enum: google.protobuf.NullValue and google.protobuf.Empty members.
func isUnitVariant(f *model.Field) bool {
	return f.Kind == model.KindJSONNull || f.Kind == model.KindUnit
}

func (r *renderer) renderEnum(e *model.Enum) {
	name := localName(e.FullName)
	// Open enum: unknown numeric values are preserved, so the type is i32.
	fmt.Fprintf(&r.body, "/// Open enum for `%s`; unknown numeric values are preserved.\n", e.FullName)
	fmt.Fprintf(&r.body, "pub type %s = i32;\n\n", name)
	fmt.Fprintf(&r.body, "/// Named values of `%s`.\n", name)
	fmt.Fprintf(&r.body, "pub mod %s {\n", heckSnake(name))
	for _, v := range e.Values {
		fmt.Fprintf(&r.body, "    /// %s.\n", v.Name)
		fmt.Fprintf(&r.body, "    pub const %s: i32 = %d;\n", v.Name, v.Number)
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderMessage(m *model.Message) {
	name := localName(m.FullName)
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "/// Values of the `%s` oneof in `%s`; the message field is None when unset.\n", o.Name, name)
		r.body.WriteString("#[allow(clippy::enum_variant_names, clippy::large_enum_variant)]\n")
		r.body.WriteString("#[derive(Clone, Debug, PartialEq)]\n")
		fmt.Fprintf(&r.body, "pub enum %s {\n", oneofTypeName(m, o))
		for _, f := range oneofFields(m, o) {
			fmt.Fprintf(&r.body, "    /// The `%s` variant.\n", f.Name)
			if isUnitVariant(f) {
				fmt.Fprintf(&r.body, "    %s,\n", heckUpperCamel(f.Name))
			} else {
				fmt.Fprintf(&r.body, "    %s(%s),\n", heckUpperCamel(f.Name), r.fieldType(f))
			}
		}
		r.body.WriteString("}\n\n")
	}
	fmt.Fprintf(&r.body, "/// Native message type for `%s`.\n", m.FullName)
	r.body.WriteString("#[derive(Clone, Debug, Default, PartialEq)]\n")
	fmt.Fprintf(&r.body, "pub struct %s {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		if f.Presence == model.ExplicitPresence {
			fmt.Fprintf(&r.body, "    /// The `%s` field; None when unset.\n", f.Name)
			fmt.Fprintf(&r.body, "    pub %s: Option<%s>,\n", escapeIdent(f.Name), r.fieldType(f))
		} else {
			fmt.Fprintf(&r.body, "    /// The `%s` field.\n", f.Name)
			fmt.Fprintf(&r.body, "    pub %s: %s,\n", escapeIdent(f.Name), r.fieldType(f))
		}
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "    /// The `%s` oneof; None when unset.\n", o.Name)
		fmt.Fprintf(&r.body, "    pub %s: Option<%s>,\n", escapeIdent(o.Name), oneofTypeName(m, o))
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderConversions(m *model.Message) {
	name := localName(m.FullName)
	wireName := wireTypeName(m.FullName)
	r.features.v1 = true
	param := "value"
	if len(m.Fields) == 0 && len(m.Oneofs) == 0 {
		param = "_value"
	}

	fmt.Fprintf(&r.body, "/// Converts a native `%s` to its wire message.\n", name)
	fmt.Fprintf(&r.body, "pub fn %s(%s: %s) -> v1::%s {\n", toWireFunc(m.FullName), param, name, wireName)
	fmt.Fprintf(&r.body, "    v1::%s {\n", wireName)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		ident := escapeIdent(f.Name)
		fmt.Fprintf(&r.body, "        %s: %s,\n", ident, r.fieldToWire(f, "value."+ident))
	}
	for _, o := range m.Oneofs {
		ident := escapeIdent(o.Name)
		fmt.Fprintf(&r.body, "        %s: value.%s.map(%s),\n", ident, ident, oneofToWireFunc(m, o))
	}
	r.body.WriteString("    }\n}\n\n")

	fmt.Fprintf(&r.body, "/// Converts a wire `%s` to its native message.\n", name)
	fmt.Fprintf(&r.body, "pub fn %s(%s: v1::%s) -> %s {\n", fromWireFunc(m.FullName), param, wireName, name)
	fmt.Fprintf(&r.body, "    %s {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		ident := escapeIdent(f.Name)
		fmt.Fprintf(&r.body, "        %s: %s,\n", ident, r.fieldFromWire(f, "value."+ident))
	}
	for _, o := range m.Oneofs {
		ident := escapeIdent(o.Name)
		fmt.Fprintf(&r.body, "        %s: value.%s.map(%s),\n", ident, ident, oneofFromWireFunc(m, o))
	}
	r.body.WriteString("    }\n}\n\n")

	for _, o := range m.Oneofs {
		r.renderOneofConverters(m, o)
	}
}

func (r *renderer) renderOneofConverters(m *model.Message, o *model.Oneof) {
	unionName := oneofTypeName(m, o)
	wireKind := wireOneofKind(m, o)

	fmt.Fprintf(&r.body, "fn %s(value: %s) -> %s {\n", oneofToWireFunc(m, o), unionName, wireKind)
	r.body.WriteString("    match value {\n")
	for _, f := range oneofFields(m, o) {
		variant := heckUpperCamel(f.Name)
		if isUnitVariant(f) {
			fmt.Fprintf(&r.body, "        %s::%s => %s::%s(%s),\n", unionName, variant, wireKind, variant, unitWirePayload(f))
		} else {
			fmt.Fprintf(&r.body, "        %s::%s(value) => %s::%s(%s),\n", unionName, variant, wireKind, variant, r.toWireExpr(fieldRef(f), "value"))
		}
	}
	r.body.WriteString("    }\n}\n\n")

	fmt.Fprintf(&r.body, "fn %s(value: %s) -> %s {\n", oneofFromWireFunc(m, o), wireKind, unionName)
	r.body.WriteString("    match value {\n")
	for _, f := range oneofFields(m, o) {
		variant := heckUpperCamel(f.Name)
		if isUnitVariant(f) {
			fmt.Fprintf(&r.body, "        %s::%s(_) => %s::%s,\n", wireKind, variant, unionName, variant)
		} else {
			fmt.Fprintf(&r.body, "        %s::%s(value) => %s::%s(%s),\n", wireKind, variant, unionName, variant, r.fromWireExpr(fieldRef(f), "value"))
		}
	}
	r.body.WriteString("    }\n}\n\n")
}

// unitWirePayload renders the wire payload for a native unit variant.
func unitWirePayload(f *model.Field) string {
	if f.Kind == model.KindJSONNull {
		return "prost_types::NullValue::NullValue as i32"
	}
	return "()"
}

// streamWrapper is one typed server-stream wrapper emitted after a client.
type streamWrapper struct {
	name      string // wrapper struct name
	wireFrame string // wire frame message prost ident
	native    string // native frame type
	fromFn    string // wire-to-native frame converter
}

func (r *renderer) renderClient(svc *model.Service) {
	name := localName(svc.FullName)
	r.features.v1 = true
	r.use("GestaltError")
	module := wireClientModule(svc)
	wireType := wireClientType(svc)

	fmt.Fprintf(&r.body, "/// Client for the `%s` service.\n", svc.FullName)
	fmt.Fprintf(&r.body, "pub struct %sClient {\n", name)
	fmt.Fprintf(&r.body, "    inner: v1::%s::%s<tonic::transport::Channel>,\n", module, wireType)
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl %sClient {\n", name)
	r.body.WriteString("    /// Creates a client over an established channel.\n")
	r.body.WriteString("    pub fn new(channel: tonic::transport::Channel) -> Self {\n")
	r.body.WriteString("        Self {\n")
	fmt.Fprintf(&r.body, "            inner: v1::%s::%s::new(channel),\n", module, wireType)
	r.body.WriteString("        }\n    }\n\n")

	var wrappers []streamWrapper
	for _, method := range svc.Methods {
		wrappers = append(wrappers, r.renderMethod(svc, method)...)
	}
	r.body.WriteString("}\n\n")

	for _, w := range wrappers {
		r.renderStreamWrapper(w)
	}
}

func (r *renderer) renderMethod(svc *model.Service, m *model.Method) []streamWrapper {
	methodName := escapeIdent(heckSnake(m.Name))
	requestParam := ""
	requestArg := "()"
	if !m.InputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		switch m.Stream {
		case model.ClientStream, model.Bidi:
			r.features.streamExt = true
			requestParam = "requests: impl tokio_stream::Stream<Item = " + requestType + "> + Send + 'static"
			requestArg = "requests.map(" + r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + ")"
		default:
			requestParam = "request: " + requestType
			requestArg = r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		}
	}
	streamInput := requestParam != "" && (m.Stream == model.ClientStream || m.Stream == model.Bidi)

	switch m.Stream {
	case model.ServerStream, model.Bidi:
		responseType := r.messageType(m.Output.FullName)
		wrapper := streamWrapper{
			name:      localName(svc.FullName) + m.Name + "Stream",
			wireFrame: wireTypeName(m.Output.FullName),
			native:    responseType,
			fromFn:    r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)),
		}
		fmt.Fprintf(&r.body, "    /// Calls `%s.%s`, returning a stream of converted frames.\n", svc.FullName, m.Name)
		r.renderSignature(methodName, requestParam, "Result<"+wrapper.name+", GestaltError>", streamInput)
		fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await?;\n", methodName, requestArg)
		fmt.Fprintf(&r.body, "        Ok(%s {\n            inner: response.into_inner(),\n        })\n    }\n\n", wrapper.name)
		return []streamWrapper{wrapper}
	default:
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "    /// Calls `%s.%s`.\n", svc.FullName, m.Name)
			r.renderSignature(methodName, requestParam, "Result<(), GestaltError>", streamInput)
			fmt.Fprintf(&r.body, "        self.inner.%s(%s).await?;\n        Ok(())\n    }\n\n", methodName, requestArg)
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "    /// Calls `%s.%s`.\n", svc.FullName, m.Name)
			r.renderSignature(methodName, requestParam, "Result<"+responseType+", GestaltError>", streamInput)
			fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await?;\n", methodName, requestArg)
			fmt.Fprintf(&r.body, "        Ok(%s(response.into_inner()))\n    }\n\n", r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
		}
		return nil
	}
}

// renderSignature opens a client method; stream-input methods break the
// parameter list across lines, matching rustfmt.
func (r *renderer) renderSignature(methodName, requestParam, returnType string, streamInput bool) {
	if streamInput {
		fmt.Fprintf(&r.body, "    pub async fn %s(\n        &mut self,\n        %s,\n    ) -> %s {\n", methodName, requestParam, returnType)
		return
	}
	params := "&mut self"
	if requestParam != "" {
		params += ", " + requestParam
	}
	fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> %s {\n", methodName, params, returnType)
}

func (r *renderer) renderStreamWrapper(w streamWrapper) {
	fmt.Fprintf(&r.body, "/// Stream of converted `%s` frames; transport errors convert to GestaltError.\n", w.native)
	fmt.Fprintf(&r.body, "pub struct %s {\n", w.name)
	fmt.Fprintf(&r.body, "    inner: tonic::Streaming<v1::%s>,\n}\n\n", w.wireFrame)
	fmt.Fprintf(&r.body, "impl %s {\n", w.name)
	r.body.WriteString("    /// Receives the next frame, or None when the stream ends.\n")
	fmt.Fprintf(&r.body, "    pub async fn recv(&mut self) -> Result<Option<%s>, GestaltError> {\n", w.native)
	fmt.Fprintf(&r.body, "        Ok(self.inner.message().await?.map(%s))\n    }\n}\n\n", w.fromFn)
}

// assemble prepends the module doc and use header derived from the rendered
// body.
func (r *renderer) assemble() string {
	var b strings.Builder
	fmt.Fprintf(&b, "//! Generated native types, wire conversions, and clients for %s.proto.\n\n", r.base)

	var uses []string
	if r.features.v1 {
		uses = append(uses, "use crate::generated::v1;")
	}
	var crossBases []string
	for base := range r.features.cross {
		crossBases = append(crossBases, base)
	}
	sort.Strings(crossBases)
	for _, base := range crossBases {
		names := sortedKeys(r.features.cross[base])
		uses = append(uses, fmt.Sprintf("use crate::%s_client::{%s};", base, strings.Join(names, ", ")))
	}
	if len(r.features.support) > 0 {
		uses = append(uses, fmt.Sprintf("use crate::rpc_support::{%s};", strings.Join(sortedKeys(r.features.support), ", ")))
	}
	if r.features.streamExt {
		uses = append(uses, "use tokio_stream::StreamExt;")
	}
	sort.Strings(uses)
	for _, u := range uses {
		b.WriteString(u)
		b.WriteString("\n")
	}
	if len(uses) > 0 {
		b.WriteString("\n")
	}

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
