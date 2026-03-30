package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type contextKey string

const (
	userContextKey   contextKey = "user"
	userIDContextKey contextKey = "userID"

	anonymousEmail = "anonymous@gestalt"
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

func requestMetaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := invocation.RequestMeta{
			ClientIP:   invocation.ClientIP(r),
			RemoteAddr: invocation.RemoteAddrIP(r),
			UserAgent:  r.UserAgent(),
		}
		ctx := invocation.WithRequestMeta(r.Context(), meta)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func maxBodyMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.secureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.noAuth {
			ctx := principal.WithPrincipal(r.Context(), s.anonymousPrincipal)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		var p *principal.Principal
		var lastErr error

		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			p, lastErr = s.resolver.ResolveToken(r.Context(), c.Value)
		}

		if p == nil {
			if header := r.Header.Get("Authorization"); header != "" {
				bearer := strings.TrimPrefix(header, core.BearerScheme)
				if bearer == header {
					writeError(w, http.StatusUnauthorized, "invalid authorization header format")
					return
				}
				p, lastErr = s.resolver.ResolveToken(r.Context(), bearer)
			}
		}

		if p == nil {
			if lastErr == nil {
				writeError(w, http.StatusUnauthorized, "missing authorization")
				return
			}
			if errors.Is(lastErr, principal.ErrInvalidToken) {
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

func (s *Server) proxyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.noAuth {
			ctx := principal.WithPrincipal(r.Context(), s.anonymousPrincipal)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		var p *principal.Principal
		var lastErr error

		if header := r.Header.Get("Proxy-Authorization"); header != "" {
			bearer := strings.TrimPrefix(header, core.BearerScheme)
			if bearer != header {
				p, lastErr = s.resolver.ResolveToken(r.Context(), bearer)
			}
		}

		if p == nil {
			if header := r.Header.Get("Authorization"); header != "" {
				bearer := strings.TrimPrefix(header, core.BearerScheme)
				if bearer != header {
					p, lastErr = s.resolver.ResolveToken(r.Context(), bearer)
				}
			}
		}

		if p == nil {
			w.Header().Set("Proxy-Authenticate", "Bearer")
			if lastErr != nil && !errors.Is(lastErr, principal.ErrInvalidToken) {
				writeError(w, http.StatusInternalServerError, "token validation failed")
				return
			}
			writeError(w, http.StatusProxyAuthRequired, "proxy authentication required")
			return
		}

		ctx := principal.WithPrincipal(r.Context(), p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
