package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	mcpAuthorizationServerMetadataPath       = "/.well-known/oauth-authorization-server"
	mcpAuthorizationServerMetadataMCPPath    = "/.well-known/oauth-authorization-server/mcp"
	mcpAuthorizationEndpointPath             = "/oauth/authorize"
	mcpTokenEndpointPath                     = "/oauth/token"
	mcpRegistrationEndpointPath              = "/oauth/register"
	mcpOAuthTokenAuthMethodNone              = "none"
	mcpOAuthTokenAuthMethodClientSecretPost  = "client_secret_post"
	mcpOAuthTokenAuthMethodClientSecretBasic = "client_secret_basic"
)

var errMCPOAuthDisabled = errors.New("mcp oauth is disabled")

type mcpAuthorizationServerConfig struct {
	Issuer                            string
	AuthorizationEndpoint             string
	TokenEndpoint                     string
	RegistrationEndpoint              string
	ScopesSupported                   []string
	CodeChallengeMethodsSupported     []string
	TokenEndpointAuthMethodsSupported []string
}

type mcpAuthorizationServerMetadataResponse struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

type mcpDynamicClientRegistrationRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type mcpDynamicClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type mcpOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type mcpOAuthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (s *Server) mcpAuthorizationServerConfig(r *http.Request) (mcpAuthorizationServerConfig, error) {
	auth := s.serverAuthRuntime()
	if auth.noAuth || auth.provider == nil {
		return mcpAuthorizationServerConfig{}, errMCPOAuthDisabled
	}

	issuer, err := s.resolvePublicURL(r, "/")
	if err != nil {
		return mcpAuthorizationServerConfig{}, err
	}
	authorizationEndpoint, err := s.resolvePublicURL(r, mcpAuthorizationEndpointPath)
	if err != nil {
		return mcpAuthorizationServerConfig{}, err
	}
	tokenEndpoint, err := s.resolvePublicURL(r, mcpTokenEndpointPath)
	if err != nil {
		return mcpAuthorizationServerConfig{}, err
	}
	registrationEndpoint, err := s.resolvePublicURL(r, mcpRegistrationEndpointPath)
	if err != nil {
		return mcpAuthorizationServerConfig{}, err
	}

	return mcpAuthorizationServerConfig{
		Issuer:                            strings.TrimRight(issuer, "/"),
		AuthorizationEndpoint:             authorizationEndpoint,
		TokenEndpoint:                     tokenEndpoint,
		RegistrationEndpoint:              registrationEndpoint,
		ScopesSupported:                   s.mcpAuthorizationScopes(r),
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{mcpOAuthTokenAuthMethodNone, mcpOAuthTokenAuthMethodClientSecretPost, mcpOAuthTokenAuthMethodClientSecretBasic},
	}, nil
}

func (s *Server) mcpAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mcpAuthorizationServerConfig(r)
	if err != nil {
		if errors.Is(err, errMCPOAuthDisabled) {
			writeError(w, http.StatusNotFound, "auth is disabled")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve MCP authorization server metadata")
		return
	}
	writeJSON(w, http.StatusOK, mcpAuthorizationServerMetadataResponse{
		Issuer:                            cfg.Issuer,
		AuthorizationEndpoint:             cfg.AuthorizationEndpoint,
		TokenEndpoint:                     cfg.TokenEndpoint,
		RegistrationEndpoint:              cfg.RegistrationEndpoint,
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ScopesSupported:                   cfg.ScopesSupported,
		CodeChallengeMethodsSupported:     cfg.CodeChallengeMethodsSupported,
		TokenEndpointAuthMethodsSupported: cfg.TokenEndpointAuthMethodsSupported,
	})
}

