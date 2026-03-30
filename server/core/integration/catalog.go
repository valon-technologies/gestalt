package integration

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func OperationsList(c *catalog.Catalog) []core.Operation {
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

func EndpointsMap(c *catalog.Catalog) (map[string]Endpoint, error) {
	endpoints := make(map[string]Endpoint, len(c.Operations))
	for i := range c.Operations {
		op := &c.Operations[i]
		if op.Transport == transportGraphQL || op.Query != "" {
			continue
		}
		if strings.TrimSpace(op.Method) == "" {
			return nil, fmt.Errorf("catalog %q operation %q is missing method", c.Name, op.ID)
		}
		if strings.TrimSpace(op.Path) == "" {
			return nil, fmt.Errorf("catalog %q operation %q is missing path", c.Name, op.ID)
		}
		endpoints[op.ID] = Endpoint{
			Method: strings.ToUpper(strings.TrimSpace(op.Method)),
			Path:   op.Path,
		}
	}
	return endpoints, nil
}

const transportGraphQL = "graphql"

func QueriesMap(c *catalog.Catalog) map[string]string {
	queries := make(map[string]string)
	for i := range c.Operations {
		op := &c.Operations[i]
		if op.Query != "" {
			queries[op.ID] = op.Query
		}
	}
	if len(queries) == 0 {
		return nil
	}
	return queries
}
