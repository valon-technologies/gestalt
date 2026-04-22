package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
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

func requestedAuthSource(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header != "" {
		bearer := strings.TrimPrefix(header, core.BearerScheme)
		if bearer == header {
			return ""
		}
		if typ, ok := principal.ParseTokenType(bearer); ok {
			switch typ {
			case principal.TokenTypeAPI:
				return principal.SourceAPIToken.String()
			case principal.TokenTypeWorkload:
				return principal.SourceWorkloadToken.String()
			}
		}
		return principal.SourceSession.String()
	}

	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		return principal.SourceSession.String()
	}
	return ""
}

func (s *Server) resolveRequestPrincipalWithResolver(r *http.Request, resolver *principal.Resolver) (*principal.Principal, error) {
	var lastErr error

	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		p, err := resolver.ResolveToken(r.Context(), c.Value)
		if p != nil && !principal.IsWorkloadPrincipal(p) {
			return p, nil
		}
		if principal.IsWorkloadPrincipal(p) {
			lastErr = principal.ErrInvalidToken
		} else {
			lastErr = err
		}
	}

	token, err := requestBearerToken(r)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, lastErr
	}

	p, err := resolver.ResolveToken(r.Context(), token)
	if p != nil {
		return p, nil
	}
	if err != nil {
		lastErr = err
	}
	return nil, lastErr
}

func (s *Server) resolveRequestPrincipal(r *http.Request) (*principal.Principal, error) {
	return s.resolveRequestPrincipalWithResolver(r, s.resolver)
}

func (s *Server) resolveRequestPrincipalWithUserID(r *http.Request) (*principal.Principal, error) {
	p, err := s.resolveRequestPrincipal(r)
	if err != nil || p == nil {
		return p, err
	}
	return s.resolvePrincipalUserID(r.Context(), p)
}

func (s *Server) serveAuthenticated(w http.ResponseWriter, r *http.Request, next http.Handler, resolver *principal.Resolver, noAuth bool, anonymous *principal.Principal, auditProvider string) {
	if noAuth {
		ctx := principal.WithPrincipal(r.Context(), anonymous)
		next.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	p, err := s.resolveRequestPrincipalWithResolver(r, resolver)
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

	authSource := requestedAuthSource(r)
	switch {
	case err == nil:
		s.auditRequestEventWithAuthSource(r, authSource, auditProvider, "auth.authenticate", false, errors.New("missing authorization"))
		s.maybeSetMCPResourceMetadataHeader(w, r)
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return
	case errors.Is(err, errInvalidAuthorizationHeader):
		s.auditRequestEventWithAuthSource(r, authSource, auditProvider, "auth.authenticate", false, errInvalidAuthorizationHeader)
		s.maybeSetMCPResourceMetadataHeader(w, r)
		writeError(w, http.StatusUnauthorized, "invalid authorization header format")
		return
	case errors.Is(err, principal.ErrInvalidToken):
		slog.InfoContext(r.Context(), "auth: invalid token", "remote_addr", r.RemoteAddr)
		s.auditRequestEventWithAuthSource(r, authSource, auditProvider, "auth.authenticate", false, principal.ErrInvalidToken)
		s.maybeSetMCPResourceMetadataHeader(w, r)
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	default:
		slog.ErrorContext(r.Context(), "auth: token validation failed", "remote_addr", r.RemoteAddr, "error", err)
		s.auditRequestEventWithAuthSource(r, authSource, auditProvider, "auth.authenticate", false, errors.New("token validation failed"))
		writeError(w, http.StatusInternalServerError, "token validation failed")
		return
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveAuthenticated(w, r, next, s.resolver, s.noAuth, s.anonymousPrincipal, "")
	})
}

func (s *Server) pluginRouteAuthMiddleware(pluginParam string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pluginName := strings.TrimSpace(chi.URLParam(r, pluginParam))
			if pluginName == "" {
				auth := s.serverAuthRuntime()
				s.serveAuthenticated(w, r, next, auth.resolver, auth.noAuth, auth.anonymous, auth.providerName)
				return
			}

			auth, err := s.pluginAuthRuntime(pluginName)
			if err != nil {
				slog.ErrorContext(r.Context(), "plugin route auth provider is not initialized", "plugin", pluginName, "error", err)
				writeError(w, http.StatusInternalServerError, "plugin route auth provider is not initialized")
				return
			}

			s.serveAuthenticated(w, r, next, auth.resolver, auth.noAuth, auth.anonymous, auth.providerName)
		})
	}
}
