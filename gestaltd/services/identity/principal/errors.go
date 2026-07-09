package principal

import "errors"

var (
	ErrInvalidToken              = errors.New("invalid token")
	ErrCredentialSubjectRequired = errors.New("credential subject required")
)
