package mcpoauth

import "context"

// Keyed by (auth server URL, redirect URI) since the same auth server may
// issue different credentials for different redirect URIs.
type RegistrationStore interface {
	GetRegistration(ctx context.Context, authServerURL, redirectURI string) (*Registration, error)
	StoreRegistration(ctx context.Context, reg *Registration) error
}
