package ts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

func EmitWebRuntime(plan *publicsurface.EmitPlan) (*fileset.FileSet, error) {
	set := fileset.New()
	if plan == nil || len(plan.View.Services) == 0 {
		return set, nil
	}

	idx := &index{
		messages: plan.MessageIndex,
		enums:    map[string]*model.Enum{},
	}
	for _, e := range plan.ReachableEnums {
		idx.enums[e.FullName] = e
	}

	if err := set.Add("rpc_support.ts", []byte(supportFile)); err != nil {
		return nil, err
	}
	if hasJsonResult(plan.Filtered.Services) {
		if err := set.Add("invoke_support.ts", []byte(invokeSupportFile)); err != nil {
			return nil, err
		}
	}
	if err := set.Add("internal/codec/support.ts", []byte(runtimeFile)); err != nil {
		return nil, err
	}

	codecImports := WebRuntimeImports()
	for _, g := range groupFiles(plan.Filtered.Services, plan.ReachableMessages, plan.ReachableEnums) {
		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, g.base, g.base, moduleCodec)
		codec.imports = &codecImports
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("internal/codec/"+g.base+".ts", []byte(codec.assemble())); err != nil {
			return nil, err
		}
	}

	nativeImports := PublicImports{SupportPrefix: ".", FixedNativeModule: "native-types.ts"}
	if err := set.Add("native-types.ts", []byte(renderWebNativeTypes(idx, plan.ReachableMessages, plan.ReachableEnums, nativeImports))); err != nil {
		return nil, err
	}
	return set, nil
}

func renderWebNativeTypes(idx *index, messages []*model.Message, enums []*model.Enum, imports PublicImports) string {
	supportTypes := map[string]bool{}
	var body strings.Builder
	groups := groupFiles(nil, messages, enums)
	for _, g := range groups {
		for _, e := range g.enums {
			r := newRenderer(idx, g.base, g.base, modulePublic)
			r.imports = &imports
			r.renderEnum(e)
			body.WriteString(r.body.String())
			for name := range r.features.supportTypes {
				supportTypes[name] = true
			}
		}
		for _, m := range g.messages {
			r := newRenderer(idx, g.base, g.base, modulePublic)
			r.imports = &imports
			r.renderWebNativeType(m)
			body.WriteString(r.body.String())
			for name := range r.features.supportTypes {
				supportTypes[name] = true
			}
		}
	}

	var b strings.Builder
	if len(supportTypes) > 0 {
		names := make([]string, 0, len(supportTypes))
		for name := range supportTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "import type { %s } from %s;\n\n", strings.Join(names, ", "), imports.supportModuleQuoted("rpc_support.ts"))
	}
	b.WriteString(body.String())
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func (r *renderer) renderWebNativeType(m *model.Message) {
	name := localName(m.FullName)
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "export type %s =\n", oneofTypeName(m, o))
		for _, f := range oneofFields(m, o) {
			fmt.Fprintf(&r.body, "  | { case: %q; value: %s }\n", f.JSONName, r.fieldType(f))
		}
		r.body.WriteString("  | { case: undefined; value?: undefined };\n\n")
	}
	r.writeDoc(m.Doc, "")
	fmt.Fprintf(&r.body, "export interface %s {\n", name)
	for _, f := range m.Fields {
		if f.OneofIndex >= 0 {
			continue
		}
		optional := ""
		if f.Presence == model.ExplicitPresence {
			optional = "?"
		}
		r.writeDoc(f.Doc, "  ")
		fmt.Fprintf(&r.body, "  %s%s: %s;\n", f.JSONName, optional, r.fieldType(f))
	}
	for _, o := range m.Oneofs {
		fmt.Fprintf(&r.body, "  %s: %s;\n", oneofProp(o), oneofTypeName(m, o))
	}
	r.body.WriteString("}\n\n")
}
