package server

import (
	"context"
	"errors"
	"fmt"
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
	return s.authorizeMountedResourceRoles(ctx, resourceName, subjectID, mounted.AllowedRoles)
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
