package publicts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func renderMethods(view *publicsurface.View) string {
	var b strings.Builder
	b.WriteString("/**\n * Public transport method metadata.\n *\n * @module client/generated/methods\n */\n\n")
	b.WriteString("export interface PublicMethodHttp {\n")
	b.WriteString("  verb: \"GET\" | \"PUT\" | \"POST\" | \"PATCH\" | \"DELETE\";\n")
	b.WriteString("  path: string;\n")
	b.WriteString("  body: string;\n")
	b.WriteString("}\n\n")
	b.WriteString("export interface PublicMethod {\n")
	b.WriteString("  service: string;\n")
	b.WriteString("  method: string;\n")
	b.WriteString("  grpcPath: string;\n")
	b.WriteString("  http?: PublicMethodHttp | undefined;\n")
	b.WriteString("  fill: readonly string[];\n")
	b.WriteString("  reject: readonly string[];\n")
	b.WriteString("}\n\n")
	b.WriteString("export const PUBLIC_METHODS = {\n")

	byService := map[string][]*model.Method{}
	var serviceOrder []string
	for _, svc := range view.Services {
		serviceKey := lowerFirst(localName(svc.FullName))
		serviceOrder = append(serviceOrder, serviceKey)
		byService[serviceKey] = append(byService[serviceKey], svc.PublicMethods...)
	}
	sort.Strings(serviceOrder)
	for si, serviceKey := range serviceOrder {
		methods := byService[serviceKey]
		sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
		fmt.Fprintf(&b, "  %s: {\n", serviceKey)
		for mi, m := range methods {
			svc := findService(view, m)
			fmt.Fprintf(&b, "    %s: %s", lowerFirst(m.Name), renderMethodEntry(svc, m))
			if mi < len(methods)-1 {
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

func findService(view *publicsurface.View, m *model.Method) *model.Service {
	for _, svc := range view.Services {
		for _, candidate := range svc.PublicMethods {
			if candidate == m {
				return svc.Service
			}
		}
	}
	return nil
}

func renderMethodEntry(svc *model.Service, m *model.Method) string {
	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "    service: %q,\n", localName(svc.FullName))
	fmt.Fprintf(&b, "    method: %q,\n", m.Name)
	fmt.Fprintf(&b, "    grpcPath: %q,\n", m.FullMethod)
	if fill := policyList(m, true); len(fill) > 0 {
		fmt.Fprintf(&b, "    fill: %s,\n", stringList(fill))
	} else {
		b.WriteString("    fill: [],\n")
	}
	if reject := policyList(m, false); len(reject) > 0 {
		fmt.Fprintf(&b, "    reject: %s,\n", stringList(reject))
	} else {
		b.WriteString("    reject: [],\n")
	}
	if m.HTTP != nil {
		b.WriteString("    http: {\n")
		fmt.Fprintf(&b, "      verb: %q,\n", strings.ToUpper(m.HTTP.Verb))
		fmt.Fprintf(&b, "      path: %q,\n", m.HTTP.Path)
		fmt.Fprintf(&b, "      body: %q,\n", m.HTTP.Body)
		b.WriteString("    },\n")
	}
	b.WriteString("  }")
	return b.String()
}

func policyList(m *model.Method, fill bool) []string {
	if m.PublicPolicy == nil {
		return nil
	}
	if fill {
		return append([]string(nil), m.PublicPolicy.Fill...)
	}
	return append([]string(nil), m.PublicPolicy.Reject...)
}

func stringList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func renderTypes(view *publicsurface.View, idx *index) string {
	var b strings.Builder
	b.WriteString("/**\n * Public request types with fill and reject fields omitted.\n *\n * @module client/generated/types\n */\n\n")

	imports := map[string]map[string]bool{}
	var blocks []string

	for _, svc := range view.Services {
		base := generatedFileBase(svc.ProtoFile)
		for _, m := range svc.PublicMethods {
			if m.InputIsEmpty || m.Input == nil {
				continue
			}
			omit := omittedFieldNames(m)
			native := localName(m.Input.FullName)
			typeName := publicRequestTypeName(svc.Service, m)
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
				quoted[i] = fmt.Sprintf("%q", jsonFieldName(m.Input, name))
			}
			blocks = append(blocks, fmt.Sprintf(
				"export type %s = Omit<%s, %s>;",
				typeName,
				native,
				strings.Join(quoted, " | "),
			))
		}
	}

	for _, base := range sortedKeys(imports) {
		names := sortedKeys(imports[base])
		fmt.Fprintf(&b, "import type { %s } from \"../../%s.ts\";\n", strings.Join(names, ", "), base)
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

func jsonFieldName(msg *model.Message, protoName string) string {
	for _, f := range msg.Fields {
		if f.Name == protoName {
			return f.JSONName
		}
	}
	return protoName
}

func omittedFieldNames(m *model.Method) []string {
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

func renderClients(view *publicsurface.View, idx *index, restOnly bool) string {
	var b strings.Builder
	module := "rest_clients"
	if !restOnly {
		module = "grpc_clients"
	}
	fmt.Fprintf(&b, "/**\n * Generated public %s transport clients.\n *\n * @module client/generated/%s\n */\n\n", strings.TrimSuffix(module, "_clients"), module)

	needsInit := false
	needsInvoke := false
	needsJsonObject := false
	needsEmptySchema := false
	imports := clientImports{}

	var classes []string
	for _, svc := range view.Services {
		var methods []*model.Method
		for _, m := range svc.PublicMethods {
			if restOnly && m.HTTP == nil {
				continue
			}
			methods = append(methods, m)
		}
		if len(methods) == 0 {
			continue
		}
		class, used := renderClientClass(svc.Service, methods, idx, restOnly)
		classes = append(classes, class)
		needsInit = needsInit || used.needsInit
		needsInvoke = needsInvoke || used.needsInvoke
		needsJsonObject = needsJsonObject || used.needsJsonObject
		needsEmptySchema = needsEmptySchema || used.emptySchema
		mergeImports(&imports, used)
	}

	writeClientImports(&b, imports, needsInit, needsInvoke, needsJsonObject, needsEmptySchema, restOnly)
	for i, class := range classes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(class)
	}
	return b.String()
}

type clientImports struct {
	transport   bool
	methods     bool
	types       map[string]bool
	native      map[string]map[string]bool // base -> type names
	codec       map[string]map[string]bool // base -> converter names
	wire        map[string]bool            // base -> needed
	invokeFuncs map[string]bool
	nativeTypes map[string]bool // return types from native modules
	emptySchema bool
	needsInit   bool
	needsInvoke bool
	needsJsonObject bool
}

func mergeImports(dst *clientImports, used clientImports) {
	dst.transport = dst.transport || used.transport
	dst.methods = dst.methods || used.methods
	if dst.types == nil {
		dst.types = map[string]bool{}
	}
	for k, v := range used.types {
		if v {
			dst.types[k] = true
		}
	}
	if dst.native == nil {
		dst.native = map[string]map[string]bool{}
	}
	for base, names := range used.native {
		if dst.native[base] == nil {
			dst.native[base] = map[string]bool{}
		}
		for name := range names {
			dst.native[base][name] = true
		}
	}
	if dst.codec == nil {
		dst.codec = map[string]map[string]bool{}
	}
	for base, names := range used.codec {
		if dst.codec[base] == nil {
			dst.codec[base] = map[string]bool{}
		}
		for name := range names {
			dst.codec[base][name] = true
		}
	}
	if dst.wire == nil {
		dst.wire = map[string]bool{}
	}
	for base := range used.wire {
		dst.wire[base] = true
	}
	if dst.invokeFuncs == nil {
		dst.invokeFuncs = map[string]bool{}
	}
	for name := range used.invokeFuncs {
		dst.invokeFuncs[name] = true
	}
	if dst.nativeTypes == nil {
		dst.nativeTypes = map[string]bool{}
	}
	for name := range used.nativeTypes {
		dst.nativeTypes[name] = true
	}
	dst.emptySchema = dst.emptySchema || used.emptySchema
	dst.needsInit = dst.needsInit || used.needsInit
	dst.needsInvoke = dst.needsInvoke || used.needsInvoke
	dst.needsJsonObject = dst.needsJsonObject || used.needsJsonObject
}

func writeClientImports(b *strings.Builder, imports clientImports, needsInit, needsInvoke, needsJsonObject, needsEmptySchema, restOnly bool) {
	if needsEmptySchema {
		b.WriteString("import { create } from \"@bufbuild/protobuf\";\n")
		b.WriteString("import { EmptySchema } from \"@bufbuild/protobuf/wkt\";\n")
	}
	if needsJsonObject {
		b.WriteString("import type { JsonObject, JsonValue } from \"@bufbuild/protobuf\";\n")
	}
	if needsInit || needsInvoke {
		b.WriteString("import type { Init } from \"../../rpc_support.ts\";\n")
	}
	b.WriteString("import type { PublicTransport } from \"../transport.ts\";\n")
	b.WriteString("import { PUBLIC_METHODS } from \"./methods.ts\";\n")
	if len(imports.types) > 0 {
		names := sortedKeys(imports.types)
		fmt.Fprintf(b, "import type { %s } from \"./types.ts\";\n", strings.Join(names, ", "))
	}
	for _, base := range sortedKeys(imports.native) {
		names := sortedKeys(imports.native[base])
		fmt.Fprintf(b, "import type { %s } from \"../../%s.ts\";\n", strings.Join(names, ", "), base)
	}
	for _, base := range sortedKeys(imports.codec) {
		names := sortedKeys(imports.codec[base])
		fmt.Fprintf(b, "import {\n  %s,\n} from \"../../internal/codec/%s.ts\";\n", strings.Join(names, ",\n  "), base)
	}
	for _, base := range sortedKeys(imports.wire) {
		fmt.Fprintf(b, "import * as %s from \"../../internal/gen/v1/%s_pb.ts\";\n", wireAlias(base), base)
	}
	for name := range imports.invokeFuncs {
		fmt.Fprintf(b, "import { %s } from \"../../invoke_support.ts\";\n", name)
	}
	if needsInvoke && !hasNativeType(imports, "OperationResult") {
		b.WriteString("import type { OperationResult } from \"../../app.ts\";\n")
	}
	b.WriteString("\n")
	_ = restOnly
}

func renderClientClass(svc *model.Service, methods []*model.Method, idx *index, restOnly bool) (string, clientImports) {
	var b strings.Builder
	used := clientImports{
		transport:   true,
		methods:     true,
		types:       map[string]bool{},
		native:      map[string]map[string]bool{},
		codec:       map[string]map[string]bool{},
		wire:        map[string]bool{},
		invokeFuncs: map[string]bool{},
		nativeTypes: map[string]bool{},
	}
	base := generatedFileBase(svc.ProtoFile)
	className := "Public" + localName(svc.FullName) + "Client"
	fmt.Fprintf(&b, "export class %s {\n", className)
	b.WriteString("  constructor(private readonly transport: PublicTransport) {}\n\n")

	for _, m := range methods {
		skipFaithful := svc.FullName == "gestalt.provider.v1.App" &&
			(m.Name == "Invoke" || m.Name == "InvokeGraphQL")
		if !skipFaithful {
			renderClientMethod(&b, svc, m, idx, &used)
		}
		if svc.FullName == "gestalt.provider.v1.App" && m.Name == "Invoke" {
			renderAppInvokeErgonomic(&b, &used)
		}
		if svc.FullName == "gestalt.provider.v1.App" && m.Name == "InvokeGraphQL" {
			renderAppInvokeGraphQLErgonomic(&b, &used)
		}
	}
	b.WriteString("}\n")
	_ = base
	return b.String(), used
}

func wireAlias(base string) string {
	return "wire" + upperFirst(base)
}

func schemaRef(wireMod, schemaName string) string {
	if schemaName == "Empty" {
		return "EmptySchema"
	}
	return wireMod + "." + schemaName + "Schema"
}

func inputSchemaName(m *model.Method) string {
	if m.InputIsEmpty || m.Input == nil {
		return "Empty"
	}
	return localName(m.Input.FullName)
}

func renderClientMethod(b *strings.Builder, svc *model.Service, m *model.Method, idx *index, used *clientImports) {
	methodName := lowerFirst(m.Name)
	serviceKey := lowerFirst(localName(svc.FullName))
	base := generatedFileBase(svc.ProtoFile)
	wireMod := wireAlias(base)
	used.wire[base] = true

	var params string
	var requestExpr string
	if m.InputIsEmpty || m.Input == nil {
		params = ""
		requestExpr = "create(EmptySchema)"
		used.emptySchema = true
	} else {
		publicType := publicRequestTypeName(svc, m)
		nativeType := localName(m.Input.FullName)
		used.types[publicType] = true
		addNativeImport(used, svc.ProtoFile, nativeType)
		params = fmt.Sprintf("request: Init<%s>", publicType)
		requestExpr = fmt.Sprintf("toWire%s(request as Init<%s>)", nativeType, nativeType)
		addCodecImport(used, svc.ProtoFile, "toWire"+nativeType)
	}

	returnType := "void"
	var returnStmt string
	if m.OutputIsEmpty || m.Output == nil {
		if params == "" {
			params = ""
		}
		returnStmt = fmt.Sprintf(
			"await this.transport.unary(PUBLIC_METHODS.%s.%s, %s, EmptySchema, EmptySchema)",
			serviceKey, methodName, requestExpr,
		)
	} else {
		outName := localName(m.Output.FullName)
		returnType = outName
		addNativeImport(used, svc.ProtoFile, outName)
		addCodecImport(used, svc.ProtoFile, "fromWire"+outName)
		if jr := m.JsonResult; jr != nil {
			returnType = "OperationResult"
			used.needsInvoke = true
			returnStmt = fmt.Sprintf(
				"return fromWire%s(await this.transport.unary(PUBLIC_METHODS.%s.%s, %s, %s, %s));",
				outName, serviceKey, methodName, requestExpr,
				schemaRef(wireMod, inputSchemaName(m)),
				schemaRef(wireMod, outName),
			)
		} else if collapsed := collapseReturn(m, idx); collapsed != nil {
			returnType = collapsed.returnType
			addFieldTypeImports(used, idx, m.Output, collapsed.fieldProto)
			if collapsed.returnType == "JsonValue" {
				used.needsJsonObject = true
			}
			returnStmt = fmt.Sprintf(
				"const response = fromWire%s(await this.transport.unary(PUBLIC_METHODS.%s.%s, %s, %s, %s));\n    return response.%s;",
				outName, serviceKey, methodName, requestExpr,
				schemaRef(wireMod, inputSchemaName(m)),
				schemaRef(wireMod, outName),
				collapsed.fieldJSON,
			)
		} else {
			returnStmt = fmt.Sprintf(
				"return fromWire%s(await this.transport.unary(PUBLIC_METHODS.%s.%s, %s, %s, %s));",
				outName, serviceKey, methodName, requestExpr,
				schemaRef(wireMod, inputSchemaName(m)),
				schemaRef(wireMod, outName),
			)
		}
	}

	if m.OutputIsEmpty || m.Output == nil {
		fmt.Fprintf(b, "  async %s(%s): Promise<void> {\n", methodName, params)
		fmt.Fprintf(b, "    %s;\n", returnStmt)
	} else {
		fmt.Fprintf(b, "  async %s(%s): Promise<%s> {\n", methodName, params, returnType)
		fmt.Fprintf(b, "    %s\n", returnStmt)
	}
	b.WriteString("  }\n\n")
}

func renderAppInvokeErgonomic(b *strings.Builder, used *clientImports) {
	used.needsInit = true
	used.needsInvoke = true
	used.needsJsonObject = true
	used.invokeFuncs["decodeAppResult"] = true
	used.types["PublicAppInvokeRequest"] = true
	addNativeImport(used, "v1/app.proto", "AppInvokeRequest")
	addCodecImport(used, "v1/app.proto", "toWireAppInvokeRequest")
	addCodecImport(used, "v1/app.proto", "fromWireOperationResult")
	used.wire["app"] = true
	wireApp := wireAlias("app")
	fmt.Fprintf(b, `  /**
   * The result decodes with the standard JSON operation envelope semantics;
   * envelope failures throw InvokeError.
   */
  async invoke<T = unknown>(
    app: string,
    operation: string,
    params?: JsonObject,
    options?: {
      connection?: string | undefined;
      instance?: string | undefined;
      idempotencyKey?: string | undefined;
      credentialMode?: string | undefined;
    },
  ): Promise<T> {
    const request = {
      app,
      operation,
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey ?? "",
      credentialMode: options?.credentialMode ?? "",
      ...(params !== undefined ? { params } : {}),
    } satisfies Init<PublicAppInvokeRequest>;
    const response = await this.invokeRaw(request);
    return decodeAppResult<T>(request.app, request.operation, response);
  }

  async invokeRaw(request: Init<PublicAppInvokeRequest>): Promise<OperationResult> {
    const response = fromWireOperationResult(
      await this.transport.unary(
        PUBLIC_METHODS.app.invoke,
        toWireAppInvokeRequest(request as Init<AppInvokeRequest>),
        %s.AppInvokeRequestSchema,
        %s.OperationResultSchema,
      ),
    );
    return response;
  }

`, wireApp, wireApp)
}

func renderAppInvokeGraphQLErgonomic(b *strings.Builder, used *clientImports) {
	used.needsInit = true
	used.needsInvoke = true
	used.needsJsonObject = true
	used.types["PublicAppInvokeGraphQLRequest"] = true
	addNativeImport(used, "v1/app.proto", "AppInvokeGraphQLRequest")
	addCodecImport(used, "v1/app.proto", "toWireAppInvokeGraphQLRequest")
	addCodecImport(used, "v1/app.proto", "fromWireOperationResult")
	used.wire["app"] = true
	wireApp := wireAlias("app")
	fmt.Fprintf(b, `  async invokeGraphQL(
    app: string,
    document: string,
    options?: {
      connection?: string | undefined;
      instance?: string | undefined;
      idempotencyKey?: string | undefined;
      variables?: JsonObject | undefined;
    },
  ): Promise<OperationResult> {
    const request = {
      app,
      document,
      connection: options?.connection ?? "",
      instance: options?.instance ?? "",
      idempotencyKey: options?.idempotencyKey ?? "",
      ...(options?.variables !== undefined ? { variables: options.variables } : {}),
    } satisfies Init<PublicAppInvokeGraphQLRequest>;
    return await this.invokeGraphQLRaw(request);
  }

  async invokeGraphQLRaw(
    request: Init<PublicAppInvokeGraphQLRequest>,
  ): Promise<OperationResult> {
    const response = fromWireOperationResult(
      await this.transport.unary(
        PUBLIC_METHODS.app.invokeGraphQL,
        toWireAppInvokeGraphQLRequest(request as Init<AppInvokeGraphQLRequest>),
        %s.AppInvokeGraphQLRequestSchema,
        %s.OperationResultSchema,
      ),
    );
    return response;
  }

`, wireApp, wireApp)
}

func addFieldTypeImports(used *clientImports, idx *index, msg *model.Message, fieldProto string) {
	if msg == nil {
		return
	}
	f := findField(msg, fieldProto)
	if f == nil {
		return
	}
	collectFieldTypeImports(used, idx, f)
}

func collectFieldTypeImports(used *clientImports, idx *index, f *model.Field) {
	switch f.Kind {
	case model.KindMessage:
		addMessageImport(used, idx, f.Message)
	case model.KindEnum:
		addEnumImport(used, idx, f.Enum)
	case model.KindRepeated:
		if f.Elem != nil && f.Elem.Kind == model.KindMessage {
			addMessageImport(used, idx, f.Elem.Message)
		}
		if f.Elem != nil && f.Elem.Kind == model.KindEnum {
			addEnumImport(used, idx, f.Elem.Enum)
		}
	case model.KindMap:
		if f.MapValue != nil && f.MapValue.Kind == model.KindMessage {
			addMessageImport(used, idx, f.MapValue.Message)
		}
	}
}

func addMessageImport(used *clientImports, idx *index, fullName string) {
	if msg := idx.messages[fullName]; msg != nil {
		addNativeImport(used, msg.ProtoFile, localName(fullName))
	}
}

func addEnumImport(used *clientImports, idx *index, fullName string) {
	if e := idx.enums[fullName]; e != nil {
		addNativeImport(used, e.ProtoFile, localName(fullName))
	}
}

func addNativeImport(used *clientImports, protoFile, name string) {
	base := generatedFileBase(protoFile)
	if used.native[base] == nil {
		used.native[base] = map[string]bool{}
	}
	used.native[base][name] = true
}

func addCodecImport(used *clientImports, protoFile, name string) {
	base := generatedFileBase(protoFile)
	if used.codec[base] == nil {
		used.codec[base] = map[string]bool{}
	}
	used.codec[base][name] = true
}

func findField(m *model.Message, protoName string) *model.Field {
	if m == nil {
		return nil
	}
	for _, f := range m.Fields {
		if f.Name == protoName {
			return f
		}
	}
	return nil
}

func collapseReturn(m *model.Method, idx *index) *collapsedReturn {
	if m.Output == nil {
		return nil
	}
	if or := m.Output.OptionalResult; or != nil {
		guard := findField(m.Output, or.Guard)
		value := findField(m.Output, or.Value)
		if guard != nil && value != nil {
			return collapsedFromField(idx, value)
		}
	}
	if m.Output.Unwrap != "" {
		if f := findField(m.Output, m.Output.Unwrap); f != nil {
			return collapsedFromField(idx, f)
		}
	}
	return nil
}

func collapsedFromField(idx *index, f *model.Field) *collapsedReturn {
	returnType := fieldTypeName(idx, f)
	if f.Presence == model.ExplicitPresence {
		returnType += " | undefined"
	}
	return &collapsedReturn{
		returnType: returnType,
		fieldJSON:  f.JSONName,
		fieldProto: f.Name,
	}
}

type collapsedReturn struct {
	returnType string
	fieldJSON  string
	fieldProto string
}

func fieldTypeName(idx *index, f *model.Field) string {
	switch f.Kind {
	case model.KindRepeated:
		return fieldRefTypeName(idx, f.Elem) + "[]"
	case model.KindMap:
		return "{ [key: string]: " + fieldRefTypeName(idx, f.MapValue) + " }"
	default:
		return fieldRefTypeName(idx, fieldRef(f))
	}
}

func fieldRef(f *model.Field) *model.TypeRef {
	return &model.TypeRef{
		Kind:    f.Kind,
		Scalar:  f.Scalar,
		Message: f.Message,
		Enum:    f.Enum,
	}
}

func fieldRefTypeName(idx *index, ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		switch ref.Scalar {
		case model.ScalarBool:
			return "boolean"
		case model.ScalarString:
			return "string"
		case model.ScalarInt64, model.ScalarSint64, model.ScalarUint64, model.ScalarSfixed64, model.ScalarFixed64:
			return "bigint"
		default:
			return "number"
		}
	case model.KindBytes:
		return "Uint8Array"
	case model.KindEnum:
		return localName(ref.Enum)
	case model.KindMessage:
		return localName(ref.Message)
	case model.KindJSONStruct:
		return "JsonObject"
	case model.KindJSONValue:
		return "JsonValue"
	case model.KindJSONNull:
		return "null"
	case model.KindTimestamp:
		return "Date"
	case model.KindDuration:
		return "DurationMs"
	case model.KindUnit:
		return "Unit"
	case model.KindRPCStatus:
		return "RpcStatus"
	default:
		return "unknown"
	}
}

func jsonResultContext(m *model.Method, name string) string {
	if m.Input != nil {
		if f := findField(m.Input, name); f != nil {
			return "request." + f.JSONName
		}
	}
	return `""`
}

func hasNativeType(imports clientImports, name string) bool {
	for _, names := range imports.native {
		if names[name] {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
