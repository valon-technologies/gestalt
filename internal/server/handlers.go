package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/paraminterp"
	"github.com/valon-technologies/gestalt/internal/principal"
)

const defaultTokenInstance = "default"

const cliStatePrefix = "cli:"
const maxPort = 65535

const sessionCookieName = "session_token"
const defaultSessionCookieTTL = 24 * time.Hour

func (s *Server) resolveUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return "", false
	}
	if id := UserIDFromContext(r.Context()); id != "" {
		return id, true
	}
	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil || dbUser == nil || dbUser.ID == "" {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return "", false
	}
	return dbUser.ID, true
}

func (s *Server) healthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readinessCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.datastore.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type instanceInfo struct {
	Name string `json:"name"`
}

type integrationInfo struct {
	Name             string                         `json:"name"`
	DisplayName      string                         `json:"display_name,omitempty"`
	Description      string                         `json:"description,omitempty"`
	IconSVG          string                         `json:"icon_svg,omitempty"`
	Connected        bool                           `json:"connected"`
	Instances        []instanceInfo                 `json:"instances,omitempty"`
	AuthTypes        []string                       `json:"auth_types"`
	ConnectionParams map[string]connectionParamInfo `json:"connection_params,omitempty"`
}

type connectionParamInfo struct {
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	connected, err := s.userConnectedIntegrations(r)
	if err != nil {
		log.Printf("listing integrations: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to check integration status")
		return
	}

	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		var authTypes []string
		if atl, ok := prov.(core.AuthTypeLister); ok {
			authTypes = atl.AuthTypes()
		} else if mp, ok := prov.(core.ManualProvider); ok && mp.SupportsManualAuth() {
			authTypes = []string{"manual"}
		} else {
			authTypes = []string{"oauth"}
		}
		instances := connected[name]
		info := integrationInfo{
			Name:        name,
			DisplayName: prov.DisplayName(),
			Description: prov.Description(),
			Connected:   len(instances) > 0,
			Instances:   instances,
			AuthTypes:   authTypes,
		}
		if cp, ok := prov.(core.CatalogProvider); ok {
			if cat := cp.Catalog(); cat != nil {
				info.IconSVG = cat.IconSVG
			}
		}
		if cpp, ok := prov.(core.ConnectionParamProvider); ok {
			defs := cpp.ConnectionParamDefs()
			userParams := make(map[string]connectionParamInfo)
			for name, def := range defs {
				if def.From == "" {
					userParams[name] = connectionParamInfo{
						Required:    def.Required,
						Description: def.Description,
						Default:     def.Default,
					}
				}
			}
			if len(userParams) > 0 {
				info.ConnectionParams = userParams
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) userConnectedIntegrations(r *http.Request) (map[string][]instanceInfo, error) {
	user := UserFromContext(r.Context())
	if user == nil || user.Email == "" {
		return nil, nil
	}
	userID := UserIDFromContext(r.Context())
	if userID == "" {
		dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
		if err != nil {
			return nil, fmt.Errorf("resolving user: %w", err)
		}
		if dbUser == nil || dbUser.ID == "" {
			return nil, fmt.Errorf("resolving user: empty result")
		}
		userID = dbUser.ID
	}
	tokens, err := s.datastore.ListTokens(r.Context(), userID)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	m := make(map[string][]instanceInfo, len(tokens))
	for _, tok := range tokens {
		m[tok.Integration] = append(m[tok.Integration], instanceInfo{Name: tok.Instance})
	}
	return m, nil
}

func (s *Server) disconnectIntegration(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	name := chi.URLParam(r, "name")
	if _, ok := s.getProvider(w, name); !ok {
		return
	}

	requestedInstance := r.URL.Query().Get("instance")

	tokens, err := s.datastore.ListTokensForIntegration(r.Context(), userID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	if len(tokens) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q", name))
		return
	}

	if requestedInstance == "" && len(tokens) > 1 {
		instances := make([]string, len(tokens))
		for i, t := range tokens {
			instances[i] = t.Instance
		}
		writeError(w, http.StatusConflict, fmt.Sprintf("multiple connections exist for %q (%v); specify ?instance=NAME", name, instances))
		return
	}

	var tokenID string
	for _, tok := range tokens {
		if requestedInstance == "" || tok.Instance == requestedInstance {
			tokenID = tok.ID
			break
		}
	}

	if tokenID == "" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no connection found for integration %q instance %q", name, requestedInstance))
		return
	}

	if err := s.datastore.DeleteToken(r.Context(), tokenID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disconnect integration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) getProvider(w http.ResponseWriter, name string) (core.Provider, bool) {
	prov, err := s.providers.Get(name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return nil, false
	}
	return prov, true
}

func (s *Server) requireOAuthProvider(w http.ResponseWriter, name string) (core.OAuthProvider, bool) {
	prov, ok := s.getProvider(w, name)
	if !ok {
		return nil, false
	}
	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support OAuth", name))
		return nil, false
	}
	if atl, ok := prov.(core.AuthTypeLister); ok {
		hasOAuth := false
		for _, t := range atl.AuthTypes() {
			if t == "oauth" {
				hasOAuth = true
				break
			}
		}
		if !hasOAuth {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("integration %q uses manual auth; use POST /api/v1/auth/connect-manual instead", name))
			return nil, false
		}
	} else if mp, ok := prov.(core.ManualProvider); ok && mp.SupportsManualAuth() {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("integration %q uses manual auth; use POST /api/v1/auth/connect-manual instead", name))
		return nil, false
	}
	return oauthProv, true
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	prov, ok := s.getProvider(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, prov.ListOperations())
}

