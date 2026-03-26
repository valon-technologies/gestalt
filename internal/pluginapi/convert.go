package pluginapi

import (
	"encoding/json"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/principal"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func coreConnectionModeToProto(mode core.ConnectionMode) pluginapiv1.ConnectionMode {
	switch mode {
	case core.ConnectionModeNone:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_NONE
	case core.ConnectionModeUser, "":
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_USER
	case core.ConnectionModeIdentity:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_IDENTITY
	case core.ConnectionModeEither:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_EITHER
	default:
		return pluginapiv1.ConnectionMode_CONNECTION_MODE_UNSPECIFIED
	}
}

func protoConnectionModeToCore(mode pluginapiv1.ConnectionMode) core.ConnectionMode {
	switch mode {
	case pluginapiv1.ConnectionMode_CONNECTION_MODE_NONE:
		return core.ConnectionModeNone
	case pluginapiv1.ConnectionMode_CONNECTION_MODE_USER, pluginapiv1.ConnectionMode_CONNECTION_MODE_UNSPECIFIED:
		return core.ConnectionModeUser
	case pluginapiv1.ConnectionMode_CONNECTION_MODE_IDENTITY:
		return core.ConnectionModeIdentity
	case pluginapiv1.ConnectionMode_CONNECTION_MODE_EITHER:
		return core.ConnectionModeEither
	default:
		return core.ConnectionModeUser
	}
}

func protoPrincipalSourceToCore(src pluginapiv1.PrincipalSource) principal.Source {
	switch src {
	case pluginapiv1.PrincipalSource_PRINCIPAL_SOURCE_API_TOKEN:
		return principal.SourceAPIToken
	case pluginapiv1.PrincipalSource_PRINCIPAL_SOURCE_ENV:
		return principal.SourceEnv
	case pluginapiv1.PrincipalSource_PRINCIPAL_SOURCE_SESSION, pluginapiv1.PrincipalSource_PRINCIPAL_SOURCE_UNSPECIFIED:
		return principal.SourceSession
	default:
		return principal.SourceSession
	}
}

func principalFromProto(msg *pluginapiv1.Principal) *principal.Principal {
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

func valueFromProto(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	return v.AsInterface()
}

func parameterToProto(p core.Parameter) (*pluginapiv1.Parameter, error) {
	def, err := valueToProto(p.Default)
	if err != nil {
		return nil, fmt.Errorf("parameter %q default: %w", p.Name, err)
	}
	return &pluginapiv1.Parameter{
		Name:         p.Name,
		Type:         p.Type,
		Description:  p.Description,
		Required:     p.Required,
		DefaultValue: def,
	}, nil
}

func parameterFromProto(msg *pluginapiv1.Parameter) core.Parameter {
	if msg == nil {
		return core.Parameter{}
	}
	return core.Parameter{
		Name:        msg.GetName(),
		Type:        msg.GetType(),
		Description: msg.GetDescription(),
		Required:    msg.GetRequired(),
		Default:     valueFromProto(msg.GetDefaultValue()),
	}
}

func parametersToProto(params []core.Parameter) ([]*pluginapiv1.Parameter, error) {
	out := make([]*pluginapiv1.Parameter, 0, len(params))
	for _, p := range params {
		msg, err := parameterToProto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func parametersFromProto(params []*pluginapiv1.Parameter) []core.Parameter {
	out := make([]core.Parameter, 0, len(params))
	for _, p := range params {
		out = append(out, parameterFromProto(p))
	}
	return out
}

func operationToProto(op core.Operation) (*pluginapiv1.Operation, error) {
	params, err := parametersToProto(op.Parameters)
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", op.Name, err)
	}
	return &pluginapiv1.Operation{
		Name:        op.Name,
		Description: op.Description,
		Method:      op.Method,
		Parameters:  params,
	}, nil
}

func operationFromProto(msg *pluginapiv1.Operation) core.Operation {
	if msg == nil {
		return core.Operation{}
	}
	return core.Operation{
		Name:        msg.GetName(),
		Description: msg.GetDescription(),
		Method:      msg.GetMethod(),
		Parameters:  parametersFromProto(msg.GetParameters()),
	}
}

func operationsToProto(ops []core.Operation) ([]*pluginapiv1.Operation, error) {
	out := make([]*pluginapiv1.Operation, 0, len(ops))
	for _, op := range ops {
		msg, err := operationToProto(op)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func operationsFromProto(ops []*pluginapiv1.Operation) []core.Operation {
	out := make([]core.Operation, 0, len(ops))
	for _, op := range ops {
		out = append(out, operationFromProto(op))
	}
	return out
}

func capabilityToProto(cap *core.Capability) (*pluginapiv1.Capability, error) {
	params, err := parametersToProto(cap.Parameters)
	if err != nil {
		return nil, fmt.Errorf("capability %q/%q: %w", cap.Provider, cap.Operation, err)
	}
	return &pluginapiv1.Capability{
		Provider:    cap.Provider,
		Operation:   cap.Operation,
		Description: cap.Description,
		Parameters:  params,
	}, nil
}

func capabilitiesToProto(caps []core.Capability) ([]*pluginapiv1.Capability, error) {
	out := make([]*pluginapiv1.Capability, 0, len(caps))
	for i := range caps {
		msg, err := capabilityToProto(&caps[i])
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func connectionParamDefsToProto(defs map[string]core.ConnectionParamDef) map[string]*pluginapiv1.ConnectionParamDef {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]*pluginapiv1.ConnectionParamDef, len(defs))
	for name, def := range defs {
		out[name] = &pluginapiv1.ConnectionParamDef{
			Required:     def.Required,
			Description:  def.Description,
			DefaultValue: def.Default,
			From:         def.From,
			Field:        def.Field,
		}
	}
	return out
}

func connectionParamDefsFromProto(defs map[string]*pluginapiv1.ConnectionParamDef) map[string]core.ConnectionParamDef {
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

func tokenResponseToProto(resp *core.TokenResponse) (*pluginapiv1.TokenResponse, error) {
	if resp == nil {
		return nil, nil
	}
	extra, err := structFromMap(resp.Extra)
	if err != nil {
		return nil, fmt.Errorf("token response extra: %w", err)
	}
	return &pluginapiv1.TokenResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresIn:    int32(resp.ExpiresIn),
		TokenType:    resp.TokenType,
		Extra:        extra,
	}, nil
}

func tokenResponseFromProto(msg *pluginapiv1.TokenResponse) *core.TokenResponse {
	if msg == nil {
		return nil
	}
	return &core.TokenResponse{
		AccessToken:  msg.GetAccessToken(),
		RefreshToken: msg.GetRefreshToken(),
		ExpiresIn:    int(msg.GetExpiresIn()),
		TokenType:    msg.GetTokenType(),
		Extra:        mapFromStruct(msg.GetExtra()),
	}
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

func protoPluginMode(mode string) pluginapiv1.PluginMode {
	switch mode {
	case config.PluginModeOverlay:
		return pluginapiv1.PluginMode_PLUGIN_MODE_OVERLAY
	case config.PluginModeReplace, "":
		return pluginapiv1.PluginMode_PLUGIN_MODE_REPLACE
	default:
		return pluginapiv1.PluginMode_PLUGIN_MODE_UNSPECIFIED
	}
}
