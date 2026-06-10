package ts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// features tracks which imports a generated file needs; the import header is
// assembled after the body renders.
type features struct {
	create        bool
	jsonObject    bool
	jsonValue     bool
	nullValue     bool
	emptySchema   bool
	client        bool
	hostService   bool
	supportValues map[string]bool
	supportTypes  map[string]bool
	cross         map[string]map[string]bool // generated file base -> imported names
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
			supportValues: map[string]bool{},
			supportTypes:  map[string]bool{},
			cross:         map[string]map[string]bool{},
		},
	}
}

// use records an import from the shared rpc_support module.
func (r *renderer) use(name string, isType bool) {
	if isType {
		r.features.supportTypes[name] = true
	} else {
		r.features.supportValues[name] = true
	}
}

// crossRef records an import from another generated file and returns the name
// unchanged. References within the current file are not imports. isType marks
// names that need a type-only import under verbatimModuleSyntax.
func (r *renderer) crossRef(protoFile, name string) string {
	return r.crossRefKind(protoFile, name, false)
}

func (r *renderer) crossRefKind(protoFile, name string, isType bool) string {
	base := generatedFileBase(protoFile)
	if base != r.base {
		if r.features.cross[base] == nil {
			r.features.cross[base] = map[string]bool{}
		}
		// A name imported as both type and value stays a value import.
		if isType {
			if _, ok := r.features.cross[base][name]; !ok {
				r.features.cross[base][name] = true
			}
		} else {
			r.features.cross[base][name] = false
		}
	}
	return name
}

func (r *renderer) messageType(fullName string) string {
	return r.crossRefKind(r.idx.messages[fullName].ProtoFile, localName(fullName), true)
}

func (r *renderer) enumType(fullName string) string {
	// Enum constants are value references (and types via the same name).
	return r.crossRefKind(r.idx.enums[fullName].ProtoFile, localName(fullName), false)
}

// fnToWire returns the converter function for reference kinds converted by a
// unary function, or "" when conversion is identity or inline.
func (r *renderer) fnToWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, toWireFunc(ref.Message))
	case model.KindTimestamp:
		r.use("toWireTimestamp", false)
		return "toWireTimestamp"
	case model.KindDuration:
		r.use("toWireDuration", false)
		return "toWireDuration"

	case model.KindJSONValue:
		r.use("toWireValue", false)
		return "toWireValue"
	case model.KindRPCStatus:
		r.use("toWireStatus", false)
		return "toWireStatus"
	default:
		return ""
	}
}

func (r *renderer) fnFromWire(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindMessage:
		return r.crossRef(r.idx.messages[ref.Message].ProtoFile, fromWireFunc(ref.Message))
	case model.KindTimestamp:
		r.use("fromWireTimestamp", false)
		return "fromWireTimestamp"
	case model.KindDuration:
		r.use("fromWireDuration", false)
		return "fromWireDuration"

	case model.KindJSONValue:
		r.use("fromWireValue", false)
		return "fromWireValue"
	case model.KindRPCStatus:
		r.use("fromWireStatus", false)
		return "fromWireStatus"
	default:
		return ""
	}
}

// toWireExpr renders the wire-bound conversion of a singular value.
func (r *renderer) toWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindJSONStruct:
		return expr
	case model.KindEnum:
		return expr + " as wire." + r.wireEnum(ref.Enum)
	case model.KindJSONNull:
		r.features.nullValue = true
		return "NullValue.NULL_VALUE"
	case model.KindUnit:
		r.features.create = true
		r.features.emptySchema = true
		return "create(EmptySchema)"
	default:
		return r.fnToWire(ref) + "(" + expr + ")"
	}
}

// fromWireExpr renders the native-bound conversion of a singular wire value.
func (r *renderer) fromWireExpr(ref *model.TypeRef, expr string) string {
	switch ref.Kind {
	case model.KindScalar, model.KindBytes, model.KindEnum, model.KindJSONStruct:
		return expr
	case model.KindJSONNull:
		return "null"
	case model.KindUnit:
		return "{}"
	default:
		return r.fnFromWire(ref) + "(" + expr + ")"
	}
}

// wireEnum returns the enum name in the wire module. Enums referenced across
// generated files still live in the current file's wire module only when
// declared in the same proto file; cross-file enums resolve through their own
// wire module, which the spike protos do not require.
func (r *renderer) wireEnum(fullName string) string {
	return localName(fullName)
}

