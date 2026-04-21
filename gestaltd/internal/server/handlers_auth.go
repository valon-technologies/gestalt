package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	stdpath "path"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authcallback"
	"github.com/valon-technologies/gestalt/server/internal/config"
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
	if s.serverAuthProvider != "" {
		return s.serverAuthProvider
	}
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
	auth := s.serverAuthRuntime()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, auth.providerName, "begin_login", auditErr != nil)
		s.auditHTTPEvent(r.Context(), nil, auth.providerName, "auth.login.start", auditAllowed, auditErr)
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
	if auth.noAuth || auth.provider == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}
	loginURL, err := s.beginLogin(w, r, auth, state, "")
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
	auth := s.serverAuthRuntime()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, auth.providerName, "begin_login", auditErr != nil)
		s.auditHTTPEvent(r.Context(), nil, auth.providerName, "auth.login.start", auditAllowed, auditErr)
	}()

	nextPath, err := resolveLoginRedirectPath(r.URL.Query().Get("next"), s.allowedLoginRedirectBaseURLs())
	if err != nil {
		auditErr = err
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	auth, err = s.loginAuthRuntimeForNextPath(nextPath)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if auth.noAuth || auth.provider == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	loginURL, err := s.beginLogin(w, r, auth, browserLoginStateForNextPath(nextPath), nextPath)
	if err != nil {
		auditErr = err
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	auditAllowed = true
	auditErr = nil
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (s *Server) beginLogin(w http.ResponseWriter, r *http.Request, auth authRuntime, state, nextPath string) (string, error) {
	callbackURL, err := s.authCallbackURLForRequest(r)
	if err != nil {
		return "", errors.New("failed to resolve callback URL")
	}
	loginURLRaw, err := loginURLForRequest(r.Context(), auth.provider, &core.BeginAuthenticationRequest{
		CallbackURL: callbackURL,
		HostState:   state,
	})
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
			Provider:  auth.providerRef,
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

func (s *Server) allowedLoginRedirectBaseURLs() []string {
	if s.routeProfile != RouteProfilePublic || s.managementBaseURL == "" || s.adminRoute.AuthorizationPolicy == "" {
		return nil
	}
	return []string{strings.TrimRight(s.managementBaseURL, "/") + "/admin"}
}

func resolveLoginRedirectPath(raw string, allowedBaseURLs []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errBadLoginRedirectPath
	}
	if parsed.IsAbs() || parsed.Host != "" {
		for _, base := range allowedBaseURLs {
			if absoluteLoginRedirectAllowed(raw, base) {
				parsed.Fragment = ""
				return parsed.String(), nil
			}
		}
		return "", errBadLoginRedirectPath
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", errBadLoginRedirectPath
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.RequestURI(), nil
}

func absoluteLoginRedirectAllowed(raw, allowedBase string) bool {
	base, err := url.Parse(strings.TrimSpace(allowedBase))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return false
	}
	next, err := url.Parse(raw)
	if err != nil || !next.IsAbs() || next.Host == "" {
		return false
	}
	if next.Scheme != base.Scheme || next.Host != base.Host {
		return false
	}
	basePath, ok := normalizedAbsoluteRedirectPath(base)
	if !ok {
		return false
	}
	nextPath, ok := normalizedAbsoluteRedirectPath(next)
	if !ok {
		return false
	}
	switch {
	case basePath == "":
		return strings.HasPrefix(nextPath, "/")
	case nextPath == basePath:
		return true
	case strings.HasPrefix(nextPath, basePath+"/"):
		return true
	default:
		return false
	}
}

func normalizedAbsoluteRedirectPath(u *url.URL) (string, bool) {
	path := u.EscapedPath()
	if path == "" {
		return "/", true
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return "", false
	}
	path = stdpath.Clean(decoded)
	if path == "." {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path, true
}

func browserLoginStateForNextPath(nextPath string) string {
	parsed, err := url.Parse(nextPath)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}

func loginURLForRequest(ctx context.Context, provider core.AuthenticationProvider, req *core.BeginAuthenticationRequest) (string, error) {
	resp, err := provider.BeginAuthentication(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("authentication provider returned nil response")
	}
	upstreamState, err := authcallback.StateFromURL(resp.AuthorizationURL)
	if err != nil {
		return "", err
	}
	if len(resp.ProviderState) == 0 && upstreamState == req.HostState {
		return resp.AuthorizationURL, nil
	}
	encodedState, err := authcallback.EncodeState(req.HostState, resp.ProviderState, upstreamState)
	if err != nil {
		return "", err
	}
	wrappedURL, _, err := authcallback.WithWrappedStateParam(resp.AuthorizationURL, encodedState)
	if err != nil {
		return "", err
	}
	return wrappedURL, nil
}

func (s *Server) authCallbackURLForRequest(r *http.Request) (string, error) {
	return s.resolvePublicURL(r, config.AuthCallbackPath)
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

// SessionTokenIssuer is an optional interface that authentication providers can implement
// to issue session tokens after login.
type SessionTokenIssuer interface {
	IssueSessionToken(identity *core.UserIdentity) (string, error)
}

// SessionTokenTTLProvider is an optional interface that authentication providers can
// implement to expose their configured session TTL for cookie MaxAge.
type SessionTokenTTLProvider interface {
	SessionTokenTTL() time.Duration
}

func (s *Server) setSessionCookie(provider core.AuthenticationProvider, w http.ResponseWriter, token string) {
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if p, ok := provider.(SessionTokenTTLProvider); ok {
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

func (s *Server) loginCallback(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditAllowed := false
	auditErr := errors.New("login callback failed")
	auditSubjectID := ""
	auth := s.serverAuthRuntime()
	defer func() {
		metricutil.RecordAuthMetrics(r.Context(), startedAt, auth.providerName, "complete_login", auditErr != nil)
		if auditSubjectID != "" {
			s.auditHTTPEventWithSubjectID(r.Context(), auditSubjectID, principal.SourceSession.String(), auth.providerName, "auth.login.complete", auditAllowed, auditErr)
			return
		}
		s.auditHTTPEvent(r.Context(), nil, auth.providerName, "auth.login.complete", auditAllowed, auditErr)
	}()

	if r.URL.Query().Get("code") == "" && r.URL.Query().Get("error") == "" {
		auditErr = errors.New("missing code parameter")
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}

	loginState, err := s.loginStateForCallback(r)
	if err != nil {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", err)
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}

	auth, err = s.authRuntimeForProvider(loginState.Provider)
	if err != nil {
		auditErr = err
		slog.ErrorContext(r.Context(), "login auth provider resolution failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login auth provider is not initialized")
		return
	}
	if auth.noAuth || auth.provider == nil {
		auditErr = errors.New("auth is disabled")
		writeError(w, http.StatusNotFound, "auth is disabled")
		return
	}

	callbackURL, err := s.authCallbackURLForRequest(r)
	if err != nil {
		auditErr = errors.New("failed to resolve callback URL")
		writeError(w, http.StatusInternalServerError, "failed to resolve callback URL")
		return
	}
	hostState, providerState, upstreamState, err := authcallback.DecodeState(r.URL.Query().Get("state"))
	if err != nil {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", err)
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}
	normalizedQuery := authcallback.FirstQueryValues(r.URL.Query())
	if normalizedQuery == nil {
		normalizedQuery = map[string]string{}
	}
	if upstreamState != "" {
		normalizedQuery["state"] = upstreamState
	} else {
		delete(normalizedQuery, "state")
	}
	originalState := hostState
	if originalState == "" {
		originalState = r.URL.Query().Get("state")
	}
	identity, err := auth.provider.CompleteAuthentication(r.Context(), &core.CompleteAuthenticationRequest{
		CallbackURL:   callbackURL,
		Query:         normalizedQuery,
		ProviderState: providerState,
	})
	if err != nil {
		auditErr = errors.New("login failed")
		slog.ErrorContext(r.Context(), "login callback failed", "error", err)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}

	if !loginStatesMatch(loginState.State, originalState) {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", errors.New("login state mismatch"))
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
		auditSubjectID = principal.UserSubjectID(dbUser.ID)
		apiToken, plaintext, issueErr := s.issueAPIToken(r.Context(), dbUser.ID, cliLoginTokenName, "", true)
		if issueErr != nil {
			auditErr = errors.New("failed to issue CLI API token")
			writeError(w, http.StatusInternalServerError, "failed to issue CLI API token")
			return
		}
		s.auditHTTPEventWithSubjectID(r.Context(), principal.UserSubjectID(dbUser.ID), principal.SourceSession.String(), "", "api_token.create", true, nil)
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
			auditSubjectID = principal.UserSubjectID(dbUser.ID)
		default:
			slog.WarnContext(r.Context(), "login audit user resolution failed", "error", "authenticated principal missing user ID")
		}
	}

	resp := map[string]any{
		"email":       identity.Email,
		"displayName": identity.DisplayName,
	}

	token, err := s.issueSessionToken(auth.provider, identity)
	if err != nil {
		auditErr = errors.New("failed to issue session token")
		writeError(w, http.StatusInternalServerError, "failed to issue session token")
		return
	}
	s.setSessionCookie(auth.provider, w, token)

	auditAllowed = true
	auditErr = nil
	if loginState != nil && loginState.NextPath != "" {
		http.Redirect(w, r, loginState.NextPath, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) loginStateForCallback(r *http.Request) (*loginState, error) {
	if s.encryptor == nil {
		return &loginState{
			State:    r.URL.Query().Get("state"),
			Provider: "server",
		}, nil
	}
	cookie, err := r.Cookie(loginStateCookieName)
	if err != nil {
		return nil, fmt.Errorf("missing login state cookie")
	}
	state, err := decodeLoginState(s.encryptor, cookie.Value, s.now())
	if err != nil {
		return nil, fmt.Errorf("invalid login state cookie: %w", err)
	}
	if strings.TrimSpace(state.Provider) == "" {
		state.Provider = "server"
	}
	return state, nil
}

func loginStatesMatch(expectedState, originalState string) bool {
	if expectedState == originalState {
		return true
	}
	if rawState, ok := stripCLIStatePrefix(expectedState); ok && rawState == originalState {
		return true
	}
	if rawState, ok := stripCLIStatePrefix(originalState); ok && rawState == expectedState {
		return true
	}
	return false
}

func stripCLIStatePrefix(state string) (string, bool) {
	if !strings.HasPrefix(state, cliStatePrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(state, cliStatePrefix)
	portText, rawState, ok := strings.Cut(rest, ":")
	if !ok || portText == "" || rawState == "" {
		return "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > maxPort {
		return "", false
	}
	return rawState, true
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
