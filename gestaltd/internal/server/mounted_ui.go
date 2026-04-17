package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	stdpath "path"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/adminui"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/webui"
)

const browserLoginPath = "/api/v1/auth/login"
const adminUIDirEnv = "GESTALTD_ADMIN_UI_DIR"

type mountedWebUINavigationPathResolver interface {
	NavigationPathForRequest(string) (string, bool)
}

type protectedUILoginRedirect func(http.ResponseWriter, *http.Request) error

func normalizeAdminRouteConfig(admin AdminRouteConfig) (AdminRouteConfig, error) {
	admin.AuthorizationPolicy = strings.TrimSpace(admin.AuthorizationPolicy)
	if admin.AuthorizationPolicy == "" {
		if len(admin.AllowedRoles) > 0 {
			return AdminRouteConfig{}, fmt.Errorf("admin allowedRoles requires AuthorizationPolicy")
		}
		admin.AllowedRoles = nil
		return admin, nil
	}
	if len(admin.AllowedRoles) == 0 {
		admin.AllowedRoles = []string{"admin"}
		return admin, nil
	}

	roles, err := providerpkg.NormalizeWebUIAllowedRoles("admin allowedRoles", admin.AllowedRoles)
	if err != nil {
		return AdminRouteConfig{}, err
	}
	admin.AllowedRoles = roles
	return admin, nil
}

func validateAdminRouteRuntime(admin AdminRouteConfig, noAuth bool, publicBaseURL, managementBaseURL string, routeProfile RouteProfile) error {
	if admin.AuthorizationPolicy == "" {
		return nil
	}
	if noAuth {
		return fmt.Errorf("admin authorization requires auth to be enabled")
	}
	if routeProfile == RouteProfileAll {
		if strings.TrimSpace(managementBaseURL) != "" {
			return fmt.Errorf("ManagementBaseURL requires RouteProfilePublic or RouteProfileManagement for admin authorization")
		}
		return nil
	}

	publicURL, err := parseAbsoluteBaseURL("PublicBaseURL", publicBaseURL)
	if err != nil {
		return err
	}
	managementURL, err := parseAbsoluteBaseURL("ManagementBaseURL", managementBaseURL)
	if err != nil {
		return err
	}
	if publicURL.Hostname() != managementURL.Hostname() {
		return fmt.Errorf("PublicBaseURL and ManagementBaseURL must use the same hostname for admin authorization")
	}
	if strings.EqualFold(publicURL.Scheme, "https") && !strings.EqualFold(managementURL.Scheme, "https") {
		return fmt.Errorf("ManagementBaseURL must use https when PublicBaseURL uses https for admin authorization")
	}
	return nil
}

func parseAbsoluteBaseURL(label, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required", label)
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute URL", label)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s may not include query or fragment", label)
	}
	return parsed, nil
}

func mountedWebUIsFromEntries(entries map[string]*config.UIEntry) ([]MountedWebUI, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedWebUI, 0, len(names))
	for _, name := range names {
		entry := entries[name]
		if entry == nil {
			continue
		}
		if entry.ResolvedAssetRoot == "" {
			return nil, fmt.Errorf("ui %q configured but asset root not resolved", name)
		}

		handler, err := webui.DirHandler(entry.ResolvedAssetRoot)
		if err != nil {
			return nil, fmt.Errorf("ui %q: %w", name, err)
		}

		routes := []MountedWebUIRoute(nil)
		if spec := entry.ManifestSpec(); spec != nil && len(spec.Routes) > 0 {
			routes = make([]MountedWebUIRoute, 0, len(spec.Routes))
			for _, route := range spec.Routes {
				routes = append(routes, MountedWebUIRoute{
					Path:         route.Path,
					AllowedRoles: append([]string(nil), route.AllowedRoles...),
				})
			}
		}

		mounted = append(mounted, MountedWebUI{
			Name:                name,
			Path:                entry.Path,
			PluginName:          entry.OwnerPlugin,
			AuthorizationPolicy: entry.AuthorizationPolicy,
			Routes:              routes,
			Handler:             handler,
		})
	}

	return mounted, nil
}

func resolveBuiltinAdminUI(opts BuiltinAdminUIOptions) (http.Handler, error) {
	adminOpts := adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	}
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" {
		return adminui.DirHandler(dir, adminOpts)
	}
	handler := adminui.EmbeddedHandler(adminOpts)
	if handler == nil {
		return nil, fmt.Errorf("embedded admin ui assets not found")
	}
	return handler, nil
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
	return s.protectedUIHandler(mounted, inner, s.redirectMountedWebUILogin)
}

func (s *Server) adminUIHandler() http.Handler {
	if s.adminUI == nil {
		return http.NotFoundHandler()
	}
	mounted := s.adminMountedWebUI()
	inner := http.StripPrefix(mounted.Path, mounted.Handler)
	return s.protectedUIHandler(mounted, inner, s.redirectAdminUILogin)
}

func (s *Server) adminMountedWebUI() MountedWebUI {
	return MountedWebUI{
		Name:                "builtin_admin",
		Path:                "/admin",
		AuthorizationPolicy: s.adminRoute.AuthorizationPolicy,
		builtInAdmin:        true,
		Routes: []MountedWebUIRoute{{
			Path:         "/*",
			AllowedRoles: append([]string(nil), s.adminRoute.AllowedRoles...),
		}},
		Handler: s.adminUI,
	}
}

func (s *Server) protectedUIHandler(mounted MountedWebUI, inner http.Handler, redirectLogin protectedUILoginRedirect) http.Handler {
	if mounted.AuthorizationPolicy == "" {
		return inner
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := s.authorizeProtectedUIRequest(w, r, mounted, redirectLogin)
		if !ok {
			return
		}
		inner.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authorizeProtectedUIRequest(w http.ResponseWriter, r *http.Request, mounted MountedWebUI, redirectLogin protectedUILoginRedirect) (context.Context, bool) {
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
		if redirectLogin != nil {
			if err := redirectLogin(w, r); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
		}
		return nil, false
	}
	if principal.IsNonUserPrincipal(p) {
		writeError(w, http.StatusForbidden, "non-user callers are not allowed on this route")
		return nil, false
	}

	var (
		access  invocation.AccessContext
		allowed bool
	)
	switch {
	case mounted.PluginName != "":
		access, allowed = s.authorizer.ResolveAccess(p, mounted.PluginName)
	case mounted.builtInAdmin:
		access, allowed = s.authorizer.ResolveAdminAccess(p, mounted.AuthorizationPolicy)
	default:
		access, allowed = s.authorizer.ResolvePolicyAccess(p, mounted.AuthorizationPolicy)
	}
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

func (s *Server) redirectMountedWebUILogin(w http.ResponseWriter, r *http.Request) error {
	target := browserLoginPath + "?next=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

func (s *Server) redirectAdminUILogin(w http.ResponseWriter, r *http.Request) error {
	if s.routeProfile != RouteProfileManagement {
		return s.redirectMountedWebUILogin(w, r)
	}
	if s.publicBaseURL == "" {
		return fmt.Errorf("admin login redirect requires server.baseUrl")
	}
	if s.managementBaseURL == "" {
		return fmt.Errorf("admin login redirect requires server.management.baseUrl")
	}

	target := s.publicBaseURL + browserLoginPath + "?next=" + url.QueryEscape(s.managementBaseURL+r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusFound)
	return nil
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
