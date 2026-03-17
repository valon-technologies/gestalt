package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/toolshed/core"
)

func (s *Server) healthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type integrationInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
}

func (s *Server) listIntegrations(w http.ResponseWriter, _ *http.Request) {
	names := s.integrations.List()
	out := make([]integrationInfo, 0, len(names))
	for _, name := range names {
		integration, err := s.integrations.Get(name)
		if err != nil {
			continue
		}
		out = append(out, integrationInfo{
			Name:        name,
			DisplayName: integration.DisplayName(),
			Description: integration.Description(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	integration, err := s.integrations.Get(name)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", name))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}
	writeJSON(w, http.StatusOK, integration.ListOperations())
}

func (s *Server) executeOperation(w http.ResponseWriter, r *http.Request) {
	integrationName := chi.URLParam(r, "integration")
	operationName := chi.URLParam(r, "operation")

	integration, err := s.integrations.Get(integrationName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", integrationName))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	ops := integration.ListOperations()
	found := false
	for _, op := range ops {
		if op.Name == operationName {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found on integration %q", operationName, integrationName))
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	userID := UserIDFromContext(r.Context())
	if userID == "" {
		dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		userID = dbUser.ID
	}

	storedToken, err := s.datastore.Token(r.Context(), userID, integrationName, "default")
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", integrationName))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to retrieve integration token")
		return
	}
	if storedToken == nil {
		writeError(w, http.StatusPreconditionFailed, fmt.Sprintf("no token stored for integration %q; connect via OAuth first", integrationName))
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

	result, err := integration.Execute(r.Context(), operationName, params, storedToken.AccessToken)
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

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	url := s.auth.LoginURL(req.State)
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// SessionTokenIssuer is an optional interface that auth providers can implement
// to issue session tokens after login.
type SessionTokenIssuer interface {
	IssueSessionToken(identity *core.UserIdentity) (string, error)
}

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	identity, err := s.auth.HandleCallback(r.Context(), code)
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

func (s *Server) startIntegrationOAuth(w http.ResponseWriter, r *http.Request) {
	var req startOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	integration, err := s.integrations.Get(req.Integration)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", req.Integration))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	state, err := generateRandomHex(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	url := integration.AuthorizationURL(state, req.Scopes)
	writeJSON(w, http.StatusOK, map[string]string{"url": url, "state": state})
}

type oauthCallbackRequest struct {
	Code        string `json:"code"`
	Integration string `json:"integration"`
}

func (s *Server) integrationOAuthCallback(w http.ResponseWriter, r *http.Request) {
	var req oauthCallbackRequest
	code := r.URL.Query().Get("code")
	integrationName := r.URL.Query().Get("integration")
	if code == "" || integrationName == "" {
		writeError(w, http.StatusBadRequest, "missing code or integration parameter")
		return
	}
	req.Code = code
	req.Integration = integrationName

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	integration, err := s.integrations.Get(req.Integration)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("integration %q not found", req.Integration))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up integration")
		return
	}

	tokenResp, err := integration.ExchangeCode(r.Context(), req.Code)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("token exchange failed: %v", err))
		return
	}

	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	scopes := ""
	tok := &core.IntegrationToken{
		UserID:       dbUser.ID,
		Integration:  req.Integration,
		Instance:     "default",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		Scopes:       scopes,
		ExpiresAt:    expiresAt,
	}

	if err := s.datastore.StoreToken(r.Context(), tok); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store token")
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
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
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

	dbUser, err := s.datastore.FindOrCreateUser(r.Context(), user.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user")
		return
	}

	apiToken := &core.APIToken{
		UserID:      dbUser.ID,
		Name:        req.Name,
		HashedToken: hashed,
		Scopes:      req.Scopes,
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

	tokens, err := s.datastore.ListAPITokens(r.Context(), dbUser.ID)
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
