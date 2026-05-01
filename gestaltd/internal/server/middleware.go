package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/s3"
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

const (
	defaultMaxBodyBytes         = 1 << 20
	providerDevCallMaxBodyBytes = 128 << 20
	s3ObjectAccessMaxBodyBytes  = 10 << 30
)

func maxBodyMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyLimitForRequest(r, limit))
			next.ServeHTTP(w, r)
		})
	}
}

func maxBodyLimitForRequest(r *http.Request, defaultLimit int64) int64 {
	if isProviderDevCompleteCallRequest(r) {
		return providerDevCallMaxBodyBytes
	}
	if isS3ObjectAccessRequest(r) {
		return s3ObjectAccessMaxBodyBytes
	}
	return defaultLimit
}

func isProviderDevCompleteCallRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost || r.URL == nil {
		return false
	}
	rest, ok := strings.CutPrefix(r.URL.Path, providerdev.PathAttachments+"/")
	if !ok {
		return false
	}
	sessionID, callID, ok := strings.Cut(rest, "/calls/")
	return ok && sessionID != "" && callID != "" && !strings.Contains(sessionID, "/") && !strings.Contains(callID, "/")
}

func isS3ObjectAccessRequest(r *http.Request) bool {
	return r != nil && r.URL != nil && strings.HasPrefix(r.URL.Path, s3.ObjectAccessPathPrefix)
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
			if typ == principal.TokenTypeAPI {
				return principal.SourceAPIToken.String()
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
		if p != nil && !principal.IsNonUserPrincipal(p) {
			return p, nil
		}
		if principal.IsNonUserPrincipal(p) {
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
		p := anonymous
		if p != nil {
			enriched, err := s.resolvePrincipalUserID(r.Context(), p)
			switch {
			case err == nil && enriched != nil:
				p = enriched
			case err != nil:
				slog.WarnContext(r.Context(), "auth: unable to resolve anonymous user ID", "error", err)
			}
		}
		ctx := principal.WithPrincipal(r.Context(), p)
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
		protectedNext := s.cookieCSRFMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pluginName := strings.TrimSpace(chi.URLParam(r, pluginParam))
			if pluginName == "" {
				auth := s.serverAuthRuntime()
				s.serveAuthenticated(w, r, protectedNext, auth.resolver, auth.noAuth, auth.anonymous, auth.providerName)
				return
			}

			auth, err := s.pluginAuthRuntime(pluginName)
			if err != nil {
				slog.ErrorContext(r.Context(), "plugin route auth provider is not initialized", "plugin", pluginName, "error", err)
				writeError(w, http.StatusInternalServerError, "plugin route auth provider is not initialized")
				return
			}

			s.serveAuthenticated(w, r, protectedNext, auth.resolver, auth.noAuth, auth.anonymous, auth.providerName)
		})
	}
}
