package ts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func renderPublicMethods(methods []publicsurface.PublicMethod) string {
	var b strings.Builder
	b.WriteString("/**\n * Public transport method metadata.\n *\n * @module client/generated/methods\n */\n\n")
	b.WriteString("export interface PublicMethodHttp {\n")
	b.WriteString("  verb: \"GET\" | \"PUT\" | \"POST\" | \"PATCH\" | \"DELETE\";\n")
	b.WriteString("  path: string;\n")
	b.WriteString("  body: string;\n")
	b.WriteString("  pathFields: readonly { name: string; jsonName: string }[];\n")
	b.WriteString("  queryFields: readonly { name: string; jsonName: string }[];\n")
	b.WriteString("}\n\n")
	b.WriteString("export interface PublicMethod {\n")
	b.WriteString("  service: string;\n")
	b.WriteString("  method: string;\n")
	b.WriteString("  grpcPath: string;\n")
	b.WriteString("  http?: PublicMethodHttp | undefined;\n")
	b.WriteString("  fill: readonly string[];\n")
	b.WriteString("  reject: readonly string[];\n")
	b.WriteString("  // stream is true for server-streaming methods rendered as streaming calls.\n")
	b.WriteString("  stream: boolean;\n")
	b.WriteString("}\n\n")
	b.WriteString("export const PUBLIC_METHODS = {\n")

	byService := map[string][]publicsurface.PublicMethod{}
	var serviceOrder []string
	for _, pm := range methods {
		serviceKey := lowerFirst(publicsurface.ServiceLocalName(pm.Service))
		if _, ok := byService[serviceKey]; !ok {
			serviceOrder = append(serviceOrder, serviceKey)
		}
		byService[serviceKey] = append(byService[serviceKey], pm)
	}
	sort.Strings(serviceOrder)
	for si, serviceKey := range serviceOrder {
		group := byService[serviceKey]
		sort.Slice(group, func(i, j int) bool { return group[i].Method < group[j].Method })
		fmt.Fprintf(&b, "  %s: {\n", serviceKey)
		for mi, pm := range group {
			fmt.Fprintf(&b, "    %s: %s", lowerFirst(pm.Method), renderPublicMethodEntry(pm))
			if mi < len(group)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  }")
		if si < len(serviceOrder)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("} as const satisfies Record<string, Record<string, PublicMethod>>;\n")
	return b.String()
}

func renderPublicMethodEntry(pm publicsurface.PublicMethod) string {
	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "    service: %q,\n", publicsurface.ServiceLocalName(pm.Service))
	fmt.Fprintf(&b, "    method: %q,\n", pm.Method)
	fmt.Fprintf(&b, "    grpcPath: %q,\n", pm.FullMethod)
	if fill := publicsurface.FieldNames(pm.ServerFilled); len(fill) > 0 {
		fmt.Fprintf(&b, "    fill: %s,\n", publicStringList(fill))
	} else {
		b.WriteString("    fill: [],\n")
	}
	if reject := publicsurface.FieldNames(pm.Rejected); len(reject) > 0 {
		fmt.Fprintf(&b, "    reject: %s,\n", publicStringList(reject))
	} else {
		b.WriteString("    reject: [],\n")
	}
	fmt.Fprintf(&b, "    stream: %v,\n", pm.Stream == model.ServerStream)
	if pm.REST != nil {
		b.WriteString("    http: {\n")
		fmt.Fprintf(&b, "      verb: %q,\n", strings.ToUpper(pm.REST.Verb))
		fmt.Fprintf(&b, "      path: %q,\n", pm.REST.PathTemplate)
		body := ""
		if pm.REST.Body == publicsurface.BodyStar {
			body = "*"
		}
		fmt.Fprintf(&b, "      body: %q,\n", body)
		fmt.Fprintf(&b, "      pathFields: %s,\n", tsPublicFieldList(pm.REST.PathFields))
		fmt.Fprintf(&b, "      queryFields: %s,\n", tsPublicFieldList(pm.REST.QueryFields))
		b.WriteString("    },\n")
	}
	b.WriteString("  }")
	return b.String()
}

