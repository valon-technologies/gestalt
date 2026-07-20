package golang

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func (r *renderer) renderUnaryTransport() {
	r.body.WriteString("// UnaryTransport performs one unary public RPC. Implementations live in the\n")
	r.body.WriteString("// handwritten publicclient transport layer.\n")
	r.body.WriteString("type UnaryTransport interface {\n")
	r.body.WriteString("\tUnary(ctx context.Context, method Method, request, response gproto.Message) error\n")
	r.body.WriteString("\t// ServerStream invokes a server-streaming RPC. It returns a RecvCloser\n")
	r.body.WriteString("\t// whose Recv method decodes one frame at a time; io.EOF ends the stream.\n")
	r.body.WriteString("\tServerStream(ctx context.Context, method Method, request gproto.Message) (ServerStreamRecvCloser, error)\n")
	r.body.WriteString("}\n")
	r.body.WriteString("\n")
	r.body.WriteString("// ServerStreamRecvCloser is a streaming frame iterator returned by ServerStream.\n")
	r.body.WriteString("type ServerStreamRecvCloser interface {\n")
	r.body.WriteString("\tRecv(msg gproto.Message) error\n")
	r.body.WriteString("\tClose() error\n")
	r.body.WriteString("}\n")
}

func (r *renderer) assembleUnaryTransport() string {
	return `package generated

import (
	"context"

	gproto "google.golang.org/protobuf/proto"
)

` + r.body.String()
}

func (r *renderer) renderServiceClient(svc *model.Service) {
	if len(svc.Methods) > 0 {
		r.features.context = true
		r.features.proto = true
	}
	clientName := localName(svc.FullName) + "Client"
	r.body.WriteString("// " + clientName + " is the transport-neutral client for the public " + svc.Name + " surface.\n")
	fmt.Fprintf(&r.body, "type %s struct {\n", clientName)
	r.body.WriteString("\ttransport UnaryTransport\n")
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "// New%s creates a %s over the given transport.\n", clientName, clientName)
	fmt.Fprintf(&r.body, "func New%s(transport UnaryTransport) *%s {\n", clientName, clientName)
	fmt.Fprintf(&r.body, "\treturn &%s{transport: transport}\n", clientName)
	r.body.WriteString("}\n\n")

	wireName := localName(svc.FullName)
	for _, m := range svc.Methods {
		r.renderServiceClientMethod(clientName, wireName, m)
	}
	r.renderServiceRESTClientInterface(svc, clientName)
}

func (r *renderer) renderServiceClientMethod(clientName, wireName string, m *model.Method) {
	constName := fmt.Sprintf("Method%s%s", wireName, m.Name)

	if m.Stream == model.ServerStream {
		r.renderServiceClientStreamMethod(clientName, m, constName)
		return
	}

	if m.JsonResult != nil {
		r.renderServiceClientUnaryMethod(clientName, m, constName, m.Name+"Raw")
		r.renderServiceClientDecodedMethod(clientName, m)
		return
	}

	if m.Name == "InvokeGraphQL" {
		r.renderServiceClientUnaryMethod(clientName, m, constName, m.Name)
		r.renderInvokeGraphQLRawAlias(clientName, m)
		r.renderInvokeGraphQLDecodedMethod(clientName, m)
		return
	}

	r.renderServiceClientUnaryMethod(clientName, m, constName, m.Name)
}

func (r *renderer) renderServiceClientMethodSignature(clientName string, m *model.Method, methodName string) {
	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context", clientName, methodName)
	if !m.InputIsEmpty {
		fmt.Fprintf(&r.body, ", request *%s", r.messageType(m.Input.FullName))
	}
	if m.OutputIsEmpty {
		fmt.Fprintf(&r.body, ") error")
		return
	}
	fmt.Fprintf(&r.body, ") (*%s, error)", r.messageType(m.Output.FullName))
}

func (r *renderer) renderServiceClientUnaryMethod(
	clientName string,
	m *model.Method,
	constName, methodName string,
) {
	r.renderServiceClientMethodSignature(clientName, m, methodName)
	r.body.WriteString(" {\n")
	if m.InputIsEmpty || m.OutputIsEmpty {
		r.features.emptypb = true
	}
	if !m.InputIsEmpty {
		fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWireFunc(r.messageType(m.Input.FullName)))
	} else {
		r.body.WriteString("\twire := &emptypb.Empty{}\n")
	}
	if m.OutputIsEmpty {
		fmt.Fprintf(&r.body, "\tout := &emptypb.Empty{}\n")
		fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, out); err != nil {\n", constName)
		r.body.WriteString("\t\treturn toGestaltError(err)\n\t}\n")
		r.body.WriteString("\treturn nil\n}\n\n")
		return
	}
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, out); err != nil {\n", constName)
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn %s(out), nil\n}\n\n", fromWireFunc(r.messageType(m.Output.FullName)))
}

