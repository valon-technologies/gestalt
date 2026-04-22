package gestalt

import (
	"context"
	"net/http"
	"net/url"
)

// HTTPSubjectRequest carries one verified hosted HTTP request into an optional
// plugin-local subject resolution hook.
type HTTPSubjectRequest struct {
	Binding         string
	Method          string
	Path            string
	ContentType     string
	Headers         http.Header
	Query           url.Values
	Params          map[string]any
	RawBody         []byte
	SecurityScheme  string
	VerifiedSubject string
	VerifiedClaims  map[string]string
}

// HTTPSubjectResolver is implemented by providers that can map a verified
// hosted HTTP request to a concrete Gestalt subject before the target
// operation is authorized and executed.
type HTTPSubjectResolver interface {
	ResolveHTTPSubject(ctx context.Context, req HTTPSubjectRequest) (*Subject, error)
}
