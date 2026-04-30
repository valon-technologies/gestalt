package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

type ProviderServer = pluginservice.ProviderServer

func NewProviderServer(provider core.Provider) *ProviderServer {
	return pluginservice.NewProviderServer(provider)
}

func WithInvocationToken(ctx context.Context, token string) context.Context {
	return plugininvokerservice.WithInvocationToken(ctx, token)
}

func InvocationTokenFromContext(ctx context.Context) string {
	return plugininvokerservice.InvocationTokenFromContext(ctx)
}

var _ proto.IntegrationProviderServer = (*ProviderServer)(nil)
