package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	stdpath "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
	"github.com/valon-technologies/gestalt/server/services/ui"
	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
)

const browserLoginPath = "/api/v1/auth/login"
const adminUIDirEnv = "GESTALTD_ADMIN_UI_DIR"

type mountedUINavigationPathResolver interface {
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

	roles, err := providerpkg.NormalizeUIAllowedRoles("admin allowedRoles", admin.AllowedRoles)
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

func mountedUIsFromEntries(entries map[string]*config.UIEntry) ([]MountedUI, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	mounted := make([]MountedUI, 0, len(names))
	for _, name := range names {
		entry := entries[name]
		if entry == nil {
			continue
		}
		if entry.ResolvedAssetRoot == "" {
			return nil, fmt.Errorf("ui %q configured but asset root not resolved", name)
		}

		handler, err := ui.DirHandler(entry.ResolvedAssetRoot)
		if err != nil {
			return nil, fmt.Errorf("ui %q: %w", name, err)
		}

		routes := []MountedUIRoute(nil)
		if spec := entry.ManifestSpec(); spec != nil && len(spec.Routes) > 0 {
			routes = make([]MountedUIRoute, 0, len(spec.Routes))
			for _, route := range spec.Routes {
				routes = append(routes, MountedUIRoute{
					Path:         route.Path,
					AllowedRoles: append([]string(nil), route.AllowedRoles...),
				})
			}
		}

		mounted = append(mounted, MountedUI{
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

func resolveConfiguredAdminUI(opts BuiltinAdminUIOptions, providerName string, entries map[string]*config.UIEntry) (http.Handler, error) {
	entry, resolvedName, err := selectAdminUIProviderEntry(strings.TrimSpace(providerName), entries)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	adminDir := filepath.Join(entry.ResolvedAssetRoot, "admin")
	if _, err := os.Stat(filepath.Join(adminDir, "index.html")); err != nil {
		return nil, fmt.Errorf("ui.%s admin assets not found at %s: %w", resolvedName, adminDir, err)
	}

	handler, err := adminui.DirHandler(adminDir, adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	})
	if err != nil {
		return nil, fmt.Errorf("ui.%s admin assets: %w", resolvedName, err)
	}
	return handler, nil
}

func selectAdminUIProviderEntry(providerName string, entries map[string]*config.UIEntry) (*config.UIEntry, string, error) {
	if len(entries) == 0 {
		if providerName != "" {
			return nil, "", fmt.Errorf("server.admin.ui %q not found", providerName)
		}
		return nil, "", nil
	}

	if providerName != "" {
		entry := entries[providerName]
		if entry == nil {
			return nil, "", fmt.Errorf("server.admin.ui %q not found", providerName)
		}
		return entry, providerName, nil
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		entry := entries[name]
		if entry == nil || strings.TrimSpace(entry.Path) != "/" {
			continue
		}
		if uiEntryHasAdminShell(entry) {
			return entry, name, nil
		}
	}

	return nil, "", nil
}

func uiEntryHasAdminShell(entry *config.UIEntry) bool {
	if entry == nil || strings.TrimSpace(entry.ResolvedAssetRoot) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(entry.ResolvedAssetRoot, "admin", "index.html"))
	return err == nil && !info.IsDir()
}

func normalizeMountedUIs(mounted []MountedUI) ([]MountedUI, error) {
	if len(mounted) == 0 {
		return nil, nil
	}

	normalized := append([]MountedUI(nil), mounted...)
	for i := range normalized {
		routes, err := normalizeMountedUIRoutes(normalized[i].Routes)
		if err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
		normalized[i].Routes = routes
		if err := validatePolicyBoundMountedUIRoutes(normalized[i]); err != nil {
			name := normalized[i].Name
			if name == "" {
				name = normalized[i].Path
			}
			return nil, fmt.Errorf("normalize mounted ui %q routes: %w", name, err)
		}
	}
	return normalized, nil
}

func normalizeMountedUIRoutes(routes []MountedUIRoute) ([]MountedUIRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	normalized := append([]MountedUIRoute(nil), routes...)
	seenPaths := make(map[string]struct{}, len(normalized))
	for i := range normalized {
		routePath, err := providerpkg.NormalizeUIRoutePath(fmt.Sprintf("route %d path", i), normalized[i].Path)
		if err != nil {
			return nil, err
		}
		normalized[i].Path = routePath
		if _, exists := seenPaths[routePath]; exists {
			return nil, fmt.Errorf("route %d path %q duplicates another route", i, routePath)
		}
		seenPaths[routePath] = struct{}{}

		roles, err := providerpkg.NormalizeUIAllowedRoles(fmt.Sprintf("route %d allowedRoles", i), normalized[i].AllowedRoles)
		if err != nil {
			return nil, err
		}
		normalized[i].AllowedRoles = roles
	}

	slices.SortFunc(normalized, func(a, b MountedUIRoute) int {
		aLen, aWildcard := mountedUIRouteSpecificity(a.Path)
		bLen, bWildcard := mountedUIRouteSpecificity(b.Path)
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

func validatePolicyBoundMountedUIRoutes(mounted MountedUI) error {
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
		if providerpkg.UIRouteMatches(mounted.Routes[i].Path, "/") {
			coversRoot = true
		}
	}
	if !coversRoot {
		return fmt.Errorf("policy-bound UIs must declare a route covering /")
	}
	return nil
}

func (s *Server) mountedUIHandler(mounted MountedUI) http.Handler {
	inner := mounted.Handler
	if inner == nil {
		return http.NotFoundHandler()
	}
	if mounted.Path != "/" {
		inner = http.StripPrefix(mounted.Path, inner)
	}
	inner = s.providerDevMountedUIHandler(mounted, inner)
	return mountedUITelemetryHandler(mounted, s.protectedUIHandler(mounted, inner, s.redirectMountedUILogin))
}

func (s *Server) providerDevMountedUIHandler(mounted MountedUI, fallback http.Handler) http.Handler {
	if s.providerDevSessions == nil || strings.TrimSpace(mounted.PluginName) == "" {
		return fallback
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}
		resp, ok, err := s.providerDevSessions.ServeUIAsset(r.Context(), s.providerDevUIPrincipal(r, mounted), mounted.PluginName, providerdev.UIAssetRequest{
			Method:   r.Method,
			Path:     providerDevUIAssetPath(mounted, r.URL.Path),
			RawQuery: r.URL.RawQuery,
			Header:   providerDevUIRequestHeader(r.Header),
		})
		if err != nil {
			writeProviderDevError(w, err)
			return
		}
		if !ok {
			fallback.ServeHTTP(w, r)
			return
		}
		writeProviderDevUIAsset(w, resp)
	})
}

func (s *Server) providerDevUIPrincipal(r *http.Request, mounted MountedUI) *principal.Principal {
	if p := PrincipalFromContext(r.Context()); p != nil {
		return p
	}
	if s == nil {
		return nil
	}
	auth, err := s.mountedUIAuthRuntime(mounted)
	if err != nil {
		return nil
	}
	if auth.noAuth {
		p := auth.anonymous
		if p == nil {
			return nil
		}
		enriched, err := s.resolvePrincipalUserID(r.Context(), p)
		if err != nil {
			return p
		}
		return enriched
	}
	if auth.resolver == nil {
		return nil
	}
	p, err := s.resolveRequestPrincipalWithResolver(r, auth.resolver)
	if err != nil || p == nil {
		return nil
	}
	enriched, err := s.resolvePrincipalUserID(r.Context(), p)
	if err != nil {
		return p
	}
	return enriched
}

func (s *Server) adminUIHandler() http.Handler {
	if s.adminUI == nil {
		return http.NotFoundHandler()
	}
	mounted := s.adminMountedUI()
	inner := http.StripPrefix(mounted.Path, mounted.Handler)
	return mountedUITelemetryHandler(mounted, s.protectedUIHandler(mounted, inner, s.redirectAdminUILogin))
}

func (s *Server) adminMountedUI() MountedUI {
	return MountedUI{
		Name:                "builtin_admin",
		Path:                "/admin",
		AuthorizationPolicy: s.adminRoute.AuthorizationPolicy,
		builtInAdmin:        true,
		Routes: []MountedUIRoute{{
			Path:         "/*",
			AllowedRoles: append([]string(nil), s.adminRoute.AllowedRoles...),
		}},
		Handler: s.adminUI,
	}
}

func (s *Server) protectedUIHandler(mounted MountedUI, inner http.Handler, redirectLogin protectedUILoginRedirect) http.Handler {
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

func mountedUITelemetryHandler(mounted MountedUI, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
			ProviderName: mounted.PluginName,
			Surface:      metricutil.InvocationSurfaceUI,
			UIName:       mounted.Name,
		})
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeProtectedUIRequest(w http.ResponseWriter, r *http.Request, mounted MountedUI, redirectLogin protectedUILoginRedirect) (context.Context, bool) {
	if s.authorizer == nil {
		writeError(w, http.StatusInternalServerError, "app authorization is unavailable")
		return nil, false
	}

	p, authenticated, err := s.resolveMountedUIPrincipal(r, mounted)
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
	if err := requireUserCaller(w, p); err != nil {
		return nil, false
	}

	var (
		access  invocation.AccessContext
		allowed bool
	)
	switch {
	case mounted.PluginName != "":
		access, allowed = s.authorizer.ResolveAccess(r.Context(), p, mounted.PluginName)
	case mounted.builtInAdmin:
		access, allowed = s.authorizer.ResolveAdminAccess(r.Context(), p, mounted.AuthorizationPolicy)
	default:
		access, allowed = s.authorizer.ResolvePolicyAccess(r.Context(), p, mounted.AuthorizationPolicy)
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "app access denied")
		return nil, false
	}
	route, matched := mounted.routeForRequestPath(r.URL.Path)
	if !matched || len(route.AllowedRoles) == 0 || !mountedUIRoleAllowed(access.Role, route.AllowedRoles) {
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

func (s *Server) resolveMountedUIPrincipal(r *http.Request, mounted MountedUI) (*principal.Principal, bool, error) {
	auth, err := s.mountedUIAuthRuntime(mounted)
	if err != nil {
		return nil, false, err
	}
	if auth.noAuth {
		return auth.anonymous, true, nil
	}

	p, err := s.resolveRequestPrincipalWithResolver(r, auth.resolver)
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

func (s *Server) redirectMountedUILogin(w http.ResponseWriter, r *http.Request) error {
	target := browserLoginPath + "?next=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

func (s *Server) redirectAdminUILogin(w http.ResponseWriter, r *http.Request) error {
	if s.routeProfile != RouteProfileManagement {
		return s.redirectMountedUILogin(w, r)
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