func publicStringList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func renderPublicTypes(view *publicsurface.View, paths PublicImports) string {
	var b strings.Builder
	b.WriteString("/**\n * Public request types with fill and reject fields omitted.\n *\n * @module client/generated/types\n */\n\n")

	imports := map[string]map[string]bool{}
	var blocks []string
	useSparseInit := paths.FixedNativeModule != ""

	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.InputIsEmpty || m.Input == nil {
				continue
			}
			omit := publicOmittedFieldNames(m)
			native := localName(m.Input.FullName)
			typeName := publicRequestTypeName(svc.Service, m)
			base := generatedFileBase(m.Input.ProtoFile)
			if imports[base] == nil {
				imports[base] = map[string]bool{}
			}
			imports[base][native] = true
			typeExpr := native
			if len(omit) > 0 {
				sort.Strings(omit)
				quoted := make([]string, len(omit))
				for i, name := range omit {
					quoted[i] = fmt.Sprintf("%q", publicJSONFieldName(m.Input, name))
				}
				typeExpr = fmt.Sprintf("Omit<%s, %s>", native, strings.Join(quoted, " | "))
			}
			if useSparseInit {
				typeExpr = fmt.Sprintf("Init<%s>", typeExpr)
			}
			blocks = append(blocks, fmt.Sprintf("export type %s = %s;", typeName, typeExpr))
		}
	}

	for _, base := range sortedPublicKeys(imports) {
		names := sortedPublicKeys(imports[base])
		fmt.Fprintf(&b, "import type { %s } from %s;\n", strings.Join(names, ", "), paths.nativeTypeImportQuoted(base))
	}
	if useSparseInit {
		fmt.Fprintf(&b, "import type { Init } from %s;\n", paths.supportModuleQuoted("rpc_support.ts"))
	}
	if len(imports) > 0 || useSparseInit {
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(blocks, "\n\n"))
	if len(blocks) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}

func publicJSONFieldName(msg *model.Message, protoName string) string {
	for _, f := range msg.Fields {
		if f.Name == protoName {
			return f.JSONName
		}
	}
	return protoName
}

func publicOmittedFieldNames(m *model.Method) []string {
	out := map[string]bool{}
	for name := range publicsurface.OmittedFields(m) {
		out[name] = true
	}
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderPublicConverters(view *publicsurface.View, paths PublicImports) string {
	var b strings.Builder
	b.WriteString("/**\n * Public request converters that delegate to the generated codec.\n *\n * @module client/generated/converters\n */\n\n")

	codecImports := map[string]map[string]bool{}
	nativeImports := map[string]map[string]bool{}
	wireImports := map[string]map[string]bool{}
	typeImports := map[string]bool{}
	converters := map[string]*publicWireConverter{}

	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if m.InputIsEmpty || m.Input == nil {
				continue
			}
			key := m.Input.FullName
			conv := converters[key]
			if conv == nil {
				conv = &publicWireConverter{input: m.Input}
				converters[key] = conv
			}
			conv.requestTypes = append(conv.requestTypes, publicRequestTypeName(svc.Service, m))
		}
	}

	var blocks []string
	for _, key := range sortedPublicKeys(converters) {
		conv := converters[key]
		native := localName(conv.input.FullName)
		codecName := toWireFunc(conv.input.FullName)
		codecAlias := "codec" + upperFirst(codecName)
		protoFile := conv.input.ProtoFile
		if codecImports[protoFile] == nil {
			codecImports[protoFile] = map[string]bool{}
		}
		codecImports[protoFile][codecName] = true
		if nativeImports[protoFile] == nil {
			nativeImports[protoFile] = map[string]bool{}
		}
		nativeAlias := "Native" + native
		nativeImports[protoFile][native+" as "+nativeAlias] = true
		if wireImports[protoFile] == nil {
			wireImports[protoFile] = map[string]bool{}
		}
		wireImports[protoFile][native] = true
		for _, typeName := range conv.requestTypes {
			typeImports[typeName] = true
		}

		requestTypes := sortedPublicKeys(conv.requestTypesSet())
		blocks = append(blocks, fmt.Sprintf(
			"export function %s(request: %s): %s {\n  return %s(request as Init<%s>);\n}",
			codecName,
			strings.Join(requestTypes, " | "),
			native,
			codecAlias,
			nativeAlias,
		))
	}

	for _, protoFile := range sortedPublicKeys(codecImports) {
		names := sortedPublicKeys(codecImports[protoFile])
		var importNames []string
		for _, name := range names {
			importNames = append(importNames, name+" as codec"+upperFirst(name))
		}
		fmt.Fprintf(&b, "import {\n  %s,\n} from %q;\n", strings.Join(importNames, ",\n  "), paths.codecModulePath(protoFile))
	}
	for _, protoFile := range sortedPublicKeys(nativeImports) {
		names := sortedPublicKeys(nativeImports[protoFile])
		fmt.Fprintf(&b, "import type { %s } from %q;\n", strings.Join(names, ", "), paths.nativeModulePath(protoFile))
	}
	for _, protoFile := range sortedPublicKeys(wireImports) {
		names := sortedPublicKeys(wireImports[protoFile])
		fmt.Fprintf(&b, "import type { %s } from %s;\n", strings.Join(names, ", "), paths.genModuleQuoted(protoFile))
	}
	b.WriteString("import type { Init } from " + paths.supportModuleQuoted("rpc_support.ts") + ";\n")
	if len(blocks) > 0 {
		b.WriteString("import type {\n")
		for _, typeName := range sortedPublicKeys(typeImports) {
			fmt.Fprintf(&b, "  %s,\n", typeName)
		}
		b.WriteString("} from \"./types.ts\";\n\n")
	}
	b.WriteString(strings.Join(blocks, "\n\n"))
	if len(blocks) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}


