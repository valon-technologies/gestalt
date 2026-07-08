package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func buildPublicGatewayTransport(
	cfg *config.Config,
	auth core.IdentityProvider,
	authorization map[string]core.AuthorizationProvider,
) (*providergateway.ProviderGatewayTransport, error) {
	if auth == nil {
		return nil, nil
	}
	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		return nil, fmt.Errorf("public rpc registry: %w", err)
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(auth)
	transport.SetPublicMethods(registry)
	if cfg != nil {
		if name, _, err := cfg.SelectedAuthorizationProvider(); err == nil && name != "" && authorization != nil {
			transport.SetAuthorizationProvider(authorization[name])
		}
		transport.SetPublicBaseURL(cfg.Server.BaseURL)
	}
	return transport, nil
}
