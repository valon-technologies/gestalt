package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type contextKey string

const (
	userContextKey   contextKey = "user"
	userIDContextKey contextKey = "userID"
)

func UserFromContext(ctx context.Context) *core.UserIdentity {
	if p := principal.FromContext(ctx); p != nil {
		return p.Identity
	}
	u, _ := ctx.Value(userContextKey).(*core.UserIdentity)
	return u
}

func UserIDFromContext(ctx context.Context) string {
	if p := principal.FromContext(ctx); p != nil {
		return p.UserID
	}
	id, _ := ctx.Value(userIDContextKey).(string)
	return id
}

func PrincipalFromContext(ctx context.Context) *principal.Principal {
	return principal.FromContext(ctx)
}

func maxBodyMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.devMode {
			if email := r.Header.Get("X-Dev-User-Email"); email != "" {
				p := s.resolver.ResolveEmail(email)
				ctx := principal.WithPrincipal(r.Context(), p)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		header := r.Header.Get("Authorization")
		if header == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		token := strings.TrimPrefix(header, core.BearerScheme)
		if token == header {
			writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		}

		p, err := s.resolver.ResolveToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, principal.ErrInvalidToken) {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			writeError(w, http.StatusInternalServerError, "token validation failed")
			return
		}

		ctx := principal.WithPrincipal(r.Context(), p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// devCORS permits cross-origin requests in dev mode so the Next.js dev
// server (hot reload on a different port) can reach the API.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
