package bootstrap

import "github.com/valon-technologies/gestalt/server/internal/config"

// AppStartupCategory controls when an app provider is started relative to /ready.
type AppStartupCategory int

const (
	// AppStartupNOOP means the app provider must finish loading before /ready is returned.
	AppStartupNOOP AppStartupCategory = iota
	// AppStartupUpdate means the app provider starts after /ready and loads in the background.
	AppStartupUpdate
)

// appStartupCategory returns the startup category for an app provider.
// Currently always returns AppStartupNOOP so all apps block /ready.
func appStartupCategory(_ string, _ *config.ProviderEntry) AppStartupCategory {
	return AppStartupNOOP
}
