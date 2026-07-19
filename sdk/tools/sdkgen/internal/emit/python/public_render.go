package python

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

const unaryTransportFile = `"""Transport-neutral unary call contract for generated public clients."""

from __future__ import annotations

from typing import Protocol, TypeVar

from google.protobuf.message import Message

from .metadata import Method

ResponseT = TypeVar("ResponseT", bound=Message)


class UnaryTransport(Protocol):
    def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT: ...


class AsyncUnaryTransport(Protocol):
    async def unary(
        self,
        method: Method,
        request: Message,
        response_type: type[ResponseT],
    ) -> ResponseT: ...
`

// defKeyword returns the method prefix for async ("async def") or sync ("def").
func defKeyword(isAsync bool) string {
	if isAsync {
		return "async def"
	}
	return "def"
}

// awaitPrefix returns "await " for async transport calls or "" for sync.
func awaitPrefix(isAsync bool) string {
	if isAsync {
		return "await "
	}
	return ""
}

// asyncClientName returns the async sibling class name ("Async" + base) or the
// sync class name unchanged.
func asyncClientName(base string, isAsync bool) string {
	if isAsync {
		return "Async" + base
	}
	return base
}

func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) renderAppClient(svc *model.Service) {
	// Render the sync client (UnaryTransport) then the async client
	// (AsyncUnaryTransport) as sibling classes in the same file. The two
	// share method bodies; only the def keyword and await prefix differ.
	r.renderAppClientFlavor(svc, false)
	r.renderAppClientFlavor(svc, true)
}

func (r *renderer) renderAppClientFlavor(svc *model.Service, isAsync bool) {
	r.features.unaryTransport = true
	r.features.wire = true
	r.features.asyncTransport = r.features.asyncTransport || isAsync
	clientName := asyncClientName(localName(svc.FullName)+"Client", isAsync)
	fmt.Fprintf(&r.body, "class %s:\n", clientName)
	doc := fmt.Sprintf("Transport-neutral client for the public %s surface.", svc.FullName)
	if isAsync {
		doc = fmt.Sprintf("Async client for the public %s surface; methods are coroutines.", svc.FullName)
	}
	if svc.Doc != "" {
		doc = svc.Doc + "\n\n" + doc
	}
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	transportType := "UnaryTransport"
	if isAsync {
		transportType = "AsyncUnaryTransport"
	}
	fmt.Fprintf(&r.body, "    def __init__(self, transport: %s) -> None:\n", transportType)
	r.body.WriteString("        self._transport = transport\n\n")

	for _, m := range svc.Methods {
		if m.Stream != model.Unary {
			continue
		}
		r.renderAppClientMethod(svc, m, isAsync)
	}
	r.renderAppRESTClientProtocol(svc, isAsync)
}

func (r *renderer) renderAppRESTClientProtocol(svc *model.Service, isAsync bool) {
	clientName := asyncClientName(localName(svc.FullName)+"Client", isAsync)
	protocolName := clientName + "REST"
	var restMethods []*model.Method
	for _, m := range svc.Methods {
		if m.HTTP != nil && m.Stream == model.Unary {
			restMethods = append(restMethods, m)
		}
	}
	if len(restMethods) == 0 {
		return
	}
	r.features.protocol = true
	fmt.Fprintf(&r.body, "class %s(Protocol):\n", protocolName)
	r.writeDocstring("    ", fmt.Sprintf("REST-backed methods for the public %s surface.", svc.FullName))
	for _, m := range restMethods {
		r.renderAppRESTProtocolMethod(m, isAsync)
	}
	r.body.WriteString("\n")
}

func (r *renderer) renderAppClientMethod(svc *model.Service, m *model.Method, isAsync bool) {
	constName := appClientMethodConst(svc, m)
	methodName := pyName(snakeCase(m.Name))

	if m.JsonResult != nil {
		r.renderAppClientRawMethod(m, constName, methodName+"_raw", isAsync)
		r.renderAppClientInvokeMethod(m, methodName, isAsync)
		return
	}

	if m.Name == "InvokeGraphQL" {
		r.renderAppClientRawMethod(m, constName, methodName, isAsync)
		r.renderAppClientGraphQLRawAlias(m, methodName, isAsync)
		r.renderAppClientGraphQLDecodedMethod(m, methodName+"_decoded", isAsync)
		return
	}

	r.renderAppClientRawMethod(m, constName, methodName, isAsync)
}

