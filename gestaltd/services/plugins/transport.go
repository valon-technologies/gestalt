package plugins

import (
	"context"
	"io"
	"net/http"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
)

type StaticProviderSpec = providerhost.StaticProviderSpec
type ExecConfig = providerhost.ExecConfig
type RemoteProviderOption = providerhost.RemoteProviderOption
type DeclarativeProvider = providerhost.DeclarativeProvider
type DeclarativeProviderOption = providerhost.DeclarativeProviderOption

func NewExecutable(ctx context.Context, cfg ExecConfig) (Provider, error) {
	return providerhost.NewExecutableProvider(ctx, cfg)
}

func NewRemote(ctx context.Context, client proto.IntegrationProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (Provider, error) {
	return providerhost.NewRemoteProvider(ctx, client, spec, config, opts...)
}

func WithCloser(c io.Closer) RemoteProviderOption {
	return providerhost.WithCloser(c)
}

func WithInvocationTokens(tokens *plugininvokerservice.InvocationTokenManager) RemoteProviderOption {
	return providerhost.WithInvocationTokens(tokens)
}

func WithInvocationTokenSubject(pluginName string, grants plugininvokerservice.InvocationGrants) RemoteProviderOption {
	return providerhost.WithInvocationTokenSubject(pluginName, grants)
}

func NewServer(provider Provider) proto.IntegrationProviderServer {
	return providerhost.NewProviderServer(provider)
}

func NewDeclarativeProvider(manifest *providermanifestv1.Manifest, httpClient *http.Client, opts ...DeclarativeProviderOption) (*DeclarativeProvider, error) {
	return providerhost.NewDeclarativeProvider(manifest, httpClient, opts...)
}

func WithDeclarativeMetadataOverrides(displayName, description, iconSVG string) DeclarativeProviderOption {
	return providerhost.WithDeclarativeMetadataOverrides(displayName, description, iconSVG)
}

func WithDeclarativeConnectionMode(mode ConnectionMode) DeclarativeProviderOption {
	return providerhost.WithDeclarativeConnectionMode(mode)
}

func WithDeclarativeOperationConnections(connections map[string]string, selectors map[string]core.OperationConnectionSelector, locks map[string]bool) DeclarativeProviderOption {
	return providerhost.WithDeclarativeOperationConnections(connections, selectors, locks)
}

func ConnectionParamDefsFromManifest(defs map[string]providermanifestv1.ProviderConnectionParam) map[string]ConnectionParamDef {
	return providerhost.ConnectionParamDefsFromManifest(defs)
}

func CredentialFieldsFromManifest(fields []providermanifestv1.CredentialField) []CredentialFieldDef {
	return providerhost.CredentialFieldsFromManifest(fields)
}

func DiscoveryConfigFromManifest(discovery *providermanifestv1.ProviderDiscovery) *DiscoveryConfig {
	return providerhost.DiscoveryConfigFromManifest(discovery)
}

func NewPluginTempDir(pattern string) (string, error) {
	return providerhost.NewPluginTempDir(pattern)
}
