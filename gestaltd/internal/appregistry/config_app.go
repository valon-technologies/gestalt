package appregistry

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func resolveConfigAppEntry(configApps map[string]*config.ProviderEntry, registryAppName string) *config.ProviderEntry {
	registryAppName = strings.TrimSpace(registryAppName)
	if registryAppName == "" || configApps == nil {
		return nil
	}
	if entry := configApps[registryAppName]; entry != nil {
		return entry
	}
	for _, entry := range configApps {
		if entry == nil {
			continue
		}
		if manifest := entry.ResolvedManifest; manifest != nil {
			if appName, err := AppNameFromManifestSource(manifest.Source); err == nil && appName == registryAppName {
				return entry
			}
			if strings.TrimSpace(manifest.DisplayName) == registryAppName {
				return entry
			}
		}
	}
	return nil
}
