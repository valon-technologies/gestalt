package pluginhost

import (
	"encoding/json"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/protobuf/types/known/structpb"
)

func coreConnectionModeToProto(mode core.ConnectionMode) proto.ConnectionMode {
	switch mode {
	case core.ConnectionModeNone:
		return proto.ConnectionMode_CONNECTION_MODE_NONE
	case core.ConnectionModeUser, "":
		return proto.ConnectionMode_CONNECTION_MODE_USER
	case core.ConnectionModeIdentity:
		return proto.ConnectionMode_CONNECTION_MODE_IDENTITY
	case core.ConnectionModeEither:
		return proto.ConnectionMode_CONNECTION_MODE_EITHER
	default:
		return proto.ConnectionMode_CONNECTION_MODE_UNSPECIFIED
	}
}

func protoConnectionModeToCore(mode proto.ConnectionMode) core.ConnectionMode {
	switch mode {
	case proto.ConnectionMode_CONNECTION_MODE_NONE:
		return core.ConnectionModeNone
	case proto.ConnectionMode_CONNECTION_MODE_USER, proto.ConnectionMode_CONNECTION_MODE_UNSPECIFIED:
		return core.ConnectionModeUser
	case proto.ConnectionMode_CONNECTION_MODE_IDENTITY:
		return core.ConnectionModeIdentity
	case proto.ConnectionMode_CONNECTION_MODE_EITHER:
		return core.ConnectionModeEither
	default:
		return core.ConnectionModeUser
	}
}

func protoPrincipalSourceToCore(src proto.PrincipalSource) principal.Source {
	switch src {
	case proto.PrincipalSource_PRINCIPAL_SOURCE_API_TOKEN:
		return principal.SourceAPIToken
	case proto.PrincipalSource_PRINCIPAL_SOURCE_ENV:
		return principal.SourceEnv
	case proto.PrincipalSource_PRINCIPAL_SOURCE_SESSION, proto.PrincipalSource_PRINCIPAL_SOURCE_UNSPECIFIED:
		return principal.SourceSession
	default:
		return principal.SourceSession
	}
}

func principalFromProto(msg *proto.Principal) *principal.Principal {
	if msg == nil {
		return nil
	}
	p := &principal.Principal{
		UserID: msg.GetUserId(),
		Source: protoPrincipalSourceToCore(msg.GetSource()),
	}
	if id := msg.GetIdentity(); id != nil {
		p.Identity = &core.UserIdentity{
			Email:       id.GetEmail(),
			DisplayName: id.GetDisplayName(),
			AvatarURL:   id.GetAvatarUrl(),
		}
	}
	return p
}

func structFromMap(values map[string]any) (*structpb.Struct, error) {
	if len(values) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(values)
}

func mapFromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

func valueToProto(v any) (*structpb.Value, error) {
	if v == nil {
		return nil, nil
	}
	return structpb.NewValue(v)
}

func parameterToProto(p core.Parameter) (*proto.Parameter, error) {
	def, err := valueToProto(p.Default)
	if err != nil {
		return nil, fmt.Errorf("parameter %q default: %w", p.Name, err)
	}
	return &proto.Parameter{
		Name:         p.Name,
		Type:         p.Type,
		Description:  p.Description,
		Required:     p.Required,
		DefaultValue: def,
	}, nil
}

func parametersToProto(params []core.Parameter) ([]*proto.Parameter, error) {
	out := make([]*proto.Parameter, 0, len(params))
	for _, p := range params {
		msg, err := parameterToProto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func capabilityToProto(cap *core.Capability) (*proto.Capability, error) {
	params, err := parametersToProto(cap.Parameters)
	if err != nil {
		return nil, fmt.Errorf("capability %q/%q: %w", cap.Provider, cap.Operation, err)
	}
	return &proto.Capability{
		Provider:    cap.Provider,
		Operation:   cap.Operation,
		Description: cap.Description,
		Parameters:  params,
	}, nil
}

func capabilitiesToProto(caps []core.Capability) ([]*proto.Capability, error) {
	out := make([]*proto.Capability, 0, len(caps))
	for i := range caps {
		msg, err := capabilityToProto(&caps[i])
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func connectionParamDefsToProto(defs map[string]core.ConnectionParamDef) map[string]*proto.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]*proto.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = &proto.ConnectionParamDef{
			Required:     def.Required,
			Description:  def.Description,
			DefaultValue: def.Default,
			From:         def.From,
			Field:        def.Field,
		}
	}
	return out
}

func connectionParamDefsFromProto(defs map[string]*proto.ConnectionParamDef) map[string]core.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]core.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = core.ConnectionParamDef{
			Required:    def.GetRequired(),
			Description: def.GetDescription(),
			Default:     def.GetDefaultValue(),
			From:        def.GetFrom(),
			Field:       def.GetField(),
		}
	}
	return out
}

func catalogToJSON(cat *catalog.Catalog) (string, error) {
	if cat == nil {
		return "", nil
	}
	data, err := json.Marshal(cat)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func catalogFromJSON(raw string) (*catalog.Catalog, error) {
	if raw == "" {
		return nil, nil
	}
	var cat catalog.Catalog
	if err := json.Unmarshal([]byte(raw), &cat); err != nil {
		return nil, err
	}
	if err := cat.Validate(); err != nil {
		return nil, err
	}
	return &cat, nil
}
