package publicrust

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) renderGRPCClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName + "GRPC"
	clientModule := wireClientModule(svc)
	clientType := wireClientType(svc)
	r.features.v1 = true
	r.useType("GestaltError")

	r.body.WriteString("/// gRPC client for the public gestaltd surface.\n")
	fmt.Fprintf(&r.body, "pub struct %s {\n", name)
	fmt.Fprintf(&r.body, "    inner: v1::%s::%s<crate::public::grpc_auth::AuthChannel>,\n", clientModule, clientType)
	r.body.WriteString("    timeout: Option<std::time::Duration>,\n")
	if contextFieldOf(svc) != nil {
		r.body.WriteString("    request_context: Option<v1::RequestContext>,\n")
	}
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl %s {\n", name)
	fmt.Fprintf(&r.body, "    /// Creates a client over an injected tonic channel.\n")
	fmt.Fprintf(&r.body, "    pub fn new(channel: tonic::transport::Channel, auth: std::sync::Arc<dyn crate::public::auth::Auth>) -> Self {\n")
	fmt.Fprintf(&r.body, "        Self::with_request_context(channel, auth, None)\n")
	fmt.Fprintf(&r.body, "    }\n\n")
	if contextFieldOf(svc) != nil {
		fmt.Fprintf(&r.body, "    /// Creates a client that injects request context into outgoing RPCs.\n")
		fmt.Fprintf(&r.body, "    pub fn with_request_context(channel: tonic::transport::Channel, auth: std::sync::Arc<dyn crate::public::auth::Auth>, request_context: Option<v1::RequestContext>) -> Self {\n")
		fmt.Fprintf(&r.body, "        Self {\n")
		fmt.Fprintf(&r.body, "            inner: v1::%s::%s::with_interceptor(channel, crate::public::grpc_auth::AuthInterceptor::new(auth)),\n", clientModule, clientType)
		r.body.WriteString("            timeout: None,\n")
		r.body.WriteString("            request_context,\n")
		r.body.WriteString("        }\n")
		r.body.WriteString("    }\n\n")
	} else {
		fmt.Fprintf(&r.body, "    fn with_request_context(channel: tonic::transport::Channel, auth: std::sync::Arc<dyn crate::public::auth::Auth>, _request_context: Option<v1::RequestContext>) -> Self {\n")
		fmt.Fprintf(&r.body, "        Self {\n")
		fmt.Fprintf(&r.body, "            inner: v1::%s::%s::with_interceptor(channel, crate::public::grpc_auth::AuthInterceptor::new(auth)),\n", clientModule, clientType)
		r.body.WriteString("            timeout: None,\n")
		r.body.WriteString("        }\n")
		r.body.WriteString("    }\n\n")
	}
	r.body.WriteString("    /// Sets the default per-request timeout.\n")
	r.body.WriteString("    pub fn with_timeout(mut self, timeout: std::time::Duration) -> Self {\n")
	r.body.WriteString("        self.timeout = Some(timeout);\n")
	r.body.WriteString("        self\n")
	r.body.WriteString("    }\n\n")

	for _, method := range svc.Methods {
		if method.Stream != model.Unary {
			continue
		}
		r.renderPublicGRPCMethod(svc, method)
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderRESTClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	name := wireName + "REST"
	r.useType("GestaltError")
	r.features.restMetadata = true
	fmt.Fprintf(&r.body, "pub struct %s {\n", name)
	r.body.WriteString("    transport: std::sync::Arc<crate::public::rest_transport::RestTransport>,\n")
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "impl %s {\n", name)
	fmt.Fprintf(&r.body, "    pub fn new(transport: std::sync::Arc<crate::public::rest_transport::RestTransport>) -> Self {\n")
	r.body.WriteString("        Self { transport }\n")
	r.body.WriteString("    }\n\n")

	for _, method := range svc.Methods {
		if method.HTTP == nil || method.Stream != model.Unary {
			continue
		}
		r.renderPublicRESTMethod(wireName, method)
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderPublicGRPCMethod(svc *model.Service, m *model.Method) {
	wireMethod := escapeIdent(heckSnake(m.Name))
	methodName := publicSnake(m.Name)
	collapse := r.collapseOutput(m)
	params, requestArg, prep := r.publicMethodRequest(m)
	recv := fmt.Sprintf("    pub async fn %s(&mut self%s)", methodName, params)

	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "%s -> Result<(), GestaltError> {\n", recv)
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		requestExpr := r.timeoutPrep(requestArg)
		fmt.Fprintf(&r.body, "        self.inner.%s(%s).await.map_err(GestaltError::from)?;\n", wireMethod, requestExpr)
		r.body.WriteString("        Ok(())\n")
		r.body.WriteString("    }\n\n")
	case collapse != nil:
		fmt.Fprintf(&r.body, "%s -> Result<%s, %s> {\n", recv, collapse.returnType, collapseErrorType(collapse))
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		for _, line := range collapse.grpcPrep {
			r.body.WriteString(line + "\n")
		}
		requestExpr := r.timeoutPrep(requestArg)
		fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "        let wire = self.inner.%s(%s).await.map_err(GestaltError::from)?.into_inner();\n", wireMethod, requestExpr)
		fmt.Fprintf(&r.body, "        let response = %s(wire);\n", fromWire)
		for _, line := range collapse.lines {
			if m.JsonResult != nil {
				status := findField(m.Output, m.JsonResult.Status)
				body := findField(m.Output, m.JsonResult.Body)
				fmt.Fprintf(&r.body, "        crate::public::generated::invoke_support::decode_app_result(invoke_context_app.as_str(), invoke_context_operation.as_str(), response.%s, &response.%s).map_err(crate::public::generated::invoke_support::InvokeError::from)\n",
					escapeIdent(status.Name), escapeIdent(body.Name))
				continue
			}
			r.body.WriteString("        " + line + "\n")
		}
		r.body.WriteString("    }\n\n")
	default:
		returnType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "%s -> Result<%s, GestaltError> {\n", recv, returnType)
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		requestExpr := r.timeoutPrep(requestArg)
		fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "        let response = self.inner.%s(%s).await.map_err(GestaltError::from)?.into_inner();\n", wireMethod, requestExpr)
		fmt.Fprintf(&r.body, "        Ok(%s(response))\n", fromWire)
		r.body.WriteString("    }\n\n")
	}
	_ = svc
}

