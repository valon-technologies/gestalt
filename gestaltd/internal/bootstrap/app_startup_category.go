package bootstrap

import "github.com/valon-technologies/gestalt/server/internal/config"

type AppStartupCategory int

const (
	AppStartupNOOP AppStartupCategory = iota
	AppStartupUpdate
)

func appStartupCategory(_ string, _ *config.ProviderEntry) AppStartupCategory {
	return AppStartupNOOP
}
