// Package publicgo is the Go public gestaltd client emitter. It renders
// native types, method metadata, REST clients for HTTP-annotated methods,
// and gRPC clients for every public unary method into sdk/go/publicclient.
package publicgo

import (
	"fmt"
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetGo }

func (*Emitter) OutputRoot() string { return "sdk/go/publicclient" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Gofmt() }

type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
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
		messages: map[string]*model.Message{},
		enums:    map[string]*model.Enum{},
	}
	for _, m := range schema.Messages {
		idx.messages[m.FullName] = m
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

	set := fileset.New()
	if len(view.Services) == 0 {
		return set, nil
	}

	messages, enums := reachable(idx, filtered.Services)
	if err := set.Add("generated/rpc_support.go", []byte(supportFile)); err != nil {
		return nil, err
	}
	if hasJsonResult(filtered.Services) {
		if err := set.Add("generated/invoke_support.go", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("generated/support_codec.go", []byte(codecSupportFile)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/rest_caller.go", []byte(restCallerFile)); err != nil {
		return nil, err
	}
	meta := newRenderer(idx)
	meta.renderMetadata(view)
	if err := set.Add("generated/metadata.go", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(filtered.Services, messages, enums) {
		public := newRenderer(idx)
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
		if err := set.Add("generated/"+g.base+".go", []byte(public.assembleGenerated())); err != nil {
			return nil, err
		}

		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/"+g.base+"_codec.go", []byte(codec.assembleGenerated())); err != nil {
			return nil, err
		}
	}
	return set, nil
}

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

func groupFiles(services []*model.Service, messages []*model.Message, enums []*model.Enum) []group {
	byBase := map[string]*group{}
	for _, svc := range services {
		base := generatedFileBase(svc.ProtoFile)
		g := byBase[base]
		if g == nil {
			g = &group{base: base}
			byBase[base] = g
		}
		g.services = append(g.services, svc)
	}
	for _, m := range messages {
		base := generatedFileBase(m.ProtoFile)
		g := byBase[base]
		if g == nil {
			g = &group{base: base}
			byBase[base] = g
		}
		g.messages = append(g.messages, m)
	}
	for _, e := range enums {
		base := generatedFileBase(e.ProtoFile)
		g := byBase[base]
		if g == nil {
			g = &group{base: base}
			byBase[base] = g
		}
		g.enums = append(g.enums, e)
	}
	var bases []string
	for base := range byBase {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	out := make([]group, 0, len(bases))
	for _, base := range bases {
		out = append(out, *byBase[base])
	}
	return out
}

func reachable(idx *index, services []*model.Service) ([]*model.Message, []*model.Enum) {
	seenMessages := map[string]bool{}
	seenEnums := map[string]bool{}
	var visit func(fullName string)
	visitRef := func(ref *model.TypeRef) {
		if ref == nil {
			return
		}
		switch ref.Kind {
		case model.KindMessage:
			visit(ref.Message)
		case model.KindEnum:
			seenEnums[ref.Enum] = true
		}
	}
	visit = func(fullName string) {
		if fullName == "" || seenMessages[fullName] {
			return
		}
		seenMessages[fullName] = true
		m := idx.messages[fullName]
		if m == nil {
			return
		}
		for _, f := range m.Fields {
			switch f.Kind {
			case model.KindMessage:
				visit(f.Message)
			case model.KindEnum:
				seenEnums[f.Enum] = true
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
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
	for name := range seenMessages {
		if m := idx.messages[name]; m != nil {
			messages = append(messages, m)
		}
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].FullName < messages[j].FullName })
	var enums []*model.Enum
	for name := range seenEnums {
		if e := idx.enums[name]; e != nil {
			enums = append(enums, e)
		}
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].FullName < enums[j].FullName })
	return messages, enums
}

func (r *renderer) renderMetadata(view *publicsurface.View) {
	r.body.WriteString("// Method metadata for the public gestaltd surface.\n\n")
	r.body.WriteString("// Method describes one public unary RPC.\n")
	r.body.WriteString("type Method struct {\n")
	r.body.WriteString("\tService string\n")
	r.body.WriteString("\tName string\n")
	r.body.WriteString("\tFullMethod string\n")
	r.body.WriteString("\tHTTPVerb string\n")
	r.body.WriteString("\tHTTPPath string\n")
	r.body.WriteString("}\n\n")

	for _, svc := range view.Services {
		wireName := localName(svc.FullName)
		for _, m := range svc.PublicMethods {
			constName := fmt.Sprintf("Method%s%s", wireName, m.Name)
			r.body.WriteString("var " + constName + " = Method{\n")
			fmt.Fprintf(&r.body, "\tService: %q,\n", svc.FullName)
			fmt.Fprintf(&r.body, "\tName: %q,\n", m.Name)
			fmt.Fprintf(&r.body, "\tFullMethod: %q,\n", m.FullMethod)
			if m.HTTP != nil {
				fmt.Fprintf(&r.body, "\tHTTPVerb: %q,\n", m.HTTP.Verb)
				fmt.Fprintf(&r.body, "\tHTTPPath: %q,\n", m.HTTP.Path)
			}
			r.body.WriteString("}\n\n")
		}
	}
}
