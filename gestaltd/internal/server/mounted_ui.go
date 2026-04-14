package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	stdpath "path"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
)

const browserLoginPath = "/api/v1/auth/login"

type mountedWebUINavigationPathResolver interface {
	NavigationPathForRequest(string) (string, bool)
}

func normalizeMountedWebUIs(mounted []MountedWebUI) ([]MountedWebUI, error) {
	if len(mounted) == 0 {
		return nil, nil
	}

	normalized := append([]MountedWebUI(nil), mounted...)
	for i := range normalized {
		routes, err := normalizeMountedWebUIRoutes(normalized[i].Routes)
		if err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
		normalized[i].Routes = routes
		if err := validatePolicyBoundMountedWebUIRoutes(normalized[i]); err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
	}
	return normalized, nil
}

func normalizeMountedWebUIRoutes(routes []MountedWebUIRoute) ([]MountedWebUIRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	normalized := append([]MountedWebUIRoute(nil), routes...)
	seenPaths := make(map[string]struct{}, len(normalized))
	for i := range normalized {
		routePath, err := providerpkg.NormalizeWebUIRoutePath(fmt.Sprintf("route %d path", i), normalized[i].Path)
		if err != nil {
			return nil, err
		}
		normalized[i].Path = routePath
		if _, exists := seenPaths[routePath]; exists {
			return nil, fmt.Errorf("route %d path %q duplicates another route", i, routePath)
		}
		seenPaths[routePath] = struct{}{}

		roles, err := providerpkg.NormalizeWebUIAllowedRoles(fmt.Sprintf("route %d allowedRoles", i), normalized[i].AllowedRoles)
		if err != nil {
			return nil, err
		}
		normalized[i].AllowedRoles = roles
	}

	slices.SortFunc(normalized, func(a, b MountedWebUIRoute) int {
		aLen, aWildcard := mountedWebUIRouteSpecificity(a.Path)
		bLen, bWildcard := mountedWebUIRouteSpecificity(b.Path)
		if aLen != bLen {
			return bLen - aLen
		}
		if aWildcard != bWildcard {
			if aWildcard {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return normalized, nil
}

func validatePolicyBoundMountedWebUIRoutes(mounted MountedWebUI) error {
	if mounted.AuthorizationPolicy == "" {
		return nil
	}
	if len(mounted.Routes) == 0 {
		return fmt.Errorf("policy-bound UIs must declare at least one route")
	}
	coversRoot := false
	for i := range mounted.Routes {
		if len(mounted.Routes[i].AllowedRoles) == 0 {
			return fmt.Errorf("route %q allowedRoles must not be empty", mounted.Routes[i].Path)
		}
		if providerpkg.WebUIRouteMatches(mounted.Routes[i].Path, "/") {
			coversRoot = true
		}
	}
	if !coversRoot {
		return fmt.Errorf("policy-bound UIs must declare a route covering /")
	}
	return nil
}

func (s *Server) mountedWebUIHandler(mounted MountedWebUI) http.Handler {
	inner := mounted.Handler
	if inner == nil {
		return http.NotFoundHandler()
	}
	if mounted.Path != "/" {
		inner = http.StripPrefix(mounted.Path, inner)
	}
	if mounted.AuthorizationPolicy == "" {
		return inner
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := s.authorizeMountedWebUIRequest(w, r, mounted)
		if !ok {
			return
		}
		inner.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authorizeMountedWebUIRequest(w http.ResponseWriter, r *http.Request, mounted MountedWebUI) (context.Context, bool) {
	if s.authorizer == nil {
		writeError(w, http.StatusInternalServerError, "app authorization is unavailable")
		return nil, false
	}

	p, authenticated, err := s.resolveMountedWebUIPrincipal(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return nil, false
	}
	if !authenticated {
		s.redirectMountedWebUILogin(w, r)
		return nil, false
	}
	if p != nil && p.Kind == principal.KindWorkload {
		writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
		return nil, false
	}

	access, allowed := s.authorizer.ResolvePolicyAccess(p, mounted.AuthorizationPolicy)
	if !allowed {
		writeError(w, http.StatusForbidden, "app access denied")
		return nil, false
	}
	route, matched := mounted.routeForRequestPath(r.URL.Path)
	if !matched || len(route.AllowedRoles) == 0 || !mountedWebUIRoleAllowed(access.Role, route.AllowedRoles) {
		writeError(w, http.StatusForbidden, "app access denied")
		return nil, false
	}

	ctx := r.Context()
	if p != nil {
		ctx = principal.WithPrincipal(ctx, p)
	}
	if access.Policy != "" || access.Role != "" {
		ctx = invocation.WithAccessContext(ctx, access)
	}
	return ctx, true
}

func (s *Server) resolveMountedWebUIPrincipal(r *http.Request) (*principal.Principal, bool, error) {
	if s.noAuth {
		return s.anonymousPrincipal, true, nil
	}

	p, err := s.resolveRequestPrincipal(r)
	switch {
	case err == nil && p != nil:
		enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p)
		if enrichErr != nil {
			return nil, false, enrichErr
		}
		return enriched, true, nil
	case err == nil:
		return nil, false, nil
	case errors.Is(err, errInvalidAuthorizationHeader), errors.Is(err, principal.ErrInvalidToken):
		return nil, false, nil
	default:
		return nil, false, err
	}
}

func (s *Server) redirectMountedWebUILogin(w http.ResponseWriter, r *http.Request) {
	target := browserLoginPath + "?next=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusFound)
}

func (m MountedWebUI) routeForRequestPath(requestPath string) (MountedWebUIRoute, bool) {
	var (
		best        MountedWebUIRoute
		bestMatched bool
		bestLen     int
		bestWild    bool
	)
	for _, routePath := range m.authorizationPathsForRequest(requestPath) {
		for _, route := range m.Routes {
			if providerpkg.WebUIRouteMatches(route.Path, routePath) {
				routeLen, routeWild := mountedWebUIRouteSpecificity(route.Path)
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

func (m MountedWebUI) authorizationPathsForRequest(requestPath string) []string {
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
	requestAuthorizationPath := cleanMountedWebUIAuthorizationPath(relativePath)
	paths := []string{requestAuthorizationPath}
	if resolver, ok := m.Handler.(mountedWebUINavigationPathResolver); ok {
		if routePath, navigation := resolver.NavigationPathForRequest(relativePath); navigation {
			return appendMountedWebUIAuthorizationPath(paths, cleanMountedWebUIAuthorizationPath(routePath))
		}
		for path := cleanMountedWebUIAuthorizationPath(stdpath.Dir(relativePath)); ; {
			paths = appendMountedWebUIAuthorizationPath(paths, path)
			if path == "/" {
				break
			}
			path = cleanMountedWebUIAuthorizationPath(stdpath.Dir(path))
		}
		return paths
	}
	return paths
}

func cleanMountedWebUIAuthorizationPath(routePath string) string {
	routePath = stdpath.Clean(routePath)
	if routePath == "." {
		return "/"
	}
	return routePath
}

func appendMountedWebUIAuthorizationPath(paths []string, path string) []string {
	if len(paths) == 0 || paths[len(paths)-1] != path {
		return append(paths, path)
	}
	return paths
}

func mountedWebUIRouteSpecificity(routePath string) (int, bool) {
	if strings.HasSuffix(routePath, "/*") {
		return len(strings.TrimSuffix(routePath, "/*")), true
	}
	return len(routePath), false
}

func mountedWebUIRoleAllowed(role string, allowedRoles []string) bool {
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