// identityToWire also covers google.protobuf.Struct: protobuf-es renders
// Struct fields as plain JsonObject on the wire types.
func identityToWire(ref *model.TypeRef) bool {
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes || ref.Kind == model.KindJSONStruct
}

func identityFromWire(ref *model.TypeRef) bool {
	return ref.Kind == model.KindScalar || ref.Kind == model.KindBytes || ref.Kind == model.KindEnum || ref.Kind == model.KindJSONStruct
}

// fieldToWire renders the conversion of a whole field value.
func (r *renderer) fieldToWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityToWire(f.Elem) {
			return expr
		}
		if fn := r.fnToWire(f.Elem); fn != "" {
			return expr + ".map(" + fn + ")"
		}
		return expr + ".map((item) => " + r.toWireExpr(f.Elem, "item") + ")"
	case model.KindMap:
		if identityToWire(f.MapValue) {
			return expr
		}
		return "Object.fromEntries(Object.entries(" + expr + ").map(([key, item]) => [key, " + r.toWireExpr(f.MapValue, "item") + "]))"
	default:
		return r.toWireExpr(fieldRef(f), expr)
	}
}

func (r *renderer) fieldFromWire(f *model.Field, expr string) string {
	switch f.Kind {
	case model.KindRepeated:
		if identityFromWire(f.Elem) {
			return expr
		}
		if fn := r.fnFromWire(f.Elem); fn != "" {
			return expr + ".map(" + fn + ")"
		}
		return expr + ".map((item) => (" + r.fromWireExpr(f.Elem, "item") + "))"
	case model.KindMap:
		if identityFromWire(f.MapValue) {
			return expr
		}
		return "Object.fromEntries(Object.entries(" + expr + ").map(([key, item]) => [key, " + r.fromWireExpr(f.MapValue, "item") + "]))"
	default:
		return r.fromWireExpr(fieldRef(f), expr)
	}
}

func oneofProp(o *model.Oneof) string {
	parts := strings.Split(o.Name, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = upperFirst(parts[i])
	}
	return lowerFirst(strings.Join(parts, ""))
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

func (r *renderer) renderEnum(e *model.Enum) {
	name := localName(e.FullName)
	fmt.Fprintf(&r.body, "export const %s = {\n", name)
	for _, v := range e.Values {
		fmt.Fprintf(&r.body, "  %s: %d,\n", v.Name, v.Number)
	}
	r.body.WriteString("} as const;\n\n")
	// Open enum: unknown numeric values are preserved, so the type is number.
	fmt.Fprintf(&r.body, "export type %s = number;\n\n", name)
}

func (r *renderer) renderMessage(m *model.Message) {
	name := localName(m.FullName)
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "export type %s =\n", oneofTypeName(m, o))
		for _, f := range oneofFields(m, o) {
			fmt.Fprintf(&r.body, "  | { case: %q; value: %s }\n", f.JSONName, r.fieldType(f))
		}
		r.body.WriteString("  | { case: undefined; value?: undefined };\n\n")
	}
	fmt.Fprintf(&r.body, "export interface %s {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		optional := ""
		if f.Presence == model.ExplicitPresence {
			optional = "?"
		}
		fmt.Fprintf(&r.body, "  %s%s: %s;\n", f.JSONName, optional, r.fieldType(f))
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "  %s: %s;\n", oneofProp(o), oneofTypeName(m, o))
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderConversions(m *model.Message) {
	name := localName(m.FullName)
	r.features.create = true

	fmt.Fprintf(&r.body, "export function %s(value: %s): wire.%s {\n", toWireFunc(m.FullName), name, name)
	fmt.Fprintf(&r.body, "  return create(wire.%sSchema, {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		expr := "value." + f.JSONName
		if f.Presence == model.ExplicitPresence {
			fmt.Fprintf(&r.body, "    ...(%s !== undefined ? { %s: %s } : {}),\n", expr, f.JSONName, r.fieldToWire(f, expr))
		} else {
			fmt.Fprintf(&r.body, "    %s: %s,\n", f.JSONName, r.fieldToWire(f, expr))
		}
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "    %s: %s(value.%s),\n", oneofProp(o), oneofToWireFunc(m, o), oneofProp(o))
	}
	r.body.WriteString("  });\n}\n\n")

	fmt.Fprintf(&r.body, "export function %s(value: wire.%s): %s {\n", fromWireFunc(m.FullName), name, name)
	r.body.WriteString("  return {\n")
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		expr := "value." + f.JSONName
		if f.Presence == model.ExplicitPresence {
			fmt.Fprintf(&r.body, "    ...(%s !== undefined ? { %s: %s } : {}),\n", expr, f.JSONName, r.fieldFromWire(f, expr))
		} else {
			fmt.Fprintf(&r.body, "    %s: %s,\n", f.JSONName, r.fieldFromWire(f, expr))
		}
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "    %s: %s(value.%s),\n", oneofProp(o), oneofFromWireFunc(m, o), oneofProp(o))
	}
	r.body.WriteString("  };\n}\n\n")

	for _, o := range m.Oneofs {
		r.renderOneofConverters(m, o)
	}
}

