package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type loginRequest struct {
	State        string `json:"state"`
	CallbackPort int    `json:"callbackPort,omitempty"`
}

type authInfoResponse struct {
	Provider       string `json:"provider"`
	DisplayName    string `json:"displayName"`
	LoginSupported bool   `json:"loginSupported"`
}

func (s *Server) authProviderName() string {
	if s.auth == nil {
		return "none"
	}
	return s.auth.Name()
}

func (s *Server) authEnabled() bool {
	return s.auth != nil && !s.noAuth
}

func (s *Server) authInfo(w http.ResponseWriter, _ *http.Request) {
	provider := s.authProviderName()
	displayName := provider
	if s.auth != nil {
		if dn, ok := s.auth.(AuthProviderDisplayName); ok {
			displayName = dn.DisplayName()
		}
	}
	writeJSON(w, http.StatusOK, authInfoResponse{
		Provider:       provider,
		DisplayName:    displayName,
		LoginSupported: s.authEnabled(),
	})
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("login start failed")
	providerName := s.authProviderName()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, providerName, "begin_login", auditErr != nil)
		s.auditHTTPEvent(r.Context(), nil, s.authProviderName(), "auth.login.start", auditAllowed, auditErr)
	}()

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auditErr = errors.New("invalid JSON body")
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	state := req.State
	if req.CallbackPort > 0 && req.CallbackPort <= maxPort {
		state = fmt.Sprintf("%s%d:%s", cliStatePrefix, req.CallbackPort, req.State)
	}
	if !s.authEnabled() {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}
	loginURL, err := s.beginLogin(w, r, state, "")
	if err != nil {
		auditErr = err
		status := http.StatusInternalServerError
		if errors.Is(err, errBadLoginRedirectPath) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"url": loginURL})
}

var errBadLoginRedirectPath = errors.New("invalid next path")

