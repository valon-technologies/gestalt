package providerhost

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
)

const DefaultAuthorizationSocketEnv = authorizationservice.DefaultSocketEnv

func AuthorizationSocketTokenEnv() string {
	return authorizationservice.SocketTokenEnv()
}

func NewAuthorizationProviderServer(provider core.AuthorizationProvider) proto.AuthorizationProviderServer {
	return authorizationservice.NewProviderServer(provider)
}
