package server

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) adminAPIAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ok := s.authorizeAdminAPIRequest(w, r)
		if !ok {
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authorizeAdminAPIRequest(w http.ResponseWriter, r *http.Request) (context.Context, bool) {
	mounted := s.adminMountedUI()
	if !mountedUIRequiresAuthorization(mounted) {
		return r.Context(), true
	}

	p, authenticated, err := s.resolveMountedUIPrincipal(r, mounted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return nil, false
	}
	if !authenticated {
		writeError(w, http.StatusUnauthorized, "missing authorization")
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
