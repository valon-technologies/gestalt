package invocation

import "errors"

var (
	ErrProviderNotFound    = errors.New("provider not found")
	ErrOperationNotFound   = errors.New("operation not found")
	ErrNotAuthenticated    = errors.New("not authenticated")
	ErrAuthorizationDenied = errors.New("authorization denied")
	ErrNoCredential        = errors.New("no external credential")
	ErrReconnectRequired   = errors.New("integration reconnect required")
	ErrAmbiguousInstance   = errors.New("ambiguous instance")
	ErrUserResolution      = errors.New("user resolution failed")
	ErrInternal            = errors.New("internal error")
	ErrScopeDenied         = errors.New("token scope denied")
)
