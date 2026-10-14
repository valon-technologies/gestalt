// Package python is the Python SDK emitter. Each proto file renders as two
// modules: a public module with native dataclasses and per-service clients,
// and an internal codec module (gestalt/_codec) with the wire-stub
// conversions, keeping the wire seam off the public surface.
package python

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPython }

func (*Emitter) OutputRoot() string { return "sdk/python/gestalt" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Hash }

// Formatter is nil: the emitter renders ruff-clean code directly.
func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Ruff() }

func (*Emitter) StaleScope() func(rel string) bool { return emit.ExcludePrefixScope("public/") }

// index resolves type references during rendering.
type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
	taken    map[string]bool // generated top-level type names across all files
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
	idx.taken = takenNames(messages, enums)
	if err := set.Add("rpc_support.py", []byte(providerSupportFile)); err != nil {
		return nil, err
	}
	if emit.HasJSONResult(services) {
		if err := set.Add("invoke_support.py", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("_codec/__init__.py", []byte(codecInit)); err != nil {
		return nil, err
	}
	if err := set.Add("_codec/support.py", []byte(runtimeFile)); err != nil {
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
		if err := set.Add(outputBase+".py", []byte(public.assemble())); err != nil {
			return nil, err
		}

		if len(g.Messages) == 0 {
			continue
		}
		codec := newRenderer(idx, outputBase, g.Base, moduleCodec)
		for _, m := range g.Messages {
			codec.renderConversions(m)
		}
		if err := set.Add("_codec/"+outputBase+".py", []byte(codec.assemble())); err != nil {
			return nil, err
		}
	}
	return set, nil
}

// takenNames collects every generated top-level type name, so oneof variant
// classes can avoid colliding with them.
func takenNames(messages []*model.Message, enums []*model.Enum) map[string]bool {
	taken := map[string]bool{}
	for _, m := range messages {
		taken[localName(m.FullName)] = true
		for _, o := range m.Oneofs {
			taken[oneofTypeName(m, o)] = true
		}
	}
	for _, e := range enums {
		taken[enumName(e)] = true
		taken[enumValuesClassName(e)] = true
	}
	return taken
}