func (r *renderer) renderServiceClientDecodedMethod(clientName string, m *model.Method) {
	if m.InputIsEmpty || m.OutputIsEmpty || m.JsonResult == nil {
		return
	}
	r.features.gestaltclient = true
	requestType := r.messageType(m.Input.FullName)
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	appExpr := jsonResultContext(m, "app")
	opExpr := jsonResultContext(m, "operation")

	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, request *%s) (any, error) {\n",
		clientName, m.Name, requestType)
	fmt.Fprintf(&r.body, "\tout, err := c.%sRaw(ctx, request)\n", m.Name)
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn gestaltclient.DecodeAppResult(%s, %s, out.%s, out.%s)\n",
		appExpr, opExpr, fieldGoName(status), fieldGoName(body))
	r.body.WriteString("}\n\n")
}

// renderServiceClientStreamMethod renders a server-streaming public method
// that returns a frame iterator backed by the transport's ServerStream.
func (r *renderer) renderServiceClientStreamMethod(clientName string, m *model.Method, constName string) {
	if m.InputIsEmpty || m.OutputIsEmpty {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	outputType := r.messageType(m.Output.FullName)
	r.features.io = true
	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, request *%s) (*%sStream, error) {\n",
		clientName, m.Name, requestType, m.Name)
	fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWireFunc(requestType))
	fmt.Fprintf(&r.body, "\trecv, err := c.transport.ServerStream(ctx, %s, wire)\n", constName)
	r.body.WriteString("\tif err != nil {\n")
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n")
	r.body.WriteString("\t}\n")
	fmt.Fprintf(&r.body, "\treturn &%sStream{recv: recv}, nil\n", m.Name)
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "// %sStream is the server-stream iterator for %s.\n", m.Name, m.Name)
	fmt.Fprintf(&r.body, "type %sStream struct {\n", m.Name)
	r.body.WriteString("\trecv ServerStreamRecvCloser\n")
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "// Recv decodes the next %s frame. It returns io.EOF when the stream is exhausted.\n", outputType)
	fmt.Fprintf(&r.body, "func (s *%sStream) Recv() (*%s, error) {\n", m.Name, outputType)
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	r.body.WriteString("\tif err := s.recv.Recv(out); err != nil {\n")
	r.body.WriteString("\t\tif err == io.EOF {\n")
	r.body.WriteString("\t\t\treturn nil, err\n")
	r.body.WriteString("\t\t}\n")
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n")
	r.body.WriteString("\t}\n")
	fmt.Fprintf(&r.body, "\treturn %s(out), nil\n", fromWireFunc(outputType))
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "// Close releases the underlying stream. It is safe to call after Recv returns io.EOF.\n")
	fmt.Fprintf(&r.body, "func (s *%sStream) Close() error {\n", m.Name)
	r.body.WriteString("\treturn s.recv.Close()\n")
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderInvokeGraphQLRawAlias(clientName string, m *model.Method) {
	if m.InputIsEmpty || m.OutputIsEmpty {
		return
	}
	requestType := r.messageType(m.Input.FullName)
	outputType := r.messageType(m.Output.FullName)
	r.body.WriteString("// InvokeGraphQLRaw is an alias for InvokeGraphQL.\n")
	fmt.Fprintf(&r.body, "func (c *%s) InvokeGraphQLRaw(ctx context.Context, request *%s) (*%s, error) {\n",
		clientName, requestType, outputType)
	r.body.WriteString("\treturn c.InvokeGraphQL(ctx, request)\n}\n\n")
}

func (r *renderer) renderInvokeGraphQLDecodedMethod(clientName string, m *model.Method) {
	if m.InputIsEmpty || m.OutputIsEmpty {
		return
	}
	r.features.gestaltclient = true
	requestType := r.messageType(m.Input.FullName)
	status := findField(m.Output, "status")
	body := findField(m.Output, "body")
	appExpr := jsonResultContext(m, "app")

	fmt.Fprintf(&r.body, "func (c *%s) InvokeGraphQLDecoded(ctx context.Context, request *%s) (any, error) {\n",
		clientName, requestType)
	r.body.WriteString("\tout, err := c.InvokeGraphQL(ctx, request)\n")
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn gestaltclient.DecodeGraphQLResult(%s, out.%s, out.%s)\n",
		appExpr, fieldGoName(status), fieldGoName(body))
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderServiceRESTClientInterface(svc *model.Service, clientName string) {
	ifaceName := clientName + "REST"
	var restMethods []*model.Method
	for _, m := range svc.Methods {
		if m.HTTP != nil {
			restMethods = append(restMethods, m)
		}
	}
	if len(restMethods) == 0 {
		return
	}
	fmt.Fprintf(&r.body, "// %s exposes only REST-backed methods for %s.\n", ifaceName, svc.Name)
	fmt.Fprintf(&r.body, "type %s interface {\n", ifaceName)
	for _, m := range restMethods {
		r.renderRESTInterfaceMethods(m, ifaceName)
	}
	r.body.WriteString("}\n\n")
	fmt.Fprintf(&r.body, "var _ %s = (*%s)(nil)\n\n", ifaceName, clientName)
}