func oneofToWireFunc(m *model.Message, o *model.Oneof) string {
	return "toWire" + oneofTypeName(m, o)
}

func oneofFromWireFunc(m *model.Message, o *model.Oneof) string {
	return "fromWire" + oneofTypeName(m, o)
}

func (r *renderer) renderOneofConverters(m *model.Message, o *model.Oneof) {
	name := localName(m.FullName)
	unionName := oneofTypeName(m, o)
	prop := oneofProp(o)

	fmt.Fprintf(&r.body, "function %s(value: %s): wire.%s[%q] {\n", oneofToWireFunc(m, o), unionName, name, prop)
	r.body.WriteString("  switch (value.case) {\n")
	for _, f := range oneofFields(m, o) {
		fmt.Fprintf(&r.body, "    case %q:\n", f.JSONName)
		fmt.Fprintf(&r.body, "      return { case: %q, value: %s };\n", f.JSONName, r.toWireExpr(fieldRef(f), "value.value"))
	}
	r.body.WriteString("    default:\n      return { case: undefined };\n  }\n}\n\n")

	fmt.Fprintf(&r.body, "function %s(value: wire.%s[%q]): %s {\n", oneofFromWireFunc(m, o), name, prop, unionName)
	r.body.WriteString("  switch (value.case) {\n")
	for _, f := range oneofFields(m, o) {
		fmt.Fprintf(&r.body, "    case %q:\n", f.JSONName)
		fmt.Fprintf(&r.body, "      return { case: %q, value: %s };\n", f.JSONName, r.fromWireExpr(fieldRef(f), "value.value"))
	}
	r.body.WriteString("    default:\n      return { case: undefined };\n  }\n}\n\n")
}

