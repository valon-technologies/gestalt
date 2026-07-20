package rust

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// methodKeyword returns the fn keyword for async ("pub async fn") or sync ("pub fn").
func methodKeyword(sync bool) string {
	if sync {
		return "pub fn"
	}
	return "pub async fn"
}

// awaitSuffix returns ".await" for async methods or "" for sync methods.
func awaitSuffix(sync bool) string {
	if sync {
		return ""
	}
	return ".await"
}

// syncSuffix appends "_sync" to the method name for sync variants.
func syncSuffix(methodName string, sync bool) string {
	if sync {
		return methodName + "_sync"
	}
	return methodName
}

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
	fmt.Fprintf(&r.body, "pub struct %s<T: Send + Sync> {\n", clientName)
	r.body.WriteString("    transport: T,\n")
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl<T: Send + Sync> %s<T> {\n", clientName)
	fmt.Fprintf(&r.body, "    /// Creates a client over the given unary transport.\n")
	fmt.Fprintf(&r.body, "    pub fn new(transport: T) -> Self {\n")
	r.body.WriteString("        Self { transport }\n")
	r.body.WriteString("    }\n\n")

	r.body.WriteString("}\n\n")

	var restMethods []*model.Method
	var grpcOnlyMethods []*model.Method
	var streamingMethods []*model.Method
	for _, m := range svc.Methods {
		if m.Stream == model.ServerStream {
			streamingMethods = append(streamingMethods, m)
			continue
		}
		if m.Stream != model.Unary {
			continue
		}
		if m.HTTP != nil {
			restMethods = append(restMethods, m)
			continue
		}
		grpcOnlyMethods = append(grpcOnlyMethods, m)
	}
	if len(restMethods) > 0 {
		fmt.Fprintf(&r.body, "impl<T: UnaryTransport> %s<T> {\n", clientName)
		for _, m := range restMethods {
			r.renderAppClientMethod(svc, m, false)
		}
		r.body.WriteString("}\n\n")

		fmt.Fprintf(&r.body, "impl<T: crate::public::generated::unary_transport::SyncUnaryTransport> %s<T> {\n", clientName)
		for _, m := range restMethods {
			r.renderAppClientMethod(svc, m, true)
		}
		r.body.WriteString("}\n\n")
	}
	if len(grpcOnlyMethods) > 0 {
		fmt.Fprintf(&r.body, "impl<T: crate::public::generated::unary_transport::GrpcCapable> %s<T> {\n", clientName)
		for _, m := range grpcOnlyMethods {
			r.renderAppClientMethod(svc, m, false)
		}
		r.body.WriteString("}\n\n")

		fmt.Fprintf(&r.body, "impl<T: crate::public::generated::unary_transport::SyncGrpcCapable> %s<T> {\n", clientName)
		for _, m := range grpcOnlyMethods {
			r.renderAppClientMethod(svc, m, true)
		}
		r.body.WriteString("}\n\n")
	}
	if len(streamingMethods) > 0 {
		fmt.Fprintf(&r.body, "impl<T: crate::public::generated::unary_transport::ServerStreamingTransport> %s<T> {\n", clientName)
		for _, m := range streamingMethods {
			r.renderAppClientStreamMethod(svc, m, false)
		}
		r.body.WriteString("}\n\n")

		fmt.Fprintf(&r.body, "impl<T: crate::public::generated::unary_transport::SyncServerStreamingTransport> %s<T> {\n", clientName)
		for _, m := range streamingMethods {
			r.renderAppClientStreamMethod(svc, m, true)
		}
		r.body.WriteString("}\n\n")
		r.renderAppClientStreamTypes(streamingMethods)
	}
}

func (r *renderer) renderAppClientMethod(svc *model.Service, m *model.Method, sync bool) {
	constName := appClientMethodConst(svc, m)
	methodName := publicSnake(m.Name)

	if m.JsonResult != nil {
		r.renderAppClientRawMethod(m, constName, syncSuffix(methodName+"_raw", sync), sync)
		r.renderAppClientInvokeMethod(m, methodName, sync)
		return
	}

	if m.Name == "InvokeGraphQL" {
		r.renderAppClientRawMethod(m, constName, syncSuffix(methodName, sync), sync)
		r.renderAppClientGraphQLRawAlias(m, methodName, sync)
		r.renderAppClientGraphQLDecodedMethod(m, methodName+"_decoded", sync)
		return
	}

	r.renderAppClientRawMethod(m, constName, syncSuffix(methodName, sync), sync)
}

