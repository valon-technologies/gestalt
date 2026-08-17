package config

// ResolveGestaltCLIURL returns the canonical Gestalt server URL from GESTALT_URL
// or the gestalt CLI config file created by `gestalt init`.
func ResolveGestaltCLIURL() (string, error) {
	return defaultRemoteURL()
}

// ResolveGestaltCLIToken returns an API token from GESTALT_API_KEY or the gestalt
// CLI credentials file created by `gestalt auth login`.
func ResolveGestaltCLIToken() (string, error) {
	return defaultRemoteToken()
}