type publicWireConverter struct {
	input        *model.Message
	requestTypes []string
}

func (c *publicWireConverter) requestTypesSet() map[string]bool {
	out := map[string]bool{}
	for _, name := range c.requestTypes {
		out[name] = true
	}
	return out
}

func renderPublicUnaryTransport() string {
	return `/**
 * Transport-neutral unary RPC interface for public clients.
 *
 * @module client/generated/unary_transport
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";

import type { PublicMethod } from "./methods.ts";

export interface PublicUnaryCallOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
}

export interface UnaryTransport {
  unary<Output extends Message>(
    method: PublicMethod,
    request: Message,
    inputSchema: DescMessage,
    outputSchema: DescMessage,
    callOptions?: PublicUnaryCallOptions,
  ): Promise<Output>;

  // serverStream invokes a server-streaming method and returns an async
  // iterable of decoded response frames. The first frame is metadata. It is
  // optional because REST-only transports do not support streaming.
  serverStream?<Output extends Message>(
    method: PublicMethod,
    request: Message,
    inputSchema: DescMessage,
    outputSchema: DescMessage,
    callOptions?: PublicUnaryCallOptions,
  ): AsyncIterable<Output>;
}
`
}

func publicServiceClientFile(serviceName string) string {
	return lowerFirst(serviceName) + "_client.ts"
}

