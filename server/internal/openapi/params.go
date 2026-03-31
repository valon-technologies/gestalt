package openapi

import (
	"strings"

	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

type operationParam struct {
	Name        string
	WireName    string
	Type        string
	Location    string
	Description string
	Required    bool
}

func parseOperationParam(p *v3high.Parameter) operationParam {
	paramType := "string"
	if p.Schema != nil && p.Schema.Schema() != nil {
		if types := p.Schema.Schema().Type; len(types) > 0 {
			paramType = types[0]
		}
	}
	name, wireName := normalizeParamName(p.Name)
	return operationParam{
		Name:        name,
		WireName:    wireName,
		Type:        paramType,
		Location:    p.In,
		Description: p.Description,
		Required:    p.Required != nil && *p.Required,
	}
}

func catalogParamFromOpenAPI(p *v3high.Parameter) catalog.CatalogParameter {
	param := parseOperationParam(p)
	return catalog.CatalogParameter{
		Name:        param.Name,
		WireName:    param.WireName,
		Type:        param.Type,
		Location:    param.Location,
		Description: param.Description,
		Required:    param.Required,
	}
}

func definitionParamFromOpenAPI(p *v3high.Parameter) provider.ParameterDef {
	param := parseOperationParam(p)
	return provider.ParameterDef{
		Name:        param.Name,
		WireName:    param.WireName,
		Type:        param.Type,
		Location:    param.Location,
		Description: param.Description,
		Required:    param.Required,
	}
}

func normalizeParamName(raw string) (name, wireName string) {
	if !strings.ContainsAny(raw, "[]") {
		return raw, ""
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "[", "_"), "]", "")
	return normalized, raw
}
