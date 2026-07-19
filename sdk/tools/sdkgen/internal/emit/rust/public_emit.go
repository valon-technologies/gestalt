package rust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

const publicRPCSupportReexport = `pub use crate::rpc_support::{gestalt_error_code, GestaltError};
`

const publicInvokeSupportReexport = `pub use crate::invoke_support::*;
`

const publicUnaryTransportFile = `//! Transport-neutral unary call contract for generated public clients.

use std::future::Future;

use prost::Message;

use crate::public::generated::metadata::Method;
use crate::public::generated::rpc_support::GestaltError;

/// Performs one unary public App call.
pub trait UnaryTransport: Send + Sync {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> impl Future<Output = Result<(), GestaltError>> + Send
    where
        Req: Message + Clone + Send + Sync + 'static,
        Resp: Message + Default + Send + 'static;
}

/// Performs one unary public App call synchronously. REST-only; no gRPC
/// transport implements this trait.
pub trait SyncUnaryTransport: Send + Sync {
    fn unary<Req, Resp>(
        &self,
        method: &Method,
        request: &Req,
        response: &mut Resp,
    ) -> Result<(), GestaltError>
    where
        Req: Message + Clone + Send + Sync + 'static,
        Resp: Message + Default + Send + 'static;
}

/// Marker for transports that can call gRPC-only public methods.
pub trait GrpcCapable: UnaryTransport {}
`

// EmitPublic renders the public gestaltd Rust client under sdk/rust/src/public.
func EmitPublic(schema *model.Schema) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		return nil, err
	}
	idx := &index{
		messages:     plan.MessageIndex,
		wireMessages: map[string]*model.Message{},
		enums:        map[string]*model.Enum{},
		needToWire:   map[string]bool{},
		needFromWire: map[string]bool{},
		needWireJSON: map[string]bool{},
	}
	for _, e := range plan.ReachableEnums {
		idx.enums[e.FullName] = e
	}

	set := fileset.New()
	if len(plan.View.Services) == 0 {
		return set, nil
	}

	markConversionNeeds(idx, plan.Filtered.Services)
	reachableMessages := plan.ReachableMessages
	enums := plan.ReachableEnums
	methods := plan.Methods
	for _, m := range reachableMessages {
		idx.wireMessages[m.FullName] = m
	}
	markPublicWireJSONFromMessages(idx, reachableMessages)
	supportUses := map[string]bool{}
	codecModules := []string{"support"}
	if err := set.Add("generated/rpc_support.rs", []byte(publicRPCSupportReexport)); err != nil {
		return nil, err
	}
	if hasPublicJsonResult(plan.Filtered.Services) {
		if err := set.Add("generated/invoke_support.rs", []byte(publicInvokeSupportReexport)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("generated/unary_transport.rs", []byte(publicUnaryTransportFile)); err != nil {
		return nil, err
	}
	meta := newRenderer(idx, "metadata", "metadata", modulePublic, true)
	meta.renderPublicMetadata(methods)
	if err := set.Add("generated/metadata.rs", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(plan.Filtered.Services, reachableMessages, enums) {
		public := newRenderer(idx, g.base, g.base, modulePublic, true)
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		if err := set.Add("generated/"+g.base+".rs", []byte(public.assembleGenerated())); err != nil {
			return nil, err
		}
		for name := range public.features.supportFns {
			supportUses[name] = true
		}
		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, g.base, g.base, moduleCodec, true)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/codec/"+g.base+".rs", []byte(codec.assembleGenerated())); err != nil {
			return nil, err
		}
		codecModules = append(codecModules, g.base)
		for name := range codec.features.supportFns {
			supportUses[name] = true
		}
	}
	if err := set.Add("generated/codec.rs", []byte(renderPublicCodecIndex(codecModules))); err != nil {
		return nil, err
	}
	if err := set.Add("generated/codec/support.rs", []byte(renderCodecSupport(supportUses))); err != nil {
		return nil, err
	}

	client := newRenderer(idx, "app_client", "app", modulePublic, true)
	client.docIntro = "Generated transport-neutral App client for the public gestaltd surface."
	for _, svc := range plan.Filtered.Services {
		client.renderAppClient(svc)
	}
	if err := set.Add("generated/app_client.rs", []byte(client.assembleGenerated())); err != nil {
		return nil, err
	}

	modules := []string{"metadata", "rpc_support", "unary_transport", "app_client"}
	if hasPublicJsonResult(plan.Filtered.Services) {
		modules = append(modules, "invoke_support")
	}
	for _, g := range groupFiles(plan.Filtered.Services, reachableMessages, enums) {
		modules = append(modules, g.base)
	}
	if err := set.Add("generated/mod.rs", []byte(renderGeneratedMod(modules))); err != nil {
		return nil, err
	}
	return set, nil
}