func (r *renderer) renderAppClientRawMethod(m *model.Method, constName, methodName string, isAsync bool) {
	r.useMetadataMethod(constName)
	r.features.wire = true
	if m.InputIsEmpty || m.OutputIsEmpty {
		r.features.emptyPb = true
	}
	kw := defKeyword(isAsync)
	await := awaitPrefix(isAsync)

	if m.InputIsEmpty && m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "    %s %s(self) -> None:\n", kw, methodName)
		r.writeMethodDoc(m)
		r.body.WriteString("        wire = _empty.Empty()\n")
		r.body.WriteString("        " + await + "self._transport.unary(\n")
		fmt.Fprintf(&r.body, "            %s,\n", constName)
		r.body.WriteString("            wire,\n")
		r.body.WriteString("            _empty.Empty,\n")
		r.body.WriteString("        )\n\n")
		return
	}
	if m.InputIsEmpty {
		outputType := r.messageType(m.Output.FullName)
		wireOutputType := r.wireModule() + "." + localName(m.Output.FullName)
		fromWire := r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "    %s %s(self) -> %s:\n", kw, methodName, outputType)
		r.writeMethodDoc(m)
		r.body.WriteString("        wire = _empty.Empty()\n")
		r.body.WriteString("        wire_response = " + await + "self._transport.unary(\n")
		fmt.Fprintf(&r.body, "            %s,\n", constName)
		r.body.WriteString("            wire,\n")
		fmt.Fprintf(&r.body, "            %s,\n", wireOutputType)
		r.body.WriteString("        )\n")
		fmt.Fprintf(&r.body, "        return %s(wire_response)\n\n", fromWire)
		return
	}
	if m.OutputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		toWire := r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
		fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> None:\n", kw, methodName, requestType)
		r.writeMethodDoc(m)
		fmt.Fprintf(&r.body, "        wire = %s(request)\n", toWire)
		r.body.WriteString("        " + await + "self._transport.unary(\n")
		fmt.Fprintf(&r.body, "            %s,\n", constName)
		r.body.WriteString("            wire,\n")
		r.body.WriteString("            _empty.Empty,\n")
		r.body.WriteString("        )\n\n")
		return
	}
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	outputType := r.messageType(m.Output.FullName)
	wireOutputType := r.wireModule() + "." + localName(m.Output.FullName)
	toWire := r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	fromWire := r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))

	fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> %s:\n", kw, methodName, requestType, outputType)
	r.writeMethodDoc(m)
	fmt.Fprintf(&r.body, "        wire = %s(request)\n", toWire)
	fmt.Fprintf(&r.body, "        wire_response = %sself._transport.unary(\n", await)
	fmt.Fprintf(&r.body, "            %s,\n", constName)
	fmt.Fprintf(&r.body, "            wire,\n")
	fmt.Fprintf(&r.body, "            %s,\n", wireOutputType)
	fmt.Fprintf(&r.body, "        )\n")
	fmt.Fprintf(&r.body, "        return %s(wire_response)\n\n", fromWire)
}

func (r *renderer) renderAppClientInvokeMethod(m *model.Method, methodName string, isAsync bool) {
	if m.Input == nil || m.Output == nil || m.JsonResult == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	r.features.anyType = true
	r.useInvoke("decode_app_result")
	kw := defKeyword(isAsync)
	await := awaitPrefix(isAsync)

	fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> Any:\n", kw, methodName, requestType)
	doc := m.Doc
	if doc == "" {
		doc = "The result decodes with the standard JSON operation envelope semantics; envelope failures raise InvokeError."
	} else {
		doc += "\n\nThe result decodes with the standard JSON operation envelope semantics; envelope failures raise InvokeError."
	}
	r.writeDocstring("        ", doc)
	fmt.Fprintf(&r.body, "        response = %sself.%s_raw(request)\n", await, methodName)
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	fmt.Fprintf(
		&r.body,
		"        return decode_app_result(%s, %s, response.%s, response.%s)\n\n",
		jsonResultContext(m, "app"),
		jsonResultContext(m, "operation"),
		pyName(status.Name),
		pyName(body.Name),
	)
}

func (r *renderer) renderAppClientGraphQLRawAlias(m *model.Method, methodName string, isAsync bool) {
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	outputType := r.messageType(m.Output.FullName)
	fmt.Fprintf(&r.body, "    %s %s_raw(self, request: %s) -> %s:\n", defKeyword(isAsync), methodName, requestType, outputType)
	r.body.WriteString("        \"\"\"Alias for invoke_graphql.\"\"\"\n")
	fmt.Fprintf(&r.body, "        return "+awaitPrefix(isAsync)+"self.%s(request)\n\n", methodName)
}

func (r *renderer) renderAppClientGraphQLDecodedMethod(m *model.Method, methodName string, isAsync bool) {
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	r.features.anyType = true
	r.useInvoke("decode_graphql_result")
	kw := defKeyword(isAsync)
	await := awaitPrefix(isAsync)

	fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> Any:\n", kw, methodName, requestType)
	doc := m.Doc
	if doc == "" {
		doc = "The result decodes with GraphQL envelope semantics; envelope failures raise InvokeError."
	} else {
		doc += "\n\nThe result decodes with GraphQL envelope semantics; envelope failures raise InvokeError."
	}
	r.writeDocstring("        ", doc)
	fmt.Fprintf(&r.body, "        response = %sself.invoke_graphql(request)\n", await)
	status := findField(m.Output, "status")
	body := findField(m.Output, "body")
	fmt.Fprintf(
		&r.body,
		"        return decode_graphql_result(%s, response.%s, response.%s)\n\n",
		jsonResultContext(m, "app"),
		pyName(status.Name),
		pyName(body.Name),
	)
}

