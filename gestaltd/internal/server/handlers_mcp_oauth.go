package server

import (
	"errors"
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
	Resource                          string   `json:"resource"`
	AuthorizationServers              []string `json:"authorization_servers,omitempty"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint                     string   `json:"token_endpoint,omitempty"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported            []string `json:"bearer_methods_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
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

	authServer, err := s.mcpAuthorizationServerConfig(r)
	if err != nil {
		if errors.Is(err, errMCPOAuthDisabled) {
			return mcpProtectedResourceMetadataResponse{
				Resource: resourceURL,
			}, nil
		}
		return mcpProtectedResourceMetadataResponse{}, err
	}
	resp := mcpProtectedResourceMetadataResponse{
		Resource:                          resourceURL,
		AuthorizationServers:              []string{authServer.Issuer},
		AuthorizationEndpoint:             authServer.AuthorizationEndpoint,
		TokenEndpoint:                     authServer.TokenEndpoint,
		RegistrationEndpoint:              authServer.RegistrationEndpoint,
		ScopesSupported:                   authServer.ScopesSupported,
		BearerMethodsSupported:            []string{"header"},
		CodeChallengeMethodsSupported:     authServer.CodeChallengeMethodsSupported,
		TokenEndpointAuthMethodsSupported: authServer.TokenEndpointAuthMethodsSupported,
	}
	return resp, nil
}

func (s *Server) mcpAuthorizationScopes(r *http.Request) []string {
	auth := s.serverAuthRuntime()
	if auth.noAuth || auth.provider == nil {
		return nil
	}

	loginURLRaw, err := loginURLForRequest(r.Context(), auth.provider, mcpMetadataProbeState)
	if err != nil {
		return nil
	}
	loginURLResolved, err := s.resolvePublicURL(r, loginURLRaw)
	if err != nil {
		return nil
	}
	loginURL, err := url.Parse(loginURLResolved)
	if err != nil || !loginURL.IsAbs() || loginURL.Host == "" {
		return nil
	}
	return splitOAuthScopes(loginURL.Query().Get("scope"))
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
