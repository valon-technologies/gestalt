package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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

// contentSecurityPolicy is the CSP applied to all responses. script-src and
// style-src require 'unsafe-inline' because the Next.js static export embeds
// inline <script> tags for RSC flight data that change with every build,
// making hash-based policies impractical.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.secureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

const scopedIntegrationKey contextKey = "scopedIntegration"

func scopeIntegration(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), scopedIntegrationKey, name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func authzMiddleware(allowedUsers []string) func(http.Handler) http.Handler {
	userSet := make(map[string]struct{}, len(allowedUsers))
	for _, u := range allowedUsers {
		userSet[u] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFromContext(r.Context())
			if p == nil || p.Identity == nil {
				writeError(w, http.StatusForbidden, "access denied")
				return
			}
			if _, ok := userSet[p.Identity.Email]; ok {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, "access denied")
		})
	}
}

var errInvalidAuthorizationHeader = errors.New("invalid authorization header format")

func requestBearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", nil
	}
	bearer := strings.TrimPrefix(header, core.BearerScheme)
	if bearer == header {
		return "", errInvalidAuthorizationHeader
	}
	return bearer, nil
}

func (s *Server) resolveRequestPrincipal(r *http.Request) (*principal.Principal, error) {
	var lastErr error

	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		p, err := s.resolver.ResolveToken(r.Context(), c.Value)
		if p != nil {
			return p, nil
		}
		lastErr = err
	}

	token, err := requestBearerToken(r)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, lastErr
	}

	p, err := s.resolver.ResolveToken(r.Context(), token)
	if p != nil {
		return p, nil
	}
	if err != nil {
		lastErr = err
	}
	return nil, lastErr
}

func (s *Server) resolveRequestPrincipalWithUserID(r *http.Request) (*principal.Principal, error) {
	p, err := s.resolveRequestPrincipal(r)
	if err != nil || p == nil {
		return p, err
	}
	return s.resolvePrincipalUserID(r.Context(), p)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.noAuth {
			ctx := principal.WithPrincipal(r.Context(), s.anonymousPrincipal)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		p, err := s.resolveRequestPrincipal(r)
		if err == nil && p != nil {
			enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p)
			switch {
			case enrichErr == nil && enriched != nil:
				p = enriched
			case enrichErr != nil:
				slog.WarnContext(r.Context(), "auth: unable to resolve user ID", "error", enrichErr)
			}
			ctx := principal.WithPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		switch {
		case err == nil:
			writeError(w, http.StatusUnauthorized, "missing authorization")
			return
		case errors.Is(err, errInvalidAuthorizationHeader):
			writeError(w, http.StatusUnauthorized, "invalid authorization header format")
			return
		case errors.Is(err, principal.ErrInvalidToken):
			slog.InfoContext(r.Context(), "auth: invalid token", "remote_addr", r.RemoteAddr)
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		default:
			slog.ErrorContext(r.Context(), "auth: token validation failed", "remote_addr", r.RemoteAddr, "error", err)
			writeError(w, http.StatusInternalServerError, "token validation failed")
			return
		}
	})
}
