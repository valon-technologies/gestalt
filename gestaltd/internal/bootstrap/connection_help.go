package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const defaultNotConnectedMessageTemplate = `{DISPLAY_NAME} is not connected. Go to {CONNECT_URL} to connect {DISPLAY_NAME} first.`

func notConnectedMessageFunc(cfg *config.Config) invocation.NotConnectedMessageFunc {
	if cfg == nil {
		return nil
	}
	help := cfg.Server.ConnectionHelp
	template := strings.TrimSpace(help.NotConnectedMessage)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Server.BaseURL), "/")
	if template == "" && baseURL == "" {
		return nil
	}
	if template == "" {
		template = defaultNotConnectedMessageTemplate
	}
	pluginDefs := cfg.Plugins
	return func(providerName, connection, instance string) string {
		displayName := providerDisplayName(pluginDefs, providerName)
		replacer := strings.NewReplacer(
			"{BASE_URL}", baseURL,
			"{baseUrl}", baseURL,
			"{CONNECT_URL}", baseURL,
			"{connectUrl}", baseURL,
			"{PLUGIN}", strings.TrimSpace(providerName),
			"{plugin}", strings.TrimSpace(providerName),
			"{DISPLAY_NAME}", displayName,
			"{displayName}", displayName,
			"{CONNECTION}", strings.TrimSpace(connection),
			"{connection}", strings.TrimSpace(connection),
			"{INSTANCE}", strings.TrimSpace(instance),
			"{instance}", strings.TrimSpace(instance),
		)
		return strings.TrimSpace(replacer.Replace(template))
	}
}

func providerDisplayName(pluginDefs map[string]*config.ProviderEntry, providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if entry := pluginDefs[providerName]; entry != nil {
		if displayName := strings.TrimSpace(entry.DisplayName); displayName != "" {
			return displayName
		}
	}
	return providerName
}
