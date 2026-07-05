package server

import (
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
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

type loginRequest struct {
	State        string `json:"state"`
	CallbackPort int    `json:"callbackPort,omitempty"`
	Next         string `json:"next,omitempty"`
}

type authInfoResponse struct {
	Provider       string                   `json:"provider"`
	DisplayName    string                   `json:"displayName"`
	LoginSupported bool                     `json:"loginSupported"`
	Features       authInfoFeaturesResponse `json:"features"`
}

type authInfoFeaturesResponse struct {
	Agent bool `json:"agent"`
}

type authSessionResponse struct {
	SubjectID   string `json:"subjectId"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	subjectID := ""
	if p != nil {
		subjectID = strings.TrimSpace(p.SubjectID)
	}
	if subjectID == "" && p != nil {
		subjectID = strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	}
	if subjectID == "" && p != nil && p.Identity != nil {
		if email := strings.TrimSpace(p.Identity.Email); email != "" {
			subjectID = principal.UserSubjectID(email)
		}
	}
	if subjectID == "" {
		writeError(w, http.StatusUnauthorized, "missing authenticated session")
		return
	}

	resp := authSessionResponse{
		SubjectID: subjectID,
	}
	if p != nil && p.Identity != nil {
		resp.Email = strings.TrimSpace(p.Identity.Email)
		resp.DisplayName = strings.TrimSpace(p.Identity.DisplayName)
	}
	if resp.DisplayName == "" && p != nil {
		resp.DisplayName = strings.TrimSpace(p.DisplayName)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) authProviderName() string {
	if s.serverAuthProvider != "" {
		return s.serverAuthProvider
	}
	if s.auth == nil {
		return "none"
	}
	return "identity"
}

func (s *Server) authEnabled() bool {
	return s.auth != nil && !s.noAuth
}

func (s *Server) agentFeatureAvailable() bool {
	return s != nil && s.agentRuns != nil && s.agentRuns.Available()
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
		Features: authInfoFeaturesResponse{
			Agent: s.agentFeatureAvailable(),
		},
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
	nextPath := ""
	if strings.TrimSpace(req.Next) != "" {
		var err error
		nextPath, err = resolveLoginRedirectPath(req.Next, s.allowedLoginRedirectBaseURLs())
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
	loginURL, err := s.beginLogin(w, r, auth, state, nextPath)
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
	redirectURI, err := s.authCallbackURL(r)
	if err != nil {
		return "", errors.New("failed to resolve callback URL")
	}
	resp, err := auth.provider.Authorize(r.Context(), &core.AuthorizeRequest{
		ResponseType: "code",
		ClientID:     core.DefaultOAuthClientID,
		RedirectURI:  redirectURI,
		State:        state,
	})
	if err != nil || resp == nil || strings.TrimSpace(resp.RedirectURI) == "" {
		return "", errors.New("failed to generate login URL")
	}
	loginURL, err := s.resolvePublicURL(r, resp.RedirectURI)
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

func (s *Server) authCallbackURL(r *http.Request) (string, error) {
	base := s.publicBaseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	parsed, err := url.Parse(strings.TrimRight(base, "/") + config.AuthCallbackPath)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
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

func (s *Server) setSessionCookie(w http.ResponseWriter, accessToken string, expiresIn int) {
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if expiresIn > 0 {
		maxAge = expiresIn
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    accessToken,
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
		metricutil.RecordAuthMetrics(r.Context(), startedAt, auth.providerName, "token", auditErr != nil)
		if auditSubjectID != "" {
			s.auditHTTPEventWithSubjectID(r.Context(), auditSubjectID, principal.SourceBearer.String(), auth.providerName, "auth.login.complete", auditAllowed, auditErr)
			return
		}
		s.auditHTTPEvent(r.Context(), nil, auth.providerName, "auth.login.complete", auditAllowed, auditErr)
	}()

	code := r.URL.Query().Get("code")
	if code == "" {
		auditErr = errors.New("missing code parameter")
		writeError(w, http.StatusBadRequest, "missing code parameter")
		return
	}
	originalState := r.URL.Query().Get("state")
	loginState, err := s.loginStateForCallback(r)
	if err != nil {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", err)
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}

	if !loginStatesMatch(loginState.State, originalState) {
		auditErr = errors.New("login state validation failed")
		slog.ErrorContext(r.Context(), "login state validation failed", "error", errors.New("login state mismatch"))
		writeError(w, http.StatusForbidden, "login state validation failed")
		return
	}

	if r.URL.Query().Get("cli") != "1" {
		if port, rawState, ok := extractCLIState(loginState.State); ok {
			redirectCLIAuthorization(w, r, port, code, rawState)
			return
		}
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

	redirectURI, err := s.authCallbackURL(r)
	if err != nil {
		auditErr = errors.New("failed to resolve callback URL")
		writeError(w, http.StatusInternalServerError, "failed to resolve callback URL")
		return
	}
	tokenResp, err := auth.provider.Token(r.Context(), &core.TokenRequest{
		GrantType:   core.GrantTypeAuthorizationCode,
		Code:        code,
		RedirectURI: redirectURI,
		ClientID:    core.DefaultOAuthClientID,
		State:       loginState.State,
	})
	if err != nil || tokenResp == nil || strings.TrimSpace(tokenResp.AccessToken) == "" {
		auditErr = errors.New("login failed")
		slog.ErrorContext(r.Context(), "login callback failed", "error", err)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}

	if s.encryptor != nil {
		s.clearLoginStateCookie(w)
	}

	if p, err := s.resolver.ResolveToken(r.Context(), tokenResp.AccessToken); err == nil && p != nil {
		if enriched, enrichErr := s.resolvePrincipalUserID(r.Context(), p); enrichErr == nil && enriched != nil && strings.TrimSpace(enriched.SubjectID) != "" {
			auditSubjectID = enriched.SubjectID
		} else if strings.TrimSpace(p.SubjectID) != "" {
			auditSubjectID = p.SubjectID
		}
	}

	if r.URL.Query().Get("cli") == "1" {
		apiGrant, exchangeErr := auth.provider.Token(r.Context(), &core.TokenRequest{
			GrantType:        core.GrantTypeTokenExchange,
			SubjectToken:     tokenResp.AccessToken,
			SubjectTokenType: core.SubjectTokenTypeAccessToken,
			ClientID:         core.DefaultOAuthClientID,
		})
		if exchangeErr != nil || apiGrant == nil || strings.TrimSpace(apiGrant.AccessToken) == "" || strings.TrimSpace(apiGrant.GrantID) == "" {
			auditErr = errors.New("failed to issue CLI grant")
			writeError(w, http.StatusInternalServerError, "failed to issue CLI grant")
			return
		}
		auditAllowed = true
		auditErr = nil
		writeJSON(w, http.StatusOK, createGrantResponse{
			ID:    strings.TrimSpace(apiGrant.GrantID),
			Token: apiGrant.AccessToken,
		})
		return
	}

	s.setSessionCookie(w, tokenResp.AccessToken, tokenResp.ExpiresIn)

	auditAllowed = true
	auditErr = nil
	if loginState != nil && loginState.NextPath != "" {
		http.Redirect(w, r, loginState.NextPath, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func extractCLIState(state string) (port int, rawState string, ok bool) {
	if !strings.HasPrefix(state, cliStatePrefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(state, cliStatePrefix)
	portText, raw, found := strings.Cut(rest, ":")
	if !found || portText == "" || raw == "" {
		return 0, "", false
	}
	p, err := strconv.Atoi(portText)
	if err != nil || p < 1 || p > maxPort {
		return 0, "", false
	}
	return p, raw, true
}

func redirectCLIAuthorization(w http.ResponseWriter, r *http.Request, port int, code, rawState string) {
	target := fmt.Sprintf(
		"http://127.0.0.1:%d/?code=%s&state=%s",
		port,
		url.QueryEscape(code),
		url.QueryEscape(rawState),
	)
	http.Redirect(w, r, target, http.StatusFound)
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
	if err := requireUserCaller(w, auditPrincipal); err != nil {
		auditErr = err
		return
	}

	s.clearSessionCookie(w)
	auditAllowed = true
	auditErr = nil
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
