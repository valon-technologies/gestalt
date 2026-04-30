package runtimehost

import "github.com/valon-technologies/gestalt/server/internal/providerhost"

const HostServiceRelayTokenHeader = providerhost.HostServiceRelayTokenHeader

type HostServiceRelayTokenManager = providerhost.HostServiceRelayTokenManager
type HostServiceRelayTokenRequest = providerhost.HostServiceRelayTokenRequest
type HostServiceRelayTarget = providerhost.HostServiceRelayTarget

func NewHostServiceRelayTokenManager(secret []byte) (*HostServiceRelayTokenManager, error) {
	return providerhost.NewHostServiceRelayTokenManager(secret)
}
