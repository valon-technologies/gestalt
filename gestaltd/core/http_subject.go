package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// HTTPSubjectResolveRequest carries the verified inbound hosted HTTP request
// shape into optional provider-local subject resolution hooks.
type HTTPSubjectResolveRequest struct {
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

// HTTPResolvedSubject identifies the concrete subject a hosted HTTP request
// should execute as.
type HTTPResolvedSubject struct {
	ID          string
	Kind        string
	DisplayName string
	AuthSource  string
}

// HTTPSubjectResolver is implemented by providers that can map a verified
// hosted HTTP request to a concrete Gestalt subject before normal operation
// authorization runs.
type HTTPSubjectResolver interface {
	ResolveHTTPSubject(ctx context.Context, req *HTTPSubjectResolveRequest) (*HTTPResolvedSubject, error)
}

// HTTPSubjectResolveError reports a provider-defined HTTP rejection produced
// while resolving a hosted HTTP subject.
type HTTPSubjectResolveError struct {
	Status  int
	Message string
	Cause   error
}

func (e *HTTPSubjectResolveError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Status > 0 {
		return fmt.Sprintf("http subject resolution failed with status %d", e.Status)
	}
	return "http subject resolution failed"
}

func (e *HTTPSubjectResolveError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
