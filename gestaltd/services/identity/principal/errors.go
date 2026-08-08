package principal

import "errors"

var (
	ErrInvalidToken              = errors.New("invalid token")
	ErrCredentialSubjectRequired = errors.New("credential subject required")
	// ErrOpaqueCredentialSubject reports a user subject that is neither a
	// canonical persisted user ID nor a resolvable verified email, such as a
	// provider-opaque subject like "user:auth0|abc".
	ErrOpaqueCredentialSubject = errors.New("opaque credential subject")
)
