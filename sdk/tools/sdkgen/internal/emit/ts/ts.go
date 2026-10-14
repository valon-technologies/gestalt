// Package ts is the TypeScript SDK emitter. Each proto file renders as two
// modules: a public module with native types and per-service clients, and an
// internal codec module (src/internal/codec) with the wire-stub conversions,
// keeping the wire seam off the public surface.
package ts

import (
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

func (*Emitter) StaleScope() func(rel string) bool { return emit.ExcludePrefixScope("client/") }

// index resolves type references during rendering.
type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
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

	messages, enums := emit.Reachable(idx.messages, idx.enums, services)
	if err := set.Add("rpc_support.ts", []byte(supportFile)); err != nil {
		return nil, err
	}
	if emit.HasJSONResult(services) {
		if err := set.Add("invoke_support.ts", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("internal/codec/support.ts", []byte(runtimeFile)); err != nil {
		return nil, err
	}
	for _, g := range emit.GroupFiles(services, messages, enums) {
		outputBase := g.Base
		public := newRenderer(idx, outputBase, g.Base, modulePublic)
		for _, e := range g.Enums {
			public.renderEnum(e)
		}
		for _, m := range g.Messages {
			public.renderMessage(m)
		}
		for _, svc := range g.Services {
			public.renderClient(svc)
		}
		if err := set.Add(outputBase+".ts", []byte(public.assemble())); err != nil {
			return nil, err
		}

		if len(g.Messages) == 0 {
			continue
		}
		codec := newRenderer(idx, outputBase, g.Base, moduleCodec)
		for _, m := range g.Messages {
			codec.renderConversions(m)
		}
		if err := set.Add("internal/codec/"+outputBase+".ts", []byte(codec.assemble())); err != nil {
			return nil, err
		}
	}
	return set, nil
}
