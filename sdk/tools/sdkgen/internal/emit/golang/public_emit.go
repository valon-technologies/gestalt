package golang

import (
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// EmitPublic renders the public Go gestaltd client into sdk/go/publicclient.
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

	if err := set.Add("generated/rpc_support.go", []byte(publicRPCSupportFile)); err != nil {
		return nil, err
	}
	if err := set.Add("generated/support_codec.go", []byte(publicCodecSupportFile)); err != nil {
		return nil, err
	}

	meta := newPublicRenderer(idx)
	meta.renderMetadata(plan.Methods)
	if err := set.Add("generated/metadata.go", []byte(meta.assembleGenerated())); err != nil {
		return nil, err
	}

	transport := newPublicRenderer(idx)
	transport.renderTransport()
	if err := set.Add("generated/transport.go", []byte(transport.assembleTransport())); err != nil {
		return nil, err
	}

	for _, g := range groupFiles(plan.Filtered.Services, plan.ReachableMessages, plan.ReachableEnums) {
		types := newPublicRenderer(idx)
		for _, e := range g.enums {
			types.renderEnum(e)
		}
		for _, m := range g.messages {
			types.renderMessage(m)
		}
		if err := set.Add("generated/"+g.base+".go", []byte(types.assembleGenerated())); err != nil {
			return nil, err
		}
		if len(g.messages) == 0 {
			continue
		}
		codec := newPublicRenderer(idx)
		for _, m := range g.messages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/"+g.base+"_codec.go", []byte(codec.assembleGenerated())); err != nil {
			return nil, err
		}
	}

	for _, svc := range plan.Filtered.Services {
		client := newPublicRenderer(idx)
		client.renderServiceClient(svc)
		base := serviceClientBase(svc.Name)
		if err := set.Add("generated/"+base+"_client.go", []byte(client.assembleServiceClient())); err != nil {
			return nil, err
		}
	}

	return set, nil
}

func serviceClientBase(serviceName string) string {
	switch serviceName {
	case "ExternalCredentials":
		return "external_credentials"
	default:
		return lowerFirst(serviceName)
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