func (s *Server) mcpRegisterOAuthClient(w http.ResponseWriter, r *http.Request) {
	if _, err := s.mcpAuthorizationServerConfig(r); err != nil {
		if errors.Is(err, errMCPOAuthDisabled) {
			writeMCPOAuthError(w, http.StatusNotFound, "server_error", "auth is disabled")
			return
		}
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "MCP OAuth client registration is unavailable")
		return
	}
	if s.encryptor == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth client registration is unavailable")
		return
	}

	var req mcpDynamicClientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must contain at least one absolute URI")
		return
	}
	for _, redirectURI := range req.RedirectURIs {
		if err := validateMCPOAuthRedirectURI(redirectURI); err != nil {
			writeMCPOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}

	authMethod := normalizeMCPOAuthTokenEndpointAuthMethod(req.TokenEndpointAuthMethod)
	if !isSupportedMCPOAuthTokenEndpointAuthMethod(authMethod) {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
		return
	}

	clientID, err := encodeMCPOAuthClientRegistration(s.encryptor, mcpOAuthClientRegistrationState{
		RedirectURIs:            req.RedirectURIs,
		ClientName:              strings.TrimSpace(req.ClientName),
		TokenEndpointAuthMethod: authMethod,
		ExpiresAt:               s.now().Add(mcpOAuthClientRegistrationTTL).Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}

	resp := mcpDynamicClientRegistrationResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        s.now().Unix(),
		ClientName:              strings.TrimSpace(req.ClientName),
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: authMethod,
	}
	if authMethod != mcpOAuthTokenAuthMethodNone {
		resp.ClientSecret = s.mcpOAuthClientSecret(clientID)
		resp.ClientSecretExpiresAt = s.now().Add(mcpOAuthClientRegistrationTTL).Unix()
	}

	setMCPOAuthNoStoreHeaders(w)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) mcpOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	auth := s.serverAuthRuntime()
	if auth.noAuth || auth.provider == nil {
		writeMCPOAuthError(w, http.StatusNotFound, "server_error", "auth is disabled")
		return
	}
	if s.encryptor == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth authorization is unavailable")
		return
	}

	query := r.URL.Query()
	state := query.Get("state")
	clientID := strings.TrimSpace(query.Get("client_id"))
	if clientID == "" {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}
	client, err := decodeMCPOAuthClientRegistration(s.encryptor, clientID, s.now())
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_client", "client registration is invalid")
		return
	}
	redirectURI, err := resolveRegisteredMCPOAuthRedirectURI(client.RedirectURIs, query.Get("redirect_uri"))
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if responseType := strings.TrimSpace(query.Get("response_type")); responseType != "code" {
		redirectMCPOAuthError(w, r, redirectURI, state, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	codeChallenge := strings.TrimSpace(query.Get("code_challenge"))
	if codeChallenge == "" {
		redirectMCPOAuthError(w, r, redirectURI, state, "invalid_request", "code_challenge is required")
		return
	}
	codeChallengeMethod := normalizeMCPOAuthCodeChallengeMethod(query.Get("code_challenge_method"))
	if codeChallengeMethod != "S256" {
		redirectMCPOAuthError(w, r, redirectURI, state, "invalid_request", "only code_challenge_method=S256 is supported")
		return
	}

	p, err := s.resolveRequestPrincipalWithResolver(r, auth.resolver)
	if err != nil || p == nil || p.Identity == nil || p.Identity.Email == "" || principal.IsWorkloadPrincipal(p) {
		if strings.EqualFold(strings.TrimSpace(query.Get("prompt")), "none") {
			redirectMCPOAuthError(w, r, redirectURI, state, "login_required", "user login is required")
			return
		}
		http.Redirect(w, r, "/api/v1/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}

	code, err := encodeMCPOAuthAuthorizationCode(s.encryptor, mcpOAuthAuthorizationCodeState{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Email:               p.Identity.Email,
		DisplayName:         p.Identity.DisplayName,
		AvatarURL:           p.Identity.AvatarURL,
		Scope:               strings.TrimSpace(query.Get("scope")),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           s.now().Add(mcpOAuthAuthorizationCodeTTL).Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue authorization code")
		return
	}

	redirectMCPOAuthSuccess(w, r, redirectURI, map[string]string{
		"code":  code,
		"state": state,
	})
}

func (s *Server) mcpOAuthToken(w http.ResponseWriter, r *http.Request) {
	if _, err := s.mcpAuthorizationServerConfig(r); err != nil {
		if errors.Is(err, errMCPOAuthDisabled) {
			writeMCPOAuthError(w, http.StatusNotFound, "server_error", "auth is disabled")
			return
		}
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "MCP OAuth token exchange is unavailable")
		return
	}
	if s.encryptor == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth token exchange is unavailable")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid token request body")
		return
	}

	clientID, clientSecret, presentedAuthMethod, err := parseMCPOAuthClientAuthentication(r)
	if err != nil {
		writeMCPOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}
	if clientID == "" {
		writeMCPOAuthError(w, http.StatusUnauthorized, "invalid_client", "client_id is required")
		return
	}

	client, err := decodeMCPOAuthClientRegistration(s.encryptor, clientID, s.now())
	if err != nil {
		writeMCPOAuthError(w, http.StatusUnauthorized, "invalid_client", "client registration is invalid")
		return
	}
	if err := s.validateMCPOAuthClientAuthentication(clientID, client.TokenEndpointAuthMethod, presentedAuthMethod, clientSecret); err != nil {
		writeMCPOAuthError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}

	switch grantType := strings.TrimSpace(r.Form.Get("grant_type")); grantType {
	case "authorization_code":
		s.mcpOAuthExchangeAuthorizationCode(w, r, clientID)
	case "refresh_token":
		s.mcpOAuthRefreshAccessToken(w, r, clientID)
	default:
		writeMCPOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

func (s *Server) mcpOAuthExchangeAuthorizationCode(w http.ResponseWriter, r *http.Request, clientID string) {
	codeState, err := decodeMCPOAuthAuthorizationCode(s.encryptor, r.Form.Get("code"), s.now())
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if codeState.ClientID != clientID {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code was not issued to this client")
		return
	}

	redirectURI, err := resolveRegisteredMCPOAuthRedirectURI([]string{codeState.RedirectURI}, r.Form.Get("redirect_uri"))
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match authorization code")
		return
	}
	if redirectURI != codeState.RedirectURI {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match authorization code")
		return
	}

	verifier := strings.TrimSpace(r.Form.Get("code_verifier"))
	if verifier == "" {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "code_verifier is required")
		return
	}
	if method := normalizeMCPOAuthCodeChallengeMethod(codeState.CodeChallengeMethod); method != "S256" {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code challenge method is unsupported")
		return
	}
	if oauth.ComputeS256Challenge(verifier) != codeState.CodeChallenge {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier is invalid")
		return
	}

	refreshToken, err := encodeMCPOAuthRefreshToken(s.encryptor, mcpOAuthRefreshTokenState{
		ClientID:    clientID,
		Email:       codeState.Email,
		DisplayName: codeState.DisplayName,
		AvatarURL:   codeState.AvatarURL,
		Scope:       codeState.Scope,
		ExpiresAt:   s.now().Add(mcpOAuthRefreshTokenTTL).Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue refresh token")
		return
	}

	identity := &core.UserIdentity{
		Email:       codeState.Email,
		DisplayName: codeState.DisplayName,
		AvatarURL:   codeState.AvatarURL,
	}
	s.writeMCPOAuthTokenResponse(w, identity, codeState.Scope, refreshToken)
}

func (s *Server) mcpOAuthRefreshAccessToken(w http.ResponseWriter, r *http.Request, clientID string) {
	refreshState, err := decodeMCPOAuthRefreshToken(s.encryptor, r.Form.Get("refresh_token"), s.now())
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	if refreshState.ClientID != clientID {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was not issued to this client")
		return
	}

	refreshToken, err := encodeMCPOAuthRefreshToken(s.encryptor, mcpOAuthRefreshTokenState{
		ClientID:    clientID,
		Email:       refreshState.Email,
		DisplayName: refreshState.DisplayName,
		AvatarURL:   refreshState.AvatarURL,
		Scope:       refreshState.Scope,
		ExpiresAt:   s.now().Add(mcpOAuthRefreshTokenTTL).Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to rotate refresh token")
		return
	}

	identity := &core.UserIdentity{
		Email:       refreshState.Email,
		DisplayName: refreshState.DisplayName,
		AvatarURL:   refreshState.AvatarURL,
	}
	s.writeMCPOAuthTokenResponse(w, identity, refreshState.Scope, refreshToken)
}

func (s *Server) writeMCPOAuthTokenResponse(w http.ResponseWriter, identity *core.UserIdentity, scope, refreshToken string) {
	auth := s.serverAuthRuntime()
	accessToken, err := s.issueSessionToken(auth.provider, identity)
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}

	ttl := defaultSessionCookieTTL
	if auth.provider != nil {
		if providerWithTTL, ok := auth.provider.(SessionTokenTTLProvider); ok {
			ttl = providerWithTTL.SessionTokenTTL()
		}
	}

	setMCPOAuthNoStoreHeaders(w)
	writeJSON(w, http.StatusOK, mcpOAuthTokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	})
}

func writeMCPOAuthError(w http.ResponseWriter, status int, code, description string) {
	setMCPOAuthNoStoreHeaders(w)
	writeJSON(w, status, mcpOAuthErrorResponse{
		Error:            code,
		ErrorDescription: description,
	})
}

func setMCPOAuthNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func redirectMCPOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, description string) {
	params := map[string]string{
		"error":             code,
		"error_description": description,
	}
	if state != "" {
		params["state"] = state
	}
	redirectMCPOAuthSuccess(w, r, redirectURI, params)
}

