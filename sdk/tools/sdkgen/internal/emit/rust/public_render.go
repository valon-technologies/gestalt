package rust

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func (r *renderer) assembleGenerated() string {
	return r.assemble()
}

func (r *renderer) renderAppClient(svc *model.Service) {
	wireName := localName(svc.FullName)
	clientName := wireName + "Client"
	r.features.unaryTransport = true
	r.features.restMetadata = true
	r.useType("GestaltError")

	r.body.WriteString("/// Transport-neutral client for the public gestaltd App surface.\n")
	fmt.Fprintf(&r.body, "pub struct %s<T: UnaryTransport> {\n", clientName)
	r.body.WriteString("    transport: T,\n")
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl<T: UnaryTransport> %s<T> {\n", clientName)
	fmt.Fprintf(&r.body, "    /// Creates a client over the given unary transport.\n")
	fmt.Fprintf(&r.body, "    pub fn new(transport: T) -> Self {\n")
	r.body.WriteString("        Self { transport }\n")
	r.body.WriteString("    }\n\n")

	for _, m := range svc.Methods {
		if m.Stream != model.Unary {
			continue
		}
		r.renderAppClientMethod(svc, m)
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderAppClientMethod(svc *model.Service, m *model.Method) {
	constName := appClientMethodConst(svc, m)
	methodName := publicSnake(m.Name)

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
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	outputType := r.typeRef(m.Output.ProtoFile, localName(m.Output.FullName))
	wireResponseType := wireTypeName(m.Output.FullName)
	toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))

	fmt.Fprintf(&r.body, "    pub async fn %s(&self, request: %s) -> Result<%s, GestaltError> {\n", methodName, requestType, outputType)
	fmt.Fprintf(&r.body, "        let wire = %s(request);\n", toWire)
	fmt.Fprintf(&r.body, "        let mut wire_response = crate::generated::v1::%s::default();\n", wireResponseType)
	fmt.Fprintf(&r.body, "        self.transport.unary(&%s, &wire, &mut wire_response).await?;\n", constName)
	fmt.Fprintf(&r.body, "        Ok(%s(wire_response))\n", fromWire)
	r.body.WriteString("    }\n\n")
}

func (r *renderer) renderAppClientInvokeMethod(m *model.Method, methodName string) {
	if m.Input == nil || m.Output == nil || m.JsonResult == nil {
		return
	}
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	r.useInvoke("decode_app_result")
	r.useInvoke("InvokeError")

	fmt.Fprintf(&r.body, "    pub async fn %s(&self, request: %s) -> Result<serde_json::Value, InvokeError> {\n", methodName, requestType)
	if f := findField(m.Input, "app"); f != nil {
		fmt.Fprintf(&r.body, "        let invoke_app = request.%s.clone();\n", escapeIdent(f.Name))
	}
	if f := findField(m.Input, "operation"); f != nil {
		fmt.Fprintf(&r.body, "        let invoke_operation = request.%s.clone();\n", escapeIdent(f.Name))
	}
	fmt.Fprintf(&r.body, "        let response = self.%s_raw(request).await?;\n", methodName)
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	appExpr, opExpr := `""`, `""`
	if findField(m.Input, "app") != nil {
		appExpr = "invoke_app.as_str()"
	}
	if findField(m.Input, "operation") != nil {
		opExpr = "invoke_operation.as_str()"
	}
	fmt.Fprintf(
		&r.body,
		"        decode_app_result(%s, %s, response.%s, &response.%s).map_err(InvokeError::from)\n",
		appExpr,
		opExpr,
		escapeIdent(status.Name),
		escapeIdent(body.Name),
	)
	r.body.WriteString("    }\n\n")
}

func appClientMethodConst(svc *model.Service, m *model.Method) string {
	wireName := localName(svc.FullName)
	return fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), methodConstSuffix(m.Name))
}