func renderPublicServiceClient(svc *model.Service, paths PublicImports) string {
	var b strings.Builder
	clientName := localName(svc.FullName) + "Client"
	fmt.Fprintf(&b, "/**\n * Transport-neutral %s client.\n *\n * @module client/generated/%s\n */\n\n",
		svc.Name, publicServiceClientFile(svc.Name))

	schemaImports := map[string]map[string]bool{}
	nativeImports := map[string]map[string]bool{}
	codecImports := map[string]map[string]bool{}
	var requestTypes []string
	converterImports := map[string]bool{}
	needsEmpty := false

	for _, m := range svc.Methods {
		if m.InputIsEmpty || m.OutputIsEmpty {
			needsEmpty = true
		}
		if m.Input != nil && m.Output != nil {
			inputNative := localName(m.Input.FullName)
			outputNative := localName(m.Output.FullName)
			if schemaImports[m.Input.ProtoFile] == nil {
				schemaImports[m.Input.ProtoFile] = map[string]bool{}
			}
			schemaImports[m.Input.ProtoFile][inputNative+"Schema"] = true
			if schemaImports[m.Output.ProtoFile] == nil {
				schemaImports[m.Output.ProtoFile] = map[string]bool{}
			}
			schemaImports[m.Output.ProtoFile][outputNative+"Schema"] = true
			if nativeImports[m.Output.ProtoFile] == nil {
				nativeImports[m.Output.ProtoFile] = map[string]bool{}
			}
			nativeImports[m.Output.ProtoFile][outputNative] = true
			if codecImports[m.Output.ProtoFile] == nil {
				codecImports[m.Output.ProtoFile] = map[string]bool{}
			}
			codecImports[m.Output.ProtoFile]["fromWire"+outputNative] = true
			requestTypes = append(requestTypes, publicRequestTypeName(svc, m))
			converterImports["toWire"+inputNative] = true
		} else if m.Input != nil && !m.InputIsEmpty {
			inputNative := localName(m.Input.FullName)
			if schemaImports[m.Input.ProtoFile] == nil {
				schemaImports[m.Input.ProtoFile] = map[string]bool{}
			}
			schemaImports[m.Input.ProtoFile][inputNative+"Schema"] = true
			requestTypes = append(requestTypes, publicRequestTypeName(svc, m))
			converterImports["toWire"+inputNative] = true
		} else if m.Output != nil && !m.OutputIsEmpty {
			outputNative := localName(m.Output.FullName)
			if schemaImports[m.Output.ProtoFile] == nil {
				schemaImports[m.Output.ProtoFile] = map[string]bool{}
			}
			schemaImports[m.Output.ProtoFile][outputNative+"Schema"] = true
			if nativeImports[m.Output.ProtoFile] == nil {
				nativeImports[m.Output.ProtoFile] = map[string]bool{}
			}
			nativeImports[m.Output.ProtoFile][outputNative] = true
			if codecImports[m.Output.ProtoFile] == nil {
				codecImports[m.Output.ProtoFile] = map[string]bool{}
			}
			codecImports[m.Output.ProtoFile]["fromWire"+outputNative] = true
		}
	}

	if needsEmpty {
		b.WriteString("import { create } from \"@bufbuild/protobuf\";\n")
		b.WriteString("import { EmptySchema } from \"@bufbuild/protobuf/wkt\";\n")
	}
	for _, protoFile := range sortedPublicKeys(nativeImports) {
		names := nativeImports[protoFile]
		typeNames := sortedPublicKeys(names)
		fmt.Fprintf(&b, "import type { %s } from %q;\n", strings.Join(typeNames, ", "), paths.nativeModulePath(protoFile))
	}
	for _, protoFile := range sortedPublicKeys(codecImports) {
		names := codecImports[protoFile]
		funcNames := sortedPublicKeys(names)
		fmt.Fprintf(&b, "import { %s } from %q;\n", strings.Join(funcNames, ", "), paths.codecModulePath(protoFile))
	}
	for _, protoFile := range sortedPublicKeys(schemaImports) {
		names := sortedPublicKeys(schemaImports[protoFile])
		fmt.Fprintf(&b, "import {\n  %s,\n} from %s;\n",
			strings.Join(names, ",\n  "), paths.genModuleQuoted(protoFile))
	}
	if svc.Name == "App" {
		invokeSupportImports := []string{"decodeAppResult", "decodeGraphQLResult"}
		for _, m := range svc.Methods {
			if m.Stream == model.ServerStream {
				invokeSupportImports = append(invokeSupportImports, "mapServerStreamFrames")
				break
			}
		}
		b.WriteString("import { " + strings.Join(invokeSupportImports, ", ") + " } from " + paths.supportModuleQuoted("invoke_support.ts") + ";\n")
	}
	b.WriteString("import {\n")
	for _, name := range sortedPublicKeys(converterImports) {
		fmt.Fprintf(&b, "  %s,\n", name)
	}
	b.WriteString("} from \"./converters.ts\";\n")
	b.WriteString("import { PUBLIC_METHODS } from \"./methods.ts\";\n")
	if len(requestTypes) > 0 {
		b.WriteString("import type {\n  " + strings.Join(requestTypes, ",\n  ") + ",\n} from \"./types.ts\";\n")
	}
	b.WriteString("import type { UnaryTransport, PublicUnaryCallOptions } from \"./unary_transport.ts\";\n\n")

	fmt.Fprintf(&b, "export class %s {\n", clientName)
	b.WriteString("  constructor(private readonly transport: UnaryTransport) {}\n\n")
	serviceKey := lowerFirst(svc.Name)
	for _, m := range svc.Methods {
		renderPublicServiceClientMethod(&b, svc, m, serviceKey, paths)
	}
	b.WriteString("}\n")
	renderPublicServiceRESTInterface(&b, svc)
	return b.String()
}