func redirectMCPOAuthSuccess(w http.ResponseWriter, r *http.Request, redirectURI string, params map[string]string) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is invalid")
		return
	}
	query := target.Query()
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func normalizeMCPOAuthCodeChallengeMethod(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "S256"
	}
	return raw
}

func normalizeMCPOAuthTokenEndpointAuthMethod(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mcpOAuthTokenAuthMethodNone
	}
	return raw
}

func isSupportedMCPOAuthTokenEndpointAuthMethod(method string) bool {
	switch normalizeMCPOAuthTokenEndpointAuthMethod(method) {
	case mcpOAuthTokenAuthMethodNone, mcpOAuthTokenAuthMethodClientSecretPost, mcpOAuthTokenAuthMethodClientSecretBasic:
		return true
	default:
		return false
	}
}

func validateMCPOAuthRedirectURI(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("redirect_uri is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return errors.New("redirect_uri must be an absolute URI")
	}
	if parsed.Fragment != "" {
		return errors.New("redirect_uri must not include a fragment")
	}
	return nil
}

func resolveRegisteredMCPOAuthRedirectURI(registered []string, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if len(registered) == 1 {
			return registered[0], nil
		}
		return "", errors.New("redirect_uri is required")
	}
	for _, candidate := range registered {
		if candidate == requested {
			return requested, nil
		}
	}
	return "", errors.New("redirect_uri is not registered")
}