func (r *renderer) renderClient(svc *model.Service) {
	name := localName(svc.FullName)
	r.features.client = true

	fmt.Fprintf(&r.body, "export class %s {\n", name)
	fmt.Fprintf(&r.body, "  private readonly client: Client<typeof wire.%s>;\n\n", name)
	r.body.WriteString("  constructor(transport: Transport) {\n")
	fmt.Fprintf(&r.body, "    this.client = createClient(wire.%s, transport);\n", name)
	r.body.WriteString("  }\n\n")

	if svc.HostBinding != "" {
		r.features.hostService = true
		fmt.Fprintf(&r.body, "  static connect(name?: string): %s {\n", name)
		fmt.Fprintf(&r.body, "    const { target, token } = requireHostServiceTarget(%q);\n", svc.HostBinding)
		r.body.WriteString("    const transport = createHostServiceGrpcTransport(\n")
		fmt.Fprintf(&r.body, "      parseHostServiceTarget(%q, target),\n", svc.HostBinding)
		r.body.WriteString("      hostServiceMetadataInterceptors(token, name?.trim() ?? \"\"),\n    );\n")
		fmt.Fprintf(&r.body, "    return new %s(transport);\n  }\n\n", name)
	}

	for _, method := range svc.Methods {
		r.renderMethod(method)
	}
	r.body.WriteString("}\n\n")
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
// boundary: the ergonomic return type and the statements that derive the
// return value from the converted response.
type collapsed struct {
	returnType string
	lines      []string
}

// collapseOutput returns the response collapse for a method, or nil when the
// faithful response type is returned.
func (r *renderer) collapseOutput(m *model.Method) *collapsed {
	if m.Output == nil {
		return nil
	}
	if or := m.Output.OptionalResult; or != nil {
		guard := findField(m.Output, or.Guard)
		value := findField(m.Output, or.Value)
		return &collapsed{
			returnType: r.fieldType(value) + " | undefined",
			lines: []string{
				fmt.Sprintf("    return response.%s ? response.%s : undefined;", guard.JSONName, value.JSONName),
			},
		}
	}
	if k := m.Output.Keyed; k != nil {
		entries := findField(m.Output, k.Entries)
		entry := r.idx.messages[entries.Elem.Message]
		key := findField(entry, k.Key)
		present := findField(entry, k.Present)
		value := findField(entry, k.Value)
		mapType := "{ [key: " + scalarType(key.Scalar) + "]: " + r.fieldType(value) + " }"
		return &collapsed{
			returnType: mapType,
			lines: []string{
				// A null-prototype record: cache keys like "__proto__" must
				// become own properties, not prototype mutations.
				"    const out: " + mapType + " = Object.create(null) as " + mapType + ";",
				fmt.Sprintf("    for (const entry of response.%s) {", entries.JSONName),
				fmt.Sprintf("      if (entry.%s) {", present.JSONName),
				fmt.Sprintf("        out[entry.%s] = entry.%s;", key.JSONName, value.JSONName),
				"      }",
				"    }",
				"    return out;",
			},
		}
	}
	if m.Output.Unwrap != "" {
		field := findField(m.Output, m.Output.Unwrap)
		return &collapsed{
			returnType: r.fieldType(field),
			lines:      []string{fmt.Sprintf("    return response.%s;", field.JSONName)},
		}
	}
	return nil
}

func (r *renderer) renderMethod(m *model.Method) {
	switch {
	case m.Framing != nil && m.Stream == model.ServerStream:
		r.renderFramedRead(m)
		r.renderFaithfulMethod(m, true)
	case m.Framing != nil && m.Stream == model.ClientStream:
		r.renderFramedWrite(m)
		r.renderFaithfulMethod(m, true)
	case m.Stream == model.Unary && (len(m.Signature) > 0 || r.collapseOutput(m) != nil):
		r.renderErgonomicUnary(m)
		r.renderFaithfulMethod(m, true)
	default:
		r.renderFaithfulMethod(m, false)
	}
}

// renderErgonomicUnary renders the annotated surface of a unary method:
// flattened parameters from the signature annotation and a collapsed return
// from the response annotations.
func (r *renderer) renderErgonomicUnary(m *model.Method) {
	methodName := lowerFirst(m.Name)
	params := "request: " + r.messageType(m.Input.FullName)
	var requestLines []string
	if len(m.Signature) > 0 {
		var decls, props []string
		var spreads []string
		for _, name := range m.Signature {
			f := findField(m.Input, name)
			optional := ""
			if f.Presence == model.ExplicitPresence {
				optional = "?"
				spreads = append(spreads, fmt.Sprintf("...(%s !== undefined ? { %s } : {})", f.JSONName, f.JSONName))
			} else {
				props = append(props, f.JSONName)
			}
			decls = append(decls, fmt.Sprintf("%s%s: %s", f.JSONName, optional, r.fieldType(f)))
		}
		params = strings.Join(decls, ", ")
		requestLines = append(requestLines, fmt.Sprintf("    const request: %s = { %s };",
			r.messageType(m.Input.FullName), strings.Join(append(props, spreads...), ", ")))
	}
	requestArg := r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"

	r.use("callUnary", false)
	collapse := r.collapseOutput(m)
	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "  async %s(%s): Promise<void> {\n", methodName, params)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "    await callUnary(() => this.client.%s(%s));\n", methodName, requestArg)
		r.body.WriteString("  }\n\n")
	case collapse != nil:
		fmt.Fprintf(&r.body, "  async %s(%s): Promise<%s> {\n", methodName, params, collapse.returnType)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "    const response = %s(await callUnary(() => this.client.%s(%s)));\n",
			r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)), methodName, requestArg)
		for _, line := range collapse.lines {
			r.body.WriteString(line + "\n")
		}
		r.body.WriteString("  }\n\n")
	default:
		responseType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "  async %s(%s): Promise<%s> {\n", methodName, params, responseType)
		for _, line := range requestLines {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "    const response = await callUnary(() => this.client.%s(%s));\n", methodName, requestArg)
		fmt.Fprintf(&r.body, "    return %s(response);\n", r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
		r.body.WriteString("  }\n\n")
	}
}