func renderGeneratedMod(modules []string) string {
	sort.Strings(modules)
	var b strings.Builder
	b.WriteString("//! Generated public gestaltd client modules.\n\n")
	b.WriteString("#![allow(missing_docs)]\n\n")
	b.WriteString("pub mod codec;\n")
	for _, m := range modules {
		fmt.Fprintf(&b, "pub mod %s;\n", m)
	}
	return b.String()
}

func renderPublicCodecIndex(modules []string) string {
	sorted := append([]string(nil), modules...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("//! Crate-private wire converters for the public generated clients.\n\n")
	b.WriteString("#![allow(missing_docs)]\n\n")
	b.WriteString("pub(crate) mod support;\n")
	for _, m := range sorted {
		if m == "support" {
			continue
		}
		fmt.Fprintf(&b, "pub(crate) mod %s;\n", m)
	}
	return b.String()
}

func hasPublicJsonResult(services []*model.Service) bool {
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.JsonResult != nil {
				return true
			}
		}
	}
	return false
}

func markConversionNeeds(idx *index, services []*model.Service) {
	var visit func(need map[string]bool, fullName string)
	visit = func(need map[string]bool, fullName string) {
		if fullName == "" || need[fullName] {
			return
		}
		need[fullName] = true
		m := idx.messages[fullName]
		if m == nil {
			return
		}
		visitRef := func(ref *model.TypeRef) {
			if ref == nil || ref.Kind != model.KindMessage {
				return
			}
			visit(need, ref.Message)
		}
		for _, f := range m.Fields {
			switch f.Kind {
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
			default:
				visitRef(fieldRef(f))
			}
		}
		for _, o := range m.Oneofs {
			for _, f := range oneofFields(m, o) {
				visitRef(fieldRef(f))
			}
		}
	}
	for _, svc := range services {
		for _, method := range svc.Methods {
			if method.Input != nil {
				visit(idx.needToWire, method.Input.FullName)
			}
			if method.Output != nil {
				visit(idx.needFromWire, method.Output.FullName)
			}
		}
	}
}

func markPublicWireJSONFromMessages(idx *index, messages []*model.Message) {
	var visit func(*model.Message)
	visitRef := func(ref *model.TypeRef) {
		if ref == nil || ref.Kind != model.KindMessage {
			return
		}
		if m := idx.wireMessages[ref.Message]; m != nil {
			visit(m)
		}
	}
	visit = func(m *model.Message) {
		if m == nil || idx.needWireJSON[m.FullName] {
			return
		}
		idx.needWireJSON[m.FullName] = true
		for _, f := range m.Fields {
			switch f.Kind {
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
			default:
				visitRef(fieldRef(f))
			}
		}
		for _, o := range m.Oneofs {
			for _, f := range oneofFields(m, o) {
				visitRef(fieldRef(f))
			}
		}
	}
	for _, m := range messages {
		visit(m)
	}
}

