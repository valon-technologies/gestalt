package egress

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/apiexec"
)

// HTTPRequestSpec is the generic outbound HTTP representation shared by
// product-specific adapters.
type HTTPRequestSpec struct {
	Target      Target
	BaseURL     string
	Params      map[string]any
	QueryParams map[string]any
	Headers     map[string]string
	Body        []byte
	ContentType string
	Check       apiexec.ResponseChecker
	MaxRetries  int
	NoRetry     bool
	Credential  CredentialMaterialization
}

// BuildRequest translates a generic HTTP spec into the existing apiexec request.
func BuildRequest(spec HTTPRequestSpec) apiexec.Request {
	headers := ApplyHeaderMutations(spec.Headers, spec.Credential.Headers)
	return apiexec.Request{
		Method:        spec.Target.Method,
		BaseURL:       spec.BaseURL,
		Path:          spec.Target.Path,
		Params:        spec.Params,
		QueryParams:   spec.QueryParams,
		AuthHeader:    spec.Credential.Authorization,
		CustomHeaders: headers,
		ContentType:   spec.ContentType,
		Body:          spec.Body,
		CheckResponse: spec.Check,
		MaxRetries:    spec.MaxRetries,
		NoRetry:       spec.NoRetry,
	}
}

// ExecuteHTTP executes the generic HTTP request through the existing apiexec
// transport.
func ExecuteHTTP(ctx context.Context, client *http.Client, spec HTTPRequestSpec) (*core.OperationResult, error) {
	return apiexec.Do(ctx, client, BuildRequest(spec))
}