func (s *Server) listRuntimes(w http.ResponseWriter, _ *http.Request) {
	if s.runtimes == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, s.runtimes.List())
}

func (s *Server) listBindings(w http.ResponseWriter, _ *http.Request) {
	if s.bindings == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, s.bindings.List())
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	p := PrincipalFromContext(r.Context())

	params := make(map[string]any)
	if r.Method == http.MethodPost {
		if r.Body != nil {
			defer func() { _ = r.Body.Close() }()
			if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
	} else {
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
	}

	instance, _ := params["_instance"].(string)
	delete(params, "_instance")

	result, err := s.invoker.Invoke(r.Context(), p, providerName, instance, operationName, params)
	if err != nil {
		switch {
		case errors.Is(err, invocation.ErrProviderNotFound):
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", providerName))
		case errors.Is(err, invocation.ErrOperationNotFound):
			writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found on integration %q", operationName, providerName))
		case errors.Is(err, invocation.ErrNotAuthenticated):
			writeError(w, http.StatusUnauthorized, "not authenticated")
		case errors.Is(err, invocation.ErrNoToken):
			writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", providerName))
		case errors.Is(err, invocation.ErrAmbiguousInstance):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, invocation.ErrUserResolution):
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
		case errors.Is(err, invocation.ErrInternal):
			writeError(w, http.StatusInternalServerError, "internal error")
		case errors.Is(err, core.ErrMCPOnly):
			writeError(w, http.StatusBadRequest, "this integration is accessible only via MCP")
		default:
			log.Printf("operation %s/%s failed: %v", providerName, operationName, err)
			writeError(w, http.StatusBadGateway, "operation failed")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)
	_, _ = fmt.Fprint(w, result.Body)
}

type loginRequest struct {
	State        string `json:"state"`
	CallbackPort int    `json:"callback_port,omitempty"`
}

type authInfoResponse struct {
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	DevMode     bool   `json:"dev_mode"`
}

func (s *Server) authInfo(w http.ResponseWriter, _ *http.Request) {
	provider := s.auth.Name()
	displayName := provider
	if dn, ok := s.auth.(AuthProviderDisplayName); ok {
		displayName = dn.DisplayName()
	}
	writeJSON(w, http.StatusOK, authInfoResponse{
		Provider:    provider,
		DisplayName: displayName,
		DevMode:     s.devMode,
	})
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state := req.State
	if req.CallbackPort > 0 && req.CallbackPort <= maxPort {
		state = fmt.Sprintf("%s%d:%s", cliStatePrefix, req.CallbackPort, req.State)
	}
	url, err := s.auth.LoginURL(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate login URL")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// AuthProviderDisplayName is an optional interface for a human-readable login label.
type AuthProviderDisplayName interface {
	DisplayName() string
}

// SessionTokenIssuer is an optional interface that auth providers can implement
// to issue session tokens after login.
type SessionTokenIssuer interface {
	IssueSessionToken(identity *core.UserIdentity) (string, error)
}

// SessionTokenTTLProvider is an optional interface that auth providers can
// implement to expose their configured session TTL for cookie MaxAge.
type SessionTokenTTLProvider interface {
	SessionTokenTTL() time.Duration
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if p, ok := s.auth.(SessionTokenTTLProvider); ok {
		maxAge = int(p.SessionTokenTTL().Seconds())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !s.devMode,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !s.devMode,
		SameSite: http.SameSiteLaxMode,
	})
}

// StatefulCallbackHandler is an optional interface for auth providers that need
// the OAuth state parameter during callback (e.g., for PKCE where the
// code_verifier is encrypted in the state).
type StatefulCallbackHandler interface {
	HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error)
}

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	var identity *core.UserIdentity
	var err error

	if stateful, ok := s.auth.(StatefulCallbackHandler); ok {
		state := r.URL.Query().Get("state")
		identity, _, err = stateful.HandleCallbackWithState(r.Context(), code, state)
	} else {
		identity, err = s.auth.HandleCallback(r.Context(), code)
	}
	if err != nil {
		log.Printf("login callback failed: %v", err)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}

	resp := map[string]any{
		"email":        identity.Email,
		"display_name": identity.DisplayName,
	}

	issuer, ok := s.auth.(SessionTokenIssuer)
	if !ok {
		writeError(w, http.StatusInternalServerError, "auth provider does not support session tokens")
		return
	}
	token, err := issuer.IssueSessionToken(identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue session token")
		return
	}
	s.setSessionCookie(w, token)

	writeJSON(w, http.StatusOK, resp)
}

