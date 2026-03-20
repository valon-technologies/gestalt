package provider

import (
	"sort"
	"strings"

	"github.com/valon-technologies/toolshed/core/integration"
)

// CatalogFromDefinition converts a provider Definition to a Catalog.
func CatalogFromDefinition(def *Definition) *integration.Catalog {
	cat := &integration.Catalog{
		Name:        def.Provider,
		DisplayName: def.DisplayName,
		Description: def.Description,
		IconSVG:     def.IconSVG,
		BaseURL:     def.BaseURL,
		AuthStyle:   def.AuthStyle,
		Headers:     def.Headers,
	}

	ops := make([]integration.CatalogOperation, 0, len(def.Operations))
	for name, opDef := range def.Operations {
		catOp := integration.CatalogOperation{
			ID:          name,
			Method:      strings.ToUpper(opDef.Method),
			Path:        opDef.Path,
			Description: opDef.Description,
		}
		for _, p := range opDef.Parameters {
			catOp.Parameters = append(catOp.Parameters, integration.CatalogParameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
				Default:     p.Default,
			})
		}
		ops = append(ops, catOp)
	}

	sort.Slice(ops, func(i, j int) bool {
		return ops[i].ID < ops[j].ID
	})

	cat.Operations = ops
	return cat
}
