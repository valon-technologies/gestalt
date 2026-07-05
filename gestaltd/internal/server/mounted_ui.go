package server

import (
	"context"
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
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/ui"
	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
)

const browserLoginPath = "/login"
const adminUIDirEnv = "GESTALTD_ADMIN_UI_DIR"
const defaultAdminAuthorizationResource = "gestaltAdmin"

type mountedUINavigationPathResolver interface {
	NavigationPathForRequest(string) (string, bool)
}

type protectedUILoginRedirect func(http.ResponseWriter, *http.Request) error

func normalizeAdminRouteConfig(admin AdminRouteConfig, defaultAuthorizationResource string) (AdminRouteConfig, error) {
	admin.AuthorizationPolicy = strings.TrimSpace(admin.AuthorizationPolicy)
	if admin.AuthorizationPolicy == "" {
		admin.AuthorizationPolicy = strings.TrimSpace(defaultAuthorizationResource)
	}
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

func mountedUIsFromEntries(entries map[string]*config.UIEntry, devHandlerResolver func(string) http.Handler) ([]MountedUI, error) {
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

		routes := mountedUIRoutesFromEntry(entry)

		if entry.DevActive {
			handler := lazyDevHandler(devHandlerResolver, name)
			mounted = append(mounted, MountedUI{
				Name:                name,
				Path:                entry.Path,
				AppName:             entry.OwnerApp,
				AuthorizationPolicy: entry.AuthorizationPolicy,
				Routes:              routes,
				Handler:             handler,
				ThemeStylesheet:     entry.ResolvedThemeStylesheet,
				ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
				IsDev:               true,
			})
			continue
		}

		if entry.ResolvedAssetRoot == "" {
			return nil, fmt.Errorf("ui %q configured but asset root not resolved", name)
		}

		handler, err := ui.DirHandler(entry.ResolvedAssetRoot)
		if err != nil {
			return nil, fmt.Errorf("ui %q: %w", name, err)
		}

		mounted = append(mounted, MountedUI{
			Name:                name,
			Path:                entry.Path,
			AppName:             entry.OwnerApp,
			AuthorizationPolicy: entry.AuthorizationPolicy,
			Routes:              routes,
			Handler:             handler,
			ThemeStylesheet:     entry.ResolvedThemeStylesheet,
			ThemeAssetsDir:      entry.ResolvedThemeAssetsDir,
		})
	}

	return mounted, nil
}

func mountedUIRoutesFromEntry(entry *config.UIEntry) []MountedUIRoute {
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
	return routes
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

func resolveConfiguredAdminUI(opts BuiltinAdminUIOptions, providerName string, uiEntries map[string]*config.UIEntry, appEntries map[string]*config.ProviderEntry) (http.Handler, error) {
	assetRoot, resolvedName, sourceKind, err := selectAdminShellSource(strings.TrimSpace(providerName), uiEntries, appEntries)
	if err != nil {
		return nil, err
	}
	if assetRoot == "" {
		return nil, nil
	}

	adminDir := filepath.Join(assetRoot, "admin")
	if _, err := os.Stat(filepath.Join(adminDir, "index.html")); err != nil {
		return nil, fmt.Errorf("%s admin assets not found at %s: %w", sourceKind, adminDir, err)
	}

	handler, err := adminui.DirHandler(adminDir, adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	})
	if err != nil {
		return nil, fmt.Errorf("%s admin assets: %w", sourceKind, err)
	}
	_ = resolvedName
	return handler, nil
}

func selectAdminShellSource(providerName string, uiEntries map[string]*config.UIEntry, appEntries map[string]*config.ProviderEntry) (assetRoot, resolvedName, sourceKind string, err error) {
	if providerName != "" {
		if entry := uiEntries[providerName]; entry != nil {
			if strings.TrimSpace(entry.ResolvedAssetRoot) == "" {
				return "", "", "", fmt.Errorf("server.admin.ui %q asset root not resolved", providerName)
			}
			return entry.ResolvedAssetRoot, providerName, "ui." + providerName, nil
		}
		if entry := appEntries[providerName]; entry != nil && entry.Static != nil {
			if strings.TrimSpace(entry.ResolvedStaticRoot) == "" {
				return "", "", "", fmt.Errorf("server.admin.ui %q static asset root not resolved", providerName)
			}
			return entry.ResolvedStaticRoot, providerName, "app." + providerName, nil
		}
		return "", "", "", fmt.Errorf("server.admin.ui %q not found", providerName)
	}

	if entry, name := selectRootMountedUIAdminShell(uiEntries); entry != nil {
		return entry.ResolvedAssetRoot, name, "ui." + name, nil
	}
	if root, name := selectRootMountedAppStaticAdminShell(appEntries); root != "" {
		return root, name, "app." + name, nil
	}
	return "", "", "", nil
}

func selectRootMountedUIAdminShell(entries map[string]*config.UIEntry) (*config.UIEntry, string) {
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
			return entry, name
		}
	}
	return nil, ""
}

