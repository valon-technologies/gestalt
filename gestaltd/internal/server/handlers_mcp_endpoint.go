package server

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) mcpEndpointHandler() http.Handler {
	post := s.authMiddleware(s.mcpHandler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !s.validMCPOrigin(r) {
				writeError(w, http.StatusForbidden, "invalid origin")
				return
			}
			post.ServeHTTP(w, r)
		case http.MethodGet, http.MethodDelete, http.MethodOptions:
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "MCP only accepts JSON-RPC over POST")
		default:
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func (s *Server) validMCPOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if strings.EqualFold(origin, "null") {
		return false
	}
	publicOrigin, ok := canonicalOriginFromBaseURL(s.publicBaseURL)
	if !ok {
		return false
	}
	requestOrigin, ok := canonicalOrigin(origin)
	if !ok || requestOrigin != publicOrigin {
		return false
	}
	return validMCPForwardedHost(r, publicOrigin)
}

func canonicalOriginFromBaseURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + canonicalHost(u), true
}

func canonicalOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + canonicalHost(u), true
}

func validMCPForwardedHost(r *http.Request, publicOrigin string) bool {
	publicURL, err := url.Parse(publicOrigin)
	if err != nil || publicURL.Host == "" {
		return false
	}
	for _, header := range []string{"X-Forwarded-Host", "X-Original-Host"} {
		host := strings.TrimSpace(r.Header.Get(header))
		if host != "" && canonicalRawHost(host, publicURL.Scheme) != publicURL.Host {
			return false
		}
	}
	return true
}

func canonicalHost(u *url.URL) string {
	return canonicalRawHost(u.Host, u.Scheme)
}

func canonicalRawHost(rawHost, scheme string) string {
	host := strings.ToLower(strings.TrimSpace(rawHost))
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		host = strings.TrimSuffix(host, ":443")
	case "http":
		host = strings.TrimSuffix(host, ":80")
	}
	return host
}
