package providerhost

import (
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

const (
	HostServiceRelayTokenHeader = runtimehost.HostServiceRelayTokenHeader
)

type HostServiceRelayTokenManager = runtimehost.HostServiceRelayTokenManager
type HostServiceRelayTokenRequest = runtimehost.HostServiceRelayTokenRequest
type HostServiceRelayTarget = runtimehost.HostServiceRelayTarget

func NewHostServiceRelayTokenManager(secret []byte) (*HostServiceRelayTokenManager, error) {
	return runtimehost.NewHostServiceRelayTokenManager(secret)
}
