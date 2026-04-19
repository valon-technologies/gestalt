package server

import (
	"encoding/base64"
	"net/http"
	stdpath "path"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

func (m MountedUI) routeForRequestPath(requestPath string) (MountedUIRoute, bool) {
	var (
		best        MountedUIRoute
		bestMatched bool
		bestLen     int
		bestWild    bool
	)
	for _, routePath := range m.authorizationPathsForRequest(requestPath) {
		for _, route := range m.Routes {
			if providerpkg.UIRouteMatches(route.Path, routePath) {
				routeLen, routeWild := mountedUIRouteSpecificity(route.Path)
				if !bestMatched || routeLen > bestLen || (routeLen == bestLen && bestWild && !routeWild) {
					best = route
					bestMatched = true
					bestLen = routeLen
					bestWild = routeWild
				}
			}
		}
	}
	return best, bestMatched
}

func (m MountedUI) authorizationPathsForRequest(requestPath string) []string {
	relativePath := requestPath
	if m.Path != "/" {
		relativePath = strings.TrimPrefix(requestPath, m.Path)
	}
	if relativePath == "" {
		relativePath = "/"
	}
	if !strings.HasPrefix(relativePath, "/") {
		relativePath = "/" + relativePath
	}
	requestAuthorizationPath := cleanMountedUIAuthorizationPath(relativePath)
	paths := []string{requestAuthorizationPath}
	if resolver, ok := m.Handler.(mountedUINavigationPathResolver); ok {
		if routePath, navigation := resolver.NavigationPathForRequest(relativePath); navigation {
			return appendMountedUIAuthorizationPath(paths, cleanMountedUIAuthorizationPath(routePath))
		}
		for path := cleanMountedUIAuthorizationPath(stdpath.Dir(relativePath)); ; {
			paths = appendMountedUIAuthorizationPath(paths, path)
			if path == "/" {
				break
			}
			path = cleanMountedUIAuthorizationPath(stdpath.Dir(path))
		}
		return paths
	}
	return paths
}

func cleanMountedUIAuthorizationPath(routePath string) string {
	routePath = stdpath.Clean(routePath)
	if routePath == "." {
		return "/"
	}
	return routePath
}

func appendMountedUIAuthorizationPath(paths []string, path string) []string {
	if len(paths) == 0 || paths[len(paths)-1] != path {
		return append(paths, path)
	}
	return paths
}

func mountedUIRouteSpecificity(routePath string) (int, bool) {
	if strings.HasSuffix(routePath, "/*") {
		return len(strings.TrimSuffix(routePath, "/*")), true
	}
	return len(routePath), false
}

func mountedUIRoleAllowed(role string, allowedRoles []string) bool {
	role = strings.TrimSpace(role)
	if role == "" {
		return false
	}
	for _, allowed := range allowedRoles {
		if strings.TrimSpace(allowed) == role {
			return true
		}
	}
	return false
}

func providerDevUIAssetPath(mounted MountedUI, requestPath string) string {
	path := requestPath
	if mounted.Path != "/" {
		path = strings.TrimPrefix(path, mounted.Path)
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func providerDevUIRequestHeader(header http.Header) http.Header {
	out := http.Header{}
	copyHeaderValues(out, header, "If-Match")
	copyHeaderValues(out, header, "If-None-Match")
	copyHeaderValues(out, header, "If-Modified-Since")
	copyHeaderValues(out, header, "If-Unmodified-Since")
	copyHeaderValues(out, header, "If-Range")
	copyHeaderValues(out, header, "Range")
	if len(out) == 0 {
		return nil
	}
	return out
}

func copyHeaderValues(dst, src http.Header, name string) {
	values := src.Values(name)
	for _, value := range values {
		dst.Add(name, value)
	}
}

func writeProviderDevUIAsset(w http.ResponseWriter, resp *providerdev.UIAssetResponse) {
	if resp == nil {
		writeError(w, http.StatusNotFound, "provider dev ui asset not found")
		return
	}
	body, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid provider dev ui response")
		return
	}
	for key, values := range resp.Header {
		if isHopByHopHeader(key) {
			continue
		}
		w.Header().Del(key)
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	statusCode := resp.Status
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	if len(body) != 0 {
		_, _ = w.Write(body)
	}
}

func isHopByHopHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