func (r *renderer) renderPublicMetadata(methods []publicsurface.PublicMethod) {
	r.body.WriteString("//! Public method metadata for the gestaltd surface.\n\n")
	r.body.WriteString("use prost::Message as _;\n")
	r.body.WriteString("use serde_json::{Map, Value};\n\n")
	r.body.WriteString("/// Wire representation of `google.protobuf.Empty`.\n")
	r.body.WriteString("#[derive(Clone, Copy, PartialEq, Eq, ::prost::Message)]\n")
	r.body.WriteString("pub struct Empty {}\n\n")
	r.body.WriteString("use crate::generated::v1;\n")
	r.body.WriteString("use crate::public::generated::rpc_support::{GestaltError, gestalt_error_code};\n")

	codecImports := map[string]map[string]bool{}
	for _, pm := range methods {
		collectWireJSONCodecImports(pm, codecImports)
	}
	for _, base := range sortedKeys2(codecImports) {
		names := sortedKeys(codecImports[base])
		fmt.Fprintf(
			&r.body,
			"use crate::public::generated::codec::%s::{%s};\n",
			base,
			strings.Join(names, ", "),
		)
	}
	r.body.WriteString("\n")

	r.body.WriteString("/// Encodes a wire request message as protobuf JSON for REST transports.\n")
	r.body.WriteString("pub type EncodeRequestJson = fn(&[u8]) -> Result<Value, GestaltError>;\n\n")
	r.body.WriteString("/// Decodes a wire response message from protobuf JSON for REST transports.\n")
	r.body.WriteString("pub type DecodeResponseJson = fn(&Value) -> Result<Vec<u8>, GestaltError>;\n\n")
	r.body.WriteString("#[derive(Clone, Debug)]\n")
	r.body.WriteString("pub struct Method {\n")
	r.body.WriteString("    pub service: &'static str,\n")
	r.body.WriteString("    pub name: &'static str,\n")
	r.body.WriteString("    pub full_method: &'static str,\n")
	r.body.WriteString("    pub http_verb: &'static str,\n")
	r.body.WriteString("    pub http_path: &'static str,\n")
	r.body.WriteString("    pub http_body: &'static str,\n")
	r.body.WriteString("    pub http_path_fields: &'static [PublicField],\n")
	r.body.WriteString("    pub http_query_fields: &'static [PublicField],\n")
	r.body.WriteString("    pub fill: &'static [&'static str],\n")
	r.body.WriteString("    pub reject: &'static [&'static str],\n")
	r.body.WriteString("    pub encode_request_json: Option<EncodeRequestJson>,\n")
	r.body.WriteString("    pub decode_response_json: Option<DecodeResponseJson>,\n")
	r.body.WriteString("}\n\n")
	r.body.WriteString("#[derive(Clone, Debug, PartialEq, Eq)]\n")
	r.body.WriteString("pub struct PublicField {\n")
	r.body.WriteString("    pub name: &'static str,\n")
	r.body.WriteString("    pub json_name: &'static str,\n")
	r.body.WriteString("}\n\n")

	for _, pm := range methods {
		r.renderWireJSONShims(pm)
	}

	for _, pm := range methods {
		wireName := localName(pm.Service)
		constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), methodConstSuffix(pm.Method))
		encodeShim, decodeShim := wireJSONShimNames(pm.Method)
		fmt.Fprintf(&r.body, "pub const %s: Method = Method {\n", constName)
		fmt.Fprintf(&r.body, "    service: %q,\n", pm.Service)
		fmt.Fprintf(&r.body, "    name: %q,\n", pm.Method)
		fmt.Fprintf(&r.body, "    full_method: %q,\n", pm.FullMethod)
		if pm.REST != nil {
			fmt.Fprintf(&r.body, "    http_verb: %q,\n", pm.REST.Verb)
			fmt.Fprintf(&r.body, "    http_path: %q,\n", pm.REST.PathTemplate)
			body := ""
			if pm.REST.Body == publicsurface.BodyStar {
				body = "*"
			}
			fmt.Fprintf(&r.body, "    http_body: %q,\n", body)
			fmt.Fprintf(&r.body, "    http_path_fields: %s,\n", rustPublicFieldSlice(pm.REST.PathFields))
			fmt.Fprintf(&r.body, "    http_query_fields: %s,\n", rustPublicFieldSlice(pm.REST.QueryFields))
		} else {
			r.body.WriteString("    http_verb: \"\",\n")
			r.body.WriteString("    http_path: \"\",\n")
			r.body.WriteString("    http_body: \"\",\n")
			r.body.WriteString("    http_path_fields: &[],\n")
			r.body.WriteString("    http_query_fields: &[],\n")
		}
		fmt.Fprintf(&r.body, "    fill: %s,\n", rustStrSlice(publicsurface.FieldNames(pm.ServerFilled)))
		fmt.Fprintf(&r.body, "    reject: %s,\n", rustStrSlice(publicsurface.FieldNames(pm.Rejected)))
		if pm.Input != nil && pm.Output != nil {
			fmt.Fprintf(&r.body, "    encode_request_json: Some(%s),\n", encodeShim)
			fmt.Fprintf(&r.body, "    decode_response_json: Some(%s),\n", decodeShim)
		} else if pm.Input != nil {
			fmt.Fprintf(&r.body, "    encode_request_json: Some(%s),\n", encodeShim)
			fmt.Fprintf(&r.body, "    decode_response_json: Some(%s),\n", decodeShim)
		} else if pm.Output != nil {
			fmt.Fprintf(&r.body, "    encode_request_json: Some(%s),\n", encodeShim)
			fmt.Fprintf(&r.body, "    decode_response_json: Some(%s),\n", decodeShim)
		} else {
			fmt.Fprintf(&r.body, "    encode_request_json: Some(%s),\n", encodeShim)
			fmt.Fprintf(&r.body, "    decode_response_json: Some(%s),\n", decodeShim)
		}
		r.body.WriteString("};\n\n")
	}
}

func wireJSONShimNames(method string) (encode, decode string) {
	snake := publicSnake(method)
	return "encode_" + snake + "_request_json", "decode_" + snake + "_response_json"
}