func parseMCPOAuthClientAuthentication(r *http.Request) (clientID, clientSecret, method string, err error) {
	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			return "", "", "", errors.New("unsupported client authentication")
		}
		if decoded, decodeErr := url.QueryUnescape(clientID); decodeErr == nil {
			clientID = decoded
		}
		if decoded, decodeErr := url.QueryUnescape(clientSecret); decodeErr == nil {
			clientSecret = decoded
		}
		return clientID, clientSecret, mcpOAuthTokenAuthMethodClientSecretBasic, nil
	}
	clientID = strings.TrimSpace(r.Form.Get("client_id"))
	clientSecret = strings.TrimSpace(r.Form.Get("client_secret"))
	if clientSecret != "" {
		return clientID, clientSecret, mcpOAuthTokenAuthMethodClientSecretPost, nil
	}
	return clientID, "", mcpOAuthTokenAuthMethodNone, nil
}

func (s *Server) validateMCPOAuthClientAuthentication(clientID, registeredAuthMethod, presentedAuthMethod, clientSecret string) error {
	registeredAuthMethod = normalizeMCPOAuthTokenEndpointAuthMethod(registeredAuthMethod)
	presentedAuthMethod = normalizeMCPOAuthTokenEndpointAuthMethod(presentedAuthMethod)
	if registeredAuthMethod != presentedAuthMethod {
		return fmt.Errorf("client authentication method does not match registration")
	}
	switch registeredAuthMethod {
	case mcpOAuthTokenAuthMethodNone:
		return nil
	case mcpOAuthTokenAuthMethodClientSecretPost, mcpOAuthTokenAuthMethodClientSecretBasic:
		expected := s.mcpOAuthClientSecret(clientID)
		if expected == "" {
			return fmt.Errorf("client authentication is unavailable")
		}
		if !hmac.Equal([]byte(expected), []byte(clientSecret)) {
			return fmt.Errorf("client_secret is invalid")
		}
		return nil
	default:
		return fmt.Errorf("client authentication method is unsupported")
	}
}

func (s *Server) mcpOAuthClientSecret(clientID string) string {
	if len(s.sessionIssuer) == 0 || clientID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.sessionIssuer)
	_, _ = mac.Write([]byte("mcp-oauth-client-secret\x00"))
	_, _ = mac.Write([]byte(clientID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