type startOAuthRequest struct {
	Integration      string            `json:"integration"`
	Instance         string            `json:"instance"`
	Scopes           []string          `json:"scopes"`
	ConnectionParams map[string]string `json:"connection_params"`
}

type oauthStarter interface {
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
}

type oauthVerifierExchanger interface {
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
}

func (s *Server) startIntegrationOAuth(w http.ResponseWriter, r *http.Request) {
	var req startOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	oauthProv, ok := s.requireOAuthProvider(w, req.Integration)
	if !ok {
		return
	}

	prov, _ := s.providers.Get(req.Integration)

	if s.stateCodec == nil {
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return
	}

	dbUserID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	instance := req.Instance
	if instance == "" {
		instance = "default"
	}
	if !safeParamValue.MatchString(instance) {
		writeError(w, http.StatusBadRequest, "instance name contains invalid characters")
		return
	}

	var connParams map[string]string
	if cpp, ok := prov.(core.ConnectionParamProvider); ok {
		var valErr error
		connParams, valErr = validateConnectionParams(cpp.ConnectionParamDefs(), req.ConnectionParams)
		if valErr != nil {
			writeError(w, http.StatusBadRequest, valErr.Error())
			return
		}
	}

	var (
		authURL  string
		verifier string
	)

	type authURLOverrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	type authBaseURLer interface{ AuthorizationBaseURL() string }

	if len(connParams) > 0 {
		if abu, ok := prov.(authBaseURLer); ok {
			rawAuthURL := abu.AuthorizationBaseURL()
			resolvedAuthURL := paraminterp.Interpolate(rawAuthURL, connParams)
			if resolvedAuthURL != rawAuthURL {
				if ov, ok := prov.(authURLOverrider); ok {
					authURL, verifier = ov.StartOAuthWithOverride(resolvedAuthURL, "_", req.Scopes)
				}
			}
		}
	}
	if authURL == "" {
		if starter, ok := prov.(oauthStarter); ok {
			authURL, verifier = starter.StartOAuth("_", req.Scopes)
		} else {
			authURL = oauthProv.AuthorizationURL("_", req.Scopes)
		}
	}

	state, err := s.stateCodec.Encode(integrationOAuthState{
		UserID:           dbUserID,
		Integration:      req.Integration,
		Instance:         instance,
		Verifier:         verifier,
		ConnectionParams: connParams,
		ExpiresAt:        s.now().Add(integrationOAuthStateTTL).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode oauth state")
		return
	}

	authURL, err = setURLQueryParam(authURL, "state", state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare oauth URL")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL, "state": state})
}

func (s *Server) integrationOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	encodedState := r.URL.Query().Get("state")
	if code == "" || encodedState == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	if s.stateCodec == nil {
		writeError(w, http.StatusInternalServerError, "oauth state encryption is not configured")
		return
	}

	state, err := s.stateCodec.Decode(encodedState, s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired oauth state")
		return
	}

	providerName := state.Integration
	oauthProv, ok := s.requireOAuthProvider(w, providerName)
	if !ok {
		return
	}

	prov, _ := s.providers.Get(providerName)

	var exchangeOpts []oauth.ExchangeOption
	connParams := state.ConnectionParams
	if len(connParams) > 0 {
		type tokenURLer interface{ TokenURL() string }
		if tup, ok := prov.(tokenURLer); ok {
			rawURL := tup.TokenURL()
			resolved := paraminterp.Interpolate(rawURL, connParams)
			if resolved != rawURL {
				exchangeOpts = append(exchangeOpts, oauth.WithTokenURL(resolved))
			}
		}
	}

	var tokenResp *core.TokenResponse
	if exchanger, ok := prov.(oauthVerifierExchanger); ok {
		tokenResp, err = exchanger.ExchangeCodeWithVerifier(r.Context(), code, state.Verifier, exchangeOpts...)
	} else {
		if len(exchangeOpts) > 0 {
			log.Printf("WARNING: %s does not support exchange options (e.g. token URL override); connection params may not apply to token exchange", providerName)
		}
		tokenResp, err = oauthProv.ExchangeCode(r.Context(), code)
	}
	if err != nil {
		log.Printf("token exchange failed for %s: %v", providerName, err)
		writeError(w, http.StatusBadGateway, "token exchange failed")
		return
	}

	metadata, metaErr := buildConnectionMetadata(prov, connParams, tokenResp)
	if metaErr != nil {
		log.Printf("connection metadata extraction failed for %s: %v", providerName, metaErr)
		writeError(w, http.StatusBadGateway, "failed to extract connection metadata from token response")
		return
	}

	now := s.now().UTC().Truncate(time.Second)
	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	callbackInstance := state.Instance
	if callbackInstance == "" {
		callbackInstance = defaultTokenInstance
	}

	tok := &core.IntegrationToken{
		ID:           uuid.NewString(),
		UserID:       state.UserID,
		Integration:  providerName,
		Instance:     callbackInstance,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		MetadataJSON: metadata,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.datastore.StoreToken(r.Context(), tok); err != nil {
		log.Printf("failed to store token for %s: %v", providerName, err)
		writeError(w, http.StatusInternalServerError, "failed to store token")
		return
	}

	if err := s.runPostConnectHook(r.Context(), prov, tok); err != nil {
		log.Printf("post_connect hook failed for %s: %v", providerName, err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	http.Redirect(w, r, "/integrations?connected="+url.QueryEscape(providerName), http.StatusSeeOther)
}

type connectManualRequest struct {
	Integration      string            `json:"integration"`
	Instance         string            `json:"instance"`
	Credential       string            `json:"credential"`
	ConnectionParams map[string]string `json:"connection_params"`
}

func (s *Server) connectManual(w http.ResponseWriter, r *http.Request) {
	var req connectManualRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Integration == "" || req.Credential == "" {
		writeError(w, http.StatusBadRequest, "integration and credential are required")
		return
	}

	prov, ok := s.getProvider(w, req.Integration)
	if !ok {
		return
	}

	mp, ok := prov.(core.ManualProvider)
	if !ok || !mp.SupportsManualAuth() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support manual auth; use OAuth connect instead", req.Integration))
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	manualInstance := req.Instance
	if manualInstance == "" {
		manualInstance = "default"
	}
	if !safeParamValue.MatchString(manualInstance) {
		writeError(w, http.StatusBadRequest, "instance name contains invalid characters")
		return
	}

	var connParams map[string]string
	if cpp, ok := prov.(core.ConnectionParamProvider); ok {
		var valErr error
		connParams, valErr = validateConnectionParams(cpp.ConnectionParamDefs(), req.ConnectionParams)
		if valErr != nil {
			writeError(w, http.StatusBadRequest, valErr.Error())
			return
		}
	}

	now := s.now().UTC().Truncate(time.Second)
	tok := &core.IntegrationToken{
		ID:          uuid.NewString(),
		UserID:      dbUser.ID,
		Integration: req.Integration,
		Instance:    manualInstance,
		AccessToken: req.Credential,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	manualMeta, metaErr := buildConnectionMetadata(prov, connParams, nil)
	if metaErr != nil {
		writeError(w, http.StatusBadRequest, metaErr.Error())
		return
	}
	tok.MetadataJSON = manualMeta
	if err := s.datastore.StoreToken(r.Context(), tok); err != nil {
		log.Printf("failed to store credential for %s: %v", req.Integration, err)
		writeError(w, http.StatusInternalServerError, "failed to store credential")
		return
	}

	if err := s.runPostConnectHook(r.Context(), prov, tok); err != nil {
		log.Printf("post_connect hook failed for %s: %v", req.Integration, err)
		writeError(w, http.StatusBadGateway, "connection setup failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

type createTokenRequest struct {
	Name   string `json:"name"`
	Scopes string `json:"scopes"`
}

type createTokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) createAPIToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, err := generateRandomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	hashed := principal.HashToken(plaintext)

	now := s.now().UTC().Truncate(time.Second)
	defaultExpiry := now.Add(90 * 24 * time.Hour)
	apiToken := &core.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        req.Name,
		HashedToken: hashed,
		Scopes:      req.Scopes,
		ExpiresAt:   &defaultExpiry,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.datastore.StoreAPIToken(r.Context(), apiToken); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store API token")
		return
	}

	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:        apiToken.ID,
		Name:      apiToken.Name,
		Token:     plaintext,
		ExpiresAt: apiToken.ExpiresAt,
	})
}

type apiTokenInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    string     `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) listAPITokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	tokens, err := s.datastore.ListAPITokens(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	out := make([]apiTokenInfo, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, apiTokenInfo{
			ID:        t.ID,
			Name:      t.Name,
			Scopes:    t.Scopes,
			CreatedAt: t.CreatedAt,
			ExpiresAt: t.ExpiresAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	if err := s.datastore.RevokeAPIToken(r.Context(), userID, id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) devLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	if _, err := s.datastore.FindOrCreateUser(r.Context(), req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	issuer, ok := s.auth.(SessionTokenIssuer)
	if !ok {
		writeError(w, http.StatusInternalServerError, "auth provider does not support session tokens")
		return
	}

	token, err := issuer.IssueSessionToken(&core.UserIdentity{Email: req.Email})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue session token")
		return
	}

	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"email": req.Email,
	})
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

var (
	safeParamValue         = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeTokenResponseValue = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)
)

func validateConnectionParams(defs map[string]core.ConnectionParamDef, provided map[string]string) (map[string]string, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	for key := range provided {
		if _, ok := defs[key]; !ok {
			return nil, fmt.Errorf("unknown connection parameter: %s", key)
		}
	}
	result := make(map[string]string)
	for name, def := range defs {
		if def.From != "" {
			continue
		}
		if v, ok := provided[name]; ok && v != "" {
			if !safeParamValue.MatchString(v) {
				return nil, fmt.Errorf("connection parameter %q contains invalid characters (allowed: letters, digits, hyphens, dots, underscores)", name)
			}
			result[name] = v
		} else if def.Default != "" {
			result[name] = def.Default
		} else if def.Required {
			return nil, fmt.Errorf("missing required connection parameter: %s", name)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func buildConnectionMetadata(prov core.Provider, userParams map[string]string, tokenResp *core.TokenResponse) (string, error) {
	metadata := make(map[string]string)
	for k, v := range userParams {
		metadata[k] = v
	}

	if cpp, ok := prov.(core.ConnectionParamProvider); ok && tokenResp != nil && tokenResp.Extra != nil {
		for name, def := range cpp.ConnectionParamDefs() {
			if def.From == "token_response" {
				field := def.Field
				if field == "" {
					field = name
				}
				val, ok := tokenResp.Extra[field]
				if !ok {
					if def.Required {
						return "", fmt.Errorf("token response missing required field %q for connection param %q", field, name)
					}
					continue
				}
				s := fmt.Sprintf("%v", val)
				if !safeTokenResponseValue.MatchString(s) {
					return "", fmt.Errorf("token response field %q for connection param %q contains invalid characters", field, name)
				}
				metadata[name] = s
			}
		}
	}

	if len(metadata) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(metadata)
	return string(b), nil
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

func (s *Server) runPostConnectHook(ctx context.Context, prov core.Provider, tok *core.IntegrationToken) error {
	pcp, ok := prov.(core.PostConnectProvider)
	if !ok {
		return nil
	}
	hookFn := pcp.PostConnectHook()
	if hookFn == nil {
		return nil
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &bearerTransport{token: tok.AccessToken, base: http.DefaultTransport},
	}
	extra, err := hookFn(ctx, tok, client)
	if err != nil {
		_ = s.datastore.DeleteToken(ctx, tok.ID)
		return fmt.Errorf("post_connect: %w", err)
	}

	if len(extra) == 0 {
		return nil
	}

	for k, v := range extra {
		if !safeParamValue.MatchString(k) || !safeTokenResponseValue.MatchString(v) {
			_ = s.datastore.DeleteToken(ctx, tok.ID)
			return fmt.Errorf("post_connect returned invalid key or value for %q", k)
		}
	}

	existing := make(map[string]string)
	if tok.MetadataJSON != "" {
		if err := json.Unmarshal([]byte(tok.MetadataJSON), &existing); err != nil {
			_ = s.datastore.DeleteToken(ctx, tok.ID)
			return fmt.Errorf("post_connect: corrupt MetadataJSON: %w", err)
		}
	}
	for k, v := range extra {
		existing[k] = v
	}
	b, _ := json.Marshal(existing)
	tok.MetadataJSON = string(b)

	if err := s.datastore.StoreToken(ctx, tok); err != nil {
		_ = s.datastore.DeleteToken(ctx, tok.ID)
		return fmt.Errorf("post_connect: failed to update metadata: %w", err)
	}

	return nil
}
