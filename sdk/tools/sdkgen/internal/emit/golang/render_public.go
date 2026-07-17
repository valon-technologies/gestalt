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
	clientName := svc.Name + "Client"
	r.features.context = true
	if needsEmptyPB(svc) {
		r.features.emptypb = true
	}
	fmt.Fprintf(&r.body, "// %s is the transport-neutral client for the public %s surface.\n", clientName, svc.FullName)
	fmt.Fprintf(&r.body, "type %s struct {\n\ttransport UnaryTransport\n}\n\n", clientName)
	fmt.Fprintf(&r.body, "// New%s creates a %s over the given transport.\n", clientName, clientName)
	fmt.Fprintf(&r.body, "func New%s(transport UnaryTransport) *%s {\n", clientName, clientName)
	fmt.Fprintf(&r.body, "\treturn &%s{transport: transport}\n}\n\n", clientName)

	wireName := localName(svc.FullName)
	for _, m := range svc.Methods {
		r.renderServiceClientMethod(clientName, wireName, m)
	}
}

func needsEmptyPB(svc *model.Service) bool {
	for _, m := range svc.Methods {
		if m.InputIsEmpty || m.OutputIsEmpty {
			return true
		}
	}
	return false
}

func (r *renderer) renderServiceClientMethod(clientName, wireName string, m *model.Method) {
	constName := fmt.Sprintf("Method%s%s", wireName, m.Name)
	collapse := r.collapseOutput(m)

	if m.JsonResult != nil && m.Input != nil && m.Output != nil {
		requestType := r.messageType(m.Input.FullName)
		responseType := r.messageType(m.Output.FullName)
		r.renderServiceClientUnaryMethod(clientName, m, constName, requestType, responseType, m.Name+"Raw")
		r.renderServiceClientDecodedMethod(clientName, m, requestType)
		return
	}

	if m.OutputIsEmpty {
		r.renderServiceClientEmptyOutput(clientName, m, constName)
		return
	}
	if m.InputIsEmpty {
		r.renderServiceClientEmptyInput(clientName, m, constName, collapse)
		return
	}
	if m.Input == nil || m.Output == nil {
		return
	}

	requestType := r.messageType(m.Input.FullName)
	responseType := r.messageType(m.Output.FullName)
	if collapse != nil {
		r.renderServiceClientCollapsedMethod(clientName, m, constName, requestType, collapse)
		return
	}
	r.renderServiceClientUnaryMethod(clientName, m, constName, requestType, responseType, m.Name)
}

func (r *renderer) renderServiceClientEmptyOutput(clientName string, m *model.Method, constName string) {
	recv := fmt.Sprintf("func (c *%s) %s", clientName, m.Name)
	if m.InputIsEmpty {
		fmt.Fprintf(&r.body, "%s(ctx context.Context) error {\n", recv)
		fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, &emptypb.Empty{}, &emptypb.Empty{}); err != nil {\n", constName)
	} else if m.Input != nil {
		requestType := r.messageType(m.Input.FullName)
		fmt.Fprintf(&r.body, "%s(ctx context.Context, request *%s) error {\n", recv, requestType)
		fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWireFunc(requestType))
		fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, &emptypb.Empty{}); err != nil {\n", constName)
	}
	r.body.WriteString("\t\treturn toGestaltError(err)\n\t}\n\treturn nil\n}\n\n")
}

func (r *renderer) renderServiceClientEmptyInput(clientName string, m *model.Method, constName string, collapse *collapsed) {
	if m.Output == nil {
		return
	}
	responseType := r.messageType(m.Output.FullName)
	fromWire := fromWireFunc(responseType)
	recv := fmt.Sprintf("func (c *%s) %s", clientName, m.Name)
	if collapse != nil {
		fmt.Fprintf(&r.body, "%s(ctx context.Context) (%s, error) {\n", recv, strings.Join(collapse.types, ", "))
		fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
		fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, &emptypb.Empty{}, out); err != nil {\n", constName)
		r.body.WriteString("\t\treturn " + strings.Join(collapse.zero, ", ") + ", toGestaltError(err)\n\t}\n")
		fmt.Fprintf(&r.body, "\tresponse := out\n")
		for _, line := range collapse.lines {
			r.body.WriteString("\t" + line + "\n")
		}
		r.body.WriteString("}\n\n")
		return
	}
	fmt.Fprintf(&r.body, "%s(ctx context.Context) (*%s, error) {\n", recv, responseType)
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, &emptypb.Empty{}, out); err != nil {\n", constName)
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn %s(out), nil\n}\n\n", fromWire)
}

func (r *renderer) renderServiceClientCollapsedMethod(clientName string, m *model.Method, constName, requestType string, collapse *collapsed) {
	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, request *%s) (%s, error) {\n",
		clientName, m.Name, requestType, strings.Join(collapse.types, ", "))
	fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWireFunc(requestType))
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, out); err != nil {\n", constName)
	r.body.WriteString("\t\treturn " + strings.Join(collapse.zero, ", ") + ", toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\tresponse := out\n")
	for _, line := range collapse.lines {
		r.body.WriteString("\t" + line + "\n")
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) renderServiceClientUnaryMethod(
	clientName string,
	m *model.Method,
	constName, requestType, responseType, methodName string,
) {
	toWire := toWireFunc(requestType)
	fromWire := fromWireFunc(responseType)

	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, request *%s) (*%s, error) {\n",
		clientName, methodName, requestType, responseType)
	fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWire)
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, out); err != nil {\n", constName)
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn %s(out), nil\n}\n\n", fromWire)
}

func (r *renderer) renderServiceClientDecodedMethod(clientName string, m *model.Method, requestType string) {
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	appExpr := jsonResultContext(m, "app")

	fmt.Fprintf(&r.body, "func (c *%s) %s(ctx context.Context, request *%s) (any, error) {\n",
		clientName, m.Name, requestType)
	fmt.Fprintf(&r.body, "\tout, err := c.%sRaw(ctx, request)\n", m.Name)
	r.body.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	if m.Name == "InvokeGraphQL" {
		fmt.Fprintf(&r.body, "\treturn gestaltclient.DecodeGraphQLResult(%s, out.%s, out.%s)\n",
			appExpr, fieldGoName(status), fieldGoName(body))
	} else {
		opExpr := jsonResultContext(m, "operation")
		fmt.Fprintf(&r.body, "\treturn gestaltclient.DecodeAppResult(%s, %s, out.%s, out.%s)\n",
			appExpr, opExpr, fieldGoName(status), fieldGoName(body))
	}
	r.body.WriteString("}\n\n")
}

func (r *renderer) assembleServiceClient() string {
	imports := "import (\n\t\"context\"\n\n"
	if strings.Contains(r.body.String(), "gestaltclient.") {
		imports += "\tgestaltclient \"github.com/valon-technologies/gestalt/sdk/go/client\"\n"
	}
	if strings.Contains(r.body.String(), "emptypb.") {
		imports += "\t\"google.golang.org/protobuf/types/known/emptypb\"\n"
	}
	imports += "\t" + wireImport + "\n)\n\n"
	return fmt.Sprintf("package generated\n\n%s%s", imports, r.body.String())
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
	r.body.WriteString("\tResponseIsOperationResult bool\n")
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
		r.body.WriteString("\tFill: " + goStringList(publicsurface.FieldNames(pm.ServerFilled)) + ",\n")
		r.body.WriteString("\tReject: " + goStringList(publicsurface.FieldNames(pm.Rejected)) + ",\n")
		fmt.Fprintf(&r.body, "\tResponseIsOperationResult: %t,\n", publicsurface.ResponseIsOperationResult(pm))
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
