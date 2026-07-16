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

func (r *renderer) renderAppClient(services []*model.Service) {
	r.body.WriteString("// AppClient is the transport-neutral client for the public App surface.\n")
	r.body.WriteString("type AppClient struct {\n")
	r.body.WriteString("\ttransport UnaryTransport\n")
	r.body.WriteString("}\n\n")
	r.body.WriteString("// NewAppClient creates an AppClient over the given transport.\n")
	r.body.WriteString("func NewAppClient(transport UnaryTransport) *AppClient {\n")
	r.body.WriteString("\treturn &AppClient{transport: transport}\n")
	r.body.WriteString("}\n\n")

	for _, svc := range services {
		wireName := localName(svc.FullName)
		for _, m := range svc.Methods {
			r.renderAppClientMethod(wireName, m)
		}
	}
}

func (r *renderer) renderAppClientMethod(wireName string, m *model.Method) {
	constName := fmt.Sprintf("Method%s%s", wireName, m.Name)
	requestType := r.messageType(m.Input.FullName)
	responseType := r.messageType(m.Output.FullName)

	if m.JsonResult != nil {
		r.renderAppClientUnaryMethod(m, constName, requestType, responseType, m.Name+"Raw")
		r.renderAppClientDecodedMethod(m, requestType)
		return
	}

	r.renderAppClientUnaryMethod(m, constName, requestType, responseType, m.Name)
	fmt.Fprintf(&r.body, "// %sRaw is an alias for %s.\n", m.Name, m.Name)
	fmt.Fprintf(&r.body, "func (c *AppClient) %sRaw(ctx context.Context, request *%s) (*%s, error) {\n",
		m.Name, requestType, responseType)
	fmt.Fprintf(&r.body, "\treturn c.%s(ctx, request)\n}\n\n", m.Name)
}

func (r *renderer) renderAppClientUnaryMethod(
	m *model.Method,
	constName, requestType, responseType, methodName string,
) {
	toWire := toWireFunc(requestType)
	fromWire := fromWireFunc(responseType)

	fmt.Fprintf(&r.body, "func (c *AppClient) %s(ctx context.Context, request *%s) (*%s, error) {\n",
		methodName, requestType, responseType)
	fmt.Fprintf(&r.body, "\twire := %s(request)\n", toWire)
	fmt.Fprintf(&r.body, "\tout := &%s{}\n", wireMessage(m.Output.FullName))
	fmt.Fprintf(&r.body, "\tif err := c.transport.Unary(ctx, %s, wire, out); err != nil {\n", constName)
	r.body.WriteString("\t\treturn nil, toGestaltError(err)\n\t}\n")
	fmt.Fprintf(&r.body, "\treturn %s(out), nil\n}\n\n", fromWire)
}

func (r *renderer) renderAppClientDecodedMethod(m *model.Method, requestType string) {
	status := findField(m.Output, m.JsonResult.Status)
	body := findField(m.Output, m.JsonResult.Body)
	appExpr := jsonResultContext(m, "app")

	fmt.Fprintf(&r.body, "func (c *AppClient) %s(ctx context.Context, request *%s) (any, error) {\n",
		m.Name, requestType)
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

func (r *renderer) assembleAppClient() string {
	return fmt.Sprintf(`package generated

import (
	"context"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	%s
)

`, wireImport) + r.body.String()
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
