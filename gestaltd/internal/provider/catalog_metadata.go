package provider

import (
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// CatalogFromDefinition converts a provider Definition to a Catalog.
func CatalogFromDefinition(def *Definition) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:        def.Provider,
		DisplayName: def.DisplayName,
		Description: def.Description,
		IconSVG:     def.IconSVG,
		BaseURL:     def.BaseURL,
		AuthStyle:   def.AuthStyle,
		Headers:     def.Headers,
	}

	ops := make([]catalog.CatalogOperation, 0, len(def.Operations))
	for name := range def.Operations {
		opDef := def.Operations[name] //nolint:gocritic // map values not addressable
		catOp := catalog.CatalogOperation{
			ID:          name,
			Method:      strings.ToUpper(opDef.Method),
			Path:        opDef.Path,
			Description: opDef.Description,
			Transport:   opDef.Transport,
			Query:       opDef.Query,
			InputSchema: opDef.InputSchema,
		}
		for _, p := range opDef.Parameters {
			catOp.Parameters = append(catOp.Parameters, catalog.CatalogParameter{
				Name:        p.Name,
				WireName:    p.WireName,
				Type:        p.Type,
				Location:    p.Location,
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
