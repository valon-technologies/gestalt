package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

var ErrUnknownOperation = errors.New("unknown operation")

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
		ops = append(ops, core.Operation{
			Name:        op.ID,
			Description: op.Description,
			Method:      operationMethod(*op),
			Parameters:  ConvertParameters(op.Parameters),
		})
	}
	return ops
}

const transportGraphQL = "graphql"

func lookupCatalogOperation(ctx context.Context, provider string, cat *catalog.Catalog, operation string) (catalog.CatalogOperation, error) {
	if op, ok := catalog.OperationFromContext(ctx, provider, operation); ok {
		return op, nil
	}
	if cat != nil {
		for i := range cat.Operations {
			if cat.Operations[i].ID == operation {
				return cat.Operations[i], nil
			}
		}
	}
	return catalog.CatalogOperation{}, fmt.Errorf("%w: %s", ErrUnknownOperation, operation)
}

func operationMethod(op catalog.CatalogOperation) string {
	method := strings.ToUpper(strings.TrimSpace(op.Method))
	if method == "" && isGraphQLOperation(op) {
		return http.MethodPost
	}
	return method
}

func isGraphQLOperation(op catalog.CatalogOperation) bool {
	return op.Transport == transportGraphQL || (op.Transport == "" && op.Path == "" && op.Query != "")
}
