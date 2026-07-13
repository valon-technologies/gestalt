package rust

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// serverSupportFile is the provider-side error mapping emitted as
// server_support.rs when any service carries the provider annotation.
// It converts a handler error (GestaltError or tonic::Status) to a
// tonic::Status the wire server returns to the host: GestaltError codes map
// through, tonic::Status passes through unchanged, and any other error gets
// tonic::Code::Unknown tagged with the operation string.
const serverSupportFile = `//! Provider-side error mapping and typed streams for generated adapters.

use std::pin::Pin;

use crate::rpc_support::GestaltError;
use tokio_stream::{Stream, StreamExt};

/// Transport-neutral provider stream. Errors remain SDK-owned until the
/// generated adapter maps them to tonic statuses.
pub type ProviderStream<T> = Pin<Box<dyn Stream<Item = Result<T, GestaltError>> + Send + 'static>>;

/// Wire stream returned by generated tonic adapters.
pub type WireStream<T> = Pin<Box<dyn Stream<Item = Result<T, tonic::Status>> + Send + 'static>>;

/// Converts a provider stream into a wire stream while preserving framing and
/// mapping every provider error at the adapter boundary.
pub(crate) fn map_provider_stream<T: 'static, U: 'static>(
    operation: &'static str,
    stream: ProviderStream<T>,
    convert: fn(T) -> U,
) -> WireStream<U> {
    Box::pin(stream.map(move |item| {
        item.map(convert)
            .map_err(|error| status_error(operation, error))
    }))
}

/// Converts a tonic request stream to the SDK-owned provider stream.
pub(crate) fn request_stream<T: 'static, U: 'static>(
    stream: tonic::Streaming<U>,
    convert: fn(U) -> T,
) -> ProviderStream<T> {
    Box::pin(stream.map(move |item| item.map(convert).map_err(GestaltError::from)))
}

/// Converts one handler error to the tonic Status returned to the host:
/// GestaltError carries its code through (the numeric gestalt_error_code
/// values are identical to the gRPC canonical codes), and the operation
/// string is prepended to the message.
pub(crate) fn status_error(operation: &str, err: GestaltError) -> tonic::Status {
    let code = tonic::Code::from(err.code);
    tonic::Status::new(code, format!("{operation}: {}", err.message))
}
`

// providerHandlerName renders the handler trait name for a provider service:
// the service name with a Provider suffix. A service already named *Provider
// takes a Handler suffix instead — its generated client owns the bare name
// (service AppProvider: client AppProvider, handler AppProviderHandler).
func providerHandlerName(svcName string) string {
	if strings.HasSuffix(svcName, "Provider") {
		return svcName + "Handler"
	}
	return svcName + "Provider"
}

// operationString renders the human-readable operation tag carried on handler
// errors: the lowercased service name followed by the method name split on
// word boundaries ("workflow apply definition").
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

// wireServerModule returns the tonic server module name for a service, e.g.
// app_provider_server for AppProvider.
func wireServerModule(svc *model.Service) string {
	return heckSnake(localName(svc.FullName)) + "_server"
}

// wireServerTrait returns the tonic server trait name inside wireServerModule,
// e.g. AppProvider for the AppProvider service.
func wireServerTrait(svc *model.Service) string {
	return heckUpperCamel(localName(svc.FullName))
}

func (r *renderer) providerMethodSignature(m *model.Method) (params, result string) {
	input, output := "", ""
	if !m.InputIsEmpty {
		input = r.messageType(m.Input.FullName)
	}
	if !m.OutputIsEmpty {
		output = r.messageType(m.Output.FullName)
	}
	providerStream := "crate::server_support::ProviderStream"
	switch m.Stream {
	case model.ServerStream:
		if input != "" {
			params = fmt.Sprintf(", request: %s", input)
		}
		result = providerStream + "<" + output + ">"
	case model.ClientStream:
		params = ", requests: " + providerStream + "<" + input + ">"
		result = output
	case model.Bidi:
		params = ", requests: " + providerStream + "<" + input + ">"
		result = providerStream + "<" + output + ">"
	default:
		if m.ProviderInput == model.ProviderInputClientSignature {
			params = r.providerFieldParams(m)
		} else if input != "" {
			params = ", request: " + input
		}
		result = output
	}
	if result == "" {
		result = "()"
	}
	return params, result
}

