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
`

func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) renderAppClient(svc *model.Service) {
	r.features.unaryTransport = true
	r.features.wire = true
	fmt.Fprintf(&r.body, "class %sClient:\n", localName(svc.FullName))
	doc := fmt.Sprintf("Transport-neutral client for the public %s surface.", svc.FullName)
	if svc.Doc != "" {
		doc = svc.Doc + "\n\n" + doc
	}
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	r.body.WriteString("    def __init__(self, transport: UnaryTransport) -> None:\n")
	r.body.WriteString("        self._transport = transport\n\n")

	for _, m := range svc.Methods {
		if m.Stream != model.Unary {
			continue
		}
		r.renderAppClientMethod(svc, m)
	}
}

func (r *renderer) renderAppClientMethod(svc *model.Service, m *model.Method) {
	constName := appClientMethodConst(svc, m)
	methodName := pyName(snakeCase(m.Name))

	if m.JsonResult != nil {
		r.renderAppClientRawMethod(m, constName, methodName+"_raw")
		r.renderAppClientInvokeMethod(m, methodName)
		return
	}
	r.renderAppClientRawMethod(m, constName, methodName)
}

func (r *renderer) renderAppClientRawMethod(m *model.Method, constName, methodName string) {
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	outputType := r.messageType(m.Output.FullName)
	wireOutputType := r.wireModule() + "." + localName(m.Output.FullName)
	toWire := r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	fromWire := r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
	r.useMetadataMethod(constName)

	fmt.Fprintf(&r.body, "    def %s(self, request: %s) -> %s:\n", methodName, requestType, outputType)
	r.writeMethodDoc(m)
	fmt.Fprintf(&r.body, "        wire = %s(request)\n", toWire)
	fmt.Fprintf(&r.body, "        wire_response = self._transport.unary(\n")
	fmt.Fprintf(&r.body, "            %s,\n", constName)
	fmt.Fprintf(&r.body, "            wire,\n")
	fmt.Fprintf(&r.body, "            %s,\n", wireOutputType)
	fmt.Fprintf(&r.body, "        )\n")
	fmt.Fprintf(&r.body, "        return %s(wire_response)\n\n", fromWire)
}

func (r *renderer) renderAppClientInvokeMethod(m *model.Method, methodName string) {
	if m.Input == nil || m.Output == nil || m.JsonResult == nil {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	r.features.anyType = true
	r.useInvoke("decode_app_result")

	fmt.Fprintf(&r.body, "    def %s(self, request: %s) -> Any:\n", methodName, requestType)
	doc := m.Doc
	if doc == "" {
		doc = "The result decodes with the standard JSON operation envelope semantics; envelope failures raise InvokeError."
	} else {
		doc += "\n\nThe result decodes with the standard JSON operation envelope semantics; envelope failures raise InvokeError."
	}
	r.writeDocstring("        ", doc)
	fmt.Fprintf(&r.body, "        response = self.%s_raw(request)\n", methodName)
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
	r.body.WriteString("    reject: tuple[str, ...] = ()\n")
	r.body.WriteString("    response_is_operation_result: bool = False\n\n")
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
		fmt.Fprintf(&r.body, "    response_is_operation_result=%s,\n", pythonBool(publicsurface.ResponseIsOperationResult(pm)))
		r.body.WriteString(")\n\n")
	}
}

func pythonBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
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
