// Package golang is the Go SDK emitter. Each proto file renders as two files
// in the generated client package: a public file with native types and
// per-service clients, and a codec file (<base>_codec.go) with the unexported
// wire-stub conversions, keeping the wire seam off the public surface.
package golang

import (
	"fmt"
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetGo }

func (*Emitter) OutputRoot() string { return "sdk/go" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Gofmt() }

func (*Emitter) StaleScope() func(rel string) bool { return emit.PrefixScope("client/") }

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

	messages, enums := emit.Reachable(idx.messages, idx.enums, services)
	if err := set.Add("client/rpc_support.go", []byte(supportFile)); err != nil {
		return nil, err
	}
	if emit.HasJSONResult(services) {
		if err := set.Add("client/support_invoke.go", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if contextType, err := requestContextType(idx, services); err != nil {
		return nil, err
	} else if contextType != "" {
		support := fmt.Sprintf(contextSupportFile, contextType)
		if err := set.Add("client/support_context.go", []byte(support)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("client/support_codec.go", []byte(codecSupportFile)); err != nil {
		return nil, err
	}
	for _, g := range groupFiles(services, messages, enums) {
		public := newRenderer(idx)
		outputBase := g.base
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		for _, svc := range g.services {
			public.renderClient(svc)
		}
		if err := set.Add("client/"+outputBase+".go", []byte(public.assemble())); err != nil {
			return nil, err
		}

		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("client/"+outputBase+"_codec.go", []byte(codec.assemble())); err != nil {
			return nil, err
		}
	}
	return set, nil
}

// requestContextType resolves the native type behind the services' request
// context fields, or "" when no service carries one. The client option
// machinery holds a single default, so every context field must agree.
func requestContextType(idx *index, services []*model.Service) (string, error) {
	contextType := ""
	for _, svc := range services {
		f := contextFieldOf(svc)
		if f == nil {
			continue
		}
		t := newRenderer(idx).fieldType(f)
		if contextType != "" && t != contextType {
			return "", fmt.Errorf("golang: conflicting request context types %s and %s", contextType, t)
		}
		contextType = t
	}
	return contextType, nil
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