func renderPublicServiceRESTInterface(b *strings.Builder, svc *model.Service) {
	clientName := localName(svc.FullName) + "Client"
	ifaceName := clientName + "REST"
	var restMethods []*model.Method
	for _, m := range svc.Methods {
		if m.HTTP != nil {
			restMethods = append(restMethods, m)
		}
	}
	if len(restMethods) == 0 {
		return
	}
	fmt.Fprintf(b, "\nexport interface %s {\n", ifaceName)
	for _, m := range restMethods {
		renderPublicRESTInterfaceMethod(b, svc, m)
	}
	b.WriteString("}\n")
}

func renderPublicServiceClientMethod(b *strings.Builder, svc *model.Service, m *model.Method, serviceKey string, paths PublicImports) {
	methodKey := lowerFirst(m.Name)
	methodRef := fmt.Sprintf("PUBLIC_METHODS.%s.%s", serviceKey, methodKey)

	if m.Stream == model.ServerStream && m.Input != nil && m.Output != nil {
		renderPublicServiceClientStreamMethod(b, svc, m, methodKey, methodRef)
		return
	}

	if m.JsonResult != nil && m.Input != nil && m.Output != nil {
		renderPublicServiceClientJsonMethod(b, svc, m, methodKey, methodRef, paths)
		return
	}

	if m.Name == "InvokeGraphQL" && m.Input != nil && m.Output != nil {
		renderPublicServiceClientGraphQLRawMethod(b, svc, m, methodKey, methodRef)
		renderPublicServiceClientGraphQLRawAlias(b, svc, m)
		renderPublicServiceClientGraphQLDecodedMethod(b, svc, m)
		return
	}

	sig := renderPublicServiceClientMethodSignature(svc, m, methodKey)
	returnType := publicServiceClientReturnType(m)
	if m.OutputIsEmpty {
		fmt.Fprintf(b, "  async %s%s: Promise<void> {\n", sig.prefix, sig.suffix)
	} else {
		fmt.Fprintf(b, "  async %s%s: Promise<%s> {\n", sig.prefix, sig.suffix, returnType)
	}

	wireExpr := publicServiceWireRequestExpr(m)
	inputSchema := publicServiceInputSchema(m)
	outputSchema := publicServiceOutputSchema(m)

	if m.OutputIsEmpty {
		fmt.Fprintf(b, "    await this.transport.unary(\n      %s,\n      %s,\n      %s,\n      %s,\n      callOptions,\n    );\n  }\n\n",
			methodRef, wireExpr, inputSchema, outputSchema)
	} else {
		fromWire := publicServiceFromWireExpr(m)
		fmt.Fprintf(b, "    return %s(\n      await this.transport.unary(\n        %s,\n        %s,\n        %s,\n        %s,\n        callOptions,\n      ),\n    );\n  }\n\n",
			fromWire, methodRef, wireExpr, inputSchema, outputSchema)
	}
}

type publicServiceMethodSignature struct {
	prefix         string
	suffix         string
	hasCallOptions bool
}

func renderPublicServiceClientMethodSignature(svc *model.Service, m *model.Method, methodKey string) publicServiceMethodSignature {
	sig := publicServiceMethodSignature{prefix: methodKey}
	var params []string
	if !m.InputIsEmpty {
		typeName := publicRequestTypeName(svc, m)
		params = append(params, fmt.Sprintf("request: %s", typeName))
	}
	params = append(params, "callOptions?: PublicUnaryCallOptions")
	sig.hasCallOptions = true
	sig.suffix = "(" + strings.Join(params, ", ") + ")"
	return sig
}

func publicServiceClientReturnType(m *model.Method) string {
	if m.OutputIsEmpty {
		return "void"
	}
	return localName(m.Output.FullName)
}

func publicServiceWireRequestExpr(m *model.Method) string {
	if m.InputIsEmpty {
		return "create(EmptySchema, {})"
	}
	return "toWire" + localName(m.Input.FullName) + "(request)"
}

func publicServiceInputSchema(m *model.Method) string {
	if m.InputIsEmpty {
		return "EmptySchema"
	}
	return localName(m.Input.FullName) + "Schema"
}

