// Package publicpython emits the public gestaltd Python client under
// sdk/python/gestalt/public/generated/.
package publicpython

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

func (*Emitter) Target() emit.Target { return emit.TargetPython }

func (*Emitter) OutputRoot() string { return "sdk/python/gestalt/public" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Hash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Ruff() }

type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
	taken    map[string]bool
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
	idx.taken = takenNames(messages, enums)
	if err := set.Add("generated/rpc_support.py", []byte(supportFile)); err != nil {
		return nil, err
	}
	if hasJsonResult(filtered.Services) {
		if err := set.Add("generated/invoke_support.py", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("generated/_codec/__init__.py", []byte(codecInit)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/_codec/support.py", []byte(runtimeFile)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/rest_caller.py", []byte(restCallerFile)); err != nil {
		return nil, err
	}
	meta := newRenderer(idx, "metadata", "metadata", modulePublic)
	meta.renderMetadata(view)
	if err := set.Add("generated/metadata.py", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(filtered.Services, messages, enums) {
		public := newRenderer(idx, g.base, g.base, modulePublic)
		public.publicClient = true
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
		if err := set.Add("generated/"+g.base+".py", []byte(public.assembleGenerated())); err != nil {
			return nil, err
		}
		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, g.base, g.base, moduleCodec)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/_codec/"+g.base+".py", []byte(codec.assembleGenerated())); err != nil {
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

func takenNames(messages []*model.Message, enums []*model.Enum) map[string]bool {
	taken := map[string]bool{}
	for _, m := range messages {
		taken[localName(m.FullName)] = true
		for _, o := range m.Oneofs {
			taken[oneofTypeName(m, o)] = true
		}
	}
	for _, e := range enums {
		taken[localName(e.FullName)] = true
		taken[enumValuesClassName(e.FullName)] = true
	}
	return taken
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
	r.features.dataclass = true
	r.body.WriteString("@dataclass(frozen=True, slots=True)\n")
	r.body.WriteString("class Method:\n")
	r.body.WriteString("    service: str\n")
	r.body.WriteString("    name: str\n")
	r.body.WriteString("    full_method: str\n")
	r.body.WriteString("    http_verb: str = \"\"\n")
	r.body.WriteString("    http_path: str = \"\"\n\n")

	for _, svc := range view.Services {
		wireName := localName(svc.FullName)
		for _, m := range svc.PublicMethods {
			constName := fmt.Sprintf("METHOD_%s_%s", screamingSnake(wireName), screamingSnake(m.Name))
			fmt.Fprintf(&r.body, "%s = Method(\n", constName)
			fmt.Fprintf(&r.body, "    service=%q,\n", svc.FullName)
			fmt.Fprintf(&r.body, "    name=%q,\n", m.Name)
			fmt.Fprintf(&r.body, "    full_method=%q,\n", m.FullMethod)
			if m.HTTP != nil {
				fmt.Fprintf(&r.body, "    http_verb=%q,\n", m.HTTP.Verb)
				fmt.Fprintf(&r.body, "    http_path=%q,\n", m.HTTP.Path)
			}
			r.body.WriteString(")\n\n")
		}
	}
}
