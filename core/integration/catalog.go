package integration

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/catalog"
)

func LoadCatalogYAML(data []byte) (*catalog.Catalog, error) {
	return catalog.LoadCatalogYAML(data)
}

func MustLoadCatalogYAML(data []byte) *catalog.Catalog {
	return catalog.MustLoadCatalogYAML(data)
}

func OperationsList(c *catalog.Catalog) []core.Operation {
	ops := make([]core.Operation, 0, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		params := make([]core.Parameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, core.Parameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		ops = append(ops, core.Operation{
			Name:        op.ID,
			Description: op.Description,
			Method:      strings.ToUpper(strings.TrimSpace(op.Method)),
			Parameters:  params,
		})
	}
	return ops
}

func EndpointsMap(c *catalog.Catalog) map[string]Endpoint {
	endpoints := make(map[string]Endpoint, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		endpoints[op.ID] = Endpoint{
			Method: strings.ToUpper(strings.TrimSpace(op.Method)),
			Path:   op.Path,
		}
	}
	return endpoints
}

func AuthStyleValue(c *catalog.Catalog) (AuthStyle, error) {
	if !catalog.IsValidAuthStyle(c.AuthStyle) {
		return AuthStyleBearer, fmt.Errorf("catalog %q has unknown auth_style %q", c.Name, c.AuthStyle)
	}
	switch strings.ToLower(strings.TrimSpace(c.AuthStyle)) {
	case "raw":
		return AuthStyleRaw, nil
	case "none":
		return AuthStyleNone, nil
	default:
		return AuthStyleBearer, nil
	}
}

func BaseFromCatalog(cat *catalog.Catalog, runtime Base) (Base, error) {
	if err := cat.Validate(); err != nil {
		return Base{}, err
	}

	authStyle, err := AuthStyleValue(cat)
	if err != nil {
		return Base{}, err
	}

	base := runtime
	base.IntegrationName = cat.Name
	base.IntegrationDisplay = cat.DisplayName
	base.IntegrationDesc = cat.Description
	if base.BaseURL == "" {
		base.BaseURL = cat.BaseURL
	}
	base.AuthStyle = authStyle
	base.Operations = OperationsList(cat)
	base.Endpoints = EndpointsMap(cat)
	base.Headers = mergeHeaders(cat.Headers, runtime.Headers)
	base.catalog = cat

	return base, nil
}
