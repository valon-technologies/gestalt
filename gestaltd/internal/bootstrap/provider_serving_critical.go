package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func isServingCriticalProvider(entry *config.ProviderEntry) bool {
	return entry != nil && entry.Static != nil && strings.TrimSpace(entry.Static.Mount) != ""
}

func partitionPendingServingCritical(pending []pendingProviderBuild) (critical, other []pendingProviderBuild) {
	for _, item := range pending {
		if isServingCriticalProvider(item.entry) {
			critical = append(critical, item)
		} else {
			other = append(other, item)
		}
	}
	return critical, other
}
