package providerhost

import (
	"context"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

type StaticProviderSpec = pluginservice.StaticProviderSpec
type RemoteProviderOption = pluginservice.RemoteProviderOption

func NewRemoteProvider(ctx context.Context, client proto.IntegrationProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	return pluginservice.NewRemote(ctx, client, spec, config, opts...)
}

func WithCloser(c io.Closer) RemoteProviderOption {
	return pluginservice.WithCloser(c)
}

func WithInvocationTokens(tokens *plugininvokerservice.InvocationTokenManager) RemoteProviderOption {
	return pluginservice.WithInvocationTokens(tokens)
}

func WithInvocationTokenSubject(pluginName string, grants plugininvokerservice.InvocationGrants) RemoteProviderOption {
	return pluginservice.WithInvocationTokenSubject(pluginName, grants)
}
