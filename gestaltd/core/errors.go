package core

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrAlreadyExists       = errors.New("already exists")
	ErrAlreadyRegistered   = errors.New("already registered")
	ErrMCPOnly             = errors.New("this integration is accessible only via MCP")
	ErrAmbiguousCredential = errors.New("ambiguous external credential")
	ErrReconnectRequired   = errors.New("external credential reconnect required")
	ErrProviderActivating  = errors.New("provider is still activating")
)

// CredentialInstanceConflictError identifies an instance name already owned
// by another stored credential. It preserves ErrAlreadyExists for callers
// while carrying enough context for a user-facing recovery message.
type CredentialInstanceConflictError struct {
	Instance         string
	DifferentAccount bool
}

func (e *CredentialInstanceConflictError) Error() string {
	if e == nil {
		return ErrAlreadyExists.Error()
	}
	return ErrAlreadyExists.Error() + ": credential instance " + e.Instance
}

func (e *CredentialInstanceConflictError) Unwrap() error {
	return ErrAlreadyExists
}
