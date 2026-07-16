// Package publicrust emits the public gestaltd Rust client under
// sdk/rust/src/public/generated/.
package publicrust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetRust }

func (*Emitter) OutputRoot() string { return "sdk/rust/src/public" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Rustfmt() }

type index struct {
	messages     map[string]*model.Message
	wireMessages map[string]*model.Message
	enums        map[string]*model.Enum
	needToWire   map[string]bool
	needFromWire map[string]bool
	needWireJSON map[string]bool
}

type group struct {
	base     string
	services []*model.Service
	messages []*model.Message
	enums    []*model.Enum
}

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	view := publicsurface.Build(schema)
	filtered := publicSchema(schema, view)
	idx := &index{
		messages:     map[string]*model.Message{},
		wireMessages: map[string]*model.Message{},
		enums:        map[string]*model.Enum{},
		needToWire:   map[string]bool{},
		needFromWire: map[string]bool{},
		needWireJSON: map[string]bool{},
	}
	for _, m := range schema.Messages {
		idx.messages[m.FullName] = m
		idx.wireMessages[m.FullName] = m
	}
	for _, e := range schema.Enums {
		idx.enums[e.FullName] = e
	}
	omitByInput := omittedFieldsByInput(view)
	for fullName, omitted := range omitByInput {
		if m := idx.messages[fullName]; m != nil && len(omitted) > 0 {
			idx.messages[fullName] = cloneMessageOmitting(m, omitted)
		}
	}

	markRESTWireJSON(idx, filtered.Services)

	set := fileset.New()
	if len(view.Services) == 0 {
		return set, nil
	}

	messages, enums := reachable(idx, filtered.Services)
	supportUses := map[string]bool{}
	codecModules := []string{"support"}
	if err := set.Add("generated/rpc_support.rs", []byte(supportFile)); err != nil {
		return nil, err
	}
	if hasJsonResult(filtered.Services) {
		if err := set.Add("generated/invoke_support.rs", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("generated/rest_caller.rs", []byte(restCallerFile)); err != nil {
		return nil, err
	}
	meta := newRenderer(idx, "metadata", "metadata", modulePublic)
	meta.renderMetadata(view)
	if err := set.Add("generated/metadata.rs", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(filtered.Services, messages, enums) {
		public := newRenderer(idx, g.base, g.base, modulePublic)
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		for _, svc := range g.services {
			public.renderGRPCClient(svc)
			if hasRESTMethods(svc) {
				public.renderRESTClient(svc)
			}
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
		codec := newRenderer(idx, g.base, g.base, moduleCodec)
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
	if err := set.Add("generated/codec.rs", []byte(renderCodecIndex(codecModules))); err != nil {
		return nil, err
	}
	if err := set.Add("generated/codec/support.rs", []byte(renderCodecSupport(supportUses))); err != nil {
		return nil, err
	}
	modules := []string{"metadata", "rpc_support", "rest_caller"}
	if hasJsonResult(filtered.Services) {
		modules = append(modules, "invoke_support")
	}
	for _, g := range groupFiles(filtered.Services, messages, enums) {
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

const restCallerFile = `//! REST transport callback for generated public clients.

use crate::public::generated::metadata::Method;

/// Performs one unary public REST call.
pub trait RestCaller: Send + Sync {
    fn call_unary(
        &self,
        method: &Method,
        request_json: serde_json::Value,
        response_json: &mut serde_json::Value,
    ) -> Result<(), crate::rpc_support::GestaltError>;
}
`

func hasRESTMethods(svc *model.Service) bool {
	for _, m := range svc.Methods {
		if m.HTTP != nil {
			return true
		}
	}
	return false
}

func hasJsonResult(services []*model.Service) bool {
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.JsonResult != nil {
				return true
			}
		}
	}
	return false
}

func markRESTWireJSON(idx *index, services []*model.Service) {
	var visit func(fullName string)
	visit = func(fullName string) {
		if fullName == "" || idx.needWireJSON[fullName] {
			return
		}
		wire := idx.wireMessages[fullName]
		if wire == nil {
			return
		}
		idx.needWireJSON[fullName] = true
		visitRef := func(ref *model.TypeRef) {
			if ref == nil || ref.Kind != model.KindMessage {
				return
			}
			visit(ref.Message)
		}
		for _, f := range wire.Fields {
			switch f.Kind {
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
			default:
				if ref := fieldRef(f); ref != nil {
					visitRef(ref)
				}
			}
		}
		for _, o := range wire.Oneofs {
			for _, f := range oneofFields(wire, o) {
				if ref := fieldRef(f); ref != nil {
					visitRef(ref)
				}
			}
		}
	}
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.HTTP == nil || m.Stream != model.Unary {
				continue
			}
			if m.Input != nil {
				visit(m.Input.FullName)
			}
			if m.Output != nil {
				visit(m.Output.FullName)
			}
		}
	}
}

func renderCodecIndex(modules []string) string {
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

func reachable(idx *index, services []*model.Service) ([]*model.Message, []*model.Enum) {
	seenEnums := map[string]bool{}
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
			if ref == nil {
				return
			}
			switch ref.Kind {
			case model.KindMessage:
				visit(need, ref.Message)
			case model.KindEnum:
				seenEnums[ref.Enum] = true
			}
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
	var messages []*model.Message
	seenMessages := map[string]bool{}
	for _, need := range []map[string]bool{idx.needToWire, idx.needFromWire} {
		for fullName := range need {
			if !seenMessages[fullName] {
				seenMessages[fullName] = true
				if m := idx.messages[fullName]; m != nil {
					messages = append(messages, m)
				}
			}
		}
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].FullName < messages[j].FullName })
	var enums []*model.Enum
	for fullName := range seenEnums {
		if e := idx.enums[fullName]; e != nil {
			enums = append(enums, e)
		}
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].FullName < enums[j].FullName })
	return messages, enums
}

