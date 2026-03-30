package egress

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
)

// GraphQLRequestSpec is the generic outbound GraphQL representation shared by
// product-specific adapters.
type GraphQLRequestSpec struct {
	Target     Target
	URL        string
	Query      string
	Variables  map[string]any
	Headers    map[string]string
	Credential CredentialMaterialization
}

// BuildGraphQLRequest translates a generic GraphQL spec into the existing
// apiexec request.
func BuildGraphQLRequest(spec GraphQLRequestSpec) apiexec.GraphQLRequest {
	return apiexec.GraphQLRequest{
		URL:           spec.URL,
		Query:         spec.Query,
		Variables:     spec.Variables,
		AuthHeader:    spec.Credential.Authorization,
		CustomHeaders: ApplyHeaderMutations(spec.Headers, spec.Credential.Headers),
	}
}

// ExecuteGraphQL executes the generic GraphQL request through the existing
// apiexec transport.
func ExecuteGraphQL(ctx context.Context, client *http.Client, spec GraphQLRequestSpec) (*core.OperationResult, error) {
	return apiexec.DoGraphQL(ctx, client, BuildGraphQLRequest(spec))
}
