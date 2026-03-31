package integration

import (
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

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
		method := strings.ToUpper(strings.TrimSpace(op.Method))
		if method == "" && op.Query != "" {
			method = http.MethodPost
		}
		ops = append(ops, core.Operation{
			Name:        op.ID,
			Description: op.Description,
			Method:      method,
			Parameters:  params,
		})
	}
	return ops
}

const transportGraphQL = "graphql"
