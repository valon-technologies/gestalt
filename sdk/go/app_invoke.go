package gestalt

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/client"
)

// InvokeGraphQLOptions selects the optional targeting fields of a GraphQL
// invocation.
type InvokeGraphQLOptions struct {
	// Connection is the connected account id or name to invoke against.
	Connection string
	// Instance is the provider instance id or name to invoke against.
	Instance string
	// IdempotencyKey is forwarded to the target operation.
	IdempotencyKey string
	// Variables are the GraphQL variables for the document.
	Variables map[string]any
}

// InvokeGraphQL invokes the GraphQL surface of another app through the
// generated client's InvokeGraphQL method and decodes the JSON result like
// client.DecodeGraphQLResult, returning *client.InvokeError when the response
// carries a GraphQL errors array. GraphQL invocation stays a helper by
// design: the generated method returns the raw result.
func InvokeGraphQL(ctx context.Context, app *client.App, appName, document string, options *InvokeGraphQLOptions) (any, error) {
	trimmedDocument := strings.TrimSpace(document)
	if trimmedDocument == "" {
		return nil, &client.InvokeError{
			App:       appName,
			Operation: "graphql",
			Message:   "graphql document is required",
		}
	}
	if options == nil {
		options = &InvokeGraphQLOptions{}
	}
	result, err := app.InvokeGraphQL(
		ctx,
		appName,
		trimmedDocument,
		options.Connection,
		options.Instance,
		strings.TrimSpace(options.IdempotencyKey),
		options.Variables,
	)
	if err != nil {
		return nil, err
	}
	return client.DecodeGraphQLResult(appName, result.Status, result.Body)
}
