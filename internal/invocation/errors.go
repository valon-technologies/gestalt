package invocation

import "errors"

var (
	ErrProviderNotFound     = errors.New("provider not found")
	ErrOperationNotFound    = errors.New("operation not found")
	ErrMissingRequiredParam = errors.New("missing required parameter")
	ErrNotAuthenticated     = errors.New("not authenticated")
	ErrNoToken              = errors.New("no integration token")
	ErrAmbiguousInstance    = errors.New("ambiguous instance")
	ErrUserResolution       = errors.New("user resolution failed")
	ErrInternal             = errors.New("internal error")
)
