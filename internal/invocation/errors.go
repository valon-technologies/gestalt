package invocation

import "errors"

var (
	ErrProviderNotFound  = errors.New("provider not found")
	ErrOperationNotFound = errors.New("operation not found")
	ErrNotAuthenticated  = errors.New("not authenticated")
	ErrNoToken           = errors.New("no integration token")
	ErrUserResolution    = errors.New("user resolution failed")
	ErrInternal          = errors.New("internal error")
)
