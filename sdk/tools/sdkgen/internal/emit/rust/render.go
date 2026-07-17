package rust

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// moduleKind selects which of the two generated modules per proto file is
// being rendered: the public module (native types and clients) or the
// crate-private codec module (wire converters).
type moduleKind int

const (
	modulePublic moduleKind = iota
	moduleCodec
)

// features tracks which imports a generated file needs; the use header is
// assembled after the body renders.
type features struct {
	v1           bool                       // the crate::generated::v1 wire alias
	restMetadata bool                       // public clients import method metadata constants
	unaryTransport bool                     // public AppClient imports UnaryTransport
	streamExt    bool                       // tokio_stream::StreamExt for stream mapping
	supportTypes map[string]bool            // imported from the public rpc_support module
	supportFns   map[string]bool            // imported from the codec support module
	invokeUses   map[string]bool            // imported from the public invoke_support module
	prostTypes   bool                       // google.protobuf.Empty via prost_types
	crossPublic  map[string]map[string]bool // public module base -> imported native type names
	crossCodec   map[string]map[string]bool // codec module base -> imported converter names
	wireJSONDone map[string]bool            // wire message full names with protobuf JSON helpers
}

type renderer struct {
	idx          *index
	base         string
	wireBase     string
	kind         moduleKind
	publicClient bool
	docIntro     string
	features     features
	body         strings.Builder
}

func newRenderer(idx *index, base, wireBase string, kind moduleKind, publicClient bool) *renderer {
	if wireBase == "" {
		wireBase = base
	}
	return &renderer{
		idx:          idx,
		base:         base,
		wireBase:     wireBase,
		kind:         kind,
		publicClient: publicClient,
		features: features{
			supportTypes: map[string]bool{},
			supportFns:   map[string]bool{},
			invokeUses:   map[string]bool{},
			crossPublic:  map[string]map[string]bool{},
			crossCodec:   map[string]map[string]bool{},
			wireJSONDone: map[string]bool{},
		},
	}
}

func (r *renderer) publicBase(protoFile string) string {
	return generatedFileBase(protoFile)
}

func (r *renderer) gestaltErrorRef() string {
	if r.publicClient {
		return "crate::public::generated::rpc_support"
	}
	return "crate::rpc_support"
}

// useType records an import of a public type from the shared rpc_support
// module.
func (r *renderer) useType(name string) {
	r.features.supportTypes[name] = true
}

// useFn records an import of a well-known-type converter from the shared
// codec support module.
func (r *renderer) useFn(name string) {
	r.features.supportFns[name] = true
}

// useInvoke records an import from the shared invoke_support module, emitted
// for json_result methods.
func (r *renderer) useInvoke(name string) {
	r.features.invokeUses[name] = true
}

// typeRef records an import of a native type declared in protoFile's public
// module and returns the name unchanged. References from a public module to
// its own declarations are not imports; codec modules always import native
// types from their public siblings.
func (r *renderer) typeRef(protoFile, name string) string {
	base := r.publicBase(protoFile)
	if r.kind == modulePublic && base == r.base {
		return name
	}
	if r.features.crossPublic[base] == nil {
		r.features.crossPublic[base] = map[string]bool{}
	}
	r.features.crossPublic[base][name] = true
	return name
}

// hostRef records an import of a helper from the shared crate-private
// host-service transport module and returns the name unchanged.
func (r *renderer) hostRef(name string) string {
	if r.features.crossCodec["host_service"] == nil {
		r.features.crossCodec["host_service"] = map[string]bool{}
	}
	r.features.crossCodec["host_service"][name] = true
	return name
}

// convRef records an import of a converter from protoFile's codec module and
// returns the name unchanged. References from a codec module to its own
// converters are not imports.
func (r *renderer) convRef(protoFile, name string) string {
	base := r.publicBase(protoFile)
	if r.kind == moduleCodec && base == r.base {
		return name
	}
	if r.features.crossCodec[base] == nil {
		r.features.crossCodec[base] = map[string]bool{}
	}
	r.features.crossCodec[base][name] = true
	return name
}

func (r *renderer) messageType(fullName string) string {
	return r.typeRef(r.idx.messages[fullName].ProtoFile, localName(fullName))
}

func (r *renderer) enumType(fullName string) string {
	return r.typeRef(r.idx.enums[fullName].ProtoFile, localName(fullName))
}

// fnToWire returns the converter function for reference kinds converted by a
// unary function, or "" when conversion is identity or inline.
func (r *renderer) fnToWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.convRef(r.idx.messages[ref.Message].ProtoFile, toWireFunc(ref.Message))
	case model.KindTimestamp:
		r.useFn("to_wire_timestamp")
		return "to_wire_timestamp"
	case model.KindDuration:
		r.useFn("to_wire_duration")
		return "to_wire_duration"
	case model.KindJSONStruct:
		r.useFn("to_wire_struct")
		return "to_wire_struct"
	case model.KindJSONValue:
		r.useFn("to_wire_value")
		return "to_wire_value"
	case model.KindRPCStatus:
		r.useFn("to_wire_status")
		return "to_wire_status"
	default:
		return ""
	}
}

func (r *renderer) fnFromWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.convRef(r.idx.messages[ref.Message].ProtoFile, fromWireFunc(ref.Message))
	case model.KindTimestamp:
		r.useFn("from_wire_timestamp")
		return "from_wire_timestamp"
	case model.KindDuration:
		r.useFn("from_wire_duration")
		return "from_wire_duration"
	case model.KindJSONStruct:
		r.useFn("from_wire_struct")
		return "from_wire_struct"
	case model.KindJSONValue:
		r.useFn("from_wire_value")
		return "from_wire_value"
	case model.KindRPCStatus:
		r.useFn("from_wire_status")
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

// docComment writes a proto leading comment as rustdoc lines at the given
// indent, followed by one blank doc line so the generic line every caller
// emits next starts a new paragraph. Empty docs write nothing.
func (r *renderer) docComment(indent, doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		if line = sanitizeDocLine(line); line == "" {
			r.body.WriteString(indent + "///\n")
		} else {
			r.body.WriteString(indent + "/// " + line + "\n")
		}
	}
	r.body.WriteString(indent + "///\n")
}

