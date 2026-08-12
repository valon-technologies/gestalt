package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
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
		// Always wrap so /brand.json (and theme endpoints) are served from
		// deployment assets instead of being forwarded to the Vite process.
		inner = mountedUIThemeHandlerFullPath(mounted, inner)
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
	return s.authorizeMountedResourceRoles(ctx, mountedResourceAccess{
		appKey:       strings.TrimSpace(mounted.AppName),
		resourceName: resourceName,
		subjectID:    subjectID,
		allowedRoles: mounted.AllowedRoles,
	})
}

// mountedResourceAccess carries the identifiers a mounted-UI decision needs:
// the app key that names the action, the policy/resource name, the canonical
// subject, and the roles the mount restricts access to.
type mountedResourceAccess struct {
	appKey       string
	resourceName string
	subjectID    string
	allowedRoles []string
}

// action names the authorization action for a mounted UI. It is the app key so
// that a mounted UI and an operation invocation for the same app ask the
// evaluator the same question; policy-only mounts (the built-in admin UI) fall
// back to the policy name.
func (m mountedResourceAccess) action() string {
	if m.appKey != "" {
		return m.appKey
	}
	return m.resourceName
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

// authorizeMountedResourceRoles decides mounted-UI access through the shared
// authorization evaluator. The mount's AllowedRoles still gate access: when the
// mount declares roles, the evaluator must report one of them as the relation
// that authorized the action. The resource type's defaultRole keeps its current
// meaning as the fallback role when the evaluator reports no relation of its
// own.
func (s *Server) authorizeMountedResourceRoles(ctx context.Context, access mountedResourceAccess) (invocation.AccessContext, bool, error) {
	decision, err := s.checkResourceAccess(ctx, invocation.ResourceAccessRequest{
		SubjectID:    access.subjectID,
		Action:       access.action(),
		Resource:     s.authorizationResource(access.resourceName),
		AllowedRoles: access.allowedRoles,
	})
	if err != nil {
		return invocation.AccessContext{}, false, err
	}
	if decision.Allowed && decision.Role != "" {
		return invocation.AccessContext{Policy: access.resourceName, Role: decision.Role}, true, nil
	}

	// The evaluator did not name an authorizing relation. Read the active model
	// once: it supplies both the declared actions (which say whether the
	// evaluator could answer this question at all) and the defaultRole.
	model, err := s.mountedUIResourceModel(ctx, access.resourceName)
	if err != nil {
		return invocation.AccessContext{}, false, err
	}
	if !model.answersAction(access.action()) {
		legacy, allowed, legacyErr := s.legacyDirectGrantMountedAccess(ctx, access, model)
		if legacyErr != nil {
			return invocation.AccessContext{}, false, legacyErr
		}
		if allowed {
			return legacy, true, nil
		}
	}

	if model.defaultRole != "" && (len(access.allowedRoles) == 0 || mountedUIRoleAllowed(model.defaultRole, access.allowedRoles)) {
		return invocation.AccessContext{Policy: access.resourceName, Role: model.defaultRole}, true, nil
	}
	if decision.Allowed && len(access.allowedRoles) == 0 {
		// The evaluator allowed the action without naming a relation and the
		// mount restricts no roles, so there is nothing left to gate on.
		return invocation.AccessContext{Policy: access.resourceName}, true, nil
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

// mountedUIWildcardAction is the model action name that matches every action.
const mountedUIWildcardAction = "*"

// mountedUILegacyDecisionLogMessage names the transition-shim warning so it can
// be alerted on and so its disappearance proves the shim is unused.
const mountedUILegacyDecisionLogMessage = "auth: mounted UI resource type declares no matching action; using legacy direct-grant path"

// mountedUIModelSnapshot is the active-model view of one mounted resource type.
// It is a model read, not a relationship traversal: it decides nothing, it only
// reports what the model can express.
type mountedUIModelSnapshot struct {
	typeName    string
	found       bool
	defaultRole string
	actions     []string
}

// answersAction reports whether the evaluator can express a decision about this
// action for this resource type. A resource type that is absent from the active
// model, or that declares neither the action nor a wildcard action, cannot
// answer, so its denial carries no information.
func (m mountedUIModelSnapshot) answersAction(action string) bool {
	if !m.found {
		return false
	}
	action = strings.TrimSpace(action)
	for _, name := range m.actions {
		name = strings.TrimSpace(name)
		if name == mountedUIWildcardAction {
			return true
		}
		if action != "" && name == action {
			return true
		}
	}
	return false
}

// mountedUIResourceModel reads the mount's resource type from the active model.
// Within a listing request the read is memoized per resource type, so filtering
// many apps does not re-read the same model entry once per app.
func (s *Server) mountedUIResourceModel(ctx context.Context, resourceName string) (mountedUIModelSnapshot, error) {
	if s == nil || s.authorization == nil {
		return mountedUIModelSnapshot{}, invocation.ErrAuthorizationUnavailable
	}
	typeName := strings.TrimSpace(s.authorizationResource(resourceName).GetType())
	cache := listingDecisionCacheFromContext(ctx)
	if cached, ok := cache.model(typeName); ok {
		return cached, nil
	}
	resp, err := s.authorization.ListActiveModelResourceTypes(ctx, &proto.ListActiveModelResourceTypesRequest{
		Filter:   &proto.AuthorizationModelResourceTypeFilter{Name: typeName},
		PageSize: 1,
	})
	if err != nil {
		return mountedUIModelSnapshot{}, err
	}
	snapshot := mountedUIModelSnapshot{typeName: typeName}
	for _, resourceType := range resp.GetResourceTypes() {
		if strings.TrimSpace(resourceType.GetName()) != typeName {
			continue
		}
		snapshot.found = true
		snapshot.defaultRole = strings.TrimSpace(resourceType.GetDefaultRole())
		for _, action := range resourceType.GetActions() {
			if name := strings.TrimSpace(action.GetName()); name != "" {
				snapshot.actions = append(snapshot.actions, name)
			}
		}
		break
	}
	cache.putModel(typeName, snapshot)
	return snapshot, nil
}

// maxLegacyDirectGrantPages bounds the transition shim's relationship scan so a
// paginating provider cannot make a mounted-UI request loop without limit.
const maxLegacyDirectGrantPages = 20

// legacyDirectGrantMountedAccess is a TRANSITION SHIM. Delete it once T1's
// inventory confirms every mounted resource type declares either an action
// matching its app key/policy name or a "*" wildcard action.
//
// CheckAccessRequest is strictly action-shaped and there is no effective-roles
// RPC, so a resource type that declares no matching action makes the evaluator
// answer "denied" to a question it cannot represent. Treating that as a real
// denial would lock existing direct grant holders out of their mounted UI -
// including the built-in admin UI, whose policy typically declares no actions.
// This shim runs ONLY when a successfully read model proves the evaluator
// cannot answer; a provider or transport error never reaches it and always
// denies.
func (s *Server) legacyDirectGrantMountedAccess(
	ctx context.Context,
	access mountedResourceAccess,
	model mountedUIModelSnapshot,
) (invocation.AccessContext, bool, error) {
	slog.WarnContext(ctx, mountedUILegacyDecisionLogMessage,
		"resourceType", model.typeName,
		"resource", access.resourceName,
		"action", access.action(),
		"modelKnowsResourceType", model.found,
	)

	roles, err := s.legacyDirectGrantRoles(ctx, access.subjectID, access.resourceName)
	if err != nil {
		return invocation.AccessContext{}, false, err
	}
	if len(access.allowedRoles) == 0 {
		if len(roles) > 0 {
			return invocation.AccessContext{Policy: access.resourceName, Role: roles[0]}, true, nil
		}
		return invocation.AccessContext{}, false, nil
	}
	for _, allowedRole := range access.allowedRoles {
		allowedRole = strings.TrimSpace(allowedRole)
		if allowedRole != "" && slices.Contains(roles, allowedRole) {
			return invocation.AccessContext{Policy: access.resourceName, Role: allowedRole}, true, nil
		}
	}
	return invocation.AccessContext{}, false, nil
}

// legacyDirectGrantRoles lists the subject's direct relations on the resource.
// It is part of the transition shim above and must not be called from the main
// decision path.
func (s *Server) legacyDirectGrantRoles(ctx context.Context, subjectID, resourceName string) ([]string, error) {
	roles := make([]string, 0, 4)
	pageToken := ""
	for page := 0; page < maxLegacyDirectGrantPages; page++ {
		resp, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{
			Filter: &proto.RelationshipFilter{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
						Type: "subject",
						Id:   strings.TrimSpace(subjectID),
					}},
				},
				Resource: s.authorizationResource(resourceName),
			},
			PageSize:  500,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, relationship := range resp.GetRelationships() {
			relation := strings.TrimSpace(relationship.GetTuple().GetRelation())
			if relation != "" && !slices.Contains(roles, relation) {
				roles = append(roles, relation)
			}
		}
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}
	return roles, nil
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