func publicServiceOutputSchema(m *model.Method) string {
	if m.OutputIsEmpty {
		return "EmptySchema"
	}
	return localName(m.Output.FullName) + "Schema"
}

func publicServiceFromWireExpr(m *model.Method) string {
	if m.OutputIsEmpty {
		return ""
	}
	return "fromWire" + localName(m.Output.FullName)
}

func renderPublicServiceClientJsonMethod(b *strings.Builder, svc *model.Service, m *model.Method, methodKey, methodRef string, paths PublicImports) {
	typeName := publicRequestTypeName(svc, m)
	inputNative := localName(m.Input.FullName)
	outputNative := localName(m.Output.FullName)

	fmt.Fprintf(b, "  async %sRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s> {\n",
		methodKey, typeName, outputNative)
	fmt.Fprintf(b, "    return fromWire%s(\n      await this.transport.unary(\n        %s,\n        toWire%s(request),\n        %sSchema,\n        %sSchema,\n        callOptions,\n      ),\n    );\n  }\n\n",
		outputNative, methodRef, inputNative, inputNative, outputNative)

	if m.Name == "Invoke" {
		b.WriteString("  /**\n   * The result decodes with the standard JSON operation envelope semantics;\n   * envelope failures throw InvokeError.\n   */\n")
	}
	fmt.Fprintf(b, "  async %s<T = unknown>(request: %s, callOptions?: PublicUnaryCallOptions): Promise<T> {\n", methodKey, typeName)
	appField := publicJSONResultAppField(m)
	fmt.Fprintf(b, "    const response = await this.%sRaw(request, callOptions);\n", methodKey)
	fmt.Fprintf(b, "    return decodeAppResult<T>(request.%s ?? \"\", request.operation ?? \"\", response);\n", appField)
	b.WriteString("  }\n\n")
}

func renderPublicServiceClientGraphQLRawMethod(b *strings.Builder, svc *model.Service, m *model.Method, methodKey, methodRef string) {
	typeName := publicRequestTypeName(svc, m)
	inputNative := localName(m.Input.FullName)
	outputNative := localName(m.Output.FullName)
	sig := renderPublicServiceClientMethodSignature(svc, m, methodKey)
	fmt.Fprintf(b, "  async %s%s: Promise<%s> {\n", sig.prefix, sig.suffix, outputNative)
	fmt.Fprintf(b, "    return fromWire%s(\n      await this.transport.unary(\n        %s,\n        toWire%s(request),\n        %sSchema,\n        %sSchema,\n        callOptions,\n      ),\n    );\n  }\n\n",
		outputNative, methodRef, inputNative, inputNative, outputNative)
	_ = typeName
}

func renderPublicServiceClientGraphQLRawAlias(b *strings.Builder, svc *model.Service, m *model.Method) {
	typeName := publicRequestTypeName(svc, m)
	outputNative := localName(m.Output.FullName)
	b.WriteString("  /** Alias for invokeGraphQL. */\n")
	fmt.Fprintf(b, "  async invokeGraphQLRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s> {\n",
		typeName, outputNative)
	b.WriteString("    return this.invokeGraphQL(request, callOptions);\n  }\n\n")
}

func renderPublicServiceClientGraphQLDecodedMethod(b *strings.Builder, svc *model.Service, m *model.Method) {
	_ = svc
	_ = m
	b.WriteString("  async invokeGraphQLDecoded<T = unknown>(request: PublicAppInvokeGraphQLRequest, callOptions?: PublicUnaryCallOptions): Promise<T> {\n")
	b.WriteString("    const response = await this.invokeGraphQL(request, callOptions);\n")
	b.WriteString("    return decodeGraphQLResult<T>(request.app ?? \"\", response);\n")
	b.WriteString("  }\n\n")
}

