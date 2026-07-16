package python

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// maxImportLine is ruff's default line length; from-imports longer than this
// are wrapped exactly as ruff's isort rule expects.
const maxImportLine = 88

// moduleKind selects which of the two generated modules per proto file is
// being rendered: the public module (native dataclasses and the client class)
// or the internal codec module (wire converters).
type moduleKind int

const (
	modulePublic moduleKind = iota
	moduleCodec
)

// features tracks which imports a generated file needs; the import header is
// assembled after the body renders.
type features struct {
	dataclass   bool
	dcField     bool
	dcReplace   bool
	datetime    bool
	anyType     bool
	iterable    bool
	iterator    bool
	overload    bool
	grpc        bool
	os          bool // public: connect reads the host-service environment
	hostService bool // public: transport helpers from ._grpc_transport
	emptyPb     bool
	structPb    bool
	wire        bool                       // codec: typed pb2 module for this file
	wireGrpc    bool                       // public: pb2_grpc stub module for this file
	native      bool                       // codec: the public counterpart module
	helpers     bool                       // public: the shared _codec.support module
	rpcTypes    map[string]bool            // public: type names from rpc_support
	invokeNames map[string]bool            // public: names from invoke_support
	support     map[string]bool            // codec: converter names from _codec.support
	crossNative map[string]map[string]bool // public: generated module base -> imported type names
	codecBases  map[string]bool            // public: codec module bases referenced by the client
	crossCodec  map[string]bool            // codec: sibling codec module bases referenced
	metadataMethods map[string]bool        // public client: METHOD_* constants from metadata
	unaryTransport  bool                   // public client: UnaryTransport from unary_transport
	jsonFormat      bool                   // public client: google.protobuf.json_format
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

func newRenderer(idx *index, base, wireBase string, kind moduleKind) *renderer {
	if wireBase == "" {
		wireBase = base
	}
	return &renderer{
		idx:      idx,
		base:     base,
		wireBase: wireBase,
		kind:     kind,
		features: features{
			rpcTypes:        map[string]bool{},
			invokeNames:     map[string]bool{},
			support:         map[string]bool{},
			crossNative:     map[string]map[string]bool{},
			codecBases:      map[string]bool{},
			crossCodec:      map[string]bool{},
			metadataMethods: map[string]bool{},
		},
	}
}

func (r *renderer) publicBase(protoFile string) string {
	return generatedFileBase(protoFile)
}

// useType records a public-module import of a native well-known type from the
// shared rpc_support module.
func (r *renderer) useType(name string) {
	r.features.rpcTypes[name] = true
}

// useInvoke records a public-module import from the shared invoke_support
// module, emitted for json_result methods, and returns the name unchanged.
func (r *renderer) useInvoke(name string) string {
	r.features.invokeNames[name] = true
	return name
}

// useMetadataMethod records a METHOD_* constant import from metadata.
func (r *renderer) useMetadataMethod(name string) {
	r.features.metadataMethods[name] = true
}

// useConverter records a codec-module import of a well-known-type converter
// from the shared _codec.support module.
func (r *renderer) useConverter(name string) string {
	r.features.support[name] = true
	return name
}

// helper qualifies a shared call helper for the public module being rendered.
// Public modules import the codec runtime as a module object (_support), so
// the circular public -> codec import resolves at import time.
func (r *renderer) helper(name string) string {
	r.features.helpers = true
	return "_support." + name
}

// wireModule names this file's pb2 import alias. The vendored .pyi stubs in
// gestalt/_gen/v1 type these modules, so generated code references them
// directly instead of going through an Any-typed alias.
func (r *renderer) wireModule() string {
	return "_" + r.wireBase + "_pb2"
}

func (r *renderer) wireGrpcModule() string {
	return "_" + r.wireBase + "_pb2_grpc"
}

// crossRef records a public-module import of a native type from another
// generated public module and returns the name unchanged. References within
// the current file are not imports.
func (r *renderer) crossRef(protoFile, name string) string {
	base := r.publicBase(protoFile)
	if base != r.base {
		if r.features.crossNative[base] == nil {
			r.features.crossNative[base] = map[string]bool{}
		}
		r.features.crossNative[base][name] = true
	}
	return name
}

// nativeRef qualifies a native type declared by this codec module's public
// counterpart. Codec modules import the public module as a module object
// (from .. import <base> as native) because the public module imports the
// codec at module level: the module object resolves during the circular
// import and the attributes are only needed at call time.
func (r *renderer) nativeRef(name string) string {
	r.features.native = true
	return "native." + name
}

// codecAlias names the public-module import alias for a codec module: the
// file's own codec is _codec, siblings carry their base name.
func (r *renderer) codecAlias(base string) string {
	if base == r.base {
		return "_codec"
	}
	return "_" + base + "_codec"
}

// codecRef qualifies a generated message converter for the module being
// rendered. Codec modules call their own converters bare and sibling codec
// modules by base name; public modules go through the imported codec module
// object, which keeps the circular import resolvable.
func (r *renderer) codecRef(protoFile, name string) string {
	base := r.publicBase(protoFile)
	if r.kind == moduleCodec {
		if base == r.base {
			return name
		}
		r.features.crossCodec[base] = true
		return base + "." + name
	}
	r.features.codecBases[base] = true
	return r.codecAlias(base) + "." + name
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
	case model.KindScalar, model.KindBytes:
		return expr
	case model.KindEnum:
		// Native enums are open ints; the wire stubs declare closed enum
		// types, so the value launders through the Any-typed converter.
		return r.useConverter("to_wire_enum") + "(" + expr + ")"
	case model.KindJSONNull:
		r.features.structPb = true
		return "_struct.NULL_VALUE"
	case model.KindUnit:
		r.features.emptyPb = true
		return "_empty.Empty()"
	case model.KindMessage:
		return r.codecRef(r.idx.messages[ref.Message].ProtoFile, toWireFunc(ref.Message)) + "(" + expr + ")"
	default:
		return r.useConverter("to_wire_"+wellKnownSuffix(ref.Kind)) + "(" + expr + ")"
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
		return r.codecRef(r.idx.messages[ref.Message].ProtoFile, fromWireFunc(ref.Message)) + "(" + expr + ")"
	default:
		return r.useConverter("from_wire_"+wellKnownSuffix(ref.Kind)) + "(" + expr + ")"
	}
}

