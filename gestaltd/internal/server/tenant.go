package server

import (
	"context"
	"net/http"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func (s *Server) tenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.tenantResolver == nil || s.tenantResolver.Empty() || s.tenantResolutionSkipped(r) {
			next.ServeHTTP(w, r)
			return
		}
		resolved, ok := s.tenantResolver.ResolveHost(r.Host)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown tenant")
			return
		}
		scope := gestalt.TenantScope{
			TenantID:    resolved.ID,
			Host:        resolved.Host,
			TenantBound: true,
		}
		ctx := gestalt.ContextWithOutgoingTenantScope(r.Context(), scope)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) tenantResolutionSkipped(r *http.Request) bool {
	if s != nil && s.routeProfile == RouteProfileManagement {
		return true
	}
	if r == nil || r.URL == nil {
		return true
	}
	path := strings.TrimRight(r.URL.Path, "/")
	switch path {
	case "/health", "/ready", "/metrics":
		return true
	default:
		return path == "/admin" || strings.HasPrefix(path, "/admin/")
	}
}

func tenantContextWithPrincipal(ctx context.Context, p *principal.Principal) context.Context {
	scope, ok := gestalt.TenantScopeFromContext(ctx)
	if !ok {
		return ctx
	}
	p = principal.Canonicalized(p)
	if p != nil {
		scope.PrincipalID = p.SubjectID
	}
	return gestalt.ContextWithOutgoingTenantScope(ctx, scope)
}
