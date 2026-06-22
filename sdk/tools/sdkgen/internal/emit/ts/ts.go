// Package ts is the TypeScript SDK emitter. Each proto file renders as two
// modules: a public module with native types and per-service clients, and an
// internal codec module (src/internal/codec) with the wire-stub conversions,
// keeping the wire seam off the public surface.
package ts

import (
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetTS }

func (*Emitter) OutputRoot() string { return "sdk/typescript/src" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Prettier() }

// index resolves type references during rendering.
type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
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
		messages: map[string]*model.Message{},
		enums:    map[string]*model.Enum{},
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
	publicBases := map[string]string{}
	for _, svc := range services {
		if rename := emit.ClientAliasFor(svc.Name); rename != nil {
			publicBases[generatedFileBase(svc.ProtoFile)] = strings.ToLower(rename.Target)
		}
	}
	if err := set.Add("rpc_support.ts", []byte(supportFile)); err != nil {
		return nil, err
	}
	if hasJsonResult(services) {
		if err := set.Add("invoke_support.ts", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("internal/codec/support.ts", []byte(runtimeFile)); err != nil {
		return nil, err
	}
	for _, g := range groupFiles(services, messages, enums) {
		outputBase := g.base
		var rename *emit.ClientAlias
		for _, svc := range g.services {
			rename = emit.ClientAliasFor(svc.Name)
			if rename != nil {
				outputBase = strings.ToLower(rename.Target)
			}
		}
		public := newRenderer(idx, outputBase, g.base, modulePublic, publicBases)
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		for _, svc := range g.services {
			public.renderClient(svc, rename)
		}
		if err := set.Add(outputBase+".ts", []byte(public.assemble())); err != nil {
			return nil, err
		}

		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, outputBase, g.base, moduleCodec, publicBases)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("internal/codec/"+outputBase+".ts", []byte(codec.assemble())); err != nil {
			return nil, err
		}
	}
	return set, nil
}

// hasJsonResult reports whether any method carries the json_result
// annotation, which pulls in the envelope decode runtime.
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

// reachable collects every message and enum referenced transitively from the
// selected services' method inputs and outputs.
func reachable(idx *index, services []*model.Service) ([]*model.Message, []*model.Enum) {
	seenMessages := map[string]bool{}
	seenEnums := map[string]bool{}
	var visit func(fullName string)
	visitRef := func(ref *model.TypeRef) {
		switch ref.Kind {
		case model.KindMessage:
			visit(ref.Message)
		case model.KindEnum:
			seenEnums[ref.Enum] = true
		}
	}
	visit = func(fullName string) {
		if seenMessages[fullName] {
			return
		}
		seenMessages[fullName] = true
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
				visit(method.Input.FullName)
			}
			if method.Output != nil {
				visit(method.Output.FullName)
			}
		}
	}

	var messages []*model.Message
	for fullName := range seenMessages {
		messages = append(messages, idx.messages[fullName])
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