func (r *renderer) renderAppRESTProtocolMethod(m *model.Method, isAsync bool) {
	if m.JsonResult != nil {
		r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name))+"_raw", false, isAsync)
		r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name)), true, isAsync)
		return
	}
	if m.Name == "InvokeGraphQL" {
		r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name)), false, isAsync)
		r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name))+"_raw", false, isAsync)
		r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name))+"_decoded", true, isAsync)
		return
	}
	r.renderAppRESTProtocolSignature(m, pyName(snakeCase(m.Name)), false, isAsync)
}

func (r *renderer) renderAppRESTProtocolSignature(m *model.Method, methodName string, decoded bool, isAsync bool) {
	kw := defKeyword(isAsync)
	if m.InputIsEmpty && m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "    %s %s(self) -> None: ...\n", kw, methodName)
		return
	}
	if decoded {
		requestType := r.messageType(m.Input.FullName)
		r.features.anyType = true
		fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> Any: ...\n", kw, methodName, requestType)
		return
	}
	if m.InputIsEmpty {
		outputType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "    %s %s(self) -> %s: ...\n", kw, methodName, outputType)
		return
	}
	if m.OutputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> None: ...\n", kw, methodName, requestType)
		return
	}
	requestType := r.messageType(m.Input.FullName)
	if m.JsonResult != nil && methodName == pyName(snakeCase(m.Name)) {
		r.features.anyType = true
		fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> Any: ...\n", kw, methodName, requestType)
		return
	}
	outputType := r.messageType(m.Output.FullName)
	fmt.Fprintf(&r.body, "    %s %s(self, request: %s) -> %s: ...\n", kw, methodName, requestType, outputType)
}

func appClientMethodConst(svc *model.Service, m *model.Method) string {
	wireName := localName(svc.FullName)
	return fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(m.Name))
}

func (r *renderer) renderMetadata(methods []publicsurface.PublicMethod) {
	r.features.dataclass = true
	r.body.WriteString("@dataclass(frozen=True, slots=True)\n")
	r.body.WriteString("class Method:\n")
	r.body.WriteString("    service: str\n")
	r.body.WriteString("    name: str\n")
	r.body.WriteString("    full_method: str\n")
	r.body.WriteString("    http_verb: str = \"\"\n")
	r.body.WriteString("    http_path: str = \"\"\n")
	r.body.WriteString("    http_body: str = \"\"\n")
	r.body.WriteString("    http_path_fields: tuple[PublicField, ...] = ()\n")
	r.body.WriteString("    http_query_fields: tuple[PublicField, ...] = ()\n")
	r.body.WriteString("    fill: tuple[str, ...] = ()\n")
	r.body.WriteString("    reject: tuple[str, ...] = ()\n\n")
	r.body.WriteString("@dataclass(frozen=True, slots=True)\n")
	r.body.WriteString("class PublicField:\n")
	r.body.WriteString("    name: str\n")
	r.body.WriteString("    json_name: str\n\n")

	for _, pm := range methods {
		wireName := localName(pm.Service)
		constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(pm.Method))
		fmt.Fprintf(&r.body, "%s = Method(\n", constName)
		fmt.Fprintf(&r.body, "    service=%q,\n", pm.Service)
		fmt.Fprintf(&r.body, "    name=%q,\n", pm.Method)
		fmt.Fprintf(&r.body, "    full_method=%q,\n", pm.FullMethod)
		if pm.REST != nil {
			fmt.Fprintf(&r.body, "    http_verb=%q,\n", pm.REST.Verb)
			fmt.Fprintf(&r.body, "    http_path=%q,\n", pm.REST.PathTemplate)
			body := ""
			if pm.REST.Body == publicsurface.BodyStar {
				body = "*"
			}
			fmt.Fprintf(&r.body, "    http_body=%q,\n", body)
			fmt.Fprintf(&r.body, "    http_path_fields=%s,\n", pythonPublicFieldTuple(pm.REST.PathFields))
			fmt.Fprintf(&r.body, "    http_query_fields=%s,\n", pythonPublicFieldTuple(pm.REST.QueryFields))
		}
		fmt.Fprintf(&r.body, "    fill=%s,\n", pythonStringTuple(publicsurface.FieldNames(pm.ServerFilled)))
		fmt.Fprintf(&r.body, "    reject=%s,\n", pythonStringTuple(publicsurface.FieldNames(pm.Rejected)))
		r.body.WriteString(")\n\n")
	}
}

func pythonPublicFieldTuple(fields []publicsurface.PublicField) string {
	if len(fields) == 0 {
		return "()"
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("PublicField(name=%q, json_name=%q)", f.Name, f.JSONName)
	}
	return "(" + strings.Join(parts, ", ") + ",)"
}
