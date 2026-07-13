package ts

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// serverFeatures tracks the extra imports that the server renderer accumulates
// while rendering provider services.
type serverFeatures struct {
	connectrpc   bool                       // ConnectError, Code, ServiceImpl
	wire         bool                       // wire.*
	gestalt      bool                       // GestaltError, GestaltErrorCode
	providerBase bool                       // ProviderBase lifecycle contract
	streams      bool                       // SDK-owned provider stream contracts
	jsonObject   bool                       // JsonObject handler fields
	jsonValue    bool                       // JsonValue handler fields
	codecBases   map[string]map[string]bool // codec base -> converter name -> true
	nativeTypes  map[string]map[string]bool // public base -> native type name -> true
}

// serverRenderer is a renderer scoped to the provider server file.  It reuses
// the index and codec-fn naming helpers from the parent package and produces
// output for sdk/typescript/src/generated/providers/<base>.ts.
type serverRenderer struct {
	idx      *index
	base     string
	features serverFeatures
	body     strings.Builder
}

func newServerRenderer(idx *index, base string) *serverRenderer {
	return &serverRenderer{
		idx:  idx,
		base: base,
		features: serverFeatures{
			codecBases:  map[string]map[string]bool{},
			nativeTypes: map[string]map[string]bool{},
		},
	}
}

// providerName returns the handler class name for a provider service: the
// service name with a "Provider" suffix unless it already ends in "Provider",
// in which case it takes a "Handler" suffix (the generated client owns the
// bare name).
func providerName(svcName string) string {
	if strings.HasSuffix(svcName, "Provider") {
		return svcName + "Handler"
	}
	return svcName + "Provider"
}

func providerKindString(kind model.ProviderKind) string {
	switch kind {
	case model.ProviderKindApp:
		return "integration"
	case model.ProviderKindIdentity:
		return "identity"
	case model.ProviderKindIndexedDB:
		return "indexeddb"
	case model.ProviderKindSecrets:
		return "secrets"
	case model.ProviderKindTelemetry:
		return "telemetry"
	case model.ProviderKindCache:
		return "cache"
	case model.ProviderKindS3:
		return "s3"
	case model.ProviderKindWorkflow:
		return "workflow"
	case model.ProviderKindAuthorization:
		return "authorization"
	case model.ProviderKindRuntime:
		return "runtime"
	case model.ProviderKindAgent:
		return "agent"
	case model.ProviderKindExternalCredential:
		return "external_credential"
	case model.ProviderKindTest:
		return "test"
	default:
		return "integration"
	}
}

