package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
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
}

func (s *Server) listIntegrations(w http.ResponseWriter, _ *http.Request) {
	names := s.providers.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		prov, err := s.providers.Get(name)
		if err != nil {
			continue
		}
		out = append(out, integrationInfo{
			Name:        name,
			DisplayName: prov.DisplayName(),
			Description: prov.Description(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	prov, err := s.providers.Get(name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}
	writeJSON(w, http.StatusOK, prov.ListOperations())
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	prov, err := s.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", providerName))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	ops := prov.ListOperations()
	found := false
	for _, op := range ops {
		if op.Name == operationName {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found on integration %q", operationName, providerName))
		return
	}

	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return
	}

	storedToken, err := s.datastore.Token(r.Context(), userID, providerName, defaultTokenInstance)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", providerName))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to retrieve integration token")
		return
	}
	if storedToken == nil {
		writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", providerName))
		return
	}

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

	accessToken, err := s.refreshTokenIfNeeded(r.Context(), prov, storedToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("token refresh failed for %q: %v", providerName, err))
		return
	}

	result, err := prov.Execute(r.Context(), operationName, params, accessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("operation failed: %v", err))
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

	prov, err := s.providers.Get(req.Integration)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", req.Integration))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support OAuth", req.Integration))
		return
	}

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
	// State placeholder is passed to the provider to build the URL template;
	// setURLQueryParam below replaces it with the encrypted state token.
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
	prov, err := s.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", providerName))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("integration %q does not support OAuth", providerName))
		return
	}

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

	hashed := hashToken(plaintext)

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

const tokenRefreshThreshold = 5 * time.Minute

func (s *Server) refreshTokenIfNeeded(ctx context.Context, prov core.Provider, token *core.IntegrationToken) (string, error) {
	if token.RefreshToken == "" || token.ExpiresAt == nil {
		return token.AccessToken, nil
	}

	if time.Until(*token.ExpiresAt) > tokenRefreshThreshold {
		return token.AccessToken, nil
	}

	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		return token.AccessToken, nil
	}

	resp, err := oauthProv.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		// Re-read to avoid overwriting a concurrent successful refresh.
		fresh, fetchErr := s.datastore.Token(ctx, token.UserID, token.Integration, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}

		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		_ = s.datastore.StoreToken(ctx, token)

		if time.Now().Before(*token.ExpiresAt) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token expired and refresh failed: %w", err)
	}

	now := time.Now()
	token.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		token.RefreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		t := now.Add(time.Duration(resp.ExpiresIn) * time.Second)
		token.ExpiresAt = &t
	} else {
		token.ExpiresAt = nil
	}
	token.LastRefreshedAt = now
	token.RefreshErrorCount = 0
	token.UpdatedAt = now

	if err := s.datastore.StoreToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