func (r *renderer) providerFieldParams(m *model.Method) string {
	var params strings.Builder
	for _, name := range append(append([]string{}, m.Signature...), m.OptionalSignature...) {
		field := findField(m.Input, name)
		if field == nil {
			continue
		}
		typ := r.fieldType(field)
		if field.Presence == model.ExplicitPresence {
			typ = "Option<" + typ + ">"
		}
		fmt.Fprintf(&params, ", %s: %s", escapeIdent(field.Name), typ)
	}
	return params.String()
}

func (r *renderer) providerFieldArgs(m *model.Method, request string) string {
	var args []string
	for _, name := range append(append([]string{}, m.Signature...), m.OptionalSignature...) {
		field := findField(m.Input, name)
		if field != nil {
			args = append(args, request+"."+escapeIdent(field.Name))
		}
	}
	return strings.Join(args, ", ")
}

// renderProviderHandler renders the native handler trait and its Unimplemented
// defaults for one provider service. The trait uses &self (matching tonic's
// Arc<T> dispatch) and exposes either the full native request or annotated
// client-signature fields.
// An Empty response collapses to the error-only form (-> Result<(), GestaltError>).
func (r *renderer) renderProviderHandler(svc *model.Service) {
	svcName := localName(svc.FullName)
	traitName := providerHandlerName(svcName)
	r.useType("GestaltError")

	r.docComment("", svc.Doc)
	fmt.Fprintf(&r.body, "/// Handler trait implemented by providers serving the `%s` service.\n", svc.FullName)
	fmt.Fprintf(&r.body, "/// Methods receive the full native request; wire conversion and error\n")
	fmt.Fprintf(&r.body, "/// mapping live in the generated adapter (see [`%sServer`]).\n", traitName)
	fmt.Fprintf(&r.body, "/// Embed [`Unimplemented%s`] to default unimplemented methods.\n", traitName)
	r.body.WriteString("#[tonic::async_trait]\n")
	fmt.Fprintf(&r.body, "pub trait %s: Send + Sync + 'static {\n", traitName)
	for _, m := range svc.Methods {
		r.docComment("    ", m.Doc)
		fmt.Fprintf(&r.body, "    /// Handles the `%s` RPC.\n", m.Name)
		params, result := r.providerMethodSignature(m)
		fmt.Fprintf(&r.body, "    async fn %s(&self%s) -> Result<%s, GestaltError>;\n", heckSnake(m.Name), params, result)
	}
	r.body.WriteString("}\n\n")

	// Unimplemented defaults struct.
	fmt.Fprintf(&r.body, "/// Fails every [`%s`] method with an Unimplemented error;\n", traitName)
	fmt.Fprintf(&r.body, "/// embed this to default the methods a provider does not implement.\n")
	fmt.Fprintf(&r.body, "#[derive(Debug, Default)]\n")
	fmt.Fprintf(&r.body, "pub struct Unimplemented%s;\n\n", traitName)

	r.body.WriteString("#[tonic::async_trait]\n")
	fmt.Fprintf(&r.body, "impl %s for Unimplemented%s {\n", traitName, traitName)
	for _, m := range svc.Methods {
		op := operationString(svcName, m.Name)
		msg := op + " is not implemented"
		r.useType("GestaltError")
		r.useType("gestalt_error_code")
		params, result := r.providerMethodSignature(m)
		params = strings.ReplaceAll(params, "request:", "_request:")
		fmt.Fprintf(&r.body, "    async fn %s(&self%s) -> Result<%s, GestaltError> {\n", heckSnake(m.Name), params, result)
		fmt.Fprintf(&r.body, "        Err(GestaltError::new(gestalt_error_code::UNIMPLEMENTED, %q))\n    }\n", msg)
	}
	r.body.WriteString("}\n\n")
}

