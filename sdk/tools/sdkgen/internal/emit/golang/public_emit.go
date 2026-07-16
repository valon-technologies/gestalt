package golang

import (
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
	transport.renderUnaryTransport()
	if err := set.Add("generated/unary_transport.go", []byte(transport.assembleUnaryTransport())); err != nil {
		return nil, err
	}

	types := newPublicRenderer(idx)
	for _, e := range plan.ReachableEnums {
		types.renderEnum(e)
	}
	for _, m := range plan.ReachableMessages {
		types.renderMessage(m)
	}
	if err := set.Add("generated/app.go", []byte(types.assembleGenerated())); err != nil {
		return nil, err
	}

	client := newPublicRenderer(idx)
	client.renderAppClient(plan.Filtered.Services)
	if err := set.Add("generated/app_client.go", []byte(client.assembleAppClient())); err != nil {
		return nil, err
	}

	if len(plan.ReachableMessages) > 0 {
		codec := newPublicRenderer(idx)
		for _, m := range plan.ReachableMessages {
			codec.renderConversions(m)
		}
		if err := set.Add("generated/app_codec.go", []byte(codec.assembleGenerated())); err != nil {
			return nil, err
		}
	}

	return set, nil
}
