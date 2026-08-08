package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/ui/adminui"
)

const browserLoginPath = "/api/v1/auth/login"
const defaultAdminAuthorizationResource = "gestaltAdmin"

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

	roles, err := packageio.NormalizeUIAllowedRoles("admin allowedRoles", admin.AllowedRoles)
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

func resolveBuiltinAdminUI(opts BuiltinAdminUIOptions) (http.Handler, error) {
	handler := adminui.EmbeddedHandler(adminui.Options{
		BrandHref: opts.BrandHref,
		LoginBase: opts.LoginBase,
	})
	if handler == nil {
		return nil, fmt.Errorf("embedded admin ui assets not found")
	}
	return handler, nil
}

func normalizeMountedUIs(mounted []MountedUI) ([]MountedUI, error) {
	if len(mounted) == 0 {
		return nil, nil
	}
	return append([]MountedUI(nil), mounted...), nil
}

func (s *Server) mountedUIHandler(mounted MountedUI) http.Handler {
	inner := mounted.Handler
	if inner == nil {
		return http.NotFoundHandler()
	}
	// Build the static-serving layer. When a tunnel registration exists for
	// this app, the tunnel proxy replaces the local static handler. The proxy
	// is wrapped by theme/strip-prefix (for the local fallback path only) and
	// then by auth, so tunnel-proxied UIs require the same authorization as
	// local UIs.
	if strings.TrimSpace(mounted.AppName) != "" {
		inner = s.tunnelUIProxyHandler(mounted.AppName, mounted.Path, inner)
	}
	if mounted.IsDev {
		if mounted.ThemeStylesheet != "" || mounted.ThemeAssetsDir != "" {
			inner = mountedUIThemeHandlerFullPath(mounted, inner)
		}
		inner = mountedUITelemetryHandler(mounted, inner)
		return withDevContentSecurityPolicy(s.protectedUIHandler(mounted, inner, s.redirectMountedUILogin))
	}
	inner = mountedUIThemeHandler(mounted, inner)
	if mounted.Path != "/" {
		inner = http.StripPrefix(mounted.Path, inner)
	}
	inner = mountedUITelemetryHandler(mounted, inner)
	return s.protectedUIHandler(mounted, inner, s.redirectMountedUILogin)
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
		AllowedRoles:        append([]string(nil), s.adminRoute.AllowedRoles...),
		builtInAdmin:        true,
		Handler:             s.adminUI,
	}
}

func (s *Server) protectedUIHandler(mounted MountedUI, inner http.Handler, redirectLogin protectedUILoginRedirect) http.Handler {
	if !mountedUIRequiresAuthorization(mounted) {
		return inner
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := s.authorizeMountedResource(w, r, mounted, redirectLogin)
		if !ok {
			return
		}
		inner.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) adminAPIAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := s.authorizeMountedResource(w, r, s.adminMountedUI(), nil)
		if !ok {
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authorizeMountedResource(
	w http.ResponseWriter,
	r *http.Request,
	mounted MountedUI,
	redirectLogin protectedUILoginRedirect,
) (context.Context, bool) {
	if !mountedUIRequiresAuthorization(mounted) {
		return r.Context(), true
	}

	auth, err := s.mountedUIAuthRuntime(mounted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve auth provider")
		return nil, false
	}
	if auth.noAuth {
		p := auth.anonymous
		if enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p); enrichErr != nil {
			slog.WarnContext(r.Context(), "auth: unable to resolve anonymous user ID", "error", enrichErr)
		} else if enriched != nil {
			p = enriched
		}
		return principal.WithPrincipal(r.Context(), p), true
	}

	p, err := s.resolveRequestPrincipalWithResolver(r, auth.resolver)
	switch {
	case errors.Is(err, errInvalidAuthorizationHeader):
		writeError(w, http.StatusUnauthorized, "invalid authorization header format")
		return nil, false
	case errors.Is(err, principal.ErrInvalidToken):
		s.writeMountedUnauthenticated(w, r, redirectLogin)
		return nil, false
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return nil, false
	case p == nil:
		s.writeMountedUnauthenticated(w, r, redirectLogin)
		return nil, false
	}

	if enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p); enrichErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return nil, false
	} else if enriched != nil {
		p = enriched
	}

	if err := requireUserCaller(w, p); err != nil {
		return nil, false
	}

	access, allowed, err := s.authorizeMountedAppAccess(principal.WithPrincipal(r.Context(), p), p, mounted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authorize app access")
		return nil, false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "app access denied")
		return nil, false
	}

	ctx := principal.WithPrincipal(r.Context(), p)
	if access.Policy != "" || access.Role != "" {
		ctx = invocation.WithAccessContext(ctx, access)
	}
	return ctx, true
}

func (s *Server) writeMountedUnauthenticated(w http.ResponseWriter, r *http.Request, redirectLogin protectedUILoginRedirect) {
	if redirectLogin != nil && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		if err := redirectLogin(w, r); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeError(w, http.StatusUnauthorized, "missing authorization")
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

func (s *Server) authorizeMountedAppAccess(ctx context.Context, p *principal.Principal, mounted MountedUI) (invocation.AccessContext, bool, error) {
	if !mountedUIRequiresAuthorization(mounted) {
		return invocation.AccessContext{}, true, nil
	}
	if s.authorization == nil {
		return invocation.AccessContext{}, true, nil
	}
	resourceName, subjectID, ok, err := s.mountedUIAuthorizationSubject(ctx, p, mounted)
	if err != nil || !ok {
		return invocation.AccessContext{}, false, err
	}
	return s.authorizeMountedResourceRoles(ctx, resourceName, subjectID, mounted.AllowedRoles)
}

// mountedUIAuthorizationSubject resolves the canonical subject the mounted UI
// boundary authorizes as. Unresolvable or provider-opaque subjects are denied;
// the raw token subject is never used.
func (s *Server) mountedUIAuthorizationSubject(
	ctx context.Context,
	p *principal.Principal,
	mounted MountedUI,
) (resourceName, subjectID string, ok bool, err error) {
	resourceName = mountedUIAuthorizationResourceName(mounted)
	if resourceName == "" {
		return "", "", false, nil
	}
	subjectID, err = principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	switch {
	case errors.Is(err, principal.ErrCredentialSubjectRequired),
		errors.Is(err, principal.ErrOpaqueCredentialSubject):
		return "", "", false, nil
	case err != nil:
		return "", "", false, err
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return "", "", false, nil
	}
	return resourceName, subjectID, true, nil
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
	if strings.HasPrefix(mounted.Name, "app:") && !mounted.AppLevelAuth {
		return false
	}
	if mounted.AppLevelAuth {
		return true
	}
	if strings.TrimSpace(mounted.AuthorizationPolicy) != "" {
		return true
	}
	if mounted.builtInAdmin {
		return false
	}
	return strings.TrimSpace(mounted.AppName) != ""
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
				Resource: invocation.AuthorizationResource(resourceName, s.providerKinds),
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
	typeName := invocation.AuthorizationResource(resourceName, s.providerKinds).GetType()
	resp, err := s.authorization.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
		Filter:   &proto.AuthorizationModelResourceTypeFilter{Name: strings.TrimSpace(typeName)},
		PageSize: 1,
	})
	if err != nil {
		return "", err
	}
	for _, resourceType := range resp.GetResourceTypes() {
		if strings.TrimSpace(resourceType.GetName()) == strings.TrimSpace(typeName) {
			return strings.TrimSpace(resourceType.GetDefaultRole()), nil
		}
	}
	return "", nil
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
