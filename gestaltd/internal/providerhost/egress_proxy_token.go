package providerhost

import egressproxyservice "github.com/valon-technologies/gestalt/server/services/egressproxy"

type EgressProxyTokenManager = egressproxyservice.TokenManager
type EgressProxyTokenRequest = egressproxyservice.TokenRequest
type EgressProxyTarget = egressproxyservice.Target

func NewEgressProxyTokenManager(secret []byte) (*EgressProxyTokenManager, error) {
	return egressproxyservice.NewTokenManager(secret)
}
