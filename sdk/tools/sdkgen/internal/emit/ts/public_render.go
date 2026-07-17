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
	b.WriteString("  responseIsOperationResult: boolean;\n")
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
	fmt.Fprintf(&b, "    responseIsOperationResult: %t,\n", publicsurface.ResponseIsOperationResult(pm))
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
			if len(omit) == 0 {
				blocks = append(blocks, fmt.Sprintf("export type %s = %s;", typeName, native))
				continue
			}
			sort.Strings(omit)
			quoted := make([]string, len(omit))
			for i, name := range omit {
				quoted[i] = fmt.Sprintf("%q", publicJSONFieldName(m.Input, name))
			}
			blocks = append(blocks, fmt.Sprintf(
				"export type %s = Omit<%s, %s>;",
				typeName,
				native,
				strings.Join(quoted, " | "),
			))
		}
	}

	for _, base := range sortedPublicKeys(imports) {
		names := sortedPublicKeys(imports[base])
		fmt.Fprintf(&b, "import type { %s } from %s;\n", strings.Join(names, ", "), paths.nativeTypeImportQuoted(base))
	}
	if len(imports) > 0 {
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
}
`
}

func renderPublicAppClient(services []*model.Service, paths PublicImports) string {
	var b strings.Builder
	b.WriteString("/**\n * Transport-neutral public service clients.\n *\n * @module client/generated/app_client\n */\n\n")
	b.WriteString("import { create } from \"@bufbuild/protobuf\";\n")
	b.WriteString("import { EmptySchema } from \"@bufbuild/protobuf/wkt\";\n\n")

	schemaImports := map[string]map[string]bool{}
	nativeImports := map[string]map[string]bool{}
	codecImports := map[string]map[string]bool{}
	var requestTypes []string
	converterImports := map[string]bool{}

	for _, svc := range services {
		for _, m := range svc.Methods {
			if !m.InputIsEmpty && m.Input == nil {
				continue
			}
			if !m.OutputIsEmpty && m.Output == nil {
				continue
			}
			if !m.InputIsEmpty && m.Input != nil {
				inputNative := localName(m.Input.FullName)
				if schemaImports[m.Input.ProtoFile] == nil {
					schemaImports[m.Input.ProtoFile] = map[string]bool{}
				}
				schemaImports[m.Input.ProtoFile][inputNative+"Schema"] = true
				requestTypes = append(requestTypes, publicRequestTypeName(svc, m))
				converterImports["toWire"+inputNative] = true
			}
			if !m.OutputIsEmpty && m.Output != nil {
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
	b.WriteString("import { decodeAppResult } from " + paths.supportModuleQuoted("invoke_support.ts") + ";\n")
	b.WriteString("import {\n")
	for _, name := range sortedPublicKeys(converterImports) {
		fmt.Fprintf(&b, "  %s,\n", name)
	}
	b.WriteString("} from \"./converters.ts\";\n")
	b.WriteString("import { PUBLIC_METHODS } from \"./methods.ts\";\n")
	b.WriteString("import type {\n  " + strings.Join(requestTypes, ",\n  ") + ",\n} from \"./types.ts\";\n")
	b.WriteString("import type { UnaryTransport, PublicUnaryCallOptions } from \"./unary_transport.ts\";\n\n")

	for _, svc := range services {
		fmt.Fprintf(&b, "export class %sClient {\n", svc.Name)
		b.WriteString("  constructor(private readonly transport: UnaryTransport) {}\n\n")
		serviceKey := lowerFirst(svc.Name)
		for _, m := range svc.Methods {
			renderPublicAppClientMethod(&b, svc, m, serviceKey)
		}
		b.WriteString("}\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderPublicAppClientMethod(b *strings.Builder, svc *model.Service, m *model.Method, serviceKey string) {
	if m.OutputIsEmpty {
		renderPublicEmptyOutputMethod(b, svc, m, serviceKey)
		return
	}
	if m.InputIsEmpty {
		renderPublicEmptyInputMethod(b, svc, m, serviceKey)
		return
	}
	if m.Input == nil || m.Output == nil {
		return
	}
	methodKey := lowerFirst(m.Name)
	typeName := publicRequestTypeName(svc, m)
	inputNative := localName(m.Input.FullName)
	outputNative := localName(m.Output.FullName)
	wireExpr := publicWireRequestExpr(m)
	schemaName := inputNative + "Schema"
	outputSchema := outputNative + "Schema"
	methodRef := fmt.Sprintf("PUBLIC_METHODS.%s.%s", serviceKey, methodKey)
	fromWire := "fromWire" + outputNative

	rawCall := fmt.Sprintf(`    return %s(
      await this.transport.unary(
        %s,
        %s,
        %s,
        %s,
        callOptions,
      ),
    );`, fromWire, methodRef, wireExpr, schemaName, outputSchema)

	if m.JsonResult != nil {
		fmt.Fprintf(b, "  async %sRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s> {\n", methodKey, typeName, outputNative)
		b.WriteString(rawCall)
		b.WriteString("  }\n\n")

		if m.Name == "Invoke" {
			b.WriteString("  /**\n   * The result decodes with the standard JSON operation envelope semantics;\n   * envelope failures throw InvokeError.\n   */\n")
		}
		fmt.Fprintf(b, "  async %s<T = unknown>(request: %s, callOptions?: PublicUnaryCallOptions): Promise<T> {\n", methodKey, typeName)
		appField := publicJSONResultAppField(m)
		fmt.Fprintf(b, "    const response = await this.%sRaw(request, callOptions);\n", methodKey)
		fmt.Fprintf(b, "    return decodeAppResult<T>(request.%s, request.operation, response);\n", appField)
		b.WriteString("  }\n\n")
		return
	}

	fmt.Fprintf(b, "  async %s(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s> {\n", methodKey, typeName, outputNative)
	b.WriteString(rawCall)
	b.WriteString("  }\n\n")

	fmt.Fprintf(b, "  async %sRaw(request: %s, callOptions?: PublicUnaryCallOptions): Promise<%s> {\n", methodKey, typeName, outputNative)
	fmt.Fprintf(b, "    return this.%s(request, callOptions);\n", methodKey)
	b.WriteString("  }\n\n")
}

func renderPublicEmptyOutputMethod(b *strings.Builder, svc *model.Service, m *model.Method, serviceKey string) {
	methodKey := lowerFirst(m.Name)
	methodRef := fmt.Sprintf("PUBLIC_METHODS.%s.%s", serviceKey, methodKey)
	if m.InputIsEmpty {
		fmt.Fprintf(b, "  async %s(callOptions?: PublicUnaryCallOptions): Promise<void> {\n", methodKey)
		fmt.Fprintf(b, "    await this.transport.unary(%s, create(EmptySchema), EmptySchema, EmptySchema, callOptions);\n", methodRef)
	} else if m.Input != nil {
		typeName := publicRequestTypeName(svc, m)
		inputNative := localName(m.Input.FullName)
		fmt.Fprintf(b, "  async %s(request: %s, callOptions?: PublicUnaryCallOptions): Promise<void> {\n", methodKey, typeName)
		fmt.Fprintf(b, "    await this.transport.unary(%s, toWire%s(request), %sSchema, EmptySchema, callOptions);\n",
			methodRef, inputNative, inputNative)
	}
	b.WriteString("  }\n\n")
}

func renderPublicEmptyInputMethod(b *strings.Builder, svc *model.Service, m *model.Method, serviceKey string) {
	if m.Output == nil {
		return
	}
	methodKey := lowerFirst(m.Name)
	methodRef := fmt.Sprintf("PUBLIC_METHODS.%s.%s", serviceKey, methodKey)
	outputNative := localName(m.Output.FullName)
	fmt.Fprintf(b, "  async %s(callOptions?: PublicUnaryCallOptions): Promise<%s> {\n", methodKey, outputNative)
	fmt.Fprintf(b, "    return fromWire%s(\n", outputNative)
	fmt.Fprintf(b, "      await this.transport.unary(%s, create(EmptySchema), EmptySchema, %sSchema, callOptions),\n", methodRef, outputNative)
	b.WriteString("    );\n  }\n\n")
}

func publicWireRequestExpr(m *model.Method) string {
	return "toWire" + localName(m.Input.FullName) + "(request)"
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

func renderPublicTransportKernel(imports PublicImports) string {
	return strings.ReplaceAll(
		transportKernelFile,
		"__RPC_SUPPORT_IMPORT__",
		imports.supportModuleQuoted("rpc_support.ts"),
	)
}

func renderPublicGrpcDispatch(services []*model.Service, imports PublicImports) string {
	var b strings.Builder
	b.WriteString("/**\n * Generated gRPC method dispatcher for Connect clients.\n *\n * @module client/generated/grpc_dispatch\n */\n\n")
	b.WriteString("import { createClient, type Client, type Transport } from \"@connectrpc/connect\";\n")
	b.WriteString("import type { Message } from \"@bufbuild/protobuf\";\n\n")

	protoFiles := map[string]bool{}
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.Input != nil && m.Input.ProtoFile != "" {
				protoFiles[m.Input.ProtoFile] = true
			} else if m.Output != nil && m.Output.ProtoFile != "" {
				protoFiles[m.Output.ProtoFile] = true
			}
		}
	}
	var protoList []string
	for pf := range protoFiles {
		protoList = append(protoList, pf)
	}
	sort.Strings(protoList)
	for _, pf := range protoList {
		wireName := serviceWireNameForProto(services, pf)
		if wireName == "" {
			continue
		}
		fmt.Fprintf(&b, "import { %s } from %s;\n", wireName, imports.genModuleQuoted(pf))
	}
	b.WriteString("\nimport {\n  GestaltError,\n  GestaltErrorCode,\n} from ")
	b.WriteString(imports.supportModuleQuoted("rpc_support.ts"))
	b.WriteString(";\n\n")
	b.WriteString("import { PUBLIC_METHODS, type PublicMethod } from \"./methods.ts\";\n\n")

	b.WriteString("export interface PublicGrpcClients {\n")
	for _, svc := range services {
		wireName := localName(svc.FullName)
		fmt.Fprintf(&b, "  readonly %s: Client<typeof %s>;\n", lowerFirst(svc.Name), wireName)
	}
	b.WriteString("}\n\n")

	b.WriteString("export function createPublicGrpcClients(transport: Transport): PublicGrpcClients {\n")
	b.WriteString("  return {\n")
	for i, svc := range services {
		wireName := localName(svc.FullName)
		key := lowerFirst(svc.Name)
		comma := ","
		if i == len(services)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "    %s: createClient(%s, transport)%s\n", key, wireName, comma)
	}
	b.WriteString("  };\n}\n\n")

	b.WriteString("export interface GrpcUnaryRequestOptions {\n")
	b.WriteString("  signal?: AbortSignal;\n")
	b.WriteString("  headers?: Record<string, string>;\n")
	b.WriteString("}\n\n")

	b.WriteString("export async function dispatchGrpcUnary<Output extends Message>(\n")
	b.WriteString("  clients: PublicGrpcClients,\n")
	b.WriteString("  method: PublicMethod,\n")
	b.WriteString("  request: Message,\n")
	b.WriteString("  requestOptions?: GrpcUnaryRequestOptions,\n")
	b.WriteString("): Promise<Output> {\n")
	b.WriteString("  switch (method.grpcPath) {\n")

	for _, svc := range services {
		serviceKey := lowerFirst(svc.Name)
		wireName := localName(svc.FullName)
		for _, m := range svc.Methods {
			if m.Stream != model.Unary {
				continue
			}
			clientMethod := lowerFirst(m.Name)
			fmt.Fprintf(
				&b,
				"    case PUBLIC_METHODS.%s.%s.grpcPath:\n      return (await clients.%s.%s(\n        request as Parameters<Client<typeof %s>[%q]>[0],\n        requestOptions,\n      )) as unknown as Output;\n",
				serviceKey,
				clientMethod,
				serviceKey,
				clientMethod,
				wireName,
				clientMethod,
			)
		}
	}

	b.WriteString("    default:\n")
	b.WriteString("      throw new GestaltError(\n")
	b.WriteString("        GestaltErrorCode.Unimplemented,\n")
	b.WriteString("        `unknown public gRPC method ${method.grpcPath}`,\n")
	b.WriteString("      );\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func serviceWireNameForProto(services []*model.Service, protoFile string) string {
	for _, svc := range services {
		if svc.ProtoFile == protoFile {
			return localName(svc.FullName)
		}
	}
	return ""
}