// wellKnownSuffix names the _codec.support converter pair for a well-known
// type.
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
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes
}

// identityFromWire includes enums: wire enum values are int subclasses, so
// they satisfy the open-enum int representation directly.
func identityFromWire(ref *model.TypeRef) bool {
	return identityToWire(ref) || ref.Kind == model.KindEnum
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

// docstringText sanitizes proto comment text for inclusion in a generated
// docstring: backslashes and triple quotes are escaped so the raw text cannot
// terminate or alter the string literal. Everything else stays verbatim.
func docstringText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"""`, `\"\"\"`)
	return s
}

// writeDocstring renders doc as a docstring at the given indent: one line
// when the text is one line, otherwise with the closing quotes on their own
// line per PEP 257.
func (r *renderer) writeDocstring(indent, doc string) {
	lines := strings.Split(docstringText(doc), "\n")
	if len(lines) == 1 {
		text := lines[0]
		if strings.HasSuffix(text, `"`) {
			// A trailing quote would run into the closing triple quote.
			text = text[:len(text)-1] + `\"`
		}
		fmt.Fprintf(&r.body, "%s\"\"\"%s\"\"\"\n", indent, text)
		return
	}
	fmt.Fprintf(&r.body, "%s\"\"\"%s\n", indent, lines[0])
	for _, line := range lines[1:] {
		if line == "" {
			r.body.WriteString("\n")
		} else {
			r.body.WriteString(indent + line + "\n")
		}
	}
	fmt.Fprintf(&r.body, "%s\"\"\"\n", indent)
}

// writeDocComment renders a field or enum-value proto comment as #: doc
// comments, which sphinx autodoc attaches to the attribute that follows.
func (r *renderer) writeDocComment(indent, doc string) {
	for _, line := range strings.Split(doc, "\n") {
		if line == "" {
			r.body.WriteString(indent + "#:\n")
		} else {
			r.body.WriteString(indent + "#: " + line + "\n")
		}
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
	return "to_wire_" + snakeCase(oneofTypeName(m, o))
}

func oneofFromWireFunc(m *model.Message, o *model.Oneof) string {
	return "from_wire_" + snakeCase(oneofTypeName(m, o))
}

// enumMemberNames returns the generated attribute names for an enum's
// members. When every member carries the enum's own SCREAMING_SNAKE name as a
// prefix, the prefix is stripped: PresignMethodValues exposes GET, not
// PRESIGN_METHOD_GET. Members keep their proto names verbatim when the prefix
// is not uniform across the enum (CursorDirection's CURSOR_NEXT), or when any
// stripped name would start with a digit or collide with a Python keyword.
func enumMemberNames(e *model.Enum) []string {
	names := make([]string, len(e.Values))
	for i, v := range e.Values {
		names[i] = v.Name
	}
	prefix := strings.ToUpper(snakeCase(localName(e.FullName))) + "_"
	stripped := make([]string, len(e.Values))
	for i, v := range e.Values {
		s := strings.TrimPrefix(v.Name, prefix)
		if s == v.Name || s == "" || pythonKeywords[s] || (s[0] >= '0' && s[0] <= '9') {
			return names
		}
		stripped[i] = s
	}
	return stripped
}

func (r *renderer) renderEnum(e *model.Enum) {
	name := localName(e.FullName)
	r.body.WriteString("# Open enum: unknown numeric values are preserved, so the type is int.\n")
	fmt.Fprintf(&r.body, "%s = int\n\n\n", name)
	fmt.Fprintf(&r.body, "class %s:\n", enumValuesClassName(e.FullName))
	doc := fmt.Sprintf("Named values for the open %s enum.", name)
	if e.Doc != "" {
		doc = e.Doc + "\n\n" + doc
	}
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	memberNames := enumMemberNames(e)
	for i, v := range e.Values {
		if v.Doc != "" {
			r.writeDocComment("    ", v.Doc)
		}
		fmt.Fprintf(&r.body, "    %s: %s = %d\n", memberNames[i], name, v.Number)
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
	attrs := len(m.Oneofs)
	for _, f := range m.Fields {
		if f.OneofIndex < 0 {
			attrs++
		}
	}
	if m.Doc != "" {
		r.writeDocstring("    ", m.Doc)
		if attrs > 0 {
			r.body.WriteString("\n")
		}
	} else if attrs == 0 {
		r.body.WriteString("    pass\n")
	}
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		if f.Doc != "" {
			r.writeDocComment("    ", f.Doc)
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
	nativeName := r.nativeRef(name)
	r.features.anyType = true
	r.features.wire = true

	// vulture reports unused parameters, so empty messages take an
	// underscore-prefixed (ignored) parameter.
	param := "value"
	if len(m.Fields) == 0 {
		param = "_value"
	}

	fmt.Fprintf(&r.body, "def %s(%s: %s) -> Any:\n", toWireFunc(m.FullName), param, nativeName)
	if len(m.Fields) == 0 {
		fmt.Fprintf(&r.body, "    return %s.%s()\n\n\n", r.wireModule(), name)
	} else {
		fmt.Fprintf(&r.body, "    return %s.%s(\n", r.wireModule(), name)
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

	fmt.Fprintf(&r.body, "def %s(%s: Any) -> %s:\n", fromWireFunc(m.FullName), param, nativeName)
	if len(m.Fields) == 0 {
		fmt.Fprintf(&r.body, "    return %s()\n\n\n", nativeName)
	} else {
		fmt.Fprintf(&r.body, "    return %s(\n", nativeName)
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
	unionName := r.nativeRef(oneofTypeName(m, o))
	r.features.anyType = true

	fmt.Fprintf(&r.body, "def %s(value: %s) -> dict[str, Any]:\n", oneofToWireFunc(m, o), unionName)
	for _, f := range oneofFields(m, o) {
		fmt.Fprintf(&r.body, "    if isinstance(value, %s):\n", r.nativeRef(r.variantClassName(m, o, f)))
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
			fmt.Fprintf(&r.body, "        return %s()\n", r.nativeRef(r.variantClassName(m, o, f)))
		default:
			fmt.Fprintf(&r.body, "        return %s(value=%s)\n", r.nativeRef(r.variantClassName(m, o, f)), r.fromWireExpr(fieldRef(f), wireFieldExpr(f.Name)))
		}
	}
	r.body.WriteString("    return None\n\n\n")
}

func (r *renderer) renderClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName
	r.features.grpc = true
	r.features.wireGrpc = true

	fmt.Fprintf(&r.body, "class %s:\n", name)
	doc := fmt.Sprintf("Client for the %s service.", svc.FullName)
	if svc.Doc != "" {
		doc = svc.Doc + "\n\n" + doc
	}
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	ctxField := r.contextFieldOf(svc)
	if ctxField != nil {
		ctxType := r.fieldType(ctxField)
		fmt.Fprintf(&r.body, "    def __init__(self, channel: grpc.Channel, *, context: %s | None = None, timeout: float | None = None) -> None:\n", ctxType)
		r.body.WriteString("        self._channel = channel\n")
		fmt.Fprintf(&r.body, "        self._stub = %s.%sStub(channel)\n", r.wireGrpcModule(), wireName)
		r.body.WriteString("        self._context = context\n")
		r.body.WriteString("        self._timeout = timeout\n")
		r.body.WriteString("        self._owns_channel = False\n\n")
	} else {
		r.body.WriteString("    def __init__(self, channel: grpc.Channel, *, timeout: float | None = None) -> None:\n")
		r.body.WriteString("        self._channel = channel\n")
		fmt.Fprintf(&r.body, "        self._stub = %s.%sStub(channel)\n", r.wireGrpcModule(), wireName)
		r.body.WriteString("        self._timeout = timeout\n")
		r.body.WriteString("        self._owns_channel = False\n\n")
	}

	if svc.HostBinding != "" {
		r.features.os = true
		r.features.hostService = true
		params := "cls, name: str | None = None, *"
		forward := "channel"
		if ctxField != nil {
			params += ", context: " + r.fieldType(ctxField) + " | None = None"
			forward = "channel, context=context"
		}
		params += ", timeout: float | None = None"
		forward += ", timeout=timeout"
		r.body.WriteString("    @classmethod\n")
		fmt.Fprintf(&r.body, "    def connect(%s) -> %s:\n", params, name)
		r.body.WriteString("        target = os.environ.get(ENV_HOST_SERVICE_SOCKET, \"\")\n")
		r.body.WriteString("        if not target:\n")
		r.body.WriteString("            raise RuntimeError(f\"{ENV_HOST_SERVICE_SOCKET} is not set\")\n")
		r.body.WriteString("        token = os.environ.get(ENV_HOST_SERVICE_TOKEN, \"\")\n")
		r.body.WriteString("        channel = host_service_channel(\n")
		fmt.Fprintf(&r.body, "            %q, target, token=token.strip(), binding=(name or \"\").strip()\n", svc.HostBinding)
		r.body.WriteString("        )\n")
		fmt.Fprintf(&r.body, "        client = cls(%s)\n", forward)
		r.body.WriteString("        client._owns_channel = True\n")
		r.body.WriteString("        return client\n\n")
	}

	r.body.WriteString("    def close(self) -> None:\n")
	r.writeDocstring("        ", "Close the owned gRPC channel; a no-op for injected channels.")
	r.body.WriteString("\n")
	r.body.WriteString("        if self._owns_channel:\n")
	r.body.WriteString("            self._channel.close()\n\n")
	fmt.Fprintf(&r.body, "    def __enter__(self) -> %s:\n", name)
	r.writeDocstring("        ", "Return the client for ``with`` statements.")
	r.body.WriteString("\n")
	r.body.WriteString("        return self\n\n")
	r.body.WriteString("    def __exit__(self, *args: object) -> None:\n")
	r.writeDocstring("        ", "Close the client at the end of a context manager block.")
	r.body.WriteString("\n")
	r.body.WriteString("        self.close()\n\n")

	for _, method := range svc.Methods {
		r.renderMethod(method)
	}
	r.body.WriteString("\n")
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
// boundary: the ergonomic return type, one generated doc line describing the
// collapse, and the statements that derive the return value from the
// converted response.
type collapsed struct {
	returnType string
	doc        string
	lines      []string
}

// collapseOutput returns the response collapse for a method, or nil when the
// faithful response type is returned.
func (r *renderer) collapseOutput(m *model.Method) *collapsed {
	if m.Output == nil {
		return nil
	}
	// json_result decodes the HTTP-shaped result's JSON envelope: the method
	// returns the decoded payload and surfaces envelope failures as
	// InvokeError.
	if jr := m.JsonResult; jr != nil {
		status := findField(m.Output, jr.Status)
		body := findField(m.Output, jr.Body)
		r.features.anyType = true
		return &collapsed{
			returnType: "Any",
			doc: "The result decodes with the standard JSON operation envelope\n" +
				"semantics; envelope failures raise InvokeError.",
			lines: []string{
				fmt.Sprintf("        return %s(%s, %s, response.%s, response.%s)",
					r.useInvoke("decode_app_result"),
					jsonResultContext(m, "app"), jsonResultContext(m, "operation"),
					pyName(status.Name), pyName(body.Name)),
			},
		}
	}
	if or := m.Output.OptionalResult; or != nil {
		guard := findField(m.Output, or.Guard)
		value := findField(m.Output, or.Value)
		return &collapsed{
			returnType: r.fieldType(value) + " | None",
			lines: []string{
				fmt.Sprintf("        return response.%s if response.%s else None", pyName(value.Name), pyName(guard.Name)),
			},
		}
	}
	if k := m.Output.Keyed; k != nil {
		entries := findField(m.Output, k.Entries)
		entry := r.idx.messages[entries.Elem.Message]
		key := findField(entry, k.Key)
		present := findField(entry, k.Present)
		value := findField(entry, k.Value)
		mapType := "dict[" + scalarType(key.Scalar) + ", " + r.fieldType(value) + "]"
		return &collapsed{
			returnType: mapType,
			lines: []string{
				"        out: " + mapType + " = {}",
				fmt.Sprintf("        for entry in response.%s:", pyName(entries.Name)),
				fmt.Sprintf("            if entry.%s:", pyName(present.Name)),
				fmt.Sprintf("                out[entry.%s] = entry.%s", pyName(key.Name), pyName(value.Name)),
				"        return out",
			},
		}
	}
	if m.Output.Unwrap != "" {
		field := findField(m.Output, m.Output.Unwrap)
		returnType := r.fieldType(field)
		// A presence-bearing unwrapped field is absent-capable: the native
		// dataclass declares it `T | None`, so the collapse does too.
		if field.Presence == model.ExplicitPresence {
			returnType += " | None"
		}
		return &collapsed{
			returnType: returnType,
			lines:      []string{fmt.Sprintf("        return response.%s", pyName(field.Name))},
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
			return "request." + pyName(f.Name)
		}
	}
	return `""`
}

func (r *renderer) renderMethod(m *model.Method) {
	switch {
	case m.Initial != nil && m.Stream == model.ServerStream:
		r.renderFramedRead(m)
		r.renderFaithfulMethod(m, true)
	case m.Initial != nil && m.Stream == model.ClientStream:
		r.renderFramedWrite(m)
		r.renderFaithfulMethod(m, true)
	case m.Stream == model.Unary && (len(r.effectiveOptionalFields(m)) > 0 || r.collapseOutput(m) != nil):
		r.renderErgonomicUnary(m)
		// The dual-mode method already accepts the full request, so the
		// faithful sibling exists only when the response differs.
		if r.collapseOutput(m) != nil {
			r.renderFaithfulMethod(m, true)
		}
	default:
		r.renderFaithfulMethod(m, false)
	}
}

// renderErgonomicUnary renders the annotated surface of a unary method. A
// signature or optional_signature annotation makes the method dual-mode: it
// accepts the request message or the listed fields as keyword-only arguments
// (signature fields first, then optional-signature fields), with @overload
// stubs advertising the two forms. The response annotations collapse the
// return.
func (r *renderer) renderErgonomicUnary(m *model.Method) {
	methodName := pyName(snakeCase(m.Name))
	collapse := r.collapseOutput(m)
	returnType := "None"
	switch {
	case m.OutputIsEmpty:
	case collapse != nil:
		returnType = collapse.returnType
	default:
		returnType = r.messageType(m.Output.FullName)
	}

	params := ""
	requestArg := "_empty.Empty()"
	var requestLines []string
	switch {
	case m.InputIsEmpty:
		// Empty-input methods take no request parameter at all.
		r.features.emptyPb = true
	case len(r.effectiveOptionalFields(m)) > 0:
		requestType := r.messageType(m.Input.FullName)
		r.features.overload = true
		// Optional-signature fields follow the signature fields as additional
		// keyword-only parameters; the dual-mode machinery already treats
		// every keyword argument as optional, so both lists render alike.
		names := r.effectiveOptionalFields(m)
		var stubDecls, implDecls, guards, args []string
		for _, name := range names {
			f := findField(m.Input, name)
			param := pyName(f.Name)
			typ := r.fieldType(f)
			arg := param + "=" + param
			stubType := typ + " | None"
			switch {
			case f.Presence == model.ExplicitPresence:
				// None is the field's own unset value.
			case f.Kind == model.KindJSONValue:
				// JsonValue includes None: absent and JSON null conflate.
				stubType = typ
			case f.Kind == model.KindRepeated:
				stubType = typ
				arg = fmt.Sprintf("%s=%s if %s is not None else []", param, param, param)
			case f.Kind == model.KindMap, f.Kind == model.KindJSONStruct:
				stubType = typ
				arg = fmt.Sprintf("%s=%s if %s is not None else {}", param, param, param)
			case f.Kind == model.KindBytes:
				stubType = typ
				arg = fmt.Sprintf(`%s=%s or b""`, param, param)
			case f.Kind == model.KindEnum:
				stubType = typ
				arg = fmt.Sprintf("%s=%s or 0", param, param)
			case f.Kind == model.KindScalar:
				stubType = typ
				arg = fmt.Sprintf("%s=%s or %s", param, param, scalarDefault(f.Scalar))
			}
			stubDecls = append(stubDecls, param+": "+stubType+" = ...")
			implDecls = append(implDecls, param+": "+typ+" | None = None")
			guards = append(guards, param+" is not None")
			args = append(args, arg)
		}
		fmt.Fprintf(&r.body, "    @overload\n    def %s(self, request: %s) -> %s: ...\n\n",
			methodName, requestType, returnType)
		fmt.Fprintf(&r.body, "    @overload\n    def %s(self, *, %s) -> %s: ...\n\n",
			methodName, strings.Join(stubDecls, ", "), returnType)
		params = fmt.Sprintf(", request: %s | None = None, *, %s", requestType, strings.Join(implDecls, ", "))
		requestLines = append(requestLines,
			"        if request is None:",
			fmt.Sprintf("            request = %s(%s)", requestType, strings.Join(args, ", ")),
			fmt.Sprintf("        elif %s:", strings.Join(guards, " or ")),
			`            raise ValueError("pass either request or keyword arguments, not both")`,
		)
		requestLines = append(requestLines, r.contextLines(m)...)
		requestArg = r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	default:
		params = ", request: " + r.messageType(m.Input.FullName)
		requestLines = append(requestLines, r.contextLines(m)...)
		requestArg = r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	}

	stubCall := fmt.Sprintf("self._stub.%s(%s, timeout=self._timeout)", m.Name, requestArg)
	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "    def %s(self%s) -> None:\n", methodName, params)
		r.writeMethodDoc(m)
		r.writeLines(requestLines)
		fmt.Fprintf(&r.body, "        %s(lambda: %s)\n\n", r.helper("call_unary"), stubCall)
	case collapse != nil:
		fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, params, returnType)
		doc := m.Doc
		if collapse.doc != "" {
			if doc != "" {
				doc += "\n\n" + collapse.doc
			} else {
				doc = collapse.doc
			}
		}
		if doc != "" {
			r.writeDocstring("        ", doc)
		}
		r.writeLines(requestLines)
		fmt.Fprintf(&r.body, "        response = %s(%s(lambda: %s))\n",
			r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)), r.helper("call_unary"), stubCall)
		r.writeLines(collapse.lines)
		r.body.WriteString("\n")
	default:
		fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, params, returnType)
		r.writeMethodDoc(m)
		r.writeLines(requestLines)
		fmt.Fprintf(&r.body, "        response = %s(lambda: %s)\n", r.helper("call_unary"), stubCall)
		fmt.Fprintf(&r.body, "        return %s(response)\n\n", r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	}
}

func (r *renderer) writeLines(lines []string) {
	for _, line := range lines {
		r.body.WriteString(line + "\n")
	}
}

// renderFramedRead renders a server-streaming method with the framing
// annotation: the header frame and a native payload stream replace raw frames.
func (r *renderer) renderFramedRead(m *model.Method) {
	methodName := pyName(snakeCase(m.Name))
	frames := m.Output
	header := findField(frames, m.Initial.HeaderField)
	chunk := findField(frames, m.Initial.ChunkField)
	oneof := frames.Oneofs[header.OneofIndex]
	prop := pyName(oneof.Name)
	headerClass := r.crossRef(frames.ProtoFile, r.variantClassName(frames, oneof, header))
	chunkClass := r.crossRef(frames.ProtoFile, r.variantClassName(frames, oneof, chunk))

	requestParam := ""
	requestArg := "_empty.Empty()"
	if m.InputIsEmpty {
		r.features.emptyPb = true
	} else {
		requestParam = ", request: " + r.messageType(m.Input.FullName)
		requestArg = r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	}
	r.features.iterator = true

	// Byte payload streams gain the boto3-StreamingBody-style read();
	// other chunk types stay plain iterators.
	streamType := "Iterator[" + r.fieldType(chunk) + "]"
	streamOpen, streamClose := "", ""
	if chunk.Kind == model.KindBytes {
		r.useType("ByteStream")
		streamType = "ByteStream"
		streamOpen, streamClose = "ByteStream(", ")"
	}

	fmt.Fprintf(&r.body, "    def %s(self%s) -> tuple[%s, %s]:\n",
		methodName, requestParam, r.fieldType(header), streamType)
	r.writeLines(r.contextLines(m))
	fmt.Fprintf(&r.body, "        frames = %s(self._stub.%s(%s), %s)\n",
		r.helper("map_recv"), m.Name, requestArg, r.codecRef(frames.ProtoFile, fromWireFunc(frames.FullName)))
	fmt.Fprintf(&r.body, "        %s = %s(frames, lambda frame: frame.%s.value if isinstance(frame.%s, %s) else None)\n",
		pyName(header.Name), r.helper("read_header_frame"), prop, prop, headerClass)
	fmt.Fprintf(&r.body, "        return %s, %s%s(frames, lambda frame: frame.%s.value if isinstance(frame.%s, %s) else None)%s\n\n",
		pyName(header.Name), streamOpen, r.helper("chunk_frames"), prop, prop, chunkClass, streamClose)
}

// renderFramedWrite renders a client-streaming method with the framing
// annotation: the header and a native payload stream replace raw frames.
func (r *renderer) renderFramedWrite(m *model.Method) {
	methodName := pyName(snakeCase(m.Name))
	frames := m.Input
	header := findField(frames, m.Initial.HeaderField)
	chunk := findField(frames, m.Initial.ChunkField)
	oneof := frames.Oneofs[header.OneofIndex]
	prop := pyName(oneof.Name)
	framesType := r.messageType(frames.FullName)
	headerClass := r.crossRef(frames.ProtoFile, r.variantClassName(frames, oneof, header))
	chunkClass := r.crossRef(frames.ProtoFile, r.variantClassName(frames, oneof, chunk))
	toWire := r.codecRef(frames.ProtoFile, toWireFunc(frames.FullName))
	r.features.iterable = true

	returnType := "None"
	if !m.OutputIsEmpty {
		returnType = r.messageType(m.Output.FullName)
	}
	fmt.Fprintf(&r.body, "    def %s(self, %s: %s, %s: Iterable[%s]) -> %s:\n",
		methodName, pyName(header.Name), r.fieldType(header), pyName(chunk.Name), r.fieldType(chunk), returnType)
	fmt.Fprintf(&r.body, "        requests = %s(\n", r.helper("framed_send"))
	fmt.Fprintf(&r.body, "            %s(%s(%s=%s(value=%s))),\n", toWire, framesType, prop, headerClass, pyName(header.Name))
	fmt.Fprintf(&r.body, "            %s,\n", pyName(chunk.Name))
	fmt.Fprintf(&r.body, "            lambda chunk: %s(%s(%s=%s(value=chunk))),\n", toWire, framesType, prop, chunkClass)
	r.body.WriteString("        )\n")
	if m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "        %s(lambda: self._stub.%s(requests))\n\n", r.helper("call_unary"), m.Name)
	} else {
		fmt.Fprintf(&r.body, "        response = %s(lambda: self._stub.%s(requests))\n", r.helper("call_unary"), m.Name)
		fmt.Fprintf(&r.body, "        return %s(response)\n\n", r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	}
}

// renderFaithfulMethod renders the descriptor-faithful method. When an
// annotated surface owns the natural name, the faithful variant keeps a _raw
// suffix so both remain available.
func (r *renderer) renderFaithfulMethod(m *model.Method, rawSuffix bool) {
	methodName := pyName(snakeCase(m.Name))
	if rawSuffix {
		methodName = snakeCase(m.Name) + "_raw"
	}
	requestParam := ""
	requestArg := "_empty.Empty()"
	if m.InputIsEmpty {
		r.features.emptyPb = true
	} else {
		requestType := r.messageType(m.Input.FullName)
		switch m.Stream {
		case model.ClientStream, model.Bidi:
			r.features.iterable = true
			requestParam = ", requests: Iterable[" + requestType + "]"
			requestArg = r.helper("map_send") + "(requests, " + r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + ")"
		default:
			requestParam = ", request: " + requestType
			requestArg = r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		}
	}

	// Only unary stub calls carry the client-level timeout; streaming and
	// framed calls are excluded.
	stubArgs := requestArg
	if m.Stream == model.Unary {
		stubArgs += ", timeout=self._timeout"
	}
	stubCall := fmt.Sprintf("self._stub.%s(%s)", m.Name, stubArgs)
	switch m.Stream {
	case model.ServerStream, model.Bidi:
		responseType := r.messageType(m.Output.FullName)
		r.features.iterator = true
		fmt.Fprintf(&r.body, "    def %s(self%s) -> Iterator[%s]:\n", methodName, requestParam, responseType)
		r.writeMethodDoc(m)
		if m.Stream == model.ServerStream {
			r.writeLines(r.contextLines(m))
		}
		fmt.Fprintf(&r.body, "        return %s(%s, %s)\n\n", r.helper("map_recv"), stubCall, r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	default:
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "    def %s(self%s) -> None:\n", methodName, requestParam)
			r.writeMethodDoc(m)
			r.writeLines(r.contextLines(m))
			fmt.Fprintf(&r.body, "        %s(lambda: %s)\n\n", r.helper("call_unary"), stubCall)
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, requestParam, responseType)
			r.writeMethodDoc(m)
			r.writeLines(r.contextLines(m))
			fmt.Fprintf(&r.body, "        response = %s(lambda: %s)\n", r.helper("call_unary"), stubCall)
			fmt.Fprintf(&r.body, "        return %s(response)\n\n", r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
		}
	}
}

// effectiveOptionalFields returns signature and optional_signature fields,
// omitting public fill/reject fields for public clients.
func (r *renderer) effectiveOptionalFields(m *model.Method) []string {
	names := append(append([]string{}, m.Signature...), m.OptionalSignature...)
	if !r.publicClient {
		return names
	}
	omitted := publicsurface.OmittedFields(m)
	if len(omitted) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !omitted[name] {
			out = append(out, name)
		}
	}
	return out
}

// contextFieldOf returns a service's first request context field, which
// determines whether the client carries a default RequestContext. Public
// clients never carry RequestContext; it is server-filled.
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

// contextLines renders the default-context injection for a request-bearing
// method, or nil when the request has no context field. Public clients never
// inject context; it is server-filled via the public policy.
func (r *renderer) contextLines(m *model.Method) []string {
	if r.publicClient {
		return nil
	}
	if m.InputIsEmpty || findField(m.Input, "context") == nil {
		return nil
	}
	r.features.dcReplace = true
	return []string{
		"        if request.context is None and self._context is not None:",
		"            request = replace(request, context=self._context)",
	}
}

// genImportDots returns the relative-import prefix for gestalt._gen.v1.
func (r *renderer) genImportDots() string {
	if r.publicClient {
		if r.kind == moduleCodec {
			return "...."
		}
		return "..."
	}
	if r.kind == moduleCodec {
		return ".."
	}
	return "."
}

func (r *renderer) supportImportModule() string {
	if r.publicClient {
		return "gestalt.rpc_support"
	}
	return ".rpc_support"
}

func (r *renderer) invokeImportModule() string {
	if r.publicClient {
		return "gestalt.invoke_support"
	}
	return ".invoke_support"
}

// writeMethodDoc renders a client method docstring from the proto comment on
// the RPC, when present.
func (r *renderer) writeMethodDoc(m *model.Method) {
	if m.Doc != "" {
		r.writeDocstring("        ", m.Doc)
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

// localImports renders the relative-import block in the order ruff's isort
// rule expects: parent-package imports (..) before same-package ones (.),
// each level sorted by module path, then by imported name.
func (r *renderer) localImports() string {
	var b strings.Builder
	if r.kind == moduleCodec {
		if r.features.native {
			fmt.Fprintf(&b, "from .. import %s as native\n", r.base)
		}
		if r.features.wire {
			fmt.Fprintf(&b, "from %s_gen.v1 import %s_pb2 as _%s_pb2\n", r.genImportDots(), r.wireBase, r.wireBase)
		}
		if len(r.features.crossCodec) > 0 {
			fmt.Fprintf(&b, "from . import %s\n", strings.Join(sortedKeys(r.features.crossCodec), ", "))
		}
		if len(r.features.support) > 0 {
			b.WriteString(fromImport(".support", sortedKeys(r.features.support)))
		}
		return b.String()
	}

	// Aliased imports from ._codec stay one per line (isort keeps "as"
	// imports separate), sorted with the support module by submodule name.
	codecModules := map[string]string{}
	for base := range r.features.codecBases {
		codecModules[base] = r.codecAlias(base)
	}
	if r.features.helpers {
		codecModules["support"] = "_support"
	}
	type localImport struct {
		module string
		lines  string
	}
	var locals []localImport
	if len(codecModules) > 0 {
		var lines strings.Builder
		for _, name := range sortedKeys2(codecModules) {
			fmt.Fprintf(&lines, "from ._codec import %s as %s\n", name, codecModules[name])
		}
		locals = append(locals, localImport{module: "._codec", lines: lines.String()})
	}
	if r.features.wireGrpc {
		locals = append(locals, localImport{
			module: r.genImportDots() + "_gen.v1",
			lines:  fmt.Sprintf("from %s_gen.v1 import %s_pb2_grpc as _%s_pb2_grpc\n", r.genImportDots(), r.wireBase, r.wireBase),
		})
	}
	if r.publicClient && r.features.wire {
		locals = append(locals, localImport{
			module: r.genImportDots() + "_gen.v1",
			lines:  fmt.Sprintf("from %s_gen.v1 import %s_pb2 as _%s_pb2\n", r.genImportDots(), r.wireBase, r.wireBase),
		})
	}
	if r.features.hostService {
		locals = append(locals, localImport{
			module: "._grpc_transport",
			lines: fromImport("._grpc_transport", []string{
				"ENV_HOST_SERVICE_SOCKET",
				"ENV_HOST_SERVICE_TOKEN",
				"host_service_channel",
			}),
		})
	}
	for base, names := range r.features.crossNative {
		locals = append(locals, localImport{module: "." + base, lines: fromImport("."+base, sortedKeys(names))})
	}
	if len(r.features.invokeNames) > 0 {
		locals = append(locals, localImport{module: r.invokeImportModule(), lines: fromImport(r.invokeImportModule(), sortedKeys(r.features.invokeNames))})
	}
	if len(r.features.metadataMethods) > 0 {
		locals = append(locals, localImport{module: ".metadata", lines: fromImport(".metadata", sortedKeys(r.features.metadataMethods))})
	}
	if r.features.unaryTransport {
		b.WriteString("from .unary_transport import UnaryTransport\n")
	}
	if r.features.jsonFormat {
		b.WriteString("from google.protobuf import json_format\n")
	}
	if len(r.features.rpcTypes) > 0 {
		locals = append(locals, localImport{module: r.supportImportModule(), lines: fromImport(r.supportImportModule(), sortedKeys(r.features.rpcTypes))})
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].module < locals[j].module })
	for _, imp := range locals {
		b.WriteString(imp.lines)
	}
	return b.String()
}

// assemble prepends the docstring, import header, and wire-module aliases
// derived from the rendered body.
func (r *renderer) assemble() string {
	var b strings.Builder
	if r.kind == moduleCodec {
		fmt.Fprintf(&b, "\"\"\"Generated wire codec for %s.proto.\"\"\"\n\n", r.base)
	} else if r.docIntro != "" {
		fmt.Fprintf(&b, "\"\"\"%s\"\"\"\n\n", r.docIntro)
	} else if r.publicClient {
		fmt.Fprintf(&b, "\"\"\"Generated public gestaltd surface for %s.proto.\"\"\"\n\n", r.base)
	} else {
		fmt.Fprintf(&b, "\"\"\"Generated provider SDK surface for %s.proto.\"\"\"\n\n", r.base)
	}
	b.WriteString("from __future__ import annotations\n")

	var stdlib []string
	if r.features.datetime {
		stdlib = append(stdlib, "import datetime\n")
	}
	if r.features.os {
		stdlib = append(stdlib, "import os\n")
	}
	if r.features.dataclass || r.features.dcReplace {
		var names []string
		if r.features.dataclass {
			names = append(names, "dataclass")
		}
		if r.features.dcField {
			names = append(names, "field")
		}
		if r.features.dcReplace {
			names = append(names, "replace")
		}
		stdlib = append(stdlib, fromImport("dataclasses", names))
	}
	var typingNames []string
	if r.features.anyType || r.features.emptyPb || r.features.structPb {
		typingNames = append(typingNames, "Any")
	}
	if r.features.iterable {
		typingNames = append(typingNames, "Iterable")
	}
	if r.features.iterator {
		typingNames = append(typingNames, "Iterator")
	}
	if r.features.overload {
		typingNames = append(typingNames, "overload")
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
	b.WriteString(r.localImports())

	// The protobuf wheel ships no stubs for the well-known modules, so their
	// members are invisible to the type checker and the aliases stay Any. The
	// _gen pb2 modules are typed by the vendored .pyi stubs and need no alias.
	var aliases []string
	if r.features.emptyPb {
		aliases = append(aliases, "_empty: Any = _empty_pb2\n")
	}
	if r.features.structPb {
		aliases = append(aliases, "_struct: Any = _struct_pb2\n")
	}
	if len(aliases) > 0 {
		b.WriteString("\n")
		for _, line := range aliases {
			b.WriteString(line)
		}
	}

	// ruff's isort expects one blank line between the import block and a
	// following comment, and two before a class or function. The alias block,
	// when present, ends the import block itself.
	if len(aliases) == 0 && strings.HasPrefix(r.body.String(), "#") {
		b.WriteString("\n")
	} else {
		b.WriteString("\n\n")
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

func sortedKeys2(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
