package python

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// providerName renders the handler ABC name for a provider service: the
// service name with a Handler suffix. A service already named *Provider takes
// a Handler suffix directly — its generated client owns the bare name (service
// CacheProvider: client CacheProvider, handler CacheProviderHandler). A
// service not named *Provider also takes a Handler suffix (service Cache:
// handler CacheHandler).
func providerName(svcName string) string {
	return svcName + "Handler"
}

// operationString renders the human-readable operation tag carried on handler
// errors: the lowercased service name followed by the method name split on
// word boundaries ("workflow apply definition").
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

// serverRenderer holds state for rendering one provider service's handler and
// dispatch adapter to a standalone Python module. It is separate from the
// public/codec renderer so the server files stay independent.
type serverRenderer struct {
	idx  *index
	base string // generated file base (e.g. "cache")
	body strings.Builder

	// tracked imports
	needsABC       bool
	needsAbsMethod bool
	needsGrpc      bool
	needsAny       bool
	needsEmptyPb   bool
	codecBase      string // codec module alias for this file's converters
	wireGrpcBase   string // pb2_grpc module alias
}

func newServerRenderer(idx *index, base string) *serverRenderer {
	return &serverRenderer{idx: idx, base: base}
}

// handlerParams renders the abstract method signature for a handler method.
// Handlers are transport-shaped: the full native request in, the full native
// response out. Empty input takes no request parameter; empty output returns
// None. Types are qualified with the native module alias (_native).
func (r *serverRenderer) handlerParams(m *model.Method) (params, retType string) {
	if !m.InputIsEmpty {
		reqType := "_native." + localName(m.Input.FullName)
		params = "request: " + reqType
	}
	if m.OutputIsEmpty {
		retType = "None"
	} else {
		retType = "_native." + localName(m.Output.FullName)
	}
	return params, retType
}

// renderProviderHandler renders the native handler ABC and its Unimplemented
// default mixin for one provider service.
func (r *serverRenderer) renderProviderHandler(svc *model.Service) {
	svcName := localName(svc.FullName)
	name := providerName(svcName)
	r.needsABC = true
	r.needsAbsMethod = true

	// Handler ABC
	doc := fmt.Sprintf(
		"%s is the handler interface implemented by providers serving\nthe %s service. Methods receive the full native request;\nwire conversion and error mapping live in the generated dispatch servicer.",
		name, svc.FullName,
	)
	if svc.Doc != "" {
		doc = svc.Doc + "\n\n" + doc
	}
	fmt.Fprintf(&r.body, "class %s(ABC):\n", name)
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	for _, m := range svc.Methods {
		params, retType := r.handlerParams(m)
		r.body.WriteString("    @abstractmethod\n")
		if params != "" {
			fmt.Fprintf(&r.body, "    def %s(self, %s) -> %s:\n        ...\n\n", snakeCase(m.Name), params, retType)
		} else {
			fmt.Fprintf(&r.body, "    def %s(self) -> %s:\n        ...\n\n", snakeCase(m.Name), retType)
		}
	}
	r.body.WriteString("\n")

	// Unimplemented mixin
	fmt.Fprintf(&r.body, "class Unimplemented%s(%s):\n", name, name)
	fmt.Fprintf(&r.body,
		"    \"\"\"Unimplemented%s fails every %s method with a\n    GestaltErrorCode.UNIMPLEMENTED error; subclass it to default the\n    methods a provider does not implement.\"\"\"\n\n",
		name, name)
	for _, m := range svc.Methods {
		params, retType := r.handlerParams(m)
		op := operationString(svcName, m.Name)
		if params != "" {
			fmt.Fprintf(&r.body, "    def %s(self, %s) -> %s:\n", snakeCase(m.Name), params, retType)
		} else {
			fmt.Fprintf(&r.body, "    def %s(self) -> %s:\n", snakeCase(m.Name), retType)
		}
		fmt.Fprintf(&r.body, "        raise GestaltError(GestaltErrorCode.UNIMPLEMENTED, %q)\n\n", op+" is not implemented")
	}
	r.body.WriteString("\n")
}

// renderProviderServer renders the wire dispatch servicer for one provider
// service: a subclass of the grpc-generated <Svc>Servicer that converts wire
// requests to native via the existing _codec.from_wire_*, calls the handler,
// converts native responses back via _codec.to_wire_*, and maps handler errors
// to gRPC status via status_error.
func (r *serverRenderer) renderProviderServer(svc *model.Service) {
	svcName := localName(svc.FullName)
	name := providerName(svcName)
	adapterClass := "_" + name + "Servicer"
	wireGrpcMod := "_" + r.base + "_pb2_grpc"
	codecMod := "_codec"

	r.needsGrpc = true
	r.wireGrpcBase = r.base
	r.codecBase = r.base

	// Factory function
	fmt.Fprintf(&r.body,
		"def new_%s_server(handler: %s) -> %s.%sServicer:\n",
		snakeCase(name), name, wireGrpcMod, svcName,
	)
	fmt.Fprintf(&r.body,
		"    \"\"\"Adapt handler to the wire-level %s.%sServicer: requests convert\n    from the wire, responses convert to the wire, and handler errors map to\n    gRPC statuses via status_error.\"\"\"\n",
		wireGrpcMod, svcName,
	)
	fmt.Fprintf(&r.body, "    return %s(handler)\n\n\n", adapterClass)

	// Adapter class
	fmt.Fprintf(&r.body, "class %s(%s.%sServicer):\n", adapterClass, wireGrpcMod, svcName)
	fmt.Fprintf(&r.body, "    def __init__(self, handler: %s) -> None:\n        self._handler = handler\n\n", name)

	for _, m := range svc.Methods {
		op := operationString(svcName, m.Name)
		r.renderDispatchMethod(m, op, codecMod)
	}
}

