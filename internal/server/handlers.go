package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/invocation"
	"github.com/valon-technologies/toolshed/internal/principal"
)

const defaultTokenInstance = "default"

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

type integrationInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	IconSVG     string `json:"icon_svg,omitempty"`
}

func (s *Server) listIntegrations(w http.ResponseWriter, _ *http.Request) {
	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		info := integrationInfo{
			Name:        name,
			DisplayName: prov.DisplayName(),
			Description: prov.Description(),
		}
		if cp, ok := prov.(core.CatalogProvider); ok {
			if cat := cp.Catalog(); cat != nil {
				info.IconSVG = cat.IconSVG
			}
		}
		out = append(out, info)
	}
	writeJSON(w, http.StatusOK, out)
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
	if mp, ok := prov.(core.ManualProvider); ok && mp.SupportsManualAuth() {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("integration %q uses manual auth; use POST /api/v1/auth/connect-manual instead", name))
		return nil, false
	}
	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support OAuth", name))
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

	result, err := s.invoker.Invoke(r.Context(), p, providerName, operationName, params)
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
		case errors.Is(err, invocation.ErrUserResolution):
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
		case errors.Is(err, invocation.ErrInternal):
			writeError(w, http.StatusInternalServerError, "internal error")
		default:
			writeError(w, http.StatusBadGateway, fmt.Sprintf("operation failed: %v", err))
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)
	_, _ = fmt.Fprint(w, result.Body)
}

type loginRequest struct {
	State string `json:"state"`
}

func (s *Server) authInfo(w http.ResponseWriter, _ *http.Request) {
	provider := s.auth.Name()
	displayName := provider
	if dn, ok := s.auth.(AuthProviderDisplayName); ok {
		displayName = dn.DisplayName()
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"provider":     provider,
		"display_name": displayName,
	})
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	url, err := s.auth.LoginURL(req.State)
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
		writeError(w, http.StatusUnauthorized, fmt.Sprintf("login failed: %v", err))
		return
	}

	resp := map[string]any{
		"email":        identity.Email,
		"display_name": identity.DisplayName,
	}

	if issuer, ok := s.auth.(SessionTokenIssuer); ok {
		token, err := issuer.IssueSessionToken(identity)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue session token")
			return
		}
		resp["token"] = token
	}

	writeJSON(w, http.StatusOK, resp)
}

type startOAuthRequest struct {
	Integration string   `json:"integration"`
	Scopes      []string `json:"scopes"`
}

type oauthStarter interface {
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
}

type oauthVerifierExchanger interface {
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error)
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

	var (
		authURL  string
		verifier string
	)
	if starter, ok := prov.(oauthStarter); ok {
		authURL, verifier = starter.StartOAuth("_", req.Scopes)
	} else {
		authURL = oauthProv.AuthorizationURL("_", req.Scopes)
	}

	state, err := s.stateCodec.Encode(integrationOAuthState{
		UserID:      dbUserID,
		Integration: req.Integration,
		Verifier:    verifier,
		ExpiresAt:   s.now().Add(integrationOAuthStateTTL).Unix(),
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

	var tokenResp *core.TokenResponse
	if exchanger, ok := prov.(oauthVerifierExchanger); ok && state.Verifier != "" {
		tokenResp, err = exchanger.ExchangeCodeWithVerifier(r.Context(), code, state.Verifier)
	} else {
		tokenResp, err = oauthProv.ExchangeCode(r.Context(), code)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("token exchange failed: %v", err))
		return
	}

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := s.now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	tok := &core.IntegrationToken{
		UserID:       state.UserID,
		Integration:  providerName,
		Instance:     defaultTokenInstance,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
	}

	if err := s.datastore.StoreToken(r.Context(), tok); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "connected",
		"integration": providerName,
	})
}

type connectManualRequest struct {
	Integration string `json:"integration"`
	Credential  string `json:"credential"`
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

	tok := &core.IntegrationToken{
		ID:          uuid.NewString(),
		UserID:      dbUser.ID,
		Integration: req.Integration,
		Instance:    "default",
		AccessToken: req.Credential,
	}
	if err := s.datastore.StoreToken(r.Context(), tok); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store credential")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

type createTokenRequest struct {
	Name   string `json:"name"`
	Scopes string `json:"scopes"`
}

type createTokenResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
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
	apiToken := &core.APIToken{
		ID:          uuid.NewString(),
		UserID:      userID,
		Name:        req.Name,
		HashedToken: hashed,
		Scopes:      req.Scopes,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.datastore.StoreAPIToken(r.Context(), apiToken); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store API token")
		return
	}

	writeJSON(w, http.StatusCreated, createTokenResponse{
		ID:    apiToken.ID,
		Name:  apiToken.Name,
		Token: plaintext,
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
	id := chi.URLParam(r, "id")
	if err := s.datastore.RevokeAPIToken(r.Context(), id); err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
