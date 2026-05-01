package server

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

var (
	errCSRFValidationFailed        = errors.New("CSRF validation failed")
	errUnsupportedLoginContentType = errors.New("login initiation requires application/json")
)

func (s *Server) cookieCSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.validateCookieAuthenticatedMutation(r); err != nil {
			writeError(w, http.StatusForbidden, errCSRFValidationFailed.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validateCookieAuthenticatedMutation(r *http.Request) error {
	if !isMutatingMethod(r.Method) {
		return nil
	}
	p := PrincipalFromContext(r.Context())
	if p == nil || p.Source != principal.SourceSession {
		return nil
	}
	if c, err := r.Cookie(sessionCookieName); err != nil || c.Value == "" {
		return nil
	}
	return s.validateSameOriginRequestHeaders(r)
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) validateLoginInitiation(r *http.Request, extraAllowedOrigins ...requestOrigin) error {
	if r.Method == http.MethodPost && !hasJSONContentType(r) {
		return errUnsupportedLoginContentType
	}
	return s.validateSameOriginRequestHeaders(r, extraAllowedOrigins...)
}

func hasJSONContentType(r *http.Request) bool {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/json")
}

func (s *Server) validateSameOriginRequestHeaders(r *http.Request, extraAllowedOrigins ...requestOrigin) error {
	if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site != "" {
		switch site {
		case "same-origin", "none":
		case "same-site":
			if !s.hasAllowedOriginEvidence(r, extraAllowedOrigins...) {
				return fmt.Errorf("%w: same-site fetch metadata without allowed origin", errCSRFValidationFailed)
			}
		case "cross-site":
			return fmt.Errorf("%w: cross-site fetch metadata", errCSRFValidationFailed)
		default:
			return fmt.Errorf("%w: invalid fetch metadata", errCSRFValidationFailed)
		}
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if !s.requestOriginAllowed(r, origin, extraAllowedOrigins...) {
			return fmt.Errorf("%w: origin mismatch", errCSRFValidationFailed)
		}
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		if !s.requestRefererAllowed(r, referer, extraAllowedOrigins...) {
			return fmt.Errorf("%w: referer mismatch", errCSRFValidationFailed)
		}
	}
	return nil
}

func (s *Server) hasAllowedOriginEvidence(r *http.Request, extraAllowedOrigins ...requestOrigin) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return s.requestOriginAllowed(r, origin, extraAllowedOrigins...)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return s.requestRefererAllowed(r, referer, extraAllowedOrigins...)
	}
	return false
}

func (s *Server) requestOriginAllowed(r *http.Request, rawOrigin string, extraAllowedOrigins ...requestOrigin) bool {
	if strings.EqualFold(rawOrigin, "null") {
		return false
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.Path != "" {
		return false
	}
	return s.originAllowed(r, origin.Scheme, origin.Host, extraAllowedOrigins...)
}

func (s *Server) requestRefererAllowed(r *http.Request, rawReferer string, extraAllowedOrigins ...requestOrigin) bool {
	referer, err := url.Parse(rawReferer)
	if err != nil || referer.Scheme == "" || referer.Host == "" {
		return false
	}
	return s.originAllowed(r, referer.Scheme, referer.Host, extraAllowedOrigins...)
}

func (s *Server) originAllowed(r *http.Request, scheme, host string, extraAllowedOrigins ...requestOrigin) bool {
	for _, allowed := range s.allowedRequestOrigins(r, extraAllowedOrigins...) {
		if strings.EqualFold(allowed.Scheme, scheme) && strings.EqualFold(allowed.Host, host) {
			return true
		}
	}
	return false
}

type requestOrigin struct {
	Scheme string
	Host   string
}

func (s *Server) allowedRequestOrigins(r *http.Request, extraAllowedOrigins ...requestOrigin) []requestOrigin {
	origins := append([]requestOrigin(nil), extraAllowedOrigins...)
	if s.publicBaseURL != "" {
		if parsed, err := url.Parse(s.publicBaseURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			origins = append(origins, requestOrigin{Scheme: parsed.Scheme, Host: parsed.Host})
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Host != "" {
		origins = append(origins, requestOrigin{Scheme: scheme, Host: r.Host})
	}
	return origins
}