// renderFramedRead renders a server-streaming method with the framing
// annotation: the header frame and a native payload stream replace raw frames.
func (r *renderer) renderFramedRead(m *model.Method) {
	methodName := lowerFirst(m.Name)
	frames := m.Output
	oneof := frames.Oneofs[findField(frames, m.Framing.HeaderField).OneofIndex]
	prop := oneofProp(oneof)
	header := findField(frames, m.Framing.HeaderField)
	chunk := findField(frames, m.Framing.ChunkField)

	requestParam := ""
	requestArg := "{}"
	if !m.InputIsEmpty {
		requestParam = "request: " + r.messageType(m.Input.FullName)
		requestArg = r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	}
	r.use("mapRecv", false)
	r.use("readHeaderFrame", false)
	r.use("chunkFrames", false)

	fmt.Fprintf(&r.body, "  async %s(%s): Promise<{ %s: %s; %s: AsyncIterable<%s> }> {\n",
		methodName, requestParam, header.JSONName, r.fieldType(header), chunk.JSONName, r.fieldType(chunk))
	fmt.Fprintf(&r.body, "    const frames = mapRecv(this.client.%s(%s), %s)[Symbol.asyncIterator]();\n",
		methodName, requestArg, r.crossRef(frames.ProtoFile, fromWireFunc(frames.FullName)))
	fmt.Fprintf(&r.body, "    const %s = await readHeaderFrame(frames, (frame) => (frame.%s.case === %q ? frame.%s.value : undefined));\n",
		header.JSONName, prop, header.JSONName, prop)
	fmt.Fprintf(&r.body, "    return { %s, %s: chunkFrames(frames, (frame) => (frame.%s.case === %q ? frame.%s.value : undefined)) };\n",
		header.JSONName, chunk.JSONName, prop, chunk.JSONName, prop)
	r.body.WriteString("  }\n\n")
}

// renderFramedWrite renders a client-streaming method with the framing
// annotation: the header and a native payload stream replace raw frames.
func (r *renderer) renderFramedWrite(m *model.Method) {
	methodName := lowerFirst(m.Name)
	frames := m.Input
	prop := oneofProp(frames.Oneofs[findField(frames, m.Framing.HeaderField).OneofIndex])
	header := findField(frames, m.Framing.HeaderField)
	chunk := findField(frames, m.Framing.ChunkField)
	toWire := r.crossRef(frames.ProtoFile, toWireFunc(frames.FullName))

	r.use("callUnary", false)
	r.use("framedSend", false)

	returnType := "void"
	if !m.OutputIsEmpty {
		returnType = r.messageType(m.Output.FullName)
	}
	fmt.Fprintf(&r.body, "  async %s(%s: %s, %s: AsyncIterable<%s>): Promise<%s> {\n",
		methodName, header.JSONName, r.fieldType(header), chunk.JSONName, r.fieldType(chunk), returnType)
	r.body.WriteString("    const requests = framedSend(\n")
	fmt.Fprintf(&r.body, "      %s({ %s: { case: %q, value: %s } }),\n", toWire, prop, header.JSONName, header.JSONName)
	fmt.Fprintf(&r.body, "      %s,\n", chunk.JSONName)
	fmt.Fprintf(&r.body, "      (chunk) => %s({ %s: { case: %q, value: chunk } }),\n", toWire, prop, chunk.JSONName)
	r.body.WriteString("    );\n")
	if m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "    await callUnary(() => this.client.%s(requests));\n", methodName)
	} else {
		fmt.Fprintf(&r.body, "    const response = await callUnary(() => this.client.%s(requests));\n", methodName)
		fmt.Fprintf(&r.body, "    return %s(response);\n", r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
	}
	r.body.WriteString("  }\n\n")
}

