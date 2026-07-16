package publicclient

import "github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"

// DecodeAppResult decodes one app operation result with the standard JSON envelope.
var DecodeAppResult = generated.DecodeAppResult

// DecodeGraphQLResult decodes one GraphQL invocation result.
var DecodeGraphQLResult = generated.DecodeGraphQLResult

// InvokeError is the canonical app invocation payload error.
type InvokeError = generated.InvokeError
