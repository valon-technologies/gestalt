// Package egressproxy exposes signed egress proxy token primitives.
package egressproxy

import "github.com/valon-technologies/gestalt/server/internal/providerhost"

type TokenManager = providerhost.EgressProxyTokenManager
type TokenRequest = providerhost.EgressProxyTokenRequest
type Target = providerhost.EgressProxyTarget

func NewTokenManager(secret []byte) (*TokenManager, error) {
	return providerhost.NewEgressProxyTokenManager(secret)
}
