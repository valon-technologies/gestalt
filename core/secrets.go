package core

import (
	"context"
	"errors"
	"fmt"
)

// SecretManager resolves secret values by name. Implementations must be safe
// for concurrent use and must not include secret values in returned errors.
type SecretManager interface {
	GetSecret(ctx context.Context, name string) (string, error)
}

var ErrSecretNotFound = errors.New("secret not found")

type SecretResolutionError struct {
	Name string
	Err  error
}

func (e *SecretResolutionError) Error() string {
	return fmt.Sprintf("resolving secret %q: %v", e.Name, e.Err)
}

func (e *SecretResolutionError) Unwrap() error {
	return e.Err
}
