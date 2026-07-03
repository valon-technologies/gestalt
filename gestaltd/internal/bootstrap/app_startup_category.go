package bootstrap

import (
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// resolveAutoActivate reports whether all app providers should start
// immediately (bypassing the version-changed deferral). An explicit
// server.autoActivate config value wins; otherwise it defaults to true when
// K_REVISION is absent (local dev) and false on Cloud Run.
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

// newAppStartupCategorizer decides, per app, whether it starts immediately
// (AppStartupNOOP — same version as the last successful install, so it blocks
// startup) or is deferred to /activate (AppStartupUpdate — version changed or
// never installed).
//
// When autoActivate is set, every app is NOOP: the deferral split is bypassed
// and all providers start immediately. This is the local-dev default, where
// path/source providers carry no artifact SHA and would otherwise all look
// changed on every restart.
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