func (s *Server) startBrowserLogin(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("login start failed")
	providerName := s.authProviderName()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, providerName, "begin_login", auditErr != nil)
		s.auditHTTPEvent(r.Context(), nil, s.authProviderName(), "auth.login.start", auditAllowed, auditErr)
	}()

	nextPath, err := resolveLoginRedirectPath(r.URL.Query().Get("next"))
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.authEnabled() {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	loginURL, err := s.beginLogin(w, r, browserLoginStateForNextPath(nextPath), nextPath)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	auditAllowed = true
	auditErr = nil
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (s *Server) beginLogin(w http.ResponseWriter, r *http.Request, state, nextPath string) (string, error) {
	loginURLRaw, err := loginURLForRequest(r.Context(), s.auth, state)
	if err != nil {
		return "", errors.New("failed to generate login URL")
	}
	loginURL, err := s.resolvePublicURL(r, loginURLRaw)
	if err != nil {
		return "", errors.New("failed to resolve login URL")
	}
	if s.encryptor != nil {
		encoded, encErr := encodeLoginState(s.encryptor, loginState{
			State:     state,
			NextPath:  nextPath,
			ExpiresAt: s.now().Add(loginStateTTL).Unix(),
		})
		if encErr != nil {
			return "", errors.New("failed to encode login state")
		}
		http.SetCookie(w, &http.Cookie{
			Name:     loginStateCookieName,
			Value:    encoded,
			Path:     "/",
			MaxAge:   int(loginStateTTL.Seconds()),
			HttpOnly: true,
			Secure:   s.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
	}
	return loginURL, nil
}

func resolveLoginRedirectPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errBadLoginRedirectPath
	}
	if parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", errBadLoginRedirectPath
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.RequestURI(), nil
}

func browserLoginStateForNextPath(nextPath string) string {
	parsed, err := url.Parse(nextPath)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}

func loginURLForRequest(ctx context.Context, provider core.AuthProvider, state string) (string, error) {
	if providerWithContext, ok := provider.(core.LoginURLContextProvider); ok {
		return providerWithContext.LoginURLContext(ctx, state)
	}
	return provider.LoginURL(state)
}

func (s *Server) resolvePublicURL(r *http.Request, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}

	base := s.publicBaseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if baseURL.Path == "" {
		baseURL.Path = "/"
	}
	return baseURL.ResolveReference(parsed).String(), nil
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
		Secure:   s.secureCookies,
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
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// StatefulCallbackHandler is an optional interface for auth providers that need
// the OAuth state parameter during callback (e.g., for PKCE where the
// code_verifier is encrypted in the state).
type StatefulCallbackHandler interface {
	HandleCallbackWithState(ctx context.Context, code, state string) (*core.UserIdentity, string, error)
}

// RequestCallbackHandler is an optional interface for auth providers that need
// the full callback query map. This is used by executable auth plugins so the
// host can preserve callback state and provider-specific query parameters.
type RequestCallbackHandler interface {
	HandleCallbackRequest(ctx context.Context, query url.Values) (*core.UserIdentity, string, error)
}

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("login callback failed")
	auditUserID := ""
	providerName := s.authProviderName()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, providerName, "complete_login", auditErr != nil)
		if auditUserID != "" {
			s.auditHTTPEventWithUserID(r.Context(), auditUserID, principal.SourceSession.String(), s.authProviderName(), "auth.login.complete", auditAllowed, auditErr)
			return
		}
		s.auditHTTPEvent(r.Context(), nil, s.authProviderName(), "auth.login.complete", auditAllowed, auditErr)
	}()

	code := r.URL.Query().Get("code")
	if code == "" {
		auditErr = errors.New("missing code parameter")
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}
	if !s.authEnabled() {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	var identity *core.UserIdentity
	var originalState string
	var err error

	if handler, ok := s.auth.(RequestCallbackHandler); ok {
		identity, originalState, err = handler.HandleCallbackRequest(r.Context(), r.URL.Query())
	} else if stateful, ok := s.auth.(StatefulCallbackHandler); ok {
		state := r.URL.Query().Get("state")
		identity, originalState, err = stateful.HandleCallbackWithState(r.Context(), code, state)
	} else {
		originalState = r.URL.Query().Get("state")
		identity, err = s.auth.HandleCallback(r.Context(), code)
	}
	if err != nil {
		auditErr = errors.New("login failed")
		slog.ErrorContext(r.Context(), "login callback failed", "error", err)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}

	loginState, csrfErr := s.validateLoginState(r, originalState)
	if csrfErr != nil {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", csrfErr)
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}
	if s.encryptor != nil {
		s.clearLoginStateCookie(w)
	}

	if r.URL.Query().Get("cli") == "1" {
		dbUser, dbErr := s.users.FindOrCreateUser(r.Context(), identity.Email)
		if dbErr != nil || dbUser == nil || dbUser.ID == "" {
			auditErr = errors.New("failed to resolve user")
			writeError(w, http.StatusInternalServerError, "failed to resolve user")
			return
		}
		auditUserID = dbUser.ID
		apiToken, plaintext, issueErr := s.issueAPIToken(r.Context(), dbUser.ID, cliLoginTokenName, "", true)
		if issueErr != nil {
			auditErr = errors.New("failed to issue CLI API token")
			writeError(w, http.StatusInternalServerError, "failed to issue CLI API token")
			return
		}
		s.auditHTTPEventWithUserID(r.Context(), dbUser.ID, principal.SourceSession.String(), "", "api_token.create", true, nil)
		auditAllowed = true
		auditErr = nil
		writeJSON(w, http.StatusOK, createTokenResponse{
			ID:        apiToken.ID,
			Name:      apiToken.Name,
			Token:     plaintext,
			ExpiresAt: apiToken.ExpiresAt,
		})
		return
	}

	if identity != nil && identity.Email != "" {
		dbUser, auditPrincipalErr := s.users.FindOrCreateUser(r.Context(), identity.Email)
		switch {
		case auditPrincipalErr != nil:
			slog.WarnContext(r.Context(), "login audit user resolution failed", "error", auditPrincipalErr)
		case dbUser != nil && dbUser.ID != "":
			auditUserID = dbUser.ID
		default:
			slog.WarnContext(r.Context(), "login audit user resolution failed", "error", "authenticated principal missing user ID")
		}
	}

	resp := map[string]any{
		"email":       identity.Email,
		"displayName": identity.DisplayName,
	}

	token, err := s.issueSessionToken(identity)
	if err != nil {
		auditErr = errors.New("failed to issue session token")
		writeError(w, http.StatusInternalServerError, "failed to issue session token")
		return
	}
	s.setSessionCookie(w, token)

	auditAllowed = true
	auditErr = nil
	if loginState != nil && loginState.NextPath != "" {
		http.Redirect(w, r, loginState.NextPath, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) validateLoginState(r *http.Request, originalState string) (*loginState, error) {
	if s.encryptor == nil {
		return &loginState{State: originalState}, nil
	}
	cookie, err := r.Cookie(loginStateCookieName)
	if err != nil {
		return nil, fmt.Errorf("missing login state cookie")
	}
	expected, err := decodeLoginState(s.encryptor, cookie.Value, s.now())
	if err != nil {
		return nil, fmt.Errorf("invalid login state cookie: %w", err)
	}
	if expected.State != originalState {
		return nil, fmt.Errorf("login state mismatch")
	}
	return expected, nil
}

func (s *Server) clearLoginStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	auditAllowed := false
	auditErr := errors.New("logout failed")
	var auditPrincipal *principal.Principal
	if !s.noAuth {
		p, err := s.resolveRequestPrincipalWithUserID(r)
		switch {
		case err == nil:
			auditPrincipal = p
		case errors.Is(err, errInvalidAuthorizationHeader), errors.Is(err, principal.ErrInvalidToken):
			slog.InfoContext(r.Context(), "logout: unable to resolve caller for audit", "error", err)
		default:
			slog.WarnContext(r.Context(), "logout: unable to resolve caller for audit", "error", err)
		}
	}
	defer func() {
		s.auditHTTPEvent(r.Context(), auditPrincipal, s.authProviderName(), "auth.logout", auditAllowed, auditErr)
	}()
	if auditPrincipal != nil && auditPrincipal.Kind == principal.KindWorkload {
		auditErr = errWorkloadForbidden
		writeError(w, http.StatusForbidden, "workload callers are not allowed on this route")
		return
	}

	s.clearSessionCookie(w)
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