// renderProviderServer renders the wire dispatch adapter for one provider
// service: a struct wrapping Arc<P> that implements the tonic wire server
// trait, converting requests from wire to native, calling the handler, and
// converting responses back. Handler errors map through status_error.
func (r *renderer) renderProviderServer(svc *model.Service) {
	svcName := localName(svc.FullName)
	traitName := providerHandlerName(svcName)
	adapterName := traitName + "Server"
	serverMod := wireServerModule(svc)
	serverTrait := wireServerTrait(svc)
	r.features.v1 = true

	fmt.Fprintf(&r.body, "/// Wire dispatch adapter: wraps an `Arc<P>` implementing [`%s`]\n", traitName)
	fmt.Fprintf(&r.body, "/// and implements the tonic wire trait `v1::%s::%s`.\n", serverMod, serverTrait)
	fmt.Fprintf(&r.body, "/// Requests convert from the wire, responses convert to the wire, and\n")
	fmt.Fprintf(&r.body, "/// handler errors map to gRPC statuses via `status_error`.\n")
	fmt.Fprintf(&r.body, "pub struct %s<P> {\n", adapterName)
	fmt.Fprintf(&r.body, "    handler: std::sync::Arc<P>,\n")
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "impl<P: %s> %s<P> {\n", traitName, adapterName)
	fmt.Fprintf(&r.body, "    /// Creates a new dispatch adapter wrapping `handler`.\n")
	fmt.Fprintf(&r.body, "    pub fn new(handler: std::sync::Arc<P>) -> Self {\n")
	fmt.Fprintf(&r.body, "        Self { handler }\n    }\n}\n\n")

	fmt.Fprintf(&r.body, "#[tonic::async_trait]\n")
	fmt.Fprintf(&r.body, "impl<P: %s> v1::%s::%s for %s<P> {\n", traitName, serverMod, serverTrait, adapterName)
	for _, m := range svc.Methods {
		op := operationString(svcName, m.Name)
		methodSnake := heckSnake(m.Name)
		streamAssoc := heckUpperCamel(m.Name) + "Stream"

		var wireIn, wireOut string
		if m.InputIsEmpty {
			wireIn = "()"
		} else {
			// Use v1:: prefix: the generated file has `use crate::generated::v1`.
			wireIn = "v1::" + wireTypeName(m.Input.FullName)
		}
		if m.OutputIsEmpty {
			wireOut = "()"
		} else {
			wireOut = "v1::" + wireTypeName(m.Output.FullName)
		}

		fromFn, toFn := "", ""
		if !m.InputIsEmpty {
			fromFn = r.convRef(m.Input.ProtoFile, fromWireFunc(m.Input.FullName))
		}
		if !m.OutputIsEmpty {
			toFn = r.convRef(m.Output.ProtoFile, toWireFunc(m.Output.FullName))
		}
		switch m.Stream {
		case model.ServerStream:
			fmt.Fprintf(&r.body, "    type %s = crate::server_support::WireStream<%s>;\n", streamAssoc, wireOut)
			if m.InputIsEmpty {
				fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        _request: tonic::Request<%s>,\n    ) -> Result<tonic::Response<Self::%s>, tonic::Status> {\n", methodSnake, wireIn, streamAssoc)
				fmt.Fprintf(&r.body, "        let response = self.handler.%s().await.map_err(|e| crate::server_support::status_error(%q, e))?;\n", methodSnake, op)
			} else {
				fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        request: tonic::Request<%s>,\n    ) -> Result<tonic::Response<Self::%s>, tonic::Status> {\n", methodSnake, wireIn, streamAssoc)
				fmt.Fprintf(&r.body, "        let response = self.handler.%s(%s(request.into_inner())).await.map_err(|e| crate::server_support::status_error(%q, e))?;\n", methodSnake, fromFn, op)
			}
			fmt.Fprintf(&r.body, "        Ok(tonic::Response::new(crate::server_support::map_provider_stream(%q, response, %s)))\n    }\n", op, toFn)
		case model.ClientStream:
			fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        request: tonic::Request<tonic::Streaming<%s>>,\n    ) -> Result<tonic::Response<%s>, tonic::Status> {\n", methodSnake, wireIn, wireOut)
			fmt.Fprintf(&r.body, "        let requests = crate::server_support::request_stream(request.into_inner(), %s);\n", fromFn)
			fmt.Fprintf(&r.body, "        let response = self.handler.%s(requests).await.map_err(|e| crate::server_support::status_error(%q, e))?;\n", methodSnake, op)
			if m.OutputIsEmpty {
				r.body.WriteString("        Ok(tonic::Response::new(()))\n    }\n")
			} else {
				fmt.Fprintf(&r.body, "        Ok(tonic::Response::new(%s(response)))\n    }\n", toFn)
			}
		case model.Bidi:
			fmt.Fprintf(&r.body, "    type %s = crate::server_support::WireStream<%s>;\n", streamAssoc, wireOut)
			fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        request: tonic::Request<tonic::Streaming<%s>>,\n    ) -> Result<tonic::Response<Self::%s>, tonic::Status> {\n", methodSnake, wireIn, streamAssoc)
			fmt.Fprintf(&r.body, "        let requests = crate::server_support::request_stream(request.into_inner(), %s);\n", fromFn)
			fmt.Fprintf(&r.body, "        let response = self.handler.%s(requests).await.map_err(|e| crate::server_support::status_error(%q, e))?;\n", methodSnake, op)
			fmt.Fprintf(&r.body, "        Ok(tonic::Response::new(crate::server_support::map_provider_stream(%q, response, %s)))\n    }\n", op, toFn)
		default:
			if m.InputIsEmpty {
				fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        _request: tonic::Request<%s>,\n    ) -> Result<tonic::Response<%s>, tonic::Status> {\n", methodSnake, wireIn, wireOut)
			} else {
				fmt.Fprintf(&r.body, "    async fn %s(\n        &self,\n        request: tonic::Request<%s>,\n    ) -> Result<tonic::Response<%s>, tonic::Status> {\n", methodSnake, wireIn, wireOut)
			}
			callExpr := fmt.Sprintf("self.handler.%s()", methodSnake)
			callPrefix := ""
			if !m.InputIsEmpty {
				if m.ProviderInput == model.ProviderInputClientSignature {
					callPrefix = fmt.Sprintf("let native_request = %s(request.into_inner());\n        ", fromFn)
					callExpr = fmt.Sprintf("self.handler.%s(%s)", methodSnake, r.providerFieldArgs(m, "native_request"))
				} else {
					callExpr = fmt.Sprintf("self.handler.%s(%s(request.into_inner()))", methodSnake, fromFn)
				}
			}
			if m.OutputIsEmpty {
				fmt.Fprintf(&r.body, "        %s%s.await.map_err(|e| crate::server_support::status_error(%q, e))?;\n        Ok(tonic::Response::new(()))\n    }\n", callPrefix, callExpr, op)
			} else {
				fmt.Fprintf(&r.body, "        %slet response = %s.await.map_err(|e| crate::server_support::status_error(%q, e))?;\n        Ok(tonic::Response::new(%s(response)))\n    }\n", callPrefix, callExpr, op, toFn)
			}
		}
	}
	r.body.WriteString("}\n\n")
}

// renderProviderFile renders the complete provider handler surface for one
// group: the handler trait, Unimplemented defaults, and wire dispatch adapter
// for every provider service in the group. It returns the assembled Rust
// source or an empty string when the group has no provider services.
func renderProviderFile(idx *index, base string, services []*model.Service) string {
	var providerServices []*model.Service
	for _, svc := range services {
		if svc.Provider {
			providerServices = append(providerServices, svc)
		}
	}
	if len(providerServices) == 0 {
		return ""
	}
	// Use a unique base that never matches any proto file base, so that native
	// types from the same proto file are correctly recorded as cross-public
	// imports (use crate::<base>::{T}) rather than treated as local.
	r := newRenderer(idx, base+"_provider_handler", base+"_provider_handler", modulePublic)
	for _, svc := range providerServices {
		r.renderProviderHandler(svc)
		r.renderProviderServer(svc)
	}
	return "#![allow(dead_code, unused_imports, unused_variables)]\n\n" + r.assemble()
}