func collectWireJSONCodecImports(pm publicsurface.PublicMethod, codecImports map[string]map[string]bool) {
	if pm.Input != nil {
		codecBase := generatedFileBase(pm.Input.ProtoFile)
		if codecImports[codecBase] == nil {
			codecImports[codecBase] = map[string]bool{}
		}
		codecImports[codecBase][encodeWireJSONFunc(pm.Input.FullName)] = true
	}
	if pm.Output != nil {
		codecBase := generatedFileBase(pm.Output.ProtoFile)
		if codecImports[codecBase] == nil {
			codecImports[codecBase] = map[string]bool{}
		}
		codecImports[codecBase][decodeWireJSONFunc(pm.Output.FullName)] = true
	}
}

func (r *renderer) renderWireJSONShims(pm publicsurface.PublicMethod) {
	encodeShim, decodeShim := wireJSONShimNames(pm.Method)

	if pm.Input == nil && pm.Output == nil {
		fmt.Fprintf(&r.body, "fn %s(_bytes: &[u8]) -> Result<Value, GestaltError> {\n", encodeShim)
		r.body.WriteString("    Ok(Value::Object(Map::new()))\n")
		r.body.WriteString("}\n\n")
		fmt.Fprintf(&r.body, "fn %s(_value: &Value) -> Result<Vec<u8>, GestaltError> {\n", decodeShim)
		r.body.WriteString("    Ok(Empty::default().encode_to_vec())\n")
		r.body.WriteString("}\n\n")
		return
	}
	if pm.Input == nil {
		outputWire := wireTypeName(pm.Output.FullName)
		decodeFn := decodeWireJSONFunc(pm.Output.FullName)
		fmt.Fprintf(&r.body, "fn %s(_bytes: &[u8]) -> Result<Value, GestaltError> {\n", encodeShim)
		r.body.WriteString("    Ok(Value::Object(Map::new()))\n")
		r.body.WriteString("}\n\n")
		fmt.Fprintf(&r.body, "fn %s(value: &Value) -> Result<Vec<u8>, GestaltError> {\n", decodeShim)
		fmt.Fprintf(&r.body, "    let wire = %s(value)?;\n", decodeFn)
		r.body.WriteString("    Ok(wire.encode_to_vec())\n")
		r.body.WriteString("}\n\n")
		_ = outputWire
		return
	}
	if pm.Output == nil {
		inputWire := wireTypeName(pm.Input.FullName)
		encodeFn := encodeWireJSONFunc(pm.Input.FullName)
		fmt.Fprintf(&r.body, "fn %s(bytes: &[u8]) -> Result<Value, GestaltError> {\n", encodeShim)
		fmt.Fprintf(&r.body, "    let wire = v1::%s::decode(bytes).map_err(|err| {\n", inputWire)
		r.body.WriteString("        GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())\n")
		r.body.WriteString("    })?;\n")
		fmt.Fprintf(&r.body, "    Ok(%s(&wire))\n", encodeFn)
		r.body.WriteString("}\n\n")
		fmt.Fprintf(&r.body, "fn %s(_value: &Value) -> Result<Vec<u8>, GestaltError> {\n", decodeShim)
		r.body.WriteString("    Ok(Empty::default().encode_to_vec())\n")
		r.body.WriteString("}\n\n")
		return
	}
	inputWire := wireTypeName(pm.Input.FullName)
	encodeFn := encodeWireJSONFunc(pm.Input.FullName)
	decodeFn := decodeWireJSONFunc(pm.Output.FullName)

	fmt.Fprintf(&r.body, "fn %s(bytes: &[u8]) -> Result<Value, GestaltError> {\n", encodeShim)
	fmt.Fprintf(&r.body, "    let wire = v1::%s::decode(bytes).map_err(|err| {\n", inputWire)
	r.body.WriteString("        GestaltError::new(gestalt_error_code::INVALID_ARGUMENT, err.to_string())\n")
	r.body.WriteString("    })?;\n")
	fmt.Fprintf(&r.body, "    Ok(%s(&wire))\n", encodeFn)
	r.body.WriteString("}\n\n")

	fmt.Fprintf(&r.body, "fn %s(value: &Value) -> Result<Vec<u8>, GestaltError> {\n", decodeShim)
	fmt.Fprintf(&r.body, "    let wire = %s(value)?;\n", decodeFn)
	r.body.WriteString("    Ok(wire.encode_to_vec())\n")
	r.body.WriteString("}\n\n")
}

func rustPublicFieldSlice(fields []publicsurface.PublicField) string {
	if len(fields) == 0 {
		return "&[]"
	}
	var b strings.Builder
	b.WriteString("&[")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "PublicField { name: %q, json_name: %q }", f.Name, f.JSONName)
	}
	b.WriteString("]")
	return b.String()
}