// operationString returns the human-readable operation tag carried on handler
// errors: the lowercased service name followed by the space-split method name
// ("cache get", "workflow apply definition").
func operationString(svcName, methodName string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(svcName))
	for i, r := range methodName {
		if i == 0 || (r >= 'A' && r <= 'Z') {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(strings.Join(strings.Fields(b.String()), " "))
}

// useCodec records a converter import from a codec base and returns the name.
func (r *serverRenderer) useCodec(protoFile, name string) string {
	base := generatedFileBase(protoFile)
	if r.features.codecBases[base] == nil {
		r.features.codecBases[base] = map[string]bool{}
	}
	r.features.codecBases[base][name] = true
	return name
}

// useNative records a native-type import from a public module base and returns
// the type name.
func (r *serverRenderer) useNative(protoFile, name string) string {
	base := generatedFileBase(protoFile)
	if r.features.nativeTypes[base] == nil {
		r.features.nativeTypes[base] = map[string]bool{}
	}
	r.features.nativeTypes[base][name] = true
	return name
}

// nativeType resolves the native TypeScript type name for a message, recording
// its public-module import.
func (r *serverRenderer) nativeType(fullName string) string {
	msg := r.idx.messages[fullName]
	name := localName(fullName)
	if msg != nil {
		return r.useNative(msg.ProtoFile, name)
	}
	return name
}

// handlerMethodSignature returns the TypeScript parameter and return-type
// strings for one handler method.  Handlers are transport-shaped: full native
// request in, full native response out.  An Empty response collapses to
// Promise<void>.
func (r *serverRenderer) handlerMethodSignature(m *model.Method) (params, returnType string) {
	if m.InputIsEmpty {
		params = ""
	} else if m.ProviderInput == model.ProviderInputClientSignature {
		params = r.providerFieldParams(m)
	} else {
		params = "request: " + r.nativeType(m.Input.FullName)
	}
	input, output := "", ""
	if !m.InputIsEmpty {
		input = r.nativeType(m.Input.FullName)
	}
	if !m.OutputIsEmpty {
		output = r.providerOutputType(m)
	}
	switch m.Stream {
	case model.ServerStream:
		r.features.streams = true
		if params == "" {
			params = "stream: ServerStream<" + output + ">"
		} else if m.ProviderInput != model.ProviderInputClientSignature {
			params += ", stream: ServerStream<" + output + ">"
		}
		returnType = "Promise<void>"
	case model.ClientStream:
		r.features.streams = true
		params = "stream: ClientStream<" + input + ">"
		if output == "" {
			returnType = "Promise<void>"
		} else {
			returnType = "Promise<" + output + ">"
		}
	case model.Bidi:
		r.features.streams = true
		params = "stream: BidiStream<" + input + ", " + output + ">"
		returnType = "Promise<void>"
	default:
		if output == "" {
			returnType = "Promise<void>"
		} else {
			returnType = "Promise<" + output + ">"
		}
	}
	return
}

func (r *serverRenderer) providerOutputType(m *model.Method) string {
	if m.Output == nil {
		return "void"
	}
	if m.Output.Unwrap != "" {
		if field := findField(m.Output, m.Output.Unwrap); field != nil {
			return r.providerFieldType(field)
		}
	}
	return r.nativeType(m.Output.FullName)
}

func (r *serverRenderer) providerOutputWire(m *model.Method, outFn, response string) string {
	if m.Output != nil && m.Output.Unwrap != "" {
		if field := findField(m.Output, m.Output.Unwrap); field != nil {
			return fmt.Sprintf("%s({ %s: %s })", outFn, field.JSONName, response)
		}
	}
	return fmt.Sprintf("%s(%s)", outFn, response)
}

func (r *serverRenderer) providerFieldParams(m *model.Method) string {
	var params []string
	names := append(append([]string{}, m.Signature...), m.OptionalSignature...)
	for _, name := range names {
		field := findField(m.Input, name)
		if field != nil {
			params = append(params, lowerFirst(field.JSONName)+": "+r.providerFieldType(field))
		}
	}
	return strings.Join(params, ", ")
}

func (r *serverRenderer) providerFieldType(field *model.Field) string {
	var typ string
	switch field.Kind {
	case model.KindRepeated:
		typ = r.providerRefType(field.Elem) + "[]"
	case model.KindMap:
		typ = "{ [key: " + mapKeyType(field.MapKey) + "]: " + r.providerRefType(field.MapValue) + " }"
	default:
		typ = r.providerRefType(fieldRef(field))
	}
	if field.Presence == model.ExplicitPresence {
		return typ + " | undefined"
	}
	return typ
}

func (r *serverRenderer) providerRefType(ref *model.TypeRef) string {
	switch ref.Kind {
	case model.KindScalar:
		return scalarType(ref.Scalar)
	case model.KindBytes:
		return "Uint8Array"
	case model.KindEnum, model.KindMessage:
		if ref.Kind == model.KindEnum {
			return r.useNative(r.idx.enums[ref.Enum].ProtoFile, localName(ref.Enum))
		}
		return r.nativeType(ref.Message)
	case model.KindJSONStruct:
		r.features.jsonObject = true
		return "JsonObject"
	case model.KindJSONValue:
		r.features.jsonValue = true
		return "JsonValue"
	case model.KindTimestamp:
		return "Date"
	case model.KindDuration:
		return "number"
	default:
		return "unknown"
	}
}

func (r *serverRenderer) providerFieldArgs(m *model.Method, request string) string {
	var args []string
	names := append(append([]string{}, m.Signature...), m.OptionalSignature...)
	for _, name := range names {
		field := findField(m.Input, name)
		if field != nil {
			args = append(args, request+"."+field.JSONName)
		}
	}
	return strings.Join(args, ", ")
}

// renderProviderHandler renders the native handler class for one provider
// service. The class owns both the RPC contract and ProviderBase lifecycle
// hooks, so authored providers only extend one generated surface.
func (r *serverRenderer) renderProviderHandler(svc *model.Service) {
	svcName := localName(svc.FullName)
	name := providerName(svcName)

	r.features.gestalt = true
	providerLifecycle := svc.ProviderKind != model.ProviderKindApp
	if providerLifecycle {
		r.features.providerBase = true
	}

	// Class doc comment.
	fmt.Fprintf(&r.body, "/**\n * %s is the base handler class for the %s service.\n *\n", name, svc.FullName)
	fmt.Fprintf(&r.body, " * Extend this class and override the methods your provider implements.\n")
	fmt.Fprintf(&r.body, " * Unoverridden methods throw a GestaltError with code Unimplemented.\n")
	fmt.Fprintf(&r.body, " */\n")
	if providerLifecycle {
		fmt.Fprintf(&r.body, "export abstract class %s extends ProviderBase {\n", name)
		fmt.Fprintf(&r.body, "  readonly kind = %q as const;\n\n", providerKindString(svc.ProviderKind))
	} else {
		fmt.Fprintf(&r.body, "export abstract class %s {\n", name)
	}
	for _, m := range svc.Methods {
		params, returnType := r.handlerMethodSignature(m)
		op := operationString(svcName, m.Name)
		if params == "" {
			fmt.Fprintf(&r.body, "  async %s(): %s {\n", lowerFirst(m.Name), returnType)
		} else {
			fmt.Fprintf(&r.body, "  async %s(%s): %s {\n", lowerFirst(m.Name), params, returnType)
		}
		fmt.Fprintf(&r.body, "    throw new GestaltError(\n")
		fmt.Fprintf(&r.body, "      GestaltErrorCode.Unimplemented,\n")
		fmt.Fprintf(&r.body, "      %q,\n", op+" is not implemented")
		fmt.Fprintf(&r.body, "    );\n")
		fmt.Fprintf(&r.body, "  }\n\n")
	}
	fmt.Fprintf(&r.body, "}\n\n")
}

// renderProviderService renders the wire dispatch adapter factory for one
// provider service: a function returning Partial<ServiceImpl<typeof wire.Svc>>
// that converts requests from wire, calls the handler, converts responses to
// wire, and maps handler errors via the inline statusError helper.
func (r *serverRenderer) renderProviderService(svc *model.Service) {
	svcName := localName(svc.FullName)
	name := providerName(svcName)

	r.features.connectrpc = true
	r.features.wire = true

	fmt.Fprintf(&r.body, "/**\n * create%sService adapts a %s handler to the connect-es\n", name, name)
	fmt.Fprintf(&r.body, " * ServiceImpl surface for %s, converting wire types via the generated\n", svcName)
	fmt.Fprintf(&r.body, " * codec and mapping handler errors to ConnectError.\n */\n")
	fmt.Fprintf(&r.body, "export function create%sService(\n", name)
	fmt.Fprintf(&r.body, "  handler: %s,\n", name)
	fmt.Fprintf(&r.body, "): Partial<ServiceImpl<typeof wire.%s>> {\n", svcName)
	fmt.Fprintf(&r.body, "  return {\n")

	for _, m := range svc.Methods {
		op := operationString(svcName, m.Name)
		methodName := lowerFirst(m.Name)

		if m.Stream == model.ServerStream {
			r.features.streams = true
			fmt.Fprintf(&r.body, "    async *%s(request) {\n", methodName)
		} else if m.Stream == model.Bidi {
			r.features.streams = true
			fmt.Fprintf(&r.body, "    async *%s(requests) {\n", methodName)
		} else if m.Stream == model.ClientStream {
			r.features.streams = true
			fmt.Fprintf(&r.body, "    async %s(requests) {\n", methodName)
		} else if m.InputIsEmpty {
			fmt.Fprintf(&r.body, "    async %s(_request) {\n", methodName)
		} else {
			fmt.Fprintf(&r.body, "    async %s(request) {\n", methodName)
		}
		fmt.Fprintf(&r.body, "      try {\n")

		if m.Stream == model.ServerStream {
			outType := "unknown"
			if !m.OutputIsEmpty {
				outType = r.nativeType(m.Output.FullName)
			}
			fmt.Fprintf(&r.body, "        const values: %s[] = [];\n", outType)
			fmt.Fprintf(&r.body, "        const stream: ServerStream<%s> = { send(value) { values.push(value); } };\n", outType)
			if m.InputIsEmpty {
				fmt.Fprintf(&r.body, "        await handler.%s(stream);\n", methodName)
			} else if m.ProviderInput == model.ProviderInputClientSignature {
				inFn := r.useCodec(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
				fmt.Fprintf(&r.body, "        const nativeRequest = %s(request);\n        await handler.%s(%s, stream);\n", inFn, methodName, r.providerFieldArgs(m, "nativeRequest"))
			} else {
				inFn := r.useCodec(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
				fmt.Fprintf(&r.body, "        await handler.%s(%s(request), stream);\n", methodName, inFn)
			}
			if !m.OutputIsEmpty {
				outFn := r.useCodec(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
				fmt.Fprintf(&r.body, "        for (const value of values) yield %s(value);\n", outFn)
			}
		} else if m.Stream == model.ClientStream {
			inFn := r.useCodec(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
			fmt.Fprintf(&r.body, "        const nativeRequests = (async function*() { for await (const request of requests) yield %s(request); })();\n", inFn)
			fmt.Fprintf(&r.body, "        const response = await handler.%s(nativeRequests);\n", methodName)
			if !m.OutputIsEmpty {
				outFn := r.useCodec(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
				fmt.Fprintf(&r.body, "        return %s;\n", r.providerOutputWire(m, outFn, "response"))
			} else {
				fmt.Fprintf(&r.body, "        return {};\n")
			}
		} else if m.Stream == model.Bidi {
			inFn := r.useCodec(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
			outFn := r.useCodec(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
			outType := r.nativeType(m.Output.FullName)
			fmt.Fprintf(&r.body, "        const values: %s[] = [];\n", outType)
			fmt.Fprintf(&r.body, "        const nativeRequests = (async function*() { for await (const request of requests) yield %s(request); })();\n", inFn)
			fmt.Fprintf(&r.body, "        const stream: BidiStream<%s, %s> = { [Symbol.asyncIterator]: () => nativeRequests[Symbol.asyncIterator](), send(value) { values.push(value); } };\n", r.nativeType(m.Input.FullName), outType)
			fmt.Fprintf(&r.body, "        await handler.%s(stream);\n", methodName)
			fmt.Fprintf(&r.body, "        for (const value of values) yield %s(value);\n", outFn)
		} else if m.InputIsEmpty {
			if m.OutputIsEmpty {
				fmt.Fprintf(&r.body, "        await handler.%s();\n", methodName)
				fmt.Fprintf(&r.body, "        return {};\n")
			} else {
				outFn := r.useCodec(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
				fmt.Fprintf(&r.body, "        const response = await handler.%s();\n", methodName)
				fmt.Fprintf(&r.body, "        return %s;\n", r.providerOutputWire(m, outFn, "response"))
			}
		} else {
			inFn := r.useCodec(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
			if m.OutputIsEmpty {
				if m.ProviderInput == model.ProviderInputClientSignature {
					fmt.Fprintf(&r.body, "        const nativeRequest = %s(request);\n        await handler.%s(%s);\n", inFn, methodName, r.providerFieldArgs(m, "nativeRequest"))
				} else {
					fmt.Fprintf(&r.body, "        await handler.%s(%s(request));\n", methodName, inFn)
				}
				fmt.Fprintf(&r.body, "        return {};\n")
			} else {
				outFn := r.useCodec(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
				if m.ProviderInput == model.ProviderInputClientSignature {
					fmt.Fprintf(&r.body, "        const nativeRequest = %s(request);\n        const response = await handler.%s(%s);\n", inFn, methodName, r.providerFieldArgs(m, "nativeRequest"))
				} else {
					fmt.Fprintf(&r.body, "        const response = await handler.%s(%s(request));\n", methodName, inFn)
				}
				fmt.Fprintf(&r.body, "        return %s;\n", r.providerOutputWire(m, outFn, "response"))
			}
		}

		fmt.Fprintf(&r.body, "      } catch (error) {\n")
		fmt.Fprintf(&r.body, "        throw statusError(%q, error);\n", op)
		fmt.Fprintf(&r.body, "      }\n")
		fmt.Fprintf(&r.body, "    },\n")
	}

	fmt.Fprintf(&r.body, "  };\n}\n\n")
}

// assemble builds the final TypeScript server module from the rendered body,
// prepending the import header derived from the accumulated features.
// The file lives at sdk/typescript/src/generated/providers/<base>.ts; paths are
// relative to that location.
func (r *serverRenderer) assemble() string {
	var b strings.Builder

	// Header comment.
	fmt.Fprintf(&b, "/**\n * Generated provider handler base and service adapter for %s.proto.\n *\n", r.base)
	fmt.Fprintf(&b, " * @module providers/%s\n */\n\n", r.base)

	// @connectrpc/connect: ConnectError, Code, ServiceImpl.
	if r.features.connectrpc {
		b.WriteString("import { ConnectError, Code, type ServiceImpl } from \"@connectrpc/connect\";\n")
	}

	// Wire stubs: ../gen/v1/<base>_pb.ts (one level up from internal/provider/).
	if r.features.wire {
		fmt.Fprintf(&b, "import * as wire from \"../../internal/gen/v1/%s_pb.ts\";\n", r.base)
	}

	// Codec converters: ../codec/<base>.ts.
	for _, base := range sortedKeys(flattenStringBoolMap(r.features.codecBases)) {
		names := r.features.codecBases[base]
		nameList := sortedKeys(names)
		fmt.Fprintf(&b, "import { %s } from \"../../internal/codec/%s.ts\";\n",
			strings.Join(nameList, ", "), base)
	}

	// Native types from public modules: ../../<base>.ts (two levels up to src/).
	for _, base := range sortedKeys(flattenStringBoolMap(r.features.nativeTypes)) {
		names := r.features.nativeTypes[base]
		typeSpecs := make([]string, 0, len(names))
		for _, name := range sortedKeys(names) {
			typeSpecs = append(typeSpecs, "type "+name)
		}
		fmt.Fprintf(&b, "import { %s } from \"../../%s.ts\";\n",
			strings.Join(typeSpecs, ", "), base)
	}

	// Gestalt error types: ../../rpc_support.ts.
	if r.features.gestalt {
		b.WriteString("import { GestaltError, GestaltErrorCode } from \"../../rpc_support.ts\";\n")
	}
	if r.features.providerBase {
		b.WriteString("import { ProviderBase } from \"../../provider-base.ts\";\n")
	}
	if r.features.jsonObject || r.features.jsonValue {
		var names []string
		if r.features.jsonObject {
			names = append(names, "JsonObject")
		}
		if r.features.jsonValue {
			names = append(names, "JsonValue")
		}
		fmt.Fprintf(&b, "import type { %s } from \"@bufbuild/protobuf\";\n", strings.Join(names, ", "))
	}
	if r.features.streams {
		b.WriteString("import { type BidiStream, type ClientStream, type ServerStream } from \"./support.ts\";\n")
	}

	// Inline the statusError helper (requires ConnectError, Code, GestaltError in scope).
	b.WriteString("\n")
	b.WriteString(serverSupportSnippet)
	b.WriteString("\n")
	b.WriteString(r.body.String())
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// flattenStringBoolMap returns the sorted keys of a map[string]map[string]bool.
func flattenStringBoolMap(m map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	for base := range m {
		out[base] = true
	}
	return out
}

// serverSupportFile is the provider-side error mapping, exported as a
// standalone module (support_server.ts) that providers may import directly.
// The same logic is inlined into each generated server file via
// serverSupportSnippet so individual files remain self-contained.
const serverSupportFile = `import { ConnectError, Code } from "@connectrpc/connect";
import { GestaltError } from "../../rpc_support.ts";

export type ServerStream<T> = { send(value: T): void };
export type ClientStream<T> = AsyncIterable<T>;
export type BidiStream<In, Out> = AsyncIterable<In> & { send(value: Out): void };

// statusError converts one handler error to the ConnectError returned to the
// host: GestaltError carries its code through, ConnectError passes through
// unchanged, and any other error is tagged Code.Unknown with the operation.
export function statusError(operation: string, error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  if (error instanceof GestaltError) {
    return new ConnectError(error.message, error.code as Code);
  }
  return new ConnectError(
    ` + "`" + `${operation}: ${error instanceof Error ? error.message : String(error)}` + "`" + `,
    Code.Unknown,
  );
}
`

// serverSupportSnippet is the inline statusError helper emitted into each
// provider server module.  ConnectError, Code (from @connectrpc/connect) and
// GestaltError (from rpc_support) must be in scope.
const serverSupportSnippet = `// statusError converts one handler error to a ConnectError for the host.
function statusError(operation: string, error: unknown): ConnectError {
  if (error instanceof ConnectError) {
    return error;
  }
  if (error instanceof GestaltError) {
    return new ConnectError(error.message, error.code as Code);
  }
  return new ConnectError(
    ` + "`" + `${operation}: ${error instanceof Error ? error.message : String(error)}` + "`" + `,
    Code.Unknown,
  );
}
`

func renderProviderFile(idx *index, base string, services []*model.Service) string {
	r := newServerRenderer(idx, base)
	for _, svc := range services {
		r.renderProviderHandler(svc)
		r.renderProviderService(svc)
	}
	return r.assemble()
}
