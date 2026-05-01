package providerpkg

import "github.com/valon-technologies/gestalt/server/services/plugins/packageio"

func NormalizeUIRoutePath(label, routePath string) (string, error) {
	return packageio.NormalizeUIRoutePath(label, routePath)
}

func NormalizeUIAllowedRoles(label string, allowedRoles []string) ([]string, error) {
	return packageio.NormalizeUIAllowedRoles(label, allowedRoles)
}

func UIRouteMatches(routePath, requestPath string) bool {
	return packageio.UIRouteMatches(routePath, requestPath)
}
