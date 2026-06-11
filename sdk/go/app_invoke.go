package gestalt

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

// InvokeGraphQL invokes the GraphQL surface of another app through the
// generated client's InvokeGraphQL method and decodes the JSON result like
// client.DecodeGraphQLResult, returning *client.InvokeError when the response
// carries a GraphQL errors array. GraphQL invocation stays a helper by
// design: the generated method returns the raw result.
func InvokeGraphQL(ctx context.Context, app *client.App, appName, document string, options *client.AppInvokeGraphQLOptions) (any, error) {
	trimmedDocument := strings.TrimSpace(document)
	if trimmedDocument == "" {
		return nil, &client.InvokeError{
			App:       appName,
			Operation: "graphql",
			Message:   "graphql document is required",
		}
	}
	if options == nil {
		options = &client.AppInvokeGraphQLOptions{}
	}
	trimmed := *options
	trimmed.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	result, err := app.InvokeGraphQL(ctx, appName, trimmedDocument, &trimmed)
	if err != nil {
		return nil, err
	}
	return client.DecodeGraphQLResult(appName, result.Status, result.Body)
}
