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
