// Package golang is the Go SDK emitter. Each proto file renders as two files
// in the generated client package: a public file with native types and
// per-service clients, and a codec file (<base>_codec.go) with the unexported
// wire-stub conversions, keeping the wire seam off the public surface.
package golang

import (
	"fmt"

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
	for _, g := range emit.GroupFiles(services, messages, enums) {
		public := newRenderer(idx)
		outputBase := g.Base
		for _, e := range g.Enums {
			public.renderEnum(e)
		}
		for _, m := range g.Messages {
			public.renderMessage(m)
		}
		for _, svc := range g.Services {
			public.renderClient(svc)
		}
		if err := set.Add("client/"+outputBase+".go", []byte(public.assemble())); err != nil {
			return nil, err
		}

		if len(g.Messages) == 0 {
			continue
		}
		codec := newRenderer(idx)
		for _, m := range g.Messages {
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
		f := model.ContextField(svc)
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