func (r *renderer) renderPublicRESTMethod(wireName string, m *model.Method) {
	constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(m.Name))
	methodName := publicSnake(m.Name)
	collapse := r.collapseOutput(m)
	params, requestArg, prep := r.publicMethodRequestREST(m)
	recv := fmt.Sprintf("    pub fn %s(&self%s)", methodName, params)

	switch {
	case m.OutputIsEmpty:
		fmt.Fprintf(&r.body, "%s -> Result<(), GestaltError> {\n", recv)
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		for _, line := range r.restRequestJSONPrep(m, requestArg) {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "        let mut response_json = serde_json::Value::Null;\n")
		fmt.Fprintf(&r.body, "        RestCaller::call_unary(self.transport.as_ref(), &%s, request_json, &mut response_json)?;\n", constName)
		r.body.WriteString("        Ok(())\n")
		r.body.WriteString("    }\n\n")
	case collapse != nil:
		fmt.Fprintf(&r.body, "%s -> Result<%s, %s> {\n", recv, collapse.returnType, collapseErrorType(collapse))
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		if m.JsonResult != nil && m.Input != nil {
			if f := findField(m.Input, "app"); f != nil {
				fmt.Fprintf(&r.body, "        let invoke_app = request.%s.clone();\n", escapeIdent(f.Name))
			}
			if f := findField(m.Input, "operation"); f != nil {
				fmt.Fprintf(&r.body, "        let invoke_operation = request.%s.clone();\n", escapeIdent(f.Name))
			}
		}
		for _, line := range r.restRequestJSONPrep(m, requestArg) {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "        let mut response_json = serde_json::Value::Null;\n")
		fmt.Fprintf(&r.body, "        RestCaller::call_unary(self.transport.as_ref(), &%s, request_json, &mut response_json)?;\n", constName)
		for _, line := range r.restResponseDecodeLines(m.Output.FullName, "response") {
			r.body.WriteString(line + "\n")
		}
		for _, line := range collapse.lines {
			if m.JsonResult != nil {
				status := findField(m.Output, m.JsonResult.Status)
				body := findField(m.Output, m.JsonResult.Body)
				appExpr, opExpr := `""`, `""`
				if m.Input != nil {
					if f := findField(m.Input, "app"); f != nil {
						appExpr = "invoke_app.as_str()"
					}
					if f := findField(m.Input, "operation"); f != nil {
						opExpr = "invoke_operation.as_str()"
					}
				}
				fmt.Fprintf(&r.body, "        crate::public::generated::invoke_support::decode_app_result(%s, %s, response.%s, &response.%s).map_err(crate::public::generated::invoke_support::InvokeError::from)\n",
					appExpr, opExpr, escapeIdent(status.Name), escapeIdent(body.Name))
				continue
			}
			r.body.WriteString("        " + line + "\n")
		}
		r.body.WriteString("    }\n\n")
	default:
		returnType := r.messageType(m.Output.FullName)
		fmt.Fprintf(&r.body, "%s -> Result<%s, GestaltError> {\n", recv, returnType)
		for _, line := range prep {
			r.body.WriteString(line + "\n")
		}
		for _, line := range r.restRequestJSONPrep(m, requestArg) {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "        let mut response_json = serde_json::Value::Null;\n")
		fmt.Fprintf(&r.body, "        RestCaller::call_unary(self.transport.as_ref(), &%s, request_json, &mut response_json)?;\n", constName)
		for _, line := range r.restResponseDecodeLines(m.Output.FullName, "response") {
			r.body.WriteString(line + "\n")
		}
		fmt.Fprintf(&r.body, "        Ok(response)\n")
		r.body.WriteString("    }\n\n")
	}
}

