package providerhost

import (
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
)

type DeclarativeProvider = pluginservice.DeclarativeProvider
type DeclarativeProviderOption = pluginservice.DeclarativeProviderOption

func NewDeclarativeProvider(manifest *providermanifestv1.Manifest, httpClient *http.Client, opts ...DeclarativeProviderOption) (*DeclarativeProvider, error) {
	return pluginservice.NewDeclarativeProvider(manifest, httpClient, opts...)
}

func WithDeclarativeMetadataOverrides(displayName, description, iconSVG string) DeclarativeProviderOption {
	return pluginservice.WithDeclarativeMetadataOverrides(displayName, description, iconSVG)
}

func WithDeclarativeConnectionMode(mode core.ConnectionMode) DeclarativeProviderOption {
	return pluginservice.WithDeclarativeConnectionMode(mode)
}

func WithDeclarativeOperationConnections(connections map[string]string, selectors map[string]core.OperationConnectionSelector, locks map[string]bool) DeclarativeProviderOption {
	return pluginservice.WithDeclarativeOperationConnections(connections, selectors, locks)
}