// renderDispatchMethod renders one dispatch method of the adapter servicer.
func (r *serverRenderer) renderDispatchMethod(m *model.Method, op, codecMod string) {
	methodName := m.Name // gRPC servicer methods use PascalCase matching proto
	r.needsAny = true
	if m.InputIsEmpty {
		fmt.Fprintf(&r.body, "    def %s(self, _request: Any, context: grpc.ServicerContext) -> Any:\n", methodName)
	} else {
		fmt.Fprintf(&r.body, "    def %s(self, request: Any, context: grpc.ServicerContext) -> Any:\n", methodName)
	}

	// Convert wire request to native
	if m.InputIsEmpty {
		if m.OutputIsEmpty {
			r.needsEmptyPb = true
			fmt.Fprintf(&r.body, "        try:\n            self._handler.%s()\n        except Exception as err:\n            status_error(context, %q, err)\n            return\n        return _empty_pb2.Empty()\n\n", snakeCase(m.Name), op)
		} else {
			outFunc := toWireFunc(m.Output.FullName)
			fmt.Fprintf(&r.body, "        try:\n            response = self._handler.%s()\n        except Exception as err:\n            return status_error(context, %q, err)\n        return %s.%s(response)\n\n", snakeCase(m.Name), op, codecMod, outFunc)
		}
	} else {
		inFunc := fromWireFunc(m.Input.FullName)
		if m.OutputIsEmpty {
			r.needsEmptyPb = true
			fmt.Fprintf(&r.body, "        native_req = %s.%s(request)\n        try:\n            self._handler.%s(native_req)\n        except Exception as err:\n            status_error(context, %q, err)\n            return\n        return _empty_pb2.Empty()\n\n", codecMod, inFunc, snakeCase(m.Name), op)
		} else {
			outFunc := toWireFunc(m.Output.FullName)
			fmt.Fprintf(&r.body, "        native_req = %s.%s(request)\n        try:\n            response = self._handler.%s(native_req)\n        except Exception as err:\n            return status_error(context, %q, err)\n        return %s.%s(response)\n\n", codecMod, inFunc, snakeCase(m.Name), op, codecMod, outFunc)
		}
	}
}

// writeDocstring renders doc as a docstring at the given indent.
func (r *serverRenderer) writeDocstring(indent, doc string) {
	lines := strings.Split(docstringText(doc), "\n")
	if len(lines) == 1 {
		text := lines[0]
		if strings.HasSuffix(text, `"`) {
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

// assembleServer assembles the final module source, prepending the module
// docstring, imports, and the body rendered by renderProviderHandler +
// renderProviderServer.
func (r *serverRenderer) assembleServer(base string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"Generated provider handler and dispatch servicer for %s.proto.\"\"\"\n\n", base)
	b.WriteString("from __future__ import annotations\n")

	var stdlib []string
	if r.needsABC || r.needsAbsMethod {
		stdlib = append(stdlib, fromImport("abc", []string{"ABC", "abstractmethod"}))
	}
	if len(stdlib) > 0 {
		b.WriteString("\n")
		for _, line := range stdlib {
			b.WriteString(line)
		}
	}

	var thirdParty []string
	if r.needsGrpc {
		thirdParty = append(thirdParty, "import grpc\n")
	}
	if r.needsAny {
		thirdParty = append(thirdParty, "from typing import Any\n")
	}
	if r.needsEmptyPb {
		thirdParty = append(thirdParty, "from google.protobuf import empty_pb2 as _empty_pb2\n")
	}
	if len(thirdParty) > 0 {
		b.WriteString("\n")
		for _, line := range thirdParty {
			b.WriteString(line)
		}
	}

	// Local imports: native module, codec, pb2_grpc, rpc_support, support.
	// The native module is imported as _native so type annotations in the ABC
	// and Unimplemented class resolve; from __future__ import annotations makes
	// them lazy, but the import is still needed for static analysis.
	b.WriteString("\n")
	if base != "" {
		fmt.Fprintf(&b, "from .. import %s as _native\n", base)
	}
	if r.codecBase != "" {
		fmt.Fprintf(&b, "from .._codec import %s as _codec\n", r.codecBase)
	}
	if r.wireGrpcBase != "" {
		fmt.Fprintf(&b, "from .._gen.v1 import %s_pb2_grpc as _%s_pb2_grpc\n", r.wireGrpcBase, r.wireGrpcBase)
	}
	b.WriteString("from ..rpc_support import GestaltError, GestaltErrorCode\n")
	b.WriteString("from .support import status_error\n")

	b.WriteString("\n\n")
	b.WriteString(r.body.String())
	return strings.TrimRight(b.String(), "\n") + "\n"
}