// renderFaithfulMethod renders the descriptor-faithful method. When an
// annotated surface owns the natural name, the faithful variant keeps a Raw
// suffix so both remain available.
func (r *renderer) renderFaithfulMethod(m *model.Method, rawSuffix bool) {
	methodName := lowerFirst(m.Name)
	if rawSuffix {
		methodName += "Raw"
	}
	requestParam := ""
	requestArg := "{}"
	if !m.InputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		switch m.Stream {
		case model.ClientStream, model.Bidi:
			requestParam = "requests: AsyncIterable<" + requestType + ">"
			r.use("mapSend", false)
			requestArg = "mapSend(requests, " + r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + ")"
		default:
			requestParam = "request: " + requestType
			requestArg = r.crossRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
		}
	}

	switch m.Stream {
	case model.ServerStream, model.Bidi:
		responseType := r.messageType(m.Output.FullName)
		r.use("mapRecv", false)
		fmt.Fprintf(&r.body, "  %s(%s): AsyncIterable<%s> {\n", methodName, requestParam, responseType)
		fmt.Fprintf(&r.body, "    return mapRecv(this.client.%s(%s), %s);\n", lowerFirst(m.Name), requestArg, r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
		r.body.WriteString("  }\n\n")
	default:
		r.use("callUnary", false)
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "  async %s(%s): Promise<void> {\n", methodName, requestParam)
			fmt.Fprintf(&r.body, "    await callUnary(() => this.client.%s(%s));\n", lowerFirst(m.Name), requestArg)
			r.body.WriteString("  }\n\n")
		} else {
			responseType := r.messageType(m.Output.FullName)
			fmt.Fprintf(&r.body, "  async %s(%s): Promise<%s> {\n", methodName, requestParam, responseType)
			fmt.Fprintf(&r.body, "    const response = await callUnary(() => this.client.%s(%s));\n", lowerFirst(m.Name), requestArg)
			fmt.Fprintf(&r.body, "    return %s(response);\n", r.crossRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName)))
			r.body.WriteString("  }\n\n")
		}
	}
}

// assemble prepends the import header derived from the rendered body.
func (r *renderer) assemble() string {
	var b strings.Builder
	if r.features.create {
		b.WriteString("import { create } from \"@bufbuild/protobuf\";\n")
	}
	var jsonTypes []string
	if r.features.jsonObject {
		jsonTypes = append(jsonTypes, "JsonObject")
	}
	if r.features.jsonValue {
		jsonTypes = append(jsonTypes, "JsonValue")
	}
	if len(jsonTypes) > 0 {
		fmt.Fprintf(&b, "import type { %s } from \"@bufbuild/protobuf\";\n", strings.Join(jsonTypes, ", "))
	}
	var wkt []string
	if r.features.emptySchema {
		wkt = append(wkt, "EmptySchema")
	}
	if r.features.nullValue {
		wkt = append(wkt, "NullValue")
	}
	if len(wkt) > 0 {
		fmt.Fprintf(&b, "import { %s } from \"@bufbuild/protobuf/wkt\";\n", strings.Join(wkt, ", "))
	}
	if r.features.client {
		b.WriteString("import { createClient } from \"@connectrpc/connect\";\n")
		b.WriteString("import type { Client, Transport } from \"@connectrpc/connect\";\n")
	}
	if r.features.hostService {
		b.WriteString("\nimport {\n  createHostServiceGrpcTransport,\n  hostServiceMetadataInterceptors,\n  parseHostServiceTarget,\n  requireHostServiceTarget,\n} from \"./host-service.ts\";")
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "\nimport * as wire from \"./internal/gen/v1/%s_pb.ts\";\n", r.base)

	var crossBases []string
	for base := range r.features.cross {
		crossBases = append(crossBases, base)
	}
	sort.Strings(crossBases)
	for _, base := range crossBases {
		var names []string
		for _, name := range sortedKeys(r.features.cross[base]) {
			if r.features.cross[base][name] {
				names = append(names, "type "+name)
			} else {
				names = append(names, name)
			}
		}
		fmt.Fprintf(&b, "import { %s } from \"./%s.ts\";\n", strings.Join(names, ", "), base)
	}

	supportNames := append([]string{}, sortedKeys(r.features.supportValues)...)
	for _, name := range sortedKeys(r.features.supportTypes) {
		supportNames = append(supportNames, "type "+name)
	}
	if len(supportNames) > 0 {
		fmt.Fprintf(&b, "import { %s } from \"./rpc_support.ts\";\n", strings.Join(supportNames, ", "))
	}

	b.WriteString("\n")
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
