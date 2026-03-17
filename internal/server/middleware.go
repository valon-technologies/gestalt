package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/valon-technologies/toolshed/core"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext returns the authenticated user from the request context,
// or nil if no user is present.
func UserFromContext(ctx context.Context) *core.UserIdentity {
	u, _ := ctx.Value(userContextKey).(*core.UserIdentity)
	return u
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dev mode bypass: trust the X-Dev-User-Email header.
		if s.devMode {
			if email := r.Header.Get("X-Dev-User-Email"); email != "" {
				identity := &core.UserIdentity{Email: email}
				ctx := context.WithValue(r.Context(), userContextKey, identity)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		header := r.Header.Get("Authorization")
		if header == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		if token == header {
			writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		// Try session token first.
		identity, err := s.auth.ValidateToken(r.Context(), token)
		if err == nil && identity != nil {
			ctx := context.WithValue(r.Context(), userContextKey, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fall back to API token: hash and look up.
		hashed := hashToken(token)
		apiToken, err := s.datastore.ValidateAPIToken(r.Context(), hashed)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			writeError(w, http.StatusInternalServerError, "token validation failed")
			return
		}
		if apiToken == nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		user, err := s.datastore.FindOrCreateUser(r.Context(), apiToken.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}

		identity = &core.UserIdentity{
			Email:       user.Email,
			DisplayName: user.DisplayName,
		}
		ctx := context.WithValue(r.Context(), userContextKey, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
