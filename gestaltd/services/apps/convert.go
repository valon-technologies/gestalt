package plugins

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

func catalogFromProto(src *proto.Catalog) (*catalog.Catalog, error) {
	if src == nil {
		return nil, nil
	}
	cat := &catalog.Catalog{
		Name:        src.GetName(),
		DisplayName: src.GetDisplayName(),
		Description: src.GetDescription(),
		IconSVG:     src.GetIconSvg(),
		Operations:  make([]catalog.CatalogOperation, 0, len(src.GetOperations())),
	}
	for _, op := range src.GetOperations() {
		catOp := catalog.CatalogOperation{
			ID:             op.GetId(),
			Method:         op.GetMethod(),
			Title:          op.GetTitle(),
			Description:    op.GetDescription(),
			InputSchema:    jsonRawFromString(op.GetInputSchema()),
			OutputSchema:   jsonRawFromString(op.GetOutputSchema()),
			AllowedRoles:   op.GetAllowedRoles(),
			RequiredScopes: op.GetRequiredScopes(),
			Tags:           op.GetTags(),
			ReadOnly:       op.GetReadOnly(),
			Visible:        op.Visible,
			Transport:      op.GetTransport(),
		}
		if ann := op.GetAnnotations(); ann != nil {
			catOp.Annotations = catalog.CapabilityAnnotations{
				ReadOnlyHint:    ann.ReadOnlyHint,
				IdempotentHint:  ann.IdempotentHint,
				DestructiveHint: ann.DestructiveHint,
				OpenWorldHint:   ann.OpenWorldHint,
			}
		}
		for _, p := range op.GetParameters() {
			catOp.Parameters = append(catOp.Parameters, catalog.CatalogParameter{
				Name:        p.GetName(),
				Type:        p.GetType(),
				Description: p.GetDescription(),
				Required:    p.GetRequired(),
				Default:     protoutil.ValueToAny(p.GetDefault()),
			})
		}
		cat.Operations = append(cat.Operations, catOp)
	}
	if err := cat.Validate(); err != nil {
		return nil, err
	}
	return cat, nil
}

func catalogToProto(cat *catalog.Catalog) *proto.Catalog {
	if cat == nil {
		return nil
	}
	out := &proto.Catalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSvg:     cat.IconSVG,
		Operations:  make([]*proto.CatalogOperation, 0, len(cat.Operations)),
	}
	for i := range cat.Operations {
		op := &cat.Operations[i]
		pOp := &proto.CatalogOperation{
			Id:             op.ID,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    string(op.InputSchema),
			OutputSchema:   string(op.OutputSchema),
			AllowedRoles:   op.AllowedRoles,
			RequiredScopes: op.RequiredScopes,
			Tags:           op.Tags,
			ReadOnly:       op.ReadOnly,
			Visible:        op.Visible,
			Transport:      op.Transport,
		}
		ann := op.Annotations
		if ann.ReadOnlyHint != nil || ann.IdempotentHint != nil || ann.DestructiveHint != nil || ann.OpenWorldHint != nil {
			pOp.Annotations = &proto.OperationAnnotations{
				ReadOnlyHint:    ann.ReadOnlyHint,
				IdempotentHint:  ann.IdempotentHint,
				DestructiveHint: ann.DestructiveHint,
				OpenWorldHint:   ann.OpenWorldHint,
			}
		}
		for _, p := range op.Parameters {
			param := &proto.CatalogParameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
			}
			if p.Default != nil {
				if v, err := structpb.NewValue(p.Default); err == nil {
					param.Default = v
				}
			}
			pOp.Parameters = append(pOp.Parameters, param)
		}
		out.Operations = append(out.Operations, pOp)
	}
	return out
}

func jsonRawFromString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}
