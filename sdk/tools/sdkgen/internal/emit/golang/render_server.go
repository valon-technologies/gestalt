package golang

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// providerName renders the handler interface name for a provider service:
// the service name with a Provider suffix. A service already named *Provider
// takes a Handler suffix instead — its generated client owns the bare name
// (service AppProvider: client AppProvider, handler AppProviderHandler).
func providerName(svcName string) string {
	if strings.HasSuffix(svcName, "Provider") {
		return svcName + "Handler"
	}
	return svcName + "Provider"
}

// operationString renders the human-readable operation tag carried on
// handler errors: the lowercased service name followed by the method name
// split on word boundaries ("workflow apply definition").
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

// handlerSignature renders one handler method's parameter and result lists.
// Handlers are transport-shaped: the full native request in, the full native
// response out. Client-side call ergonomics (signature flattening, response
// collapses) intentionally do not apply.
func (r *renderer) handlerSignature(m *model.Method) (params, results string) {
	params = "ctx context.Context"
	if !m.InputIsEmpty {
		params += ", request *" + r.messageType(m.Input.FullName)
	}
	if m.OutputIsEmpty {
		return params, "error"
	}
	return params, fmt.Sprintf("(*%s, error)", r.messageType(m.Output.FullName))
}

// renderProviderHandler renders the native handler interface and its
// Unimplemented defaults struct for one provider service.
func (r *renderer) renderProviderHandler(svc *model.Service) {
	name := providerName(localName(svc.FullName))
	r.features.context = true

	r.writeIdentDoc("", fmt.Sprintf("%s is the handler interface implemented by providers serving\nthe %s service. Methods receive the full native request;\nwire conversion and error mapping live in the generated adapter (see\n[New%sServer]). Embed [Unimplemented%s] to default\nunimplemented methods.", name, svc.FullName, name, name), svc.Doc)
	fmt.Fprintf(&r.body, "type %s interface {\n", name)
	for _, m := range svc.Methods {
		r.writeIdentDoc("\t", fmt.Sprintf("%s handles the %s RPC.", m.Name, m.Name), m.Doc)
		params, results := r.handlerSignature(m)
		fmt.Fprintf(&r.body, "\t%s(%s) %s\n", m.Name, params, results)
	}
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "// Unimplemented%s fails every %s method with a\n// GestaltErrorCodeUnimplemented error; embed it to default the methods a\n// provider does not implement.\n", name, name)
	fmt.Fprintf(&r.body, "type Unimplemented%s struct{}\n\n", name)
	for _, m := range svc.Methods {
		params, results := r.handlerSignature(m)
		op := operationString(localName(svc.FullName), m.Name)
		fmt.Fprintf(&r.body, "// %s returns Unimplemented; embed Unimplemented%s to default\n// unimplemented handler methods.\n", m.Name, name)
		fmt.Fprintf(&r.body, "func (Unimplemented%s) %s(%s) %s {\n", name, m.Name, stripParamNames(params), results)
		failure := fmt.Sprintf("&GestaltError{Code: GestaltErrorCodeUnimplemented, Message: %q}", op+" is not implemented")
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "\treturn %s\n}\n\n", failure)
		} else {
			fmt.Fprintf(&r.body, "\treturn nil, %s\n}\n\n", failure)
		}
	}
}

// stripParamNames rewrites a parameter list to types only, for method stubs
// whose bodies ignore every argument.
func stripParamNames(params string) string {
	parts := strings.Split(params, ", ")
	for i, p := range parts {
		if j := strings.IndexByte(p, ' '); j >= 0 {
			parts[i] = p[j+1:]
		}
	}
	return strings.Join(parts, ", ")
}

// renderProviderServer renders the wire dispatch adapter for one provider
// service: a wire-level gRPC server that converts requests from the wire,
// calls the handler, converts responses back, and maps handler errors to
// gRPC statuses.
func (r *renderer) renderProviderServer(svc *model.Service) {
	svcName := localName(svc.FullName)
	name := providerName(svcName)
	adapter := lowerFirst(name) + "Server"
	r.features.context = true
	r.features.proto = true

	fmt.Fprintf(&r.body, "// New%sServer adapts provider to the wire-level\n// [proto.%sServer]: requests convert from the wire, responses convert to\n// the wire, and handler errors map to gRPC statuses.\n", name, svcName)
	fmt.Fprintf(&r.body, "func New%sServer(provider %s) proto.%sServer {\n", name, name, svcName)
	fmt.Fprintf(&r.body, "\treturn &%s{provider: provider}\n}\n\n", adapter)

	fmt.Fprintf(&r.body, "type %s struct {\n\tproto.Unimplemented%sServer\n\tprovider %s\n}\n\n", adapter, svcName, name)

	for _, m := range svc.Methods {
		op := operationString(svcName, m.Name)
		wireIn, wireOut := "*emptypb.Empty", "*emptypb.Empty"
		if !m.InputIsEmpty {
			wireIn = "*" + wireMessage(m.Input.FullName)
		}
		if !m.OutputIsEmpty {
			wireOut = "*" + wireMessage(m.Output.FullName)
		}
		if m.InputIsEmpty || m.OutputIsEmpty {
			r.features.emptypb = true
		}

		requestParam := "request " + wireIn
		callArgs := "ctx"
		if m.InputIsEmpty {
			requestParam = "_ " + wireIn
		} else {
			callArgs += ", " + fromWireFunc(r.messageType(m.Input.FullName)) + "(request)"
		}

		fmt.Fprintf(&r.body, "func (s *%s) %s(ctx context.Context, %s) (%s, error) {\n", adapter, m.Name, requestParam, wireOut)
		if m.OutputIsEmpty {
			fmt.Fprintf(&r.body, "\tif err := s.provider.%s(%s); err != nil {\n\t\treturn nil, statusError(%q, err)\n\t}\n", m.Name, callArgs, op)
			r.body.WriteString("\treturn &emptypb.Empty{}, nil\n}\n\n")
		} else {
			fmt.Fprintf(&r.body, "\tresponse, err := s.provider.%s(%s)\n", m.Name, callArgs)
			fmt.Fprintf(&r.body, "\tif err != nil {\n\t\treturn nil, statusError(%q, err)\n\t}\n", op)
			fmt.Fprintf(&r.body, "\treturn %s(response), nil\n}\n\n", toWireFunc(r.messageType(m.Output.FullName)))
		}
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
