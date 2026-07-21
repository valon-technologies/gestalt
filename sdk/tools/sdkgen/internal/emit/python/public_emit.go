package python

import (
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)


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

 	sharedLocal := map[string]bool{}
 	sharedCodecFn := map[string]bool{}
 	for fullName := range plan.SharedMessages {
 		sharedLocal[localName(fullName)] = true
 		sharedCodecFn[toWireFunc(fullName)] = true
 		sharedCodecFn[fromWireFunc(fullName)] = true
 	}
 	for _, e := range plan.ReachableEnums {
 		sharedLocal[localName(e.FullName)] = true
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
	if err := set.Add("generated/unary_transport.py", []byte(unaryTransportFile)); err != nil {
		return nil, err
	}

 	meta := newRenderer(idx, "metadata", "metadata", modulePublic)
	meta.publicClient = true
 	meta.shared = sharedLocal
 	meta.sharedCodec = sharedCodecFn
	meta.renderMetadata(plan.Methods)
	if err := set.Add("generated/metadata.py", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(plan.Filtered.Services, plan.ReachableMessages, plan.ReachableEnums) {
 		public := newRenderer(idx, g.base, g.base, modulePublic)
		public.publicClient = true
 		public.shared = sharedLocal
 		public.sharedCodec = sharedCodecFn
 		hasProjected := false
		for _, m := range g.messages {
 			if plan.SharedMessages[m.FullName] {
 				continue
 			}
 			public.renderMessage(m)
 			hasProjected = true
		}
 		if hasProjected {
 			if err := set.Add("generated/"+g.base+".py", []byte(public.assembleGenerated())); err != nil {
 				return nil, err
 			}
		}
 		if !hasProjected {
			continue
		}
 		codec := newRenderer(idx, g.base, g.base, moduleCodec)
		codec.publicClient = true
 		codec.shared = sharedLocal
 		codec.sharedCodec = sharedCodecFn
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
 		if codec.body.Len() > 0 {
 			if err := set.Add("generated/_codec/"+g.base+".py", []byte(codec.assembleGenerated())); err != nil {
 				return nil, err
 			}
		}
	}

	for _, svc := range plan.Filtered.Services {
		wireBase := generatedFileBase(svc.ProtoFile)
		clientFile := serviceClientFile(svc.Name)
 		client := newRenderer(idx, strings.TrimSuffix(clientFile, ".py"), wireBase, modulePublic)
		client.publicClient = true
 		client.shared = sharedLocal
 		client.sharedCodec = sharedCodecFn
		client.docIntro = "Generated transport-neutral " + svc.Name + " client for the public gestaltd surface."
		client.renderAppClient(svc)
		if err := set.Add("generated/"+clientFile, []byte(client.assembleGenerated())); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func serviceClientFile(serviceName string) string {
	switch serviceName {
	case "ExternalCredentials":
		return "external_credentials_client.py"
	case "IndexedDB":
		return "indexeddb_client.py"
	default:
		return strings.ToLower(serviceName[:1]) + serviceName[1:] + "_client.py"
	}
}
