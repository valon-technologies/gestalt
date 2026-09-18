package identity

import (
	"context"
	"errors"

	"github.com/valon-technologies/gestalt/server/core"
)

// FederatedLogoutProvider is implemented by identity providers that can build
// an upstream SSO logout URL.
type FederatedLogoutProvider interface {
	FederatedLogoutURL(ctx context.Context, returnTo string) (string, error)
}

// FederatedLogoutURL returns the upstream federated logout URL when supported.
func FederatedLogoutURL(ctx context.Context, provider core.IdentityProvider, returnTo string) (string, error) {
	if provider == nil {
		return "", errors.New("auth is not configured")
	}
	federated, ok := provider.(FederatedLogoutProvider)
	if !ok {
		return "", errors.New("federated logout is not supported")
	}
	return federated.FederatedLogoutURL(ctx, returnTo)
}