// renderAppClientStreamMethod renders a server-streaming public method that
// returns a Stream handle whose recv() yields decoded frames. Only the method
// is emitted here; the stream handle type is emitted at module scope by
// renderAppClientStreamTypes.
func (r *renderer) renderAppClientStreamMethod(svc *model.Service, m *model.Method, sync bool) {
	if m.InputIsEmpty || m.OutputIsEmpty || m.Input == nil || m.Output == nil {
		return
	}
	constName := appClientMethodConst(svc, m)
	methodName := publicSnake(m.Name)
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	streamType := localName(m.Output.FullName) + "Stream"
	toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	streamMethodName := methodName
	if sync {
		streamMethodName = syncSuffix(methodName, sync)
	}
	fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<%s, GestaltError> {\n",
		methodKeyword(sync), streamMethodName, requestType, streamType)
	fmt.Fprintf(&r.body, "        let wire = %s(request);\n", toWire)
	fmt.Fprintf(&r.body, "        let recv = self.transport.server_stream::<crate::generated::v1::%s, crate::generated::v1::%s>(&%s, &wire)%s?;\n",
		wireTypeName(m.Input.FullName), wireTypeName(m.Output.FullName), constName, awaitSuffix(sync))
	fmt.Fprintf(&r.body, "        Ok(%s { recv })\n", streamType)
	r.body.WriteString("    }\n")
}

// renderAppClientStreamTypes emits the module-scope stream handle types for
// the given streaming methods. Must be called outside any impl block.
func (r *renderer) renderAppClientStreamTypes(methods []*model.Method) {
	seen := map[string]bool{}
	for _, m := range methods {
		if m.InputIsEmpty || m.OutputIsEmpty || m.Input == nil || m.Output == nil {
			continue
		}
		streamType := localName(m.Output.FullName) + "Stream"
		if seen[streamType] {
			continue
		}
		seen[streamType] = true
		outputType := r.typeRef(m.Output.ProtoFile, localName(m.Output.FullName))
		fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "/// %s is the server-stream iterator for %s.\n", streamType, m.Name)
		fmt.Fprintf(&r.body, "pub struct %s {\n", streamType)
		fmt.Fprintf(&r.body, "    recv: Box<dyn crate::public::generated::unary_transport::ServerStreamRecv<crate::generated::v1::%s>>,\n", wireTypeName(m.Output.FullName))
		r.body.WriteString("}\n\n")
		fmt.Fprintf(&r.body, "impl %s {\n", streamType)
		fmt.Fprintf(&r.body, "    /// recv decodes the next %s frame; None ends the stream.\n", outputType)
		fmt.Fprintf(&r.body, "    pub async fn recv(&mut self) -> Result<Option<%s>, GestaltError> {\n", outputType)
		r.body.WriteString("        match self.recv.recv().await? {\n")
		fmt.Fprintf(&r.body, "            Some(wire) => Ok(Some(%s(wire))),\n", fromWire)
		r.body.WriteString("            None => Ok(None),\n")
		r.body.WriteString("        }\n")
		r.body.WriteString("    }\n")
		r.body.WriteString("}\n\n")
	}
}

