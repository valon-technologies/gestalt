package invocation

import (
	"errors"
	"strings"
)

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
	ErrInvalidInvocation   = errors.New("invalid invocation")
)

type NoCredentialError struct {
	Message string
}

func (e *NoCredentialError) Error() string {
	if e == nil {
		return ErrNoCredential.Error()
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		return ErrNoCredential.Error()
	}
	return message
}

func (e *NoCredentialError) Unwrap() error {
	return ErrNoCredential
}

func NoCredentialErrorMessage(err error) (string, bool) {
	var noCredential *NoCredentialError
	if !errors.As(err, &noCredential) || noCredential == nil {
		return "", false
	}
	message := strings.TrimSpace(noCredential.Message)
	return message, message != ""
}