// bareURL matches http(s) URLs; rustdoc's bare_urls lint rejects URLs not
// wrapped in angle brackets.
var bareURL = regexp.MustCompile(`https?://[^\s<>]+`)

// bracketEscaper escapes square brackets so prose never parses as a broken
// intra-doc link, which rustdoc rejects under -D warnings.
var bracketEscaper = strings.NewReplacer("[", `\[`, "]", `\]`)

// sanitizeDocLine rewrites one proto comment line so rustdoc accepts it under
// -D warnings: square brackets are escaped and bare URLs are wrapped in angle
// brackets. Backticks pass through unchanged.
func sanitizeDocLine(line string) string {
	var b strings.Builder
	last := 0
	for _, loc := range bareURL.FindAllStringIndex(line, -1) {
		b.WriteString(bracketEscaper.Replace(line[last:loc[0]]))
		url := line[loc[0]:loc[1]]
		if loc[0] > 0 && line[loc[0]-1] == '<' {
			b.WriteString(url) // already wrapped by the comment author
		} else {
			// Trailing sentence punctuation is prose, not address.
			trimmed := strings.TrimRight(url, ".,;:!?")
			b.WriteString("<" + trimmed + ">" + url[len(trimmed):])
		}
		last = loc[1]
	}
	b.WriteString(bracketEscaper.Replace(line[last:]))
	return b.String()
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
	r.docComment("", e.Doc)
	fmt.Fprintf(&r.body, "/// Named values of `%s`.\n", name)
	fmt.Fprintf(&r.body, "pub mod %s {\n", heckSnake(name))
	for _, v := range e.Values {
		r.docComment("    ", v.Doc)
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
		if r.publicClient {
			r.body.WriteString("#[derive(Clone, Debug, PartialEq, serde::Serialize, serde::Deserialize)]\n")
		} else {
			r.body.WriteString("#[derive(Clone, Debug, PartialEq)]\n")
		}
		fmt.Fprintf(&r.body, "pub enum %s {\n", oneofTypeName(m, o))
		for _, f := range oneofFields(m, o) {
			r.docComment("    ", f.Doc)
			fmt.Fprintf(&r.body, "    /// The `%s` variant.\n", f.Name)
			if isUnitVariant(f) {
				fmt.Fprintf(&r.body, "    %s,\n", heckUpperCamel(f.Name))
			} else {
				fmt.Fprintf(&r.body, "    %s(%s),\n", heckUpperCamel(f.Name), r.fieldType(f))
			}
		}
		r.body.WriteString("}\n\n")
	}
	r.docComment("", m.Doc)
	fmt.Fprintf(&r.body, "/// Native message type for `%s`.\n", m.FullName)
	if r.publicClient {
		r.body.WriteString("#[derive(Clone, Debug, Default, PartialEq, serde::Serialize, serde::Deserialize)]\n")
		r.body.WriteString("#[serde(rename_all = \"camelCase\")]\n")
	} else {
		r.body.WriteString("#[derive(Clone, Debug, Default, PartialEq)]\n")
	}
	fmt.Fprintf(&r.body, "pub struct %s {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		r.docComment("    ", f.Doc)
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

// renderConversions emits the crate-private wire converters a message needs.
// Only directions with callers are emitted: requests convert to the wire,
// responses from it, so every pub(crate) converter stays live at crate level.
func (r *renderer) renderConversions(m *model.Message) {
	needTo := r.idx.needToWire[m.FullName]
	needFrom := r.idx.needFromWire[m.FullName]
	if !needTo && !needFrom {
		return
	}
	name := r.typeRef(m.ProtoFile, localName(m.FullName))
	wireName := wireTypeName(m.FullName)
	r.features.v1 = true
	param := "value"
	if len(m.Fields) == 0 && len(m.Oneofs) == 0 {
		param = "_value"
	}

	if needTo {
		wireMsg := m
		if r.publicClient {
			wireMsg = r.idx.wireMessages[m.FullName]
			if wireMsg == nil {
				wireMsg = m
			}
		}
		nativeFields := map[string]*model.Field{}
		nativeOneofs := map[string]bool{}
		if r.publicClient {
			for _, f := range m.Fields {
				if f.OneofIndex < 0 {
					nativeFields[f.Name] = f
				}
			}
			for _, o := range m.Oneofs {
				nativeOneofs[o.Name] = true
			}
		}

		fmt.Fprintf(&r.body, "/// Converts a native `%s` to its wire message.\n", name)
		fmt.Fprintf(&r.body, "pub(crate) fn %s(%s: %s) -> v1::%s {\n", toWireFunc(m.FullName), param, name, wireName)
		fmt.Fprintf(&r.body, "    v1::%s {\n", wireName)
		for _, f := range wireMsg.Fields {
			if f.OneofIndex >= 0 {
				continue
			}
			ident := escapeIdent(f.Name)
			if r.publicClient {
				if native, ok := nativeFields[f.Name]; ok {
					fmt.Fprintf(&r.body, "        %s: %s,\n", ident, r.fieldToWire(native, "value."+ident))
				} else {
					fmt.Fprintf(&r.body, "        %s: %s,\n", ident, wireFieldDefault(f))
				}
			} else {
				fmt.Fprintf(&r.body, "        %s: %s,\n", ident, r.fieldToWire(f, "value."+ident))
			}
		}
		for _, o := range wireMsg.Oneofs {
			ident := escapeIdent(o.Name)
			if r.publicClient && !nativeOneofs[o.Name] {
				fmt.Fprintf(&r.body, "        %s: None,\n", ident)
			} else {
				fmt.Fprintf(&r.body, "        %s: value.%s.map(%s),\n", ident, ident, oneofToWireFunc(m, o))
			}
		}
		if r.publicClient {
			r.body.WriteString("        ..Default::default()\n")
		}
		r.body.WriteString("    }\n}\n\n")
	}

	if needFrom {
		fmt.Fprintf(&r.body, "/// Converts a wire `%s` to its native message.\n", name)
		fmt.Fprintf(&r.body, "pub(crate) fn %s(%s: v1::%s) -> %s {\n", fromWireFunc(m.FullName), param, wireName, name)
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
	}

	for _, o := range m.Oneofs {
		if needTo {
			r.renderOneofToWire(m, o)
		}
		if needFrom {
			r.renderOneofFromWire(m, o)
		}
	}

	if r.publicClient && r.idx.needWireJSON[m.FullName] {
		r.ensureWireProtoJSON(m.FullName)
	}
}

func (r *renderer) renderOneofToWire(m *model.Message, o *model.Oneof) {
	unionName := r.typeRef(m.ProtoFile, oneofTypeName(m, o))
	wireKind := wireOneofKind(m, o)

	fmt.Fprintf(&r.body, "pub(crate) fn %s(value: %s) -> %s {\n", oneofToWireFunc(m, o), unionName, wireKind)
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
}

func (r *renderer) renderOneofFromWire(m *model.Message, o *model.Oneof) {
	unionName := r.typeRef(m.ProtoFile, oneofTypeName(m, o))
	wireKind := wireOneofKind(m, o)

	fmt.Fprintf(&r.body, "pub(crate) fn %s(value: %s) -> %s {\n", oneofFromWireFunc(m, o), wireKind, unionName)
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
	doc       string // proto comment of the method returning the stream
	wireFrame string // wire frame message prost ident
	native    string // native frame type
	fromFn    string // wire-to-native frame converter
}

func (r *renderer) renderClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName
	r.features.v1 = true
	r.useType("GestaltError")
	module := wireClientModule(svc)
	wireType := wireClientType(svc)

	// Host-bound clients route through the shared host-service transport so
	// connect() can attach the relay token and binding metadata; the explicit
	// channel constructor wraps its channel with empty metadata.
	transport := "tonic::transport::Channel"
	newArg := "channel"
	if svc.HostBinding != "" {
		transport = r.hostRef("HostServiceChannel")
		newArg = r.hostRef("plain_channel") + "(channel)"
	}

	ctxField := r.contextFieldOf(svc)
	contextInit := ""
	if ctxField != nil {
		contextInit = "\n            context: None,"
	}

	r.docComment("", svc.Doc)
	fmt.Fprintf(&r.body, "/// Client for the `%s` service.\n", svc.FullName)
	fmt.Fprintf(&r.body, "pub struct %s {\n", name)
	fmt.Fprintf(&r.body, "    inner: v1::%s::%s<%s>,\n", module, wireType, transport)
	r.body.WriteString("    timeout: Option<std::time::Duration>,\n")
	if ctxField != nil {
		fmt.Fprintf(&r.body, "    context: Option<%s>,\n", r.fieldType(ctxField))
	}
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl %s {\n", name)
	r.body.WriteString("    /// Creates a client over an established channel.\n")
	r.body.WriteString("    pub fn new(channel: tonic::transport::Channel) -> Self {\n")
	r.body.WriteString("        Self {\n")
	fmt.Fprintf(&r.body, "            inner: v1::%s::%s::new(%s),\n            timeout: None,%s\n", module, wireType, newArg, contextInit)
	r.body.WriteString("        }\n    }\n\n")

	r.body.WriteString("    /// Sets a deadline applied to every unary call; calls that run past it\n")
	r.body.WriteString("    /// fail with DEADLINE_EXCEEDED. Streaming calls are unaffected.\n")
	r.body.WriteString("    pub fn with_timeout(mut self, timeout: std::time::Duration) -> Self {\n")
	r.body.WriteString("        self.timeout = Some(timeout);\n        self\n    }\n\n")

	if ctxField != nil {
		r.body.WriteString("    /// Sets the default request context, injected into outgoing requests\n")
		r.body.WriteString("    /// that do not carry one.\n")
		fmt.Fprintf(&r.body, "    pub fn with_context(mut self, context: %s) -> Self {\n", r.fieldType(ctxField))
		r.body.WriteString("        self.context = Some(context);\n        self\n    }\n\n")
	}

	if svc.HostBinding != "" {
		fmt.Fprintf(&r.body, "    /// Connects to the `%s` host service described by the environment.\n", svc.HostBinding)
		r.body.WriteString("    pub async fn connect() -> Result<Self, GestaltError> {\n")
		r.body.WriteString("        Self::connect_named(\"\").await\n    }\n\n")
		fmt.Fprintf(&r.body, "    /// Connects to the named `%s` host-service binding.\n", svc.HostBinding)
		r.body.WriteString("    pub async fn connect_named(name: &str) -> Result<Self, GestaltError> {\n")
		r.body.WriteString("        Ok(Self {\n")
		fmt.Fprintf(&r.body, "            inner: v1::%s::%s::new(%s(%q, name).await?),\n            timeout: None,%s\n",
			module, wireType, r.hostRef("connect_host_service"), svc.HostBinding, contextInit)
		r.body.WriteString("        })\n    }\n\n")
	}

	var wrappers []streamWrapper
	var framed []framedReadWrapper
	for _, method := range svc.Methods {
		streams, data := r.renderMethod(svc, method)
		wrappers = append(wrappers, streams...)
		framed = append(framed, data...)
	}
	r.body.WriteString("}\n\n")

	for _, method := range svc.Methods {
		if len(method.OptionalSignature) > 0 {
			r.renderOptionsStruct(svc, method)
		}
	}
	for _, w := range framed {
		r.renderFramedReadWrapper(w)
	}
	for _, w := range wrappers {
		r.renderStreamWrapper(w)
	}
}

// contextFieldOf returns a service's first request context field for provider
// clients, or nil for the public SDK where RequestContext is server-filled.
func (r *renderer) contextFieldOf(svc *model.Service) *model.Field {
	if r.publicClient {
		return nil
	}
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

// contextPrep renders the default-context injection lines of a method that
// takes a single owned request.
func (r *renderer) contextPrep(m *model.Method) []string {
	if m.InputIsEmpty || findField(m.Input, "context") == nil {
		return nil
	}
	return []string{
		"        let mut request = request;",
		"        if request.context.is_none() {",
		"            request.context = self.context.clone();",
		"        }",
	}
}

func findField(m *model.Message, protoName string) *model.Field {
	for _, f := range m.Fields {
		if f.Name == protoName {
			return f
		}
	}
	return nil
}

// timeoutPrep renders the unary request wrap applying the client-level
// timeout as the per-request gRPC deadline, returning the request expression
// the call site passes to the wire client. Streaming calls keep passing their
// message or stream directly.
func (r *renderer) timeoutPrep(requestArg string) string {
	r.body.WriteString("        let mut tonic_request = tonic::Request::new(" + requestArg + ");\n")
	r.body.WriteString("        if let Some(timeout) = self.timeout {\n")
	r.body.WriteString("            tonic_request.set_timeout(timeout);\n")
	r.body.WriteString("        }\n")
	return "tonic_request"
}

// structLiteral renders a native struct literal setting the given field
// initializers; fields and oneofs outside the covered set keep their default
// values.
func structLiteral(m *model.Message, typeName string, inits []string, covered map[string]bool) string {
	needsDefault := false
	for _, f := range m.Fields {
		if f.OneofIndex < 0 && !covered[f.Name] {
			needsDefault = true
		}
	}
	for _, o := range m.Oneofs {
		if !covered[o.Name] {
			needsDefault = true
		}
	}
	if needsDefault {
		inits = append(inits, "..Default::default()")
	}
	return typeName + " { " + strings.Join(inits, ", ") + " }"
}

// collapsed describes how an annotated response collapses at the API
// boundary: the ergonomic return type and the statements that derive the
// return value from the converted `response`.
type collapsed struct {
	returnType string
	errorType  string
	grpcPrep   []string
	lines      []string
}

// collapseOutput returns the response collapse for a method, or nil when the
// faithful response type is returned.
func (r *renderer) collapseOutput(m *model.Method) *collapsed {
	if m.Output == nil {
		return nil
	}
	if r.publicClient {
		if jr := m.JsonResult; jr != nil {
			status := findField(m.Output, jr.Status)
			body := findField(m.Output, jr.Body)
			appClone, opClone := `String::new()`, `String::new()`
			if m.Input != nil {
				if f := findField(m.Input, "app"); f != nil {
					appClone = fmt.Sprintf("request.%s.clone()", escapeIdent(f.Name))
				}
				if f := findField(m.Input, "operation"); f != nil {
					opClone = fmt.Sprintf("request.%s.clone()", escapeIdent(f.Name))
				}
			}
			return &collapsed{
				returnType: "serde_json::Value",
				errorType:  "crate::public::generated::invoke_support::InvokeError",
				grpcPrep: []string{
					fmt.Sprintf("        let invoke_context_app = %s;", appClone),
					fmt.Sprintf("        let invoke_context_operation = %s;", opClone),
				},
				lines: []string{
					fmt.Sprintf(
						"crate::public::generated::invoke_support::decode_app_result(invoke_context_app.as_str(), invoke_context_operation.as_str(), response.%s, &response.%s).map_err(crate::public::generated::invoke_support::InvokeError::from)",
						escapeIdent(status.Name), escapeIdent(body.Name),
					),
				},
			}
		}
	}
	if or := m.Output.OptionalResult; or != nil {
		guard := findField(m.Output, or.Guard)
		value := findField(m.Output, or.Value)
		valueExpr := "Some(response." + escapeIdent(value.Name) + ")"
		if value.Presence == model.ExplicitPresence {
			valueExpr = "response." + escapeIdent(value.Name)
		}
		return &collapsed{
			returnType: "Option<" + r.fieldType(value) + ">",
			lines: []string{
				fmt.Sprintf("        if !response.%s {", escapeIdent(guard.Name)),
				"            return Ok(None);",
				"        }",
				"        Ok(" + valueExpr + ")",
			},
		}
	}
	if k := m.Output.Keyed; k != nil {
		entries := findField(m.Output, k.Entries)
		entry := r.idx.messages[entries.Elem.Message]
		key := findField(entry, k.Key)
		present := findField(entry, k.Present)
		value := findField(entry, k.Value)
		presentExpr := "entry." + escapeIdent(present.Name)
		if present.Presence == model.ExplicitPresence {
			presentExpr += ".unwrap_or_default()"
		}
		insert := []string{
			fmt.Sprintf("                out.insert(entry.%s, entry.%s);", escapeIdent(key.Name), escapeIdent(value.Name)),
		}
		if value.Presence == model.ExplicitPresence {
			insert = []string{
				fmt.Sprintf("                if let Some(value) = entry.%s {", escapeIdent(value.Name)),
				fmt.Sprintf("                    out.insert(entry.%s, value);", escapeIdent(key.Name)),
				"                }",
			}
		}
		lines := []string{
			"        let mut out = std::collections::BTreeMap::new();",
			fmt.Sprintf("        for entry in response.%s {", escapeIdent(entries.Name)),
			fmt.Sprintf("            if %s {", presentExpr),
		}
		lines = append(lines, insert...)
		lines = append(lines, "            }", "        }", "        Ok(out)")
		return &collapsed{
			returnType: "std::collections::BTreeMap<" + scalarType(key.Scalar) + ", " + r.fieldType(value) + ">",
			lines:      lines,
		}
	}
	if m.Output.Unwrap != "" {
		field := findField(m.Output, m.Output.Unwrap)
		returnType := r.fieldType(field)
		// Presence-bearing unwraps surface the field's optionality directly.
		if field.Presence == model.ExplicitPresence {
			returnType = "Option<" + returnType + ">"
		}
		return &collapsed{
			returnType: returnType,
			lines:      []string{fmt.Sprintf("        Ok(response.%s)", escapeIdent(field.Name))},
		}
	}
	return nil
}

// renderMethod renders the surfaces of one method: annotated methods own the
// natural snake_case name and the descriptor-faithful form keeps a `_raw`
// suffix, mirroring the TypeScript emitter's dispatch.
func (r *renderer) renderMethod(svc *model.Service, m *model.Method) ([]streamWrapper, []framedReadWrapper) {
	switch {
	case m.Initial != nil && m.Stream == model.ServerStream:
		data := r.renderFramedRead(svc, m)
		return r.renderFaithfulMethod(svc, m, true), []framedReadWrapper{data}
	case m.Initial != nil && m.Stream == model.ClientStream:
		r.renderFramedWrite(svc, m)
		return r.renderFaithfulMethod(svc, m, true), nil
	case m.Stream == model.Unary && (m.JsonResult != nil || len(m.Signature) > 0 || len(m.OptionalSignature) > 0 || r.collapseOutput(m) != nil):
		r.renderErgonomicUnary(svc, m)
		return r.renderFaithfulMethod(svc, m, true), nil
	default:
		return r.renderFaithfulMethod(svc, m, false), nil
	}
}

// optionsTypeName names the per-method struct carrying a method's
// optional_signature fields.
func optionsTypeName(svc *model.Service, m *model.Method) string {
	return localName(svc.FullName) + m.Name + "Options"
}

// renderOptionsStruct renders the options struct a method with an
// optional_signature annotation takes as its trailing parameter, adjacent to
// the client that takes it.
func (r *renderer) renderOptionsStruct(svc *model.Service, m *model.Method) {
	fmt.Fprintf(&r.body, "/// Optional parameters of [`%s::%s`]; the default value leaves every\n/// option unset.\n",
		localName(svc.FullName), escapeIdent(publicSnake(m.Name)))
	r.body.WriteString("#[derive(Clone, Debug, Default)]\n")
	fmt.Fprintf(&r.body, "pub struct %s {\n", optionsTypeName(svc, m))
	for _, fieldName := range m.OptionalSignature {
		f := findField(m.Input, fieldName)
		r.docComment("    ", f.Doc)
		if f.Presence == model.ExplicitPresence {
			fmt.Fprintf(&r.body, "    /// The `%s` field; None when unset.\n", f.Name)
			fmt.Fprintf(&r.body, "    pub %s: Option<%s>,\n", escapeIdent(f.Name), r.fieldType(f))
		} else {
			fmt.Fprintf(&r.body, "    /// The `%s` field.\n", f.Name)
			fmt.Fprintf(&r.body, "    pub %s: %s,\n", escapeIdent(f.Name), r.fieldType(f))
		}
	}
	r.body.WriteString("}\n\n")
}

// renderErgonomicUnary renders the annotated surface of a unary method:
// flattened parameters from the signature annotation, a trailing options
// struct from the optional_signature annotation, and a collapsed return from
// the response annotations. Empty-input methods take only the receiver.
func (r *renderer) renderErgonomicUnary(svc *model.Service, m *model.Method) {
	methodName := escapeIdent(publicSnake(m.Name))
	wireName := escapeIdent(heckSnake(m.Name))
	params := "&mut self"
	requestArg := "()"
	var requestLines []string
	if !m.InputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		requestArg = r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		if len(m.Signature) > 0 || len(m.OptionalSignature) > 0 {
			covered := map[string]bool{}
			var inits []string
			for _, name := range m.Signature {
				f := findField(m.Input, name)
				covered[f.Name] = true
				ident := escapeIdent(f.Name)
				paramType := r.fieldType(f)
				if f.Presence == model.ExplicitPresence {
					paramType = "Option<" + paramType + ">"
				}
				params += ", " + ident + ": " + paramType
				inits = append(inits, ident)
			}
			// Optional-signature fields ride in one trailing options struct;
			// the request literal reads them off the options value.
			if len(m.OptionalSignature) > 0 {
				params += ", options: " + optionsTypeName(svc, m)
				for _, name := range m.OptionalSignature {
					f := findField(m.Input, name)
					covered[f.Name] = true
					ident := escapeIdent(f.Name)
					inits = append(inits, ident+": options."+ident)
				}
			}
			// The flattened form has no context parameter, so the literal
			// takes the client default directly.
			if ctxF := findField(m.Input, "context"); ctxF != nil && !covered[ctxF.Name] {
				covered[ctxF.Name] = true
				inits = append(inits, escapeIdent(ctxF.Name)+": self.context.clone()")
			}
			requestLines = append(requestLines,
				"        let request = "+structLiteral(m.Input, requestType, inits, covered)+";")
		} else {
			params += ", request: " + requestType
			requestLines = append(requestLines, r.contextPrep(m)...)
		}
	}

	r.docComment("    ", m.Doc)
	fmt.Fprintf(&r.body, "    /// Calls `%s.%s`.\n", svc.FullName, m.Name)
	if m.JsonResult != nil {
		r.body.WriteString("    /// The result decodes with the standard JSON operation envelope\n    /// semantics; payload failures surface as [`InvokeError`].\n")
	}
	// Flattened signatures mirror the schema annotation; clippy's argument
	// budget does not get a vote on generated surfaces. Optional-signature
	// fields ride in one options argument.
	args := len(m.Signature) + 1
	if len(m.OptionalSignature) > 0 {
		args++
	}
	if args > 7 {
		r.body.WriteString("    #[allow(clippy::too_many_arguments)]\n")
	}
	if m.JsonResult != nil {
		r.renderJsonResultBody(m, methodName, wireName, params, requestArg, requestLines)
		return
	}
	collapse := r.collapseOutput(m)
	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> Result<(), GestaltError> {\n", methodName, params)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		arg := r.timeoutPrep(requestArg)
		fmt.Fprintf(&r.body, "        self.inner.%s(%s).await?;\n        Ok(())\n    }\n\n", wireName, arg)
	case collapse != nil:
		fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> Result<%s, GestaltError> {\n", methodName, params, collapse.returnType)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		arg := r.timeoutPrep(requestArg)
		fmt.Fprintf(&r.body, "        let response = %s(self.inner.%s(%s).await?.into_inner());\n",
			r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)), wireName, arg)
		for _, line := range collapse.lines {
			r.body.WriteString(line + "\n")
		}
		r.body.WriteString("    }\n\n")
	default:
		responseType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> Result<%s, GestaltError> {\n", methodName, params, responseType)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		arg := r.timeoutPrep(requestArg)
		fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await?;\n", wireName, arg)
		fmt.Fprintf(&r.body, "        Ok(%s(response.into_inner()))\n    }\n\n", r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	}
}

// renderJsonResultBody renders the decoded form of a json_result method: the
// envelope decodes to serde_json::Value, transport failures convert through
// GestaltError, and both error kinds surface as InvokeError.
func (r *renderer) renderJsonResultBody(m *model.Method, methodName, wireName, params, requestArg string, requestLines []string) {
	r.useType("GestaltError")
	r.useInvoke("InvokeError")
	r.useInvoke("decode_app_result")
	appExpr, opExpr := `""`, `""`
	if m.Input != nil {
		if f := findField(m.Input, "app"); f != nil {
			requestLines = append(requestLines, "        let invoke_context_app = request."+escapeIdent(f.Name)+".clone();")
			appExpr = "&invoke_context_app"
		}
		if f := findField(m.Input, "operation"); f != nil {
			requestLines = append(requestLines, "        let invoke_context_operation = request."+escapeIdent(f.Name)+".clone();")
			opExpr = "&invoke_context_operation"
		}
	}
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> Result<serde_json::Value, InvokeError> {\n", methodName, params)
	for _, line := range requestLines {
		r.body.WriteString(line + "\n")
	}
	arg := r.timeoutPrep(requestArg)
	fmt.Fprintf(&r.body, "        let response = %s(\n", r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	fmt.Fprintf(&r.body, "            self.inner\n                .%s(%s)\n                .await\n                .map_err(GestaltError::from)?\n                .into_inner(),\n        );\n", wireName, arg)
	fmt.Fprintf(&r.body, "        Ok(decode_app_result(%s, %s, response.%s, &response.%s)?)\n    }\n\n",
		appExpr, opExpr, escapeIdent(status.Name), escapeIdent(body.Name))
}

// framedReadWrapper is the ergonomic payload-stream wrapper emitted after a
// client for a framed server-streaming method.
type framedReadWrapper struct {
	name         string // wrapper struct name
	service      string // service local name, for docs
	method       string // ergonomic method name, for docs
	wireFrame    string // wire frame message prost ident
	fromFn       string // wire-to-native frame converter
	oneofIdent   string // escaped native frame oneof field ident
	enumName     string // native frame oneof enum type
	chunkVariant string // native chunk variant name
	chunkType    string // native chunk payload type
	bytesChunk   bool   // chunk payload is bytes: emit the collect buffer
}

// renderFramedRead renders the annotated surface of a framed server-streaming
// method: the call consumes the header frame and returns its value paired
// with a payload stream of chunk values.
func (r *renderer) renderFramedRead(svc *model.Service, m *model.Method) framedReadWrapper {
	methodName := escapeIdent(publicSnake(m.Name))
	frames := m.Output
	header := findField(frames, m.Initial.HeaderField)
	chunk := findField(frames, m.Initial.ChunkField)
	oneof := frames.Oneofs[header.OneofIndex]
	enumName := r.typeRef(frames.ProtoFile, oneofTypeName(frames, oneof))
	wrapper := framedReadWrapper{
		name:         localName(svc.FullName) + m.Name + "Data",
		service:      localName(svc.FullName),
		method:       methodName,
		wireFrame:    wireTypeName(frames.FullName),
		fromFn:       r.convRef(frames.ProtoFile, fromWireFunc(frames.FullName)),
		oneofIdent:   escapeIdent(oneof.Name),
		enumName:     enumName,
		chunkVariant: heckUpperCamel(chunk.Name),
		chunkType:    r.fieldType(chunk),
		bytesChunk:   chunk.Kind == model.KindBytes,
	}
	r.useType("gestalt_error_code")

	params := "&mut self"
	requestArg := "()"
	if !m.InputIsEmpty {
		params += ", request: " + r.messageType(m.Input.FullName)
		requestArg = r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	}
	headerIdent := escapeIdent(header.Name)
	r.docComment("    ", m.Doc)
	fmt.Fprintf(&r.body, "    /// Calls `%s.%s`, returning the `%s` header and a payload stream.\n", svc.FullName, m.Name, header.Name)
	fmt.Fprintf(&r.body, "    pub async fn %s(%s) -> Result<(%s, %s), GestaltError> {\n",
		methodName, params, r.fieldType(header), wrapper.name)
	for _, line := range r.contextPrep(m) {
		r.body.WriteString(line + "\n")
	}
	fmt.Fprintf(&r.body, "        let mut frames = self.inner.%s(%s).await?.into_inner();\n", escapeIdent(heckSnake(m.Name)), requestArg)
	fmt.Fprintf(&r.body, "        let frame = frames.message().await?.map(%s);\n", wrapper.fromFn)
	fmt.Fprintf(&r.body, "        let %s = match frame.and_then(|frame| frame.%s) {\n", headerIdent, wrapper.oneofIdent)
	fmt.Fprintf(&r.body, "            Some(%s::%s(value)) => value,\n", enumName, heckUpperCamel(header.Name))
	r.body.WriteString("            _ => {\n")
	r.body.WriteString("                return Err(GestaltError::new(\n")
	r.body.WriteString("                    gestalt_error_code::INTERNAL,\n")
	r.body.WriteString("                    \"stream did not begin with the expected header frame\",\n")
	r.body.WriteString("                ));\n")
	r.body.WriteString("            }\n")
	r.body.WriteString("        };\n")
	fmt.Fprintf(&r.body, "        Ok((%s, %s { inner: frames }))\n    }\n\n", headerIdent, wrapper.name)
	return wrapper
}

func (r *renderer) renderFramedReadWrapper(w framedReadWrapper) {
	fmt.Fprintf(&r.body, "/// Payload stream returned by `%s::%s`; the header frame has already been consumed.\n", w.service, w.method)
	fmt.Fprintf(&r.body, "pub struct %s {\n", w.name)
	fmt.Fprintf(&r.body, "    inner: tonic::Streaming<v1::%s>,\n}\n\n", w.wireFrame)
	fmt.Fprintf(&r.body, "impl %s {\n", w.name)
	r.body.WriteString("    /// Receives the next payload chunk, or None when the stream ends.\n")
	fmt.Fprintf(&r.body, "    pub async fn recv(&mut self) -> Result<Option<%s>, GestaltError> {\n", w.chunkType)
	fmt.Fprintf(&r.body, "        match self.inner.message().await?.map(%s) {\n", w.fromFn)
	r.body.WriteString("            None => Ok(None),\n")
	fmt.Fprintf(&r.body, "            Some(frame) => match frame.%s {\n", w.oneofIdent)
	fmt.Fprintf(&r.body, "                Some(%s::%s(value)) => Ok(Some(value)),\n", w.enumName, w.chunkVariant)
	r.body.WriteString("                _ => Err(GestaltError::new(\n")
	r.body.WriteString("                    gestalt_error_code::INTERNAL,\n")
	r.body.WriteString("                    \"unexpected frame in payload stream\",\n")
	r.body.WriteString("                )),\n")
	if !w.bytesChunk {
		r.body.WriteString("            },\n        }\n    }\n}\n\n")
		return
	}
	r.body.WriteString("            },\n        }\n    }\n\n")
	r.body.WriteString("    /// Buffers the remaining payload chunks into one byte vector, like the\n")
	r.body.WriteString("    /// AWS SDK's `ByteStream::collect`.\n")
	r.body.WriteString("    pub async fn collect(&mut self) -> Result<Vec<u8>, GestaltError> {\n")
	r.body.WriteString("        let mut out = Vec::new();\n")
	r.body.WriteString("        while let Some(chunk) = self.recv().await? {\n")
	r.body.WriteString("            out.extend_from_slice(&chunk);\n")
	r.body.WriteString("        }\n        Ok(out)\n    }\n}\n\n")
}

// renderFramedWrite renders the annotated surface of a framed
// client-streaming method: the header frame is sent first, followed by one
// frame per payload chunk.
func (r *renderer) renderFramedWrite(svc *model.Service, m *model.Method) {
	methodName := escapeIdent(publicSnake(m.Name))
	frames := m.Input
	header := findField(frames, m.Initial.HeaderField)
	chunk := findField(frames, m.Initial.ChunkField)
	oneof := frames.Oneofs[header.OneofIndex]
	frameType := r.messageType(frames.FullName)
	enumName := r.typeRef(frames.ProtoFile, oneofTypeName(frames, oneof))
	toWire := r.convRef(frames.ProtoFile, toWireFunc(frames.FullName))
	r.features.streamExt = true

	headerIdent := escapeIdent(header.Name)
	chunkIdent := escapeIdent(chunk.Name)
	covered := map[string]bool{oneof.Name: true}
	headerFrame := structLiteral(frames, frameType, []string{
		fmt.Sprintf("%s: Some(%s::%s(%s))", escapeIdent(oneof.Name), enumName, heckUpperCamel(header.Name), headerIdent),
	}, covered)
	chunkFrame := structLiteral(frames, frameType, []string{
		fmt.Sprintf("%s: Some(%s::%s(chunk))", escapeIdent(oneof.Name), enumName, heckUpperCamel(chunk.Name)),
	}, covered)

	returnType := "()"
	if !m.OutputIsEmpty {
		returnType = r.messageType(m.Output.FullName)
	}
	r.docComment("    ", m.Doc)
	fmt.Fprintf(&r.body, "    /// Calls `%s.%s`, sending the `%s` header frame and then one frame per `%s` chunk.\n",
		svc.FullName, m.Name, header.Name, chunk.Name)
	fmt.Fprintf(&r.body, "    pub async fn %s(\n        &mut self,\n        %s: %s,\n        %s: impl tokio_stream::Stream<Item = %s> + Send + 'static,\n    ) -> Result<%s, GestaltError> {\n",
		methodName, headerIdent, r.fieldType(header), chunkIdent, r.fieldType(chunk), returnType)
	fmt.Fprintf(&r.body, "        let requests = tokio_stream::once(%s)\n", headerFrame)
	fmt.Fprintf(&r.body, "            .chain(%s.map(|chunk| %s))\n", chunkIdent, chunkFrame)
	fmt.Fprintf(&r.body, "            .map(%s);\n", toWire)
	wireName := escapeIdent(heckSnake(m.Name))
	if m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "        self.inner.%s(requests).await?;\n        Ok(())\n    }\n\n", wireName)
	} else {
		fmt.Fprintf(&r.body, "        let response = self.inner.%s(requests).await?;\n", wireName)
		fmt.Fprintf(&r.body, "        Ok(%s(response.into_inner()))\n    }\n\n", r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	}
}

// renderFaithfulMethod renders the descriptor-faithful method. When an
// annotated surface owns the natural name, the faithful variant keeps a
// `_raw` suffix so both remain available.
func (r *renderer) renderFaithfulMethod(svc *model.Service, m *model.Method, rawSuffix bool) []streamWrapper {
	wireMethod := escapeIdent(heckSnake(m.Name))
	methodName := wireMethod
	if rawSuffix {
		methodName = publicSnake(m.Name) + "_raw"
	}
	requestParam := ""
	requestArg := "()"
	if !m.InputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		switch m.Stream {
		case model.ClientStream, model.Bidi:
			r.features.streamExt = true
			requestParam = "requests: impl tokio_stream::Stream<Item = " + requestType + "> + Send + 'static"
			requestArg = "requests.map(" + r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + ")"
		default:
			requestParam = "request: " + requestType
			requestArg = r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		}
	}
	streamInput := requestParam != "" && (m.Stream == model.ClientStream || m.Stream == model.Bidi)
	callLine := fmt.Sprintf("    /// Calls `%s.%s`.\n", svc.FullName, m.Name)
	if rawSuffix {
		callLine = fmt.Sprintf("    /// Calls `%s.%s` with the full request and response messages.\n", svc.FullName, m.Name)
	}

	switch m.Stream {
	case model.ServerStream, model.Bidi:
		responseType := r.messageType(m.Output.FullName)
		wrapper := streamWrapper{
			name:      localName(svc.FullName) + m.Name + "Stream",
			doc:       m.Doc,
			wireFrame: wireTypeName(m.Output.FullName),
			native:    responseType,
			fromFn:    r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)),
		}
		r.docComment("    ", m.Doc)
		fmt.Fprintf(&r.body, "    /// Calls `%s.%s`, returning a stream of converted frames.\n", svc.FullName, m.Name)
		r.renderSignature(methodName, requestParam, "Result<"+wrapper.name+", GestaltError>", streamInput)
		if !streamInput {
			for _, line := range r.contextPrep(m) {
				r.body.WriteString(line + "\n")
			}
		}
		fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await?;\n", wireMethod, requestArg)
		fmt.Fprintf(&r.body, "        Ok(%s {\n            inner: response.into_inner(),\n        })\n    }\n\n", wrapper.name)
		return []streamWrapper{wrapper}
	default:
		if m.OutputIsEmpty {
			r.docComment("    ", m.Doc)
			r.body.WriteString(callLine)
			r.renderSignature(methodName, requestParam, "Result<(), GestaltError>", streamInput)
			if !streamInput {
				for _, line := range r.contextPrep(m) {
					r.body.WriteString(line + "\n")
				}
			}
			arg := requestArg
			if m.Stream == model.Unary {
				arg = r.timeoutPrep(requestArg)
			}
			fmt.Fprintf(&r.body, "        self.inner.%s(%s).await?;\n        Ok(())\n    }\n\n", wireMethod, arg)
		} else {
			responseType := r.messageType(m.Output.FullName)
			r.docComment("    ", m.Doc)
			r.body.WriteString(callLine)
			r.renderSignature(methodName, requestParam, "Result<"+responseType+", GestaltError>", streamInput)
			if !streamInput {
				for _, line := range r.contextPrep(m) {
					r.body.WriteString(line + "\n")
				}
			}
			arg := requestArg
			if m.Stream == model.Unary {
				arg = r.timeoutPrep(requestArg)
			}
			fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await?;\n", wireMethod, arg)
			fmt.Fprintf(&r.body, "        Ok(%s(response.into_inner()))\n    }\n\n", r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
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
	r.docComment("", w.doc)
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
	if r.kind == moduleCodec {
		fmt.Fprintf(&b, "//! Generated wire conversions for %s.proto.\n\n", r.base)
		if r.publicClient {
			b.WriteString("#![allow(clippy::all, unused_variables, unused_mut, dead_code)]\n\n")
		}
	} else if r.docIntro != "" {
		fmt.Fprintf(&b, "//! %s\n\n", r.docIntro)
	} else {
		fmt.Fprintf(&b, "//! Generated native types and clients for %s.proto.\n\n", r.base)
	}

	var uses []string
	if r.features.v1 {
		uses = append(uses, "use crate::generated::v1;")
	}
	if r.publicClient && r.features.restMetadata {
		uses = append(uses, "use crate::public::generated::metadata::*;")
	}
	if r.publicClient && r.features.unaryTransport {
		uses = append(uses, "use crate::public::generated::unary_transport::UnaryTransport;")
	}
	for _, base := range sortedKeys2(r.features.crossPublic) {
		names := sortedKeys(r.features.crossPublic[base])
		if r.publicClient {
			uses = append(uses, fmt.Sprintf("use crate::public::generated::%s::{%s};", base, strings.Join(names, ", ")))
		} else {
			uses = append(uses, fmt.Sprintf("use crate::%s::{%s};", base, strings.Join(names, ", ")))
		}
	}
	for _, base := range sortedKeys2(r.features.crossCodec) {
		names := sortedKeys(r.features.crossCodec[base])
		if r.publicClient {
			uses = append(uses, fmt.Sprintf("use crate::public::generated::codec::%s::{%s};", base, strings.Join(names, ", ")))
		} else {
			uses = append(uses, fmt.Sprintf("use crate::codec::%s::{%s};", base, strings.Join(names, ", ")))
		}
	}
	if len(r.features.supportFns) > 0 {
		if r.publicClient {
			uses = append(uses, fmt.Sprintf("use crate::public::generated::codec::support::{%s};", strings.Join(sortedKeys(r.features.supportFns), ", ")))
		} else {
			uses = append(uses, fmt.Sprintf("use crate::codec::support::{%s};", strings.Join(sortedKeys(r.features.supportFns), ", ")))
		}
	}
	if len(r.features.supportTypes) > 0 {
		rpcPath := "crate::rpc_support"
		if r.publicClient {
			rpcPath = "crate::public::generated::rpc_support"
		}
		uses = append(uses, fmt.Sprintf("use %s::{%s};", rpcPath, strings.Join(sortedKeys(r.features.supportTypes), ", ")))
	}
	if len(r.features.invokeUses) > 0 {
		if r.publicClient {
			uses = append(uses, fmt.Sprintf("use crate::public::generated::invoke_support::{%s};", strings.Join(sortedKeys(r.features.invokeUses), ", ")))
		} else {
			uses = append(uses, fmt.Sprintf("use crate::invoke_support::{%s};", strings.Join(sortedKeys(r.features.invokeUses), ", ")))
		}
	}
	if r.features.streamExt {
		uses = append(uses, "use tokio_stream::StreamExt;")
	}
	if r.features.prostTypes {
		if r.publicClient {
			uses = append(uses, "use crate::public::generated::metadata::Empty;")
		} else {
			uses = append(uses, "use prost_types::Empty;")
		}
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

func sortedKeys2(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
