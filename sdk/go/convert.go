package gestalt

import (
	"encoding/json"
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func coreConnectionModeToProto(mode ConnectionMode) proto.ConnectionMode {
	switch mode {
	case ConnectionModeNone, "":
		return proto.ConnectionMode_CONNECTION_MODE_NONE
	case ConnectionModeUser:
		return proto.ConnectionMode_CONNECTION_MODE_USER
	case ConnectionModeIdentity:
		return proto.ConnectionMode_CONNECTION_MODE_IDENTITY
	case ConnectionModeEither:
		return proto.ConnectionMode_CONNECTION_MODE_EITHER
	default:
		return proto.ConnectionMode_CONNECTION_MODE_UNSPECIFIED
	}
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

func parameterToProto(p Parameter) (*proto.Parameter, error) {
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

func parametersToProto(params []Parameter) ([]*proto.Parameter, error) {
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

func principalSourceToProto(src PrincipalSource) proto.PrincipalSource {
	switch src {
	case PrincipalSourceSession:
		return proto.PrincipalSource_PRINCIPAL_SOURCE_SESSION
	case PrincipalSourceAPIToken:
		return proto.PrincipalSource_PRINCIPAL_SOURCE_API_TOKEN
	case PrincipalSourceEnv:
		return proto.PrincipalSource_PRINCIPAL_SOURCE_ENV
	default:
		return proto.PrincipalSource_PRINCIPAL_SOURCE_UNSPECIFIED
	}
}

func principalToProto(p Principal) *proto.Principal {
	msg := &proto.Principal{
		UserId: p.UserID,
		Source: principalSourceToProto(p.Source),
	}
	if p.Identity != nil {
		msg.Identity = &proto.UserIdentity{
			Email:       p.Identity.Email,
			DisplayName: p.Identity.DisplayName,
			AvatarUrl:   p.Identity.AvatarURL,
		}
	}
	return msg
}

func parameterFromProto(msg *proto.Parameter) Parameter {
	if msg == nil {
		return Parameter{}
	}
	var def any
	if msg.GetDefaultValue() != nil {
		def = msg.GetDefaultValue().AsInterface()
	}
	return Parameter{
		Name:        msg.GetName(),
		Type:        msg.GetType(),
		Description: msg.GetDescription(),
		Required:    msg.GetRequired(),
		Default:     def,
	}
}

func parametersFromProto(params []*proto.Parameter) []Parameter {
	out := make([]Parameter, 0, len(params))
	for _, p := range params {
		out = append(out, parameterFromProto(p))
	}
	return out
}

func capabilityFromProto(msg *proto.Capability) Capability {
	if msg == nil {
		return Capability{}
	}
	return Capability{
		Provider:    msg.GetProvider(),
		Operation:   msg.GetOperation(),
		Description: msg.GetDescription(),
		Parameters:  parametersFromProto(msg.GetParameters()),
	}
}

func capabilitiesFromProto(caps []*proto.Capability) []Capability {
	out := make([]Capability, 0, len(caps))
	for _, c := range caps {
		out = append(out, capabilityFromProto(c))
	}
	return out
}

func connectionParamDefsToProto(defs map[string]ConnectionParamDef) map[string]*proto.ConnectionParamDef {
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

func catalogToJSON(cat *Catalog) (string, error) {
	if cat == nil {
		return "", nil
	}

	type wireParameter struct {
		Name        string `json:"name"`
		WireName    string `json:"wireName,omitempty"`
		Type        string `json:"type"`
		Location    string `json:"location,omitempty"`
		Description string `json:"description,omitempty"`
		Required    bool   `json:"required,omitempty"`
		Default     any    `json:"default,omitempty"`
	}

	type wireOperation struct {
		ID             string               `json:"id"`
		ProviderID     string               `json:"providerId,omitempty"`
		Method         string               `json:"method"`
		Path           string               `json:"path"`
		Title          string               `json:"title,omitempty"`
		Description    string               `json:"description,omitempty"`
		InputSchema    json.RawMessage      `json:"inputSchema,omitempty"`
		OutputSchema   json.RawMessage      `json:"outputSchema,omitempty"`
		Annotations    OperationAnnotations `json:"annotations,omitempty"`
		Parameters     []wireParameter      `json:"parameters,omitempty"`
		RequiredScopes []string             `json:"requiredScopes,omitempty"`
		Tags           []string             `json:"tags,omitempty"`
		ReadOnly       bool                 `json:"readOnly,omitempty"`
		Visible        *bool                `json:"visible,omitempty"`
		Transport      string               `json:"transport,omitempty"`
		Query          string               `json:"query,omitempty"`
	}

	type wireCatalog struct {
		Name        string            `json:"name"`
		DisplayName string            `json:"displayName"`
		Description string            `json:"description"`
		IconSVG     string            `json:"iconSvg,omitempty"`
		BaseURL     string            `json:"baseUrl,omitempty"`
		AuthStyle   string            `json:"authStyle,omitempty"`
		Headers     map[string]string `json:"headers,omitempty"`
		Operations  []wireOperation   `json:"operations"`
	}

	wireOps := make([]wireOperation, len(cat.Operations))
	for i := range cat.Operations {
		op := cat.Operations[i]
		wireParams := make([]wireParameter, len(op.Parameters))
		for j := range op.Parameters {
			param := op.Parameters[j]
			wireParams[j] = wireParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			}
		}
		wireOps[i] = wireOperation{
			ID:             op.ID,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    op.InputSchema,
			OutputSchema:   op.OutputSchema,
			Annotations:    op.Annotations,
			Parameters:     wireParams,
			RequiredScopes: op.RequiredScopes,
			Tags:           op.Tags,
			ReadOnly:       op.ReadOnly,
			Visible:        op.Visible,
			Transport:      "plugin",
		}
	}

	wireCat := wireCatalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSVG:     cat.IconSVG,
		Operations:  wireOps,
	}

	data, err := json.Marshal(wireCat)
	if err != nil {
		return "", fmt.Errorf("marshal catalog: %w", err)
	}
	return string(data), nil
}
