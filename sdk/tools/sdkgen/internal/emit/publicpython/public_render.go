package publicpython

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

const restCallerFile = `"""REST transport callback for generated public clients."""

from __future__ import annotations

from typing import Protocol

from .metadata import Method


class RestCaller(Protocol):
    def call_unary(self, method: Method, request, response_type): ...
`

func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) renderGRPCClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName + "GRPC"
	r.features.grpc = true
	r.features.wireGrpc = true

	fmt.Fprintf(&r.body, "class %s:\n", name)
	doc := fmt.Sprintf("gRPC client for the public %s surface.", svc.FullName)
	if svc.Doc != "" {
		doc = svc.Doc + "\n\n" + doc
	}
	r.writeDocstring("    ", doc)
	r.body.WriteString("\n")
	r.body.WriteString("    def __init__(self, channel: grpc.Channel, *, timeout: float | None = None) -> None:\n")
	r.body.WriteString("        self._channel = channel\n")
	fmt.Fprintf(&r.body, "        self._stub = %s.%sStub(channel)\n", r.wireGrpcModule(), wireName)
	r.body.WriteString("        self._timeout = timeout\n\n")

	for _, m := range svc.Methods {
		if m.Stream != model.Unary {
			continue
		}
		r.renderMethod(m)
	}
	r.body.WriteString("\n")
}

func (r *renderer) renderRESTClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName + "REST"
	r.features.restCaller = true
	fmt.Fprintf(&r.body, "class %s:\n", name)
	r.writeDocstring("    ", fmt.Sprintf("REST client for the public %s surface.", svc.FullName))
	r.body.WriteString("\n")
	r.body.WriteString("    def __init__(self, transport: RestCaller) -> None:\n")
	r.body.WriteString("        self._transport = transport\n\n")

	for _, m := range svc.Methods {
		if m.HTTP == nil || m.Stream != model.Unary {
			continue
		}
		r.renderRESTMethod(wireName, m)
	}
}

func (r *renderer) renderRESTMethod(wireName string, m *model.Method) {
	constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(m.Name))
	r.useMetadataMethod(constName)
	methodName := pyName(snakeCase(m.Name))
	collapse := r.collapseOutput(m)

	params := ""
	requestArg := "None"
	if !m.InputIsEmpty {
		requestType := r.messageType(m.Input.FullName)
		params = fmt.Sprintf(", request: %s", requestType)
		requestArg = r.codecRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName)) + "(request)"
	}

	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "    def %s(self%s) -> None:\n", methodName, params)
		r.writeMethodDoc(m)
		fmt.Fprintf(&r.body, "        self._transport.call_unary(%s, %s, None)\n\n", constName, requestArg)
	case collapse != nil:
		r.features.wire = true
		wireResponseType := r.wireModule() + "." + localName(m.Output.FullName)
		fromWire := r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, params, collapse.returnType)
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
		fmt.Fprintf(&r.body, "        wire = self._transport.call_unary(%s, %s, %s)\n", constName, requestArg, wireResponseType)
		fmt.Fprintf(&r.body, "        response = %s(wire)\n", fromWire)
		for _, line := range collapse.lines {
			r.body.WriteString(line + "\n")
		}
		r.body.WriteString("\n")
	default:
		r.features.wire = true
		wireResponseType := r.wireModule() + "." + localName(m.Output.FullName)
		fromWire := r.codecRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		outputType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "    def %s(self%s) -> %s:\n", methodName, params, outputType)
		r.writeMethodDoc(m)
		fmt.Fprintf(&r.body, "        wire = self._transport.call_unary(%s, %s, %s)\n", constName, requestArg, wireResponseType)
		fmt.Fprintf(&r.body, "        return %s(wire)\n\n", fromWire)
	}
}
