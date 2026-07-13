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

// handlerSignature renders one provider method's transport-independent
// signature. Streaming handlers use SDK-owned stream interfaces rather than
// exposing grpc types to provider implementations.
func (r *renderer) handlerSignature(m *model.Method) (params, results string) {
	input := ""
	if !m.InputIsEmpty {
		input = "*" + r.messageType(m.Input.FullName)
	}
	output := ""
	if !m.OutputIsEmpty {
		output = "*" + r.messageType(m.Output.FullName)
	}
	switch m.Stream {
	case model.ServerStream:
		params = "ctx context.Context"
		if input != "" {
			params += ", request " + input
		}
		return params + ", stream ServerStream[" + output + "]", "error"
	case model.ClientStream:
		return "ctx context.Context, stream ClientStream[" + input + "]", resultSignature(output)
	case model.Bidi:
		return "ctx context.Context, stream BidiStream[" + input + ", " + output + "]", "error"
	default:
		params = "ctx context.Context"
		if m.ProviderInput == model.ProviderInputClientSignature {
			params += r.providerFieldParams(m)
		} else if input != "" {
			params += ", request " + input
		}
		return params, resultSignature(output)
	}
}

func (r *renderer) providerFieldParams(m *model.Method) string {
	var b strings.Builder
	names := append(append([]string{}, m.Signature...), m.OptionalSignature...)
	for _, name := range names {
		field := findField(m.Input, name)
		if field == nil {
			continue
		}
		fmt.Fprintf(&b, ", %s %s", goParamName(field.JSONName), r.fieldType(field))
	}
	return b.String()
}

func (r *renderer) providerFieldArgs(m *model.Method, request string) string {
	if m.ProviderInput != model.ProviderInputClientSignature {
		return request
	}
	var args []string
	names := append(append([]string{}, m.Signature...), m.OptionalSignature...)
	for _, name := range names {
		field := findField(m.Input, name)
		if field != nil {
			args = append(args, request+"."+fieldGoName(field))
		}
	}
	return strings.Join(args, ", ")
}

func resultSignature(output string) string {
	if output == "" {
		return "error"
	}
	return fmt.Sprintf("(%s, error)", output)
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
		if m.Stream == model.ServerStream || m.Stream == model.Bidi || m.OutputIsEmpty {
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
		wireInValue, wireOutValue := "emptypb.Empty", "emptypb.Empty"
		if !m.InputIsEmpty {
			wireInValue = wireMessage(m.Input.FullName)
		}
		if !m.OutputIsEmpty {
			wireOutValue = wireMessage(m.Output.FullName)
		}
		wireIn, wireOut := "*"+wireInValue, "*"+wireOutValue
		if m.InputIsEmpty || m.OutputIsEmpty {
			r.features.emptypb = true
		}
		inNative, outNative := "", ""
		if !m.InputIsEmpty {
			inNative = "*" + r.messageType(m.Input.FullName)
		}
		if !m.OutputIsEmpty {
			outNative = "*" + r.messageType(m.Output.FullName)
		}
		fromIn := ""
		if !m.InputIsEmpty {
			fromIn = fromWireFunc(r.messageType(m.Input.FullName)) + "(request)"
		}

		switch m.Stream {
		case model.ServerStream:
			r.features.grpc = true
			if m.InputIsEmpty {
				fmt.Fprintf(&r.body, "func (s *%s) %s(_ %s, stream grpc.ServerStreamingServer[%s]) error {\n", adapter, m.Name, wireIn, wireOutValue)
			} else {
				fmt.Fprintf(&r.body, "func (s *%s) %s(request %s, stream grpc.ServerStreamingServer[%s]) error {\n", adapter, m.Name, wireIn, wireOutValue)
			}
			call := "s.provider." + m.Name + "(stream.Context()"
			if fromIn != "" {
				call += ", " + fromIn
			}
			fmt.Fprintf(&r.body, "\terr := %s, NativeServerStream[%s, %s]{SendWire: stream.Send, ToWire: %s})\n", call, outNative, wireOutValue, toWireFunc(r.messageType(m.Output.FullName)))
			fmt.Fprintf(&r.body, "\tif err != nil { return statusError(%q, err) }\n\treturn nil\n}\n\n", op)
		case model.ClientStream:
			r.features.grpc = true
			fmt.Fprintf(&r.body, "func (s *%s) %s(stream grpc.ClientStreamingServer[%s, %s]) error {\n", adapter, m.Name, wireInValue, wireOutValue)
			call := fmt.Sprintf("s.provider.%s(stream.Context(), NativeClientStream[%s, %s]{RecvWire: stream.Recv, FromWire: %s})", m.Name, inNative, wireInValue, fromWireFunc(r.messageType(m.Input.FullName)))
			if m.OutputIsEmpty {
				fmt.Fprintf(&r.body, "\terr := %s\n\tif err != nil { return statusError(%q, err) }\n\treturn stream.SendAndClose(&emptypb.Empty{})\n}\n\n", call, op)
			} else {
				fmt.Fprintf(&r.body, "\tresponse, err := %s\n\tif err != nil { return statusError(%q, err) }\n\treturn stream.SendAndClose(%s(response))\n}\n\n", call, op, toWireFunc(r.messageType(m.Output.FullName)))
			}
		case model.Bidi:
			r.features.grpc = true
			fmt.Fprintf(&r.body, "func (s *%s) %s(stream grpc.BidiStreamingServer[%s, %s]) error {\n", adapter, m.Name, wireInValue, wireOutValue)
			call := fmt.Sprintf("s.provider.%s(stream.Context(), NativeBidiStream[%s, %s, %s, %s]{RecvWire: stream.Recv, SendWire: stream.Send, FromWire: %s, ToWire: %s})", m.Name, inNative, outNative, wireInValue, wireOutValue, fromWireFunc(r.messageType(m.Input.FullName)), toWireFunc(r.messageType(m.Output.FullName)))
			fmt.Fprintf(&r.body, "\terr := %s\n\tif err != nil { return statusError(%q, err) }\n\treturn nil\n}\n\n", call, op)
		default:
			requestParam := "request " + wireIn
			callArgs := "ctx"
			if m.InputIsEmpty {
				requestParam = "_ " + wireIn
			} else {
				if m.ProviderInput == model.ProviderInputClientSignature {
					callArgs += ", " + r.providerFieldArgs(m, fromIn)
				} else {
					callArgs += ", " + fromIn
				}
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
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
