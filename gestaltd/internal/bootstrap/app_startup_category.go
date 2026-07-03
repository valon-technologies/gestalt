package bootstrap

import (
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func resolveAutoActivate(cfg *config.Config) bool {
	if cfg != nil && cfg.Server.AutoActivate != nil {
		return *cfg.Server.AutoActivate
	}
	return strings.TrimSpace(os.Getenv("K_REVISION")) == ""
}

type AppStartupCategory int

const (
	AppStartupNOOP AppStartupCategory = iota
	AppStartupUpdate
)

func newAppStartupCategorizer(stored map[string]string, autoActivate bool) func(string, *config.ProviderEntry) AppStartupCategory {
	return func(name string, entry *config.ProviderEntry) AppStartupCategory {
		if autoActivate {
			return AppStartupNOOP
		}
		current := currentAppSHA(entry)
		if current != "" && stored[name] == current {
			return AppStartupNOOP
		}
		return AppStartupUpdate
	}
}