func (r *renderer) publicMethodRequest(m *model.Method) (param, arg string, prep []string) {
	if m.InputIsEmpty {
		return "", "()", nil
	}
	requestType := r.messageType(m.Input.FullName)
	toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	if findField(m.Input, "context") != nil {
		prep = []string{
			"        let mut wire_request = " + toWire + "(request);",
			"        if wire_request.context.is_none() {",
			"            wire_request.context = self.request_context.clone();",
			"        }",
		}
		return ", request: " + requestType, "wire_request", prep
	}
	return ", request: " + requestType, toWire + "(request)", nil
}

func (r *renderer) publicMethodRequestREST(m *model.Method) (param, arg string, prep []string) {
	if m.InputIsEmpty {
		return "", "", nil
	}
	requestType := r.messageType(m.Input.FullName)
	return ", request: " + requestType, "request", nil
}

func collapseErrorType(collapse *collapsed) string {
	if collapse == nil || collapse.errorType == "" {
		return "GestaltError"
	}
	return collapse.errorType
}

func (r *renderer) restRequestJSONPrep(m *model.Method, requestArg string) []string {
	if m.InputIsEmpty {
		return []string{"        let request_json = serde_json::Value::Object(serde_json::Map::new());"}
	}
	toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	encode := r.wireJSONEncodeRef(m.Input.ProtoFile, m.Input.FullName)
	return []string{
		fmt.Sprintf("        let wire_request = %s(%s);", toWire, requestArg),
		fmt.Sprintf("        let request_json = %s(&wire_request);", encode),
	}
}

func (r *renderer) restResponseDecodeLines(outputFullName, nativeVar string) []string {
	msg := r.idx.messages[outputFullName]
	protoFile := ""
	if msg != nil {
		protoFile = msg.ProtoFile
	}
	decode := r.wireJSONDecodeRef(protoFile, outputFullName)
	fromWire := r.convRef(protoFile, fromWireFunc(outputFullName))
	return []string{
		fmt.Sprintf("        let wire_response = %s(&response_json)?;", decode),
		fmt.Sprintf("        let %s = %s(wire_response);", nativeVar, fromWire),
	}
}

func screamingSnake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return strings.ToUpper(b.String())
}
