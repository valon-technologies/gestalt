package gestalt

import "encoding/json"

const bridgeTransportPlugin = "plugin"

type bridgeCatalog struct {
	Name        string                   `json:"name"`
	DisplayName string                   `json:"displayName"`
	Description string                   `json:"description"`
	IconSVG     string                   `json:"iconSvg,omitempty"`
	BaseURL     string                   `json:"baseUrl,omitempty"`
	AuthStyle   string                   `json:"authStyle,omitempty"`
	Headers     map[string]string        `json:"headers,omitempty"`
	Operations  []bridgeCatalogOperation `json:"operations"`
}

type bridgeCatalogOperation struct {
	ID             string                   `json:"id"`
	ProviderID     string                   `json:"providerId,omitempty"`
	Method         string                   `json:"method"`
	Path           string                   `json:"path"`
	Title          string                   `json:"title,omitempty"`
	Description    string                   `json:"description,omitempty"`
	InputSchema    json.RawMessage          `json:"inputSchema,omitempty"`
	OutputSchema   json.RawMessage          `json:"outputSchema,omitempty"`
	Annotations    OperationAnnotations     `json:"annotations,omitempty"`
	Parameters     []bridgeCatalogParameter `json:"parameters,omitempty"`
	RequiredScopes []string                 `json:"requiredScopes,omitempty"`
	Tags           []string                 `json:"tags,omitempty"`
	ReadOnly       bool                     `json:"readOnly,omitempty"`
	Visible        *bool                    `json:"visible,omitempty"`
	Transport      string                   `json:"transport,omitempty"`
	Query          string                   `json:"query,omitempty"`
}

type bridgeCatalogParameter struct {
	Name        string `json:"name"`
	WireName    string `json:"wireName,omitempty"`
	Type        string `json:"type"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
}

func bridgeCatalogFromPublic(cat *Catalog) bridgeCatalog {
	out := bridgeCatalog{
		Name:        cat.Name,
		DisplayName: cat.DisplayName,
		Description: cat.Description,
		IconSVG:     cat.IconSVG,
		Operations:  make([]bridgeCatalogOperation, len(cat.Operations)),
	}
	for i := range cat.Operations {
		op := cat.Operations[i]
		out.Operations[i] = bridgeCatalogOperation{
			ID:             op.ID,
			Method:         op.Method,
			Title:          op.Title,
			Description:    op.Description,
			InputSchema:    op.InputSchema,
			OutputSchema:   op.OutputSchema,
			Annotations:    op.Annotations,
			Parameters:     bridgeParametersFromPublic(op.Parameters),
			RequiredScopes: op.RequiredScopes,
			Tags:           op.Tags,
			ReadOnly:       op.ReadOnly,
			Visible:        op.Visible,
			Transport:      bridgeTransportPlugin,
		}
	}
	return out
}

func bridgeParametersFromPublic(params []CatalogParameter) []bridgeCatalogParameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]bridgeCatalogParameter, len(params))
	for i := range params {
		param := params[i]
		out[i] = bridgeCatalogParameter{
			Name:        param.Name,
			Type:        param.Type,
			Description: param.Description,
			Required:    param.Required,
			Default:     param.Default,
		}
	}
	return out
}