// renderPublicServiceClientStreamMethod renders a server-streaming public
// method. It returns an AsyncIterable of decoded output frames by piping the
// transport's serverStream through the per-frame fromWire codec.
func renderPublicServiceClientStreamMethod(b *strings.Builder, svc *model.Service, m *model.Method, methodKey, methodRef string) {
	typeName := publicRequestTypeName(svc, m)
	outputNative := localName(m.Output.FullName)
	fromWire := publicServiceFromWireExpr(m)
	wireExpr := publicServiceWireRequestExpr(m)
	inputSchema := publicServiceInputSchema(m)
	outputSchema := publicServiceOutputSchema(m)

	fmt.Fprintf(b, "  %s(request: %s, callOptions?: PublicUnaryCallOptions): AsyncIterable<%s> {\n",
		methodKey, typeName, outputNative)
	fmt.Fprintf(b, "    if (this.transport.serverStream === undefined) {\n      throw new Error(\"streaming is not supported by this transport\");\n    }\n")
	fmt.Fprintf(b, "    const frames = this.transport.serverStream(\n      %s,\n      %s,\n      %s,\n      %s,\n      callOptions,\n    );\n",
		methodRef, wireExpr, inputSchema, outputSchema)
	fmt.Fprintf(b, "    return mapServerStreamFrames(frames, (f) => %s(f as Parameters<typeof %s>[0]));\n  }\n\n",
		fromWire, fromWire)
}

func renderPublicRESTInterfaceMethod(b *strings.Builder, svc *model.Service, m *model.Method) {
	if m.Stream == model.ServerStream && m.Input != nil && m.Output != nil {
		typeName := publicRequestTypeName(svc, m)
		outputNative := localName(m.Output.FullName)
		fmt.Fprintf(b, "  %s(request: %s, callOptions?: PublicUnaryCallOptions): AsyncIterable<%s>;\n",
			lowerFirst(m.Name), typeName, outputNative)
		return
	}
	if m.JsonResult != nil && m.Input != nil && m.Output != nil {
		typeName := publicRequestTypeName(svc, m)
		outputNative := localName(m.Output.FullName)
		fmt.Fprintf(b, "  %sRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s>;\n",
			lowerFirst(m.Name), typeName, outputNative)
		fmt.Fprintf(b, "  %s<T = unknown>(request: %s, callOptions?: PublicUnaryCallOptions): Promise<T>;\n",
			lowerFirst(m.Name), typeName)
		return
	}
	if m.Name == "InvokeGraphQL" && m.Input != nil && m.Output != nil {
		typeName := publicRequestTypeName(svc, m)
		outputNative := localName(m.Output.FullName)
		fmt.Fprintf(b, "  invokeGraphQL(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s>;\n",
			typeName, outputNative)
		fmt.Fprintf(b, "  invokeGraphQLRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s>;\n",
			typeName, outputNative)
		b.WriteString("  invokeGraphQLDecoded<T = unknown>(request: PublicAppInvokeGraphQLRequest, callOptions?: PublicUnaryCallOptions): Promise<T>;\n")
		return
	}
	sig := renderPublicServiceClientMethodSignature(svc, m, lowerFirst(m.Name))
	returnType := publicServiceClientReturnType(m)
	if m.OutputIsEmpty {
		fmt.Fprintf(b, "  %s%s: Promise<void>;\n", sig.prefix, sig.suffix)
		return
	}
	fmt.Fprintf(b, "  %s%s: Promise<%s>;\n", sig.prefix, sig.suffix, returnType)
}

func publicJSONResultAppField(m *model.Method) string {
	if m.Input == nil {
		return "app"
	}
	for _, f := range m.Input.Fields {
		if f.Name == "app" {
			if f.JSONName != "" {
				return f.JSONName
			}
			return f.Name
		}
	}
	return "app"
}

func tsPublicFieldList(fields []publicsurface.PublicField) string {
	if len(fields) == 0 {
		return "[]"
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("{ name: %q, jsonName: %q }", f.Name, f.JSONName)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func sortedPublicKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func renderPublicGatewayError(imports PublicImports) string {
	return strings.ReplaceAll(
		gatewayErrorFile,
		"__RPC_SUPPORT_IMPORT__",
		imports.supportModuleQuoted("rpc_support.ts"),
	)
}

func renderPublicRestRequestMapping() string {
	return restRequestMappingFile
}

func renderPublicTransportSupport(imports PublicImports) string {
	return strings.ReplaceAll(
		transportSupportFile,
		"__RPC_SUPPORT_IMPORT__",
		imports.supportModuleQuoted("rpc_support.ts"),
	)
}
