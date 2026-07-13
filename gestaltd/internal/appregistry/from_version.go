package appregistry

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func configPinnedAppVersion(entry *config.ProviderEntry) string {
	if entry == nil {
		return ""
	}
	if version := entry.Source.ResolvedPackageVersion(); version != "" {
		return version
	}
	if entry.ResolvedManifest != nil {
		return strings.TrimSpace(entry.ResolvedManifest.Version)
	}
	return ""
}

func resolveFromVersion(known []*core.AppInstallation, configEntry *config.ProviderEntry) string {
	if version := coredata.LatestKnownVersion(known); version != "" {
		return version
	}
	return configPinnedAppVersion(configEntry)
}
