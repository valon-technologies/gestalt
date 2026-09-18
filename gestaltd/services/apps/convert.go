package plugins

import (
	"encoding/json"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

	"google.golang.org/protobuf/encoding/protojson"
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
		api, err := apiExposureModeFromProto(op)
		if err != nil {
			return nil, err
		}
		catOp := catalog.CatalogOperation{
			ID:             op.GetId(),
			Method:         op.GetMethod(),
			Title:          op.GetTitle(),
			Description:    op.GetDescription(),
			InputSchema:    jsonRawFromString(op.GetInputSchema()),
			Response:       responseSpecFromProto(op.GetResponse()),
			AllowedRoles:   op.GetAllowedRoles(),
			OutputSchema:   legacyOutputSchemaFromResponse(op.GetResponse()),
			RequiredScopes: op.GetRequiredScopes(),
			Tags:           op.GetTags(),
			ReadOnly:       op.GetReadOnly(),
			Visible:        op.Visible,
			API:            api,
			MCP:            op.Mcp,
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
		api, apiMode := apiExposureModeToProto(op.API)
		pOp := &proto.CatalogOperation{
			Id:             op.ID,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    string(op.InputSchema),
			Response:       responseSpecToProto(op.Response, op.OutputSchema),
			AllowedRoles:   op.AllowedRoles,
			RequiredScopes: op.RequiredScopes,
			Tags:           op.Tags,
			ReadOnly:       op.ReadOnly,
			Visible:        op.Visible,
			Api:            api,
			ApiMode:        apiMode,
			Mcp:            op.MCP,
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

func apiExposureModeFromProto(op *proto.CatalogOperation) (*catalog.APIExposureMode, error) {
	if op == nil {
		return nil, nil
	}
	if op.ApiMode != nil {
		if *op.ApiMode != proto.APIExposureMode_API_EXPOSURE_MODE_BROWSER_SESSION {
			return nil, fmt.Errorf("unknown api exposure mode %d for operation %q", *op.ApiMode, op.GetId())
		}
		mode := catalog.APIExposureBrowserSession
		return &mode, nil
	}
	if op.Api == nil {
		return nil, nil
	}
	mode := catalog.APIExposurePrivate
	if *op.Api {
		mode = catalog.APIExposurePublic
	}
	return &mode, nil
}

func apiExposureModeToProto(mode *catalog.APIExposureMode) (*bool, *proto.APIExposureMode) {
	if mode == nil {
		return nil, nil
	}
	if mode.IsBrowserSession() {
		api := false
		browser := proto.APIExposureMode_API_EXPOSURE_MODE_BROWSER_SESSION
		return &api, &browser
	}
	api := mode.IsPublic()
	return &api, nil
}

func jsonRawFromString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

// legacyOutputSchemaFromResponse extracts the unary schema from a proto
// OperationResponseSpec as a json.RawMessage, so legacy readers that still
// inspect CatalogOperation.OutputSchema (MCP, appregistry, agent tools)
// continue to see the schema after the output_schema → response migration.
func legacyOutputSchemaFromResponse(resp *proto.OperationResponseSpec) json.RawMessage {
	if resp == nil {
		return nil
	}
	if u := resp.GetUnary(); u != nil {
		return structToJSONRaw(u.GetSchema())
	}
	return nil
}

func responseSpecFromProto(resp *proto.OperationResponseSpec) *catalog.OperationResponseSpec {
	if resp == nil {
		return nil
	}
	out := &catalog.OperationResponseSpec{}
	if u := resp.GetUnary(); u != nil {
		out.Unary = &catalog.UnaryResponseSpec{Schema: structToJSONRaw(u.GetSchema())}
	}
	if s := resp.GetStream(); s != nil {
		out.Stream = &catalog.StreamResponseSpec{
			MediaType:  s.GetMediaType(),
			ItemSchema: structToJSONRaw(s.GetItemSchema()),
		}
	}
	return out
}

func responseSpecToProto(spec *catalog.OperationResponseSpec, legacyOutputSchema json.RawMessage) *proto.OperationResponseSpec {
	if spec != nil {
		if spec.Stream != nil {
			return &proto.OperationResponseSpec{
				Kind: &proto.OperationResponseSpec_Stream{
					Stream: &proto.StreamResponseSpec{
						MediaType:  spec.Stream.MediaType,
						ItemSchema: jsonRawToStruct(spec.Stream.ItemSchema),
					},
				},
			}
		}
		if spec.Unary != nil {
			return &proto.OperationResponseSpec{
				Kind: &proto.OperationResponseSpec_Unary{
					Unary: &proto.UnaryResponseSpec{
						Schema: jsonRawToStruct(spec.Unary.Schema),
					},
				},
			}
		}
	}
	if len(legacyOutputSchema) == 0 {
		return nil
	}
	return &proto.OperationResponseSpec{
		Kind: &proto.OperationResponseSpec_Unary{
			Unary: &proto.UnaryResponseSpec{
				Schema: jsonRawToStruct(legacyOutputSchema),
			},
		},
	}
}

// structToJSONRaw converts a protobuf Struct to a JSON RawMessage.
func structToJSONRaw(s *structpb.Struct) json.RawMessage {
	if s == nil {
		return nil
	}
	data, err := protojson.Marshal(s)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}

// jsonRawToStruct converts a JSON RawMessage to a protobuf Struct.
func jsonRawToStruct(raw json.RawMessage) *structpb.Struct {
	if len(raw) == 0 {
		return nil
	}
	st := &structpb.Struct{}
	if err := protojson.Unmarshal(raw, st); err != nil {
		return nil
	}
	return st
}