func (r *renderer) renderRESTInterfaceMethods(m *model.Method, ifaceName string) {
	_ = ifaceName
	if m.JsonResult != nil {
		r.writeRESTInterfaceMethodSignature(m, m.Name+"Raw")
		r.writeRESTInterfaceMethodSignatureDecoded(m, m.Name)
		return
	}
	if m.Name == "InvokeGraphQL" {
		r.writeRESTInterfaceMethodSignature(m, m.Name)
		r.writeRESTInterfaceMethodSignature(m, m.Name+"Raw")
		r.writeRESTInterfaceMethodSignatureDecoded(m, m.Name+"Decoded")
		return
	}
	r.writeRESTInterfaceMethodSignature(m, m.Name)
}

func (r *renderer) writeRESTInterfaceMethodSignature(m *model.Method, methodName string) {
	fmt.Fprintf(&r.body, "\t%s(ctx context.Context", methodName)
	if !m.InputIsEmpty {
		fmt.Fprintf(&r.body, ", request *%s", r.messageType(m.Input.FullName))
	}
	if m.OutputIsEmpty {
		r.body.WriteString(") error\n")
		return
	}
	if methodName == m.Name+"Decoded" || (m.JsonResult != nil && methodName == m.Name) {
		r.body.WriteString(") (any, error)\n")
		return
	}
	fmt.Fprintf(&r.body, ") (*%s, error)\n", r.messageType(m.Output.FullName))
}

func (r *renderer) writeRESTInterfaceMethodSignatureDecoded(m *model.Method, methodName string) {
	fmt.Fprintf(&r.body, "\t%s(ctx context.Context", methodName)
	if !m.InputIsEmpty {
		fmt.Fprintf(&r.body, ", request *%s", r.messageType(m.Input.FullName))
	}
	r.body.WriteString(") (any, error)\n")
}

func (r *renderer) assembleServiceClient() string {
	return r.assemble()
}

func (r *renderer) renderMetadata(methods []publicsurface.PublicMethod) {
	r.body.WriteString("// Method metadata for the public gestaltd surface.\n\n")
	r.body.WriteString("// Method describes one public unary RPC.\n")
	r.body.WriteString("type Method struct {\n")
	r.body.WriteString("\tService string\n")
	r.body.WriteString("\tName string\n")
	r.body.WriteString("\tFullMethod string\n")
	r.body.WriteString("\tHTTPVerb string\n")
	r.body.WriteString("\tHTTPPath string\n")
	r.body.WriteString("\tHTTPBody string\n")
	r.body.WriteString("\tHTTPPathFields []PublicField\n")
	r.body.WriteString("\tHTTPQueryFields []PublicField\n")
	r.body.WriteString("\tFill []string\n")
	r.body.WriteString("\tReject []string\n")
	r.body.WriteString("\tStream bool\n")
	r.body.WriteString("}\n\n")
	r.body.WriteString("// PublicField names one request field used by REST metadata.\n")
	r.body.WriteString("type PublicField struct {\n")
	r.body.WriteString("\tName string\n")
	r.body.WriteString("\tJSONName string\n")
	r.body.WriteString("}\n\n")

	for _, pm := range methods {
		wireName := localName(pm.Service)
		constName := fmt.Sprintf("Method%s%s", wireName, pm.Method)
		r.body.WriteString("var " + constName + " = Method{\n")
		fmt.Fprintf(&r.body, "\tService: %q,\n", pm.Service)
		fmt.Fprintf(&r.body, "\tName: %q,\n", pm.Method)
		fmt.Fprintf(&r.body, "\tFullMethod: %q,\n", pm.FullMethod)
		if pm.REST != nil {
			fmt.Fprintf(&r.body, "\tHTTPVerb: %q,\n", pm.REST.Verb)
			fmt.Fprintf(&r.body, "\tHTTPPath: %q,\n", pm.REST.PathTemplate)
			body := ""
			if pm.REST.Body == publicsurface.BodyStar {
				body = "*"
			}
			fmt.Fprintf(&r.body, "\tHTTPBody: %q,\n", body)
			fmt.Fprintf(&r.body, "\tHTTPPathFields: %s,\n", goPublicFieldSlice(pm.REST.PathFields))
			fmt.Fprintf(&r.body, "\tHTTPQueryFields: %s,\n", goPublicFieldSlice(pm.REST.QueryFields))
		}
		fmt.Fprintf(&r.body, "\tStream: %v,\n", pm.Stream == model.ServerStream)
		r.body.WriteString("\tFill: " + goStringList(publicsurface.FieldNames(pm.ServerFilled)) + ",\n")
		r.body.WriteString("\tReject: " + goStringList(publicsurface.FieldNames(pm.Rejected)) + ",\n")
		r.body.WriteString("}\n\n")
	}
}

func goPublicFieldSlice(fields []publicsurface.PublicField) string {
	if len(fields) == 0 {
		return "nil"
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("PublicField{Name: %q, JSONName: %q}", f.Name, f.JSONName)
	}
	return "[]PublicField{" + strings.Join(parts, ", ") + "}"
}