func selectRootMountedAppStaticAdminShell(entries map[string]*config.ProviderEntry) (string, string) {
	names := make([]string, 0, len(entries))
	for name, entry := range entries {
		if entry == nil || entry.Static == nil {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		entry := entries[name]
		if strings.TrimSpace(entry.Static.Mount) != "/" {
			continue
		}
		if appStaticHasAdminShell(entry) {
			return entry.ResolvedStaticRoot, name
		}
	}
	return "", ""
}

func appStaticHasAdminShell(entry *config.ProviderEntry) bool {
	if entry == nil || strings.TrimSpace(entry.ResolvedStaticRoot) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(entry.ResolvedStaticRoot, "admin", "index.html"))
	return err == nil && !info.IsDir()
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
		if normalized[i].AppLevelAuth {
			continue
		}
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
	if mounted.AppLevelAuth {
		return nil
	}
	if !mountedUIRequiresAuthorization(mounted) {
		return nil
	}
	if len(mounted.Routes) == 0 {
		return fmt.Errorf("authorization-bound UIs must declare at least one route")
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
		return fmt.Errorf("authorization-bound UIs must declare a route covering /")
	}
	return nil
}

func (s *Server) mountedUIHandler(mounted MountedUI) http.Handler {
	inner := mounted.Handler
	if inner == nil {
		return http.NotFoundHandler()
	}
	if mounted.IsDev {
		if mounted.ThemeStylesheet != "" || mounted.ThemeAssetsDir != "" {
			inner = mountedUIThemeHandlerFullPath(mounted, inner)
		}
		h := mountedUITelemetryHandler(mounted, s.protectedUIHandler(mounted, inner, s.redirectMountedUILogin))
		return withDevContentSecurityPolicy(h)
	}
	inner = mountedUIThemeHandler(mounted, inner)
	if mounted.Path != "/" {
		inner = http.StripPrefix(mounted.Path, inner)
	}
	return mountedUITelemetryHandler(mounted, s.protectedUIHandler(mounted, inner, s.redirectMountedUILogin))
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
	if !mountedUIRequiresAuthorization(mounted) {
		return inner
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mountedUIPathIsPublic(mounted, r.URL.Path) {
			inner.ServeHTTP(w, r)
			return
		}
		ctx, ok := s.authorizeProtectedUIRequest(w, r, mounted, redirectLogin)
		if !ok {
			return
		}
		inner.ServeHTTP(w, r.WithContext(ctx))
	})
}

func mountedUIPathIsPublic(mounted MountedUI, requestPath string) bool {
	if len(mounted.PublicPaths) == 0 {
		return false
	}
	relativePath := requestPath
	if mounted.Path != "/" {
		relativePath = strings.TrimPrefix(requestPath, mounted.Path)
	}
	if relativePath == "" {
		relativePath = "/"
	}
	if !strings.HasPrefix(relativePath, "/") {
		relativePath = "/" + relativePath
	}
	for _, pattern := range mounted.PublicPaths {
		if PublicPathMatches(pattern, relativePath) {
			return true
		}
	}
	return false
}

