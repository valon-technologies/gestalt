package declarative

import (
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func ConvertParameters(params []catalog.CatalogParameter) []core.Parameter {
	out := make([]core.Parameter, 0, len(params))
	for _, p := range params {
		out = append(out, core.Parameter{
			Name:        p.Name,
			Type:        p.Type,
			Description: p.Description,
			Required:    p.Required,
			Default:     p.Default,
		})
	}
	return out
}

func OperationsList(c *catalog.Catalog) []core.Operation {
	if c == nil {
		return nil
	}
	ops := make([]core.Operation, 0, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		if op.Transport == transportGraphQL && op.Query == "" {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(op.Method))
		if method == "" && op.Query != "" {
			method = http.MethodPost
		}
		ops = append(ops, core.Operation{
			Name:        op.ID,
			Description: op.Description,
			Method:      method,
			Parameters:  ConvertParameters(op.Parameters),
		})
	}
	return ops
}

const transportGraphQL = "graphql"
