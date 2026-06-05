package invocation

import "errors"

var (
	ErrProviderNotFound  = errors.New("provider not found")
	ErrOperationNotFound = errors.New("operation not found")
	ErrNoCredential      = errors.New("no external credential")
	ErrReconnectRequired = errors.New("integration reconnect required")
	ErrAmbiguousInstance = errors.New("ambiguous instance")
	ErrUserResolution    = errors.New("user resolution failed")
	ErrInternal          = errors.New("internal error")
	ErrInvalidInvocation = errors.New("invalid invocation")
)