func PublicPathMatches(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	path = strings.TrimSpace(path)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/**") {
		base := strings.TrimSuffix(pattern, "/**")
		if base == "" {
			return true
		}
		return path == base || strings.HasPrefix(path, base+"/")
	}
	if strings.HasSuffix(pattern, "/*") {
		base := strings.TrimSuffix(pattern, "/*")
		if base == "" {
			trimmed := strings.TrimPrefix(path, "/")
			return trimmed == "" || !strings.Contains(trimmed, "/")
		}
		if path == base {
			return true
		}
		rest := strings.TrimPrefix(path, base+"/")
		return strings.HasPrefix(path, base+"/") && rest != "" && !strings.Contains(rest, "/")
	}
	return path == pattern
}

func mountedUITelemetryHandler(mounted MountedUI, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
			ProviderName: mounted.AppName,
			Surface:      metricutil.InvocationSurfaceUI,
			UIName:       mounted.Name,
		})
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeProtectedUIRequest(w http.ResponseWriter, r *http.Request, mounted MountedUI, redirectLogin protectedUILoginRedirect) (context.Context, bool) {
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

	if mounted.AppLevelAuth {
		access, allowed, err := s.authorizeMountedAppAccess(r.Context(), p, mounted)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to authorize app access")
			return nil, false
		}
		if !allowed {
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

	route, matched := mounted.routeForRequestPath(r.URL.Path)
	access, allowed, err := s.authorizeMountedUIRoute(r.Context(), p, mounted, route, matched)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize app access")
		return nil, false
	}
	if !allowed {
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

func (s *Server) authorizeMountedUIRoute(ctx context.Context, p *principal.Principal, mounted MountedUI, route MountedUIRoute, matched bool) (invocation.AccessContext, bool, error) {
	if !matched || len(route.AllowedRoles) == 0 {
		return invocation.AccessContext{}, false, nil
	}
	if !mountedUIRequiresAuthorization(mounted) {
		return invocation.AccessContext{}, true, nil
	}
	if s.authorization == nil {
		return invocation.AccessContext{}, false, nil
	}
	resourceName, subjectID, ok := mountedUIAuthorizationSubject(p, mounted)
	if !ok {
		return invocation.AccessContext{}, false, nil
	}
	return s.authorizeMountedResourceRoles(ctx, resourceName, subjectID, route.AllowedRoles)
}

func (s *Server) authorizeMountedAppAccess(ctx context.Context, p *principal.Principal, mounted MountedUI) (invocation.AccessContext, bool, error) {
	if !mountedUIRequiresAuthorization(mounted) {
		return invocation.AccessContext{}, true, nil
	}
	if s.authorization == nil {
		if s.noAuth {
			return invocation.AccessContext{}, true, nil
		}
		return invocation.AccessContext{}, false, nil
	}
	resourceName, subjectID, ok := mountedUIAuthorizationSubject(p, mounted)
	if !ok {
		return invocation.AccessContext{}, false, nil
	}
	return s.authorizeMountedResourceRoles(ctx, resourceName, subjectID, nil)
}

func mountedUIAuthorizationSubject(p *principal.Principal, mounted MountedUI) (resourceName, subjectID string, ok bool) {
	resourceName = mountedUIAuthorizationResourceName(mounted)
	if resourceName == "" {
		return "", "", false
	}
	subjectID = principal.EffectiveCredentialSubjectID(principal.Canonicalized(p))
	if strings.TrimSpace(subjectID) == "" {
		return "", "", false
	}
	return resourceName, subjectID, true
}

func (s *Server) authorizeMountedResourceRoles(ctx context.Context, resourceName, subjectID string, allowedRoles []string) (invocation.AccessContext, bool, error) {
	roles, err := s.mountedUIAuthorizationRoles(ctx, subjectID, resourceName)
	if err != nil {
		return invocation.AccessContext{}, false, err
	}
	if len(allowedRoles) == 0 {
		for role := range roles {
			return invocation.AccessContext{Policy: resourceName, Role: strings.TrimSpace(role)}, true, nil
		}
	} else {
		for _, allowedRole := range allowedRoles {
			allowedRole = strings.TrimSpace(allowedRole)
			if _, ok := roles[allowedRole]; ok {
				return invocation.AccessContext{Policy: resourceName, Role: allowedRole}, true, nil
			}
		}
	}
	defaultRole, err := s.mountedUIResourceDefaultRole(ctx, resourceName)
	if err != nil {
		return invocation.AccessContext{}, false, err
	}
	if defaultRole != "" && (len(allowedRoles) == 0 || mountedUIRoleAllowed(defaultRole, allowedRoles)) {
		return invocation.AccessContext{Policy: resourceName, Role: defaultRole}, true, nil
	}
	return invocation.AccessContext{}, false, nil
}

func mountedUIAuthorizationResourceName(mounted MountedUI) string {
	if name := strings.TrimSpace(mounted.AuthorizationPolicy); name != "" {
		return name
	}
	return strings.TrimSpace(mounted.AppName)
}

func mountedUIRequiresAuthorization(mounted MountedUI) bool {
	if mounted.AppLevelAuth {
		return true
	}
	if strings.TrimSpace(mounted.AuthorizationPolicy) != "" {
		return true
	}
	if mounted.builtInAdmin {
		return false
	}
	return strings.TrimSpace(mounted.AppName) != "" && len(mounted.Routes) > 0
}

func (s *Server) mountedUIAuthorizationRoles(ctx context.Context, subjectID, resourceName string) (map[string]struct{}, error) {
	roles := map[string]struct{}{}
	pageToken := ""
	for {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
						Type: "subject",
						Id:   strings.TrimSpace(subjectID),
					}},
				},
				Resource: &proto.Resource{
					Type: strings.TrimSpace(resourceName),
					Id:   strings.TrimSpace(resourceName),
				},
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, relationship := range resp.GetRelationships() {
			relation := strings.TrimSpace(relationship.GetTuple().GetRelation())
			if relation != "" {
				roles[relation] = struct{}{}
			}
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return roles, nil
		}
	}
}

func (s *Server) mountedUIResourceDefaultRole(ctx context.Context, resourceName string) (string, error) {
	resp, err := s.authorization.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
		Filter:   &proto.AuthorizationModelResourceTypeFilter{Name: strings.TrimSpace(resourceName)},
		PageSize: 1,
	})
	if err != nil {
		return "", err
	}
	for _, resourceType := range resp.GetResourceTypes() {
		if strings.TrimSpace(resourceType.GetName()) == strings.TrimSpace(resourceName) {
			return strings.TrimSpace(resourceType.GetDefaultRole()), nil
		}
	}
	return "", nil
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
