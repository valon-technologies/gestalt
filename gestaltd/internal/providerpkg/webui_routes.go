package providerpkg

import (
	"fmt"
	stdpath "path"
	"strings"
)

func NormalizeWebUIRoutePath(label, routePath string) (string, error) {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	if !strings.HasPrefix(routePath, "/") {
		return "", fmt.Errorf("%s must start with /", label)
	}
	if strings.ContainsAny(routePath, "{}") {
		return "", fmt.Errorf("%s route patterns are not supported", label)
	}
	if strings.Contains(routePath, "*") {
		if routePath != "/*" && (!strings.HasSuffix(routePath, "/*") || strings.Count(routePath, "*") != 1) {
			return "", fmt.Errorf("%s wildcards are only supported as a terminal /*", label)
		}
		base := strings.TrimSuffix(routePath, "/*")
		if base == "" {
			return "/*", nil
		}
		base = stdpath.Clean(base)
		if base == "." {
			base = "/"
		}
		if base == "/" {
			return "/*", nil
		}
		base = strings.TrimRight(base, "/")
		return base + "/*", nil
	}

	routePath = stdpath.Clean(routePath)
	if routePath == "." {
		routePath = "/"
	}
	if routePath != "/" {
		routePath = strings.TrimRight(routePath, "/")
	}
	return routePath, nil
}

func NormalizeWebUIAllowedRoles(label string, allowedRoles []string) ([]string, error) {
	if len(allowedRoles) == 0 {
		return nil, fmt.Errorf("%s must not be empty", label)
	}
	roles := allowedRoles[:0]
	seenRoles := make(map[string]struct{}, len(allowedRoles))
	for i, role := range allowedRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			return nil, fmt.Errorf("%s[%d] is required", label, i)
		}
		if _, exists := seenRoles[role]; exists {
			continue
		}
		seenRoles[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles, nil
}

func WebUIRouteMatches(routePath, requestPath string) bool {
	if routePath == "/*" {
		return true
	}
	if strings.HasSuffix(routePath, "/*") {
		base := strings.TrimSuffix(routePath, "/*")
		return requestPath == base || strings.HasPrefix(requestPath, base+"/")
	}
	if routePath == "/" {
		return requestPath == "/"
	}
	return requestPath == routePath
}
