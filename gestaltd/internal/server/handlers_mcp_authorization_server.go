package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/plugins/oauth"
)

const (
	mcpAuthorizationServerMetadataPath       = "/.well-known/oauth-authorization-server"
	mcpAuthorizationServerMetadataMCPPath    = "/.well-known/oauth-authorization-server/mcp"
	mcpAuthorizationEndpointPath             = "/oauth/authorize"
	mcpAuthorizationConsentEndpointPath      = "/oauth/authorize/consent"
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
	if err != nil || p == nil || p.Identity == nil || p.Identity.Email == "" || principal.IsNonUserPrincipal(p) {
		if strings.EqualFold(strings.TrimSpace(query.Get("prompt")), "none") {
			redirectMCPOAuthError(w, r, redirectURI, state, "login_required", "user login is required")
			return
		}
		http.Redirect(w, r, "/api/v1/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	if strings.EqualFold(strings.TrimSpace(query.Get("prompt")), "none") {
		redirectMCPOAuthError(w, r, redirectURI, state, "interaction_required", "user consent is required")
		return
	}

	consent, err := encodeMCPOAuthConsentState(s.encryptor, mcpOAuthConsentState{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Email:               p.Identity.Email,
		DisplayName:         p.Identity.DisplayName,
		AvatarURL:           p.Identity.AvatarURL,
		Scope:               strings.TrimSpace(query.Get("scope")),
		State:               state,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           s.now().Add(mcpOAuthAuthorizationCodeTTL).Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to prepare authorization consent")
		return
	}

	writeMCPOAuthConsentPage(w, client.ClientName, strings.TrimSpace(query.Get("scope")), consent)
}

func (s *Server) mcpOAuthAuthorizeConsent(w http.ResponseWriter, r *http.Request) {
	auth := s.serverAuthRuntime()
	if auth.noAuth || auth.provider == nil {
		writeMCPOAuthError(w, http.StatusNotFound, "server_error", "auth is disabled")
		return
	}
	if s.encryptor == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth authorization is unavailable")
		return
	}
	if !s.sameOriginFormPost(r) {
		writeMCPOAuthError(w, http.StatusForbidden, "invalid_request", "authorization consent origin is invalid")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid authorization consent body")
		return
	}
	consent := strings.TrimSpace(r.Form.Get("consent"))
	if consent == "" {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "authorization consent is required")
		return
	}
	state, err := decodeMCPOAuthConsentState(s.encryptor, consent, s.now())
	if err != nil {
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_request", "authorization consent is invalid")
		return
	}

	p, err := s.resolveRequestPrincipalWithResolver(r, auth.resolver)
	if err != nil || p == nil || p.Identity == nil || p.Identity.Email == "" || principal.IsNonUserPrincipal(p) {
		writeMCPOAuthError(w, http.StatusUnauthorized, "login_required", "user login is required")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(p.Identity.Email), strings.TrimSpace(state.Email)) {
		writeMCPOAuthError(w, http.StatusForbidden, "access_denied", "authorization consent does not match the logged-in user")
		return
	}

	code, err := s.issueMCPOAuthAuthorizationCode(r, *state)
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue authorization code")
		return
	}

	redirectMCPOAuthSuccess(w, r, state.RedirectURI, map[string]string{
		"code":  code,
		"state": state.State,
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

func (s *Server) issueMCPOAuthAuthorizationCode(r *http.Request, consent mcpOAuthConsentState) (string, error) {
	if s.mcpOAuthGrants == nil {
		return "", fmt.Errorf("mcp oauth grant store is not configured")
	}
	expiresAt := s.now().Add(mcpOAuthAuthorizationCodeTTL)
	code, err := encodeMCPOAuthAuthorizationCode(s.encryptor, mcpOAuthAuthorizationCodeState{
		ClientID:            consent.ClientID,
		RedirectURI:         consent.RedirectURI,
		Email:               consent.Email,
		DisplayName:         consent.DisplayName,
		AvatarURL:           consent.AvatarURL,
		Scope:               consent.Scope,
		CodeChallenge:       consent.CodeChallenge,
		CodeChallengeMethod: consent.CodeChallengeMethod,
		ExpiresAt:           expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}
	if err := s.mcpOAuthGrants.StoreAuthorizationCode(r.Context(), code, expiresAt); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Server) mcpOAuthExchangeAuthorizationCode(w http.ResponseWriter, r *http.Request, clientID string) {
	code := r.Form.Get("code")
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

	familyID := uuid.NewString()
	refreshExpiresAt := s.now().Add(mcpOAuthRefreshTokenTTL)
	refreshToken, err := encodeMCPOAuthRefreshToken(s.encryptor, mcpOAuthRefreshTokenState{
		ClientID:    clientID,
		FamilyID:    familyID,
		Email:       codeState.Email,
		DisplayName: codeState.DisplayName,
		AvatarURL:   codeState.AvatarURL,
		Scope:       codeState.Scope,
		ExpiresAt:   refreshExpiresAt.Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue refresh token")
		return
	}
	if s.mcpOAuthGrants == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth grant storage is unavailable")
		return
	}
	if err := s.mcpOAuthGrants.ConsumeAuthorizationCodeAndStoreRefreshToken(r.Context(), code, refreshToken, familyID, refreshExpiresAt); err != nil {
		writeMCPOAuthGrantError(w, err, "authorization code is invalid")
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

	if s.mcpOAuthGrants == nil {
		writeMCPOAuthError(w, http.StatusServiceUnavailable, "server_error", "MCP OAuth grant storage is unavailable")
		return
	}
	refreshExpiresAt := s.now().Add(mcpOAuthRefreshTokenTTL)
	refreshToken, err := encodeMCPOAuthRefreshToken(s.encryptor, mcpOAuthRefreshTokenState{
		ClientID:    clientID,
		FamilyID:    refreshState.FamilyID,
		Email:       refreshState.Email,
		DisplayName: refreshState.DisplayName,
		AvatarURL:   refreshState.AvatarURL,
		Scope:       refreshState.Scope,
		ExpiresAt:   refreshExpiresAt.Unix(),
	})
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to rotate refresh token")
		return
	}
	if err := s.mcpOAuthGrants.RotateRefreshToken(r.Context(), r.Form.Get("refresh_token"), refreshToken, refreshState.FamilyID, refreshExpiresAt); err != nil {
		writeMCPOAuthGrantError(w, err, "refresh token is invalid")
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
	ttl := defaultSessionCookieTTL
	if auth.provider != nil {
		if providerWithTTL, ok := auth.provider.(SessionTokenTTLProvider); ok {
			ttl = providerWithTTL.SessionTokenTTL()
		}
	}
	accessToken, err := s.issueMCPOAuthAccessToken(identity, scope, ttl)
	if err != nil {
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
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

func writeMCPOAuthGrantError(w http.ResponseWriter, err error, description string) {
	switch {
	case errors.Is(err, coredata.ErrMCPOAuthGrantNotFound),
		errors.Is(err, coredata.ErrMCPOAuthGrantConsumed),
		errors.Is(err, coredata.ErrMCPOAuthGrantRevoked),
		errors.Is(err, coredata.ErrMCPOAuthGrantExpired):
		writeMCPOAuthError(w, http.StatusBadRequest, "invalid_grant", description)
	default:
		writeMCPOAuthError(w, http.StatusInternalServerError, "server_error", "MCP OAuth grant storage failed")
	}
}

func setMCPOAuthNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func writeMCPOAuthConsentPage(w http.ResponseWriter, clientName, scope, consent string) {
	clientName = strings.TrimSpace(clientName)
	if clientName == "" {
		clientName = "MCP client"
	}
	setMCPOAuthNoStoreHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Authorize MCP Client</title></head>
<body>
<main>
<h1>Authorize %s</h1>
<p>This client is requesting MCP access%s.</p>
<form method="post" action="%s">
<input type="hidden" name="consent" value="%s">
<button type="submit">Authorize</button>
</form>
</main>
</body>
</html>`,
		html.EscapeString(clientName),
		mcpOAuthConsentScopeText(scope),
		html.EscapeString(mcpAuthorizationConsentEndpointPath),
		html.EscapeString(consent),
	)
}

func mcpOAuthConsentScopeText(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	return " for scope " + html.EscapeString(scope)
}

func (s *Server) sameOriginFormPost(r *http.Request) bool {
	raw := strings.TrimSpace(r.Header.Get("Origin"))
	if raw == "" {
		raw = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if raw == "" {
		return true
	}
	got, err := url.Parse(raw)
	if err != nil || got.Scheme == "" || got.Host == "" {
		return false
	}
	expectedRaw, err := s.resolvePublicURL(r, "/")
	if err != nil {
		return false
	}
	expected, err := url.Parse(expectedRaw)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	return strings.EqualFold(got.Scheme, expected.Scheme) && strings.EqualFold(got.Host, expected.Host)
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
		if value == "" {
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
	if parsed.User != nil {
		return errors.New("redirect_uri must not include userinfo")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("redirect_uri scheme must be http or https")
	}
	if parsed.Host == "" || !isLoopbackRedirectHost(parsed.Hostname()) {
		return errors.New("redirect_uri host must be loopback")
	}
	return nil
}

func isLoopbackRedirectHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
