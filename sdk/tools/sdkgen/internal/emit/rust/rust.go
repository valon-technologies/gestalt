// Package rust is the Rust SDK emitter. Each proto file renders as two
// modules: a public module with native types and per-service clients, and a
// crate-private codec module (src/codec) with the wire-stub conversions,
// keeping the wire seam off the public surface.
package rust

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetRust }

func (*Emitter) OutputRoot() string { return "sdk/rust/src" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Rustfmt() }

// index resolves type references during rendering and records which
// conversion direction each reachable message needs: messages reached from
// method inputs convert to the wire, messages reached from outputs convert
// from it.
type index struct {
	messages     map[string]*model.Message
	enums        map[string]*model.Enum
	needToWire   map[string]bool
	needFromWire map[string]bool
}

// group is one generated file: every service, message, and enum declared by
// one proto file.
type group struct {
	base     string
	services []*model.Service
	messages []*model.Message
	enums    []*model.Enum
}

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	idx := &index{
		messages:     map[string]*model.Message{},
		enums:        map[string]*model.Enum{},
		needToWire:   map[string]bool{},
		needFromWire: map[string]bool{},
	}
	for _, m := range schema.Messages {
		idx.messages[m.FullName] = m
	}
	for _, e := range schema.Enums {
		idx.enums[e.FullName] = e
	}

	services := schema.Services
	set := fileset.New()
	if len(services) == 0 {
		return set, nil
	}

	messages, enums := reachable(idx, services)
	supportUses := map[string]bool{}
	codecModules := []string{"support"}
	for _, g := range groupFiles(services, messages, enums) {
		public := newRenderer(idx, g.base, modulePublic)
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		for _, svc := range g.services {
			public.renderClient(svc)
		}
		if err := set.Add(g.base+".rs", []byte(public.assemble())); err != nil {
			return nil, err
		}
		for name := range public.features.supportFns {
			supportUses[name] = true
		}

		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, g.base, moduleCodec)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("codec/"+g.base+".rs", []byte(codec.assemble())); err != nil {
			return nil, err
		}
		codecModules = append(codecModules, g.base)
		for name := range codec.features.supportFns {
			supportUses[name] = true
		}
	}
	if err := set.Add("rpc_support.rs", []byte(supportFile)); err != nil {
		return nil, err
	}
	for _, svc := range services {
		emitted := false
		for _, m := range svc.Methods {
			if m.JsonResult != nil {
				if err := set.Add("invoke_support.rs", []byte(invokeSupportFile)); err != nil {
					return nil, err
				}
				emitted = true
				break
			}
		}
		if emitted {
			break
		}
	}
	// The host-service transport is shared by every host-bound client; it is
	// crate-private, so it lives in the codec tree the generated index owns.
	for _, svc := range services {
		if svc.HostBinding == "" {
			continue
		}
		if err := set.Add("codec/host_service.rs", []byte(hostServiceFile)); err != nil {
			return nil, err
		}
		codecModules = append(codecModules, "host_service")
		break
	}
	if err := set.Add("codec.rs", []byte(renderCodecIndex(codecModules))); err != nil {
		return nil, err
	}
	if err := set.Add("codec/support.rs", []byte(renderCodecSupport(supportUses))); err != nil {
		return nil, err
	}
	return set, nil
}

// renderCodecIndex emits codec.rs, the crate-private module index declaring
// one converter module per proto file plus the shared support module. The
// codec root is registered as a private `mod codec;` in the handwritten
// lib.rs, so everything underneath stays off the public surface.
func renderCodecIndex(modules []string) string {
	sorted := append([]string(nil), modules...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("//! Crate-private wire converters for the generated clients: one module per\n")
	b.WriteString("//! proto file plus the shared well-known-type support module.\n\n")
	for _, m := range sorted {
		fmt.Fprintf(&b, "pub(crate) mod %s;\n", m)
	}
	return b.String()
}

// reachable collects every message and enum referenced transitively from the
// selected services' method inputs and outputs, filling the index's
// per-direction conversion needs along the way.
func reachable(idx *index, services []*model.Service) ([]*model.Message, []*model.Enum) {
	seenEnums := map[string]bool{}
	var visit func(need map[string]bool, fullName string)
	visit = func(need map[string]bool, fullName string) {
		if need[fullName] {
			return
		}
		need[fullName] = true
		visitRef := func(ref *model.TypeRef) {
			switch ref.Kind {
			case model.KindMessage:
				visit(need, ref.Message)
			case model.KindEnum:
				seenEnums[ref.Enum] = true
			}
		}
		for _, f := range idx.messages[fullName].Fields {
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
				messages = append(messages, idx.messages[fullName])
			}
		}
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].FullName < messages[j].FullName })
	var enums []*model.Enum
	for fullName := range seenEnums {
		enums = append(enums, idx.enums[fullName])
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].FullName < enums[j].FullName })
	return messages, enums
}

// groupFiles assigns services, messages, and enums to generated files by
// their declaring proto file.
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