func groupFiles(services []*model.Service, messages []*model.Message, enums []*model.Enum) []*group {
	groups := map[string]*group{}
	groupFor := func(protoFile string) *group {
		base := generatedFileBase(protoFile)
		g, ok := groups[base]
		if !ok {
			g = &group{base: base}
			groups[base] = g
		}
		return g
	}
	for _, svc := range services {
		groupFor(svc.ProtoFile).services = append(groupFor(svc.ProtoFile).services, svc)
	}
	for _, m := range messages {
		groupFor(m.ProtoFile).messages = append(groupFor(m.ProtoFile).messages, m)
	}
	for _, e := range enums {
		groupFor(e.ProtoFile).enums = append(groupFor(e.ProtoFile).enums, e)
	}
	var out []*group
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].base < out[j].base })
	return out
}

func (r *renderer) renderMetadata(view *publicsurface.View) {
	r.body.WriteString("//! Public method metadata for the gestaltd surface.\n\n")
	r.body.WriteString("#[derive(Clone, Debug, PartialEq, Eq)]\n")
	r.body.WriteString("pub struct Method {\n")
	r.body.WriteString("    pub service: &'static str,\n")
	r.body.WriteString("    pub name: &'static str,\n")
	r.body.WriteString("    pub full_method: &'static str,\n")
	r.body.WriteString("    pub http_verb: &'static str,\n")
	r.body.WriteString("    pub http_path: &'static str,\n")
	r.body.WriteString("}\n\n")

	for _, svc := range view.Services {
		wireName := localName(svc.FullName)
		for _, m := range svc.PublicMethods {
			constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(m.Name))
			fmt.Fprintf(&r.body, "pub const %s: Method = Method {\n", constName)
			fmt.Fprintf(&r.body, "    service: %q,\n", svc.FullName)
			fmt.Fprintf(&r.body, "    name: %q,\n", m.Name)
			fmt.Fprintf(&r.body, "    full_method: %q,\n", m.FullMethod)
			if m.HTTP != nil {
				fmt.Fprintf(&r.body, "    http_verb: %q,\n", m.HTTP.Verb)
				fmt.Fprintf(&r.body, "    http_path: %q,\n", m.HTTP.Path)
			} else {
				r.body.WriteString("    http_verb: \"\",\n")
				r.body.WriteString("    http_path: \"\",\n")
			}
			r.body.WriteString("};\n\n")
		}
	}
}
