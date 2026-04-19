package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/providerdev"
)

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
