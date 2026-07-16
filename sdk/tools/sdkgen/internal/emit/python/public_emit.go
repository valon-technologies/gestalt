package python

import (
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

var publicRuntimeFile = strings.Replace(runtimeFile, "from ..rpc_support", "from gestalt.rpc_support", 1)

// EmitPublic renders the public gestaltd Python client under
// sdk/python/gestalt/public/generated/.
func EmitPublic(schema *model.Schema) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		return nil, err
	}

	set := fileset.New()
	if len(plan.View.Services) == 0 {
		return set, nil
	}

	idx := &index{
		messages: plan.MessageIndex,
		enums:    map[string]*model.Enum{},
	}
	for _, e := range plan.ReachableEnums {
		idx.enums[e.FullName] = e
	}
	idx.taken = takenNames(plan.ReachableMessages, plan.ReachableEnums)

	if err := set.Add("generated/_codec/__init__.py", []byte(codecInit)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/_codec/support.py", []byte(publicRuntimeFile)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/unary_transport.py", []byte(unaryTransportFile)); err != nil {
		return nil, err
	}

	meta := newRenderer(idx, "metadata", "metadata", modulePublic)
	meta.publicClient = true
	meta.renderMetadata(plan.Methods)
	if err := set.Add("generated/metadata.py", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(plan.Filtered.Services, plan.ReachableMessages, plan.ReachableEnums) {
		public := newRenderer(idx, g.base, g.base, modulePublic)
		public.publicClient = true
		for _, e := range g.enums {
			public.renderEnum(e)
		}
		for _, m := range g.messages {
			public.renderMessage(m)
		}
		if err := set.Add("generated/"+g.base+".py", []byte(public.assembleGenerated())); err != nil {
			return nil, err
		}
		if len(g.messages) == 0 {
			continue
		}
		codec := newRenderer(idx, g.base, g.base, moduleCodec)
		codec.publicClient = true
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/_codec/"+g.base+".py", []byte(codec.assembleGenerated())); err != nil {
			return nil, err
		}
	}

	client := newRenderer(idx, "app_client", "app", modulePublic)
	client.publicClient = true
	client.docIntro = "Generated transport-neutral App client for the public gestaltd surface."
	for _, svc := range plan.Filtered.Services {
		client.renderAppClient(svc)
	}
	if err := set.Add("generated/app_client.py", []byte(client.assembleGenerated())); err != nil {
		return nil, err
	}
	return set, nil
}
