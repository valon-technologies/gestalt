package server

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	mcpPath                          = "/mcp"
	mcpProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource/mcp"
	mcpMetadataProbeState            = "mcp-resource-metadata"
)

type mcpProtectedResourceMetadataResponse struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

func (s *Server) mcpProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	resp, err := s.mcpProtectedResourceMetadataResponse(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve MCP metadata")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) mcpProtectedResourceMetadataResponse(r *http.Request) (mcpProtectedResourceMetadataResponse, error) {
	resourceURL, err := s.resolvePublicURL(r, mcpPath)
	if err != nil {
		return mcpProtectedResourceMetadataResponse{}, err
	}

	authorizationServers, scopes := s.mcpAuthorizationMetadata(r)
	resp := mcpProtectedResourceMetadataResponse{
		Resource:               resourceURL,
		AuthorizationServers:   authorizationServers,
		ScopesSupported:        scopes,
		BearerMethodsSupported: []string{"header"},
	}
	return resp, nil
}

func (s *Server) mcpAuthorizationMetadata(r *http.Request) ([]string, []string) {
	auth := s.serverAuthRuntime()
	if auth.noAuth || auth.provider == nil {
		return nil, nil
	}

	loginURLRaw, err := loginURLForRequest(r.Context(), auth.provider, mcpMetadataProbeState)
	if err != nil {
		return nil, nil
	}
	loginURLResolved, err := s.resolvePublicURL(r, loginURLRaw)
	if err != nil {
		return nil, nil
	}
	loginURL, err := url.Parse(loginURLResolved)
	if err != nil || !loginURL.IsAbs() || loginURL.Host == "" {
		return nil, nil
	}

	authorizationServer := (&url.URL{
		Scheme: loginURL.Scheme,
		Host:   loginURL.Host,
	}).String()
	return []string{authorizationServer}, splitOAuthScopes(loginURL.Query().Get("scope"))
}

func splitOAuthScopes(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return nil
	}

	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, scope := range fields {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func (s *Server) maybeSetMCPResourceMetadataHeader(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != mcpPath {
		return
	}
	resourceMetadataURL, err := s.resolvePublicURL(r, mcpProtectedResourceMetadataPath)
	if err != nil || resourceMetadataURL == "" {
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL+`"`)
}