func (r *renderer) renderAppClientRawMethod(m *model.Method, constName, methodName string, sync bool) {
	if m.InputIsEmpty || m.OutputIsEmpty {
		r.features.prostTypes = true
	}

	if m.InputIsEmpty && m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "    %s %s(&self) -> Result<(), GestaltError> {\n", methodKeyword(sync), methodName)
		r.body.WriteString("        let wire = Empty::default();\n")
		r.body.WriteString("        let mut wire_response = Empty::default();\n")
		fmt.Fprintf(&r.body, "        self.transport.unary(&%s, &wire, &mut wire_response)%s?;\n", constName, awaitSuffix(sync))
		r.body.WriteString("        Ok(())\n    }\n\n")
		return
	}
	if m.InputIsEmpty {
		outputType := r.typeRef(m.Output.ProtoFile, localName(m.Output.FullName))
		wireResponseType := wireTypeName(m.Output.FullName)
		fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))
		fmt.Fprintf(&r.body, "    %s %s(&self) -> Result<%s, GestaltError> {\n", methodKeyword(sync), methodName, outputType)
		r.body.WriteString("        let wire = Empty::default();\n")
		fmt.Fprintf(&r.body, "        let mut wire_response = crate::generated::v1::%s::default();\n", wireResponseType)
		fmt.Fprintf(&r.body, "        self.transport.unary(&%s, &wire, &mut wire_response)%s?;\n", constName, awaitSuffix(sync))
		fmt.Fprintf(&r.body, "        Ok(%s(wire_response))\n", fromWire)
		r.body.WriteString("    }\n\n")
		return
	}
	if m.OutputIsEmpty {
		requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
		toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
		fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<(), GestaltError> {\n", methodKeyword(sync), methodName, requestType)
		fmt.Fprintf(&r.body, "        let wire = %s(request);\n", toWire)
		r.body.WriteString("        let mut wire_response = Empty::default();\n")
		fmt.Fprintf(&r.body, "        self.transport.unary(&%s, &wire, &mut wire_response)%s?;\n", constName, awaitSuffix(sync))
		r.body.WriteString("        Ok(())\n    }\n\n")
		return
	}
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	outputType := r.typeRef(m.Output.ProtoFile, localName(m.Output.FullName))
	wireResponseType := wireTypeName(m.Output.FullName)
	toWire := r.convRef(m.Input.ProtoFile, toWireFunc(m.Input.FullName))
	fromWire := r.convRef(m.Output.ProtoFile, fromWireFunc(m.Output.FullName))

	fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<%s, GestaltError> {\n", methodKeyword(sync), methodName, requestType, outputType)
	fmt.Fprintf(&r.body, "        let wire = %s(request);\n", toWire)
	fmt.Fprintf(&r.body, "        let mut wire_response = crate::generated::v1::%s::default();\n", wireResponseType)
	fmt.Fprintf(&r.body, "        self.transport.unary(&%s, &wire, &mut wire_response)%s?;\n", constName, awaitSuffix(sync))
	fmt.Fprintf(&r.body, "        Ok(%s(wire_response))\n", fromWire)
	r.body.WriteString("    }\n\n")
}

func (r *renderer) renderAppClientInvokeMethod(m *model.Method, methodName string, sync bool) {
	if m.Input == nil || m.Output == nil || m.JsonResult == nil {
		return
	}
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	r.useInvoke("decode_app_result")
	r.useInvoke("InvokeError")

	fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<serde_json::Value, InvokeError> {\n", methodKeyword(sync), syncSuffix(methodName, sync), requestType)
	if f := findField(m.Input, "app"); f != nil {
		fmt.Fprintf(&r.body, "        let invoke_app = request.%s.clone();\n", escapeIdent(f.Name))
	}
	if f := findField(m.Input, "operation"); f != nil {
		fmt.Fprintf(&r.body, "        let invoke_operation = request.%s.clone();\n", escapeIdent(f.Name))
	}
	fmt.Fprintf(&r.body, "        let response = self.%s(request)%s?;\n", syncSuffix(methodName+"_raw", sync), awaitSuffix(sync))
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

func (r *renderer) renderAppClientGraphQLRawAlias(m *model.Method, methodName string, sync bool) {
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	outputType := r.typeRef(m.Output.ProtoFile, localName(m.Output.FullName))
	r.body.WriteString("    /// `invoke_graphql_raw` is an alias for [`Self::invoke_graphql`].\n")
	fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<%s, GestaltError> {\n",
		methodKeyword(sync), syncSuffix(methodName+"_raw", sync), requestType, outputType)
	fmt.Fprintf(&r.body, "        self.%s(request)%s\n", syncSuffix(methodName, sync), awaitSuffix(sync))
	r.body.WriteString("    }\n\n")
}

func (r *renderer) renderAppClientGraphQLDecodedMethod(m *model.Method, methodName string, sync bool) {
	if m.Input == nil || m.Output == nil {
		return
	}
	requestType := r.typeRef(m.Input.ProtoFile, localName(m.Input.FullName))
	r.useInvoke("decode_graphql_result")
	r.useInvoke("InvokeError")

	fmt.Fprintf(&r.body, "    %s %s(&self, request: %s) -> Result<serde_json::Value, InvokeError> {\n", methodKeyword(sync), syncSuffix(methodName, sync), requestType)
	appExpr := `""`
	if f := findField(m.Input, "app"); f != nil {
		fmt.Fprintf(&r.body, "        let invoke_app = request.%s.clone();\n", escapeIdent(f.Name))
		appExpr = "invoke_app.as_str()"
	}
	fmt.Fprintf(&r.body, "        let response = self.%s(request)%s?;\n", syncSuffix("invoke_graphql", sync), awaitSuffix(sync))
	status := findField(m.Output, "status")
	body := findField(m.Output, "body")
	fmt.Fprintf(
		&r.body,
		"        decode_graphql_result(%s, response.%s, &response.%s).map_err(InvokeError::from)\n",
		appExpr,
		escapeIdent(status.Name),
		escapeIdent(body.Name),
	)
	r.body.WriteString("    }\n\n")
}

func appClientMethodConst(svc *model.Service, m *model.Method) string {
	wireName := localName(svc.FullName)
	return fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), methodConstSuffix(m.Name))
}
