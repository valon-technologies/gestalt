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
	// WorkflowDefaultProvider is the configured workflow platform provider name
	// (Workflow API `provider` query). Omitted when no default workflow provider
	// is selected (empty DefaultProviderName), even if WorkflowControl is present.
	WorkflowDefaultProvider string `json:"workflowDefaultProvider,omitempty"`
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
		if resolved, err := principal.ResolveCredentialSubjectID(r.Context(), s.users, p); err == nil {
			subjectID = resolved
		}
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

// defaultWorkflowProviderName returns the deployment's configured workflow
// platform provider name from WorkflowControl. Empty means no default is
// selected (auth/info omits workflowDefaultProvider).
func (s *Server) defaultWorkflowProviderName() string {
	if s == nil || s.workflow == nil {
		return ""
	}
	return strings.TrimSpace(s.workflow.DefaultProviderName())
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
			Agent:                   s.agentFeatureAvailable(),
			WorkflowDefaultProvider: s.defaultWorkflowProviderName(),
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
	auth, nextPath, err := s.resolveLoginStartAuth(req.Next, false)
	if err != nil {
		auditErr = err
		status := http.StatusBadRequest
		if !errors.Is(err, errBadLoginRedirectPath) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
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

	auth, nextPath, err := s.resolveLoginStartAuth(r.URL.Query().Get("next"), true)
	if err != nil {
		auditErr = err
		status := http.StatusBadRequest
		if !errors.Is(err, errBadLoginRedirectPath) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
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

func (s *Server) resolveLoginStartAuth(nextRaw string, defaultRoot bool) (authRuntime, string, error) {
	nextPath, err := resolveLoginRedirectPath(nextRaw, s.allowedLoginRedirectBaseURLs())
	if err != nil {
		return authRuntime{}, "", err
	}
	if nextPath == "" && defaultRoot {
		nextPath = "/"
	}
	auth := s.serverAuthRuntime()
	if nextPath != "" {
		var authErr error
		auth, authErr = s.loginAuthRuntimeForNextPath(nextPath)
		if authErr != nil {
			return auth, nextPath, authErr
		}
	}
	return auth, nextPath, nil
}

func resolveLoginRedirectPath(raw string, allowedBaseURLs []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
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
	if r.URL.Query().Get("cli") != "1" {
		if port, rawState, ok := extractCLIState(originalState); ok {
			redirectCLIAuthorization(w, r, port, code, rawState)
			return
		}
	}

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

	mode, cliPort, cliRawState := resolveLoginCallbackMode(r, loginState.State)
	if mode == loginCallbackBounce {
		redirectCLIAuthorization(w, r, cliPort, code, cliRawState)
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

	if mode == loginCallbackCLIGrant {
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

type loginCallbackMode int

const (
	loginCallbackBrowser loginCallbackMode = iota
	loginCallbackBounce
	loginCallbackCLIGrant
)

func resolveLoginCallbackMode(r *http.Request, cookieState string) (loginCallbackMode, int, string) {
	if r.URL.Query().Get("cli") == "1" {
		return loginCallbackCLIGrant, 0, ""
	}
	if port, rawState, ok := extractCLIState(cookieState); ok {
		return loginCallbackBounce, port, rawState
	}
	return loginCallbackBrowser, 0, ""
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

func stripCLIStatePrefix(state string) (string, bool) {
	_, rawState, ok := extractCLIState(state)
	return rawState, ok
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

type federatedLogoutProvider interface {
	FederatedLogoutURL(returnTo string) (string, error)
}

func (s *Server) federatedLogoutURL(returnTo string) (string, error) {
	if s == nil || s.auth == nil {
		return "", errors.New("auth is not configured")
	}
	provider, ok := s.auth.(federatedLogoutProvider)
	if !ok {
		return "", errors.New("federated logout is not supported")
	}
	return provider.FederatedLogoutURL(returnTo)
}

func (s *Server) logoutReturnURL(r *http.Request) (string, error) {
	returnPath, err := resolveLoginRedirectPath(r.URL.Query().Get("returnTo"), s.allowedLoginRedirectBaseURLs())
	if err != nil {
		return "", err
	}
	if returnPath == "" {
		returnPath = "/"
	}
	return s.resolvePublicURL(r, returnPath)
}

func (s *Server) logoutBrowser(w http.ResponseWriter, r *http.Request) {
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

	s.clearSessionCookie(w)
	auditAllowed = true
	auditErr = nil

	returnTo, err := s.logoutReturnURL(r)
	if err != nil {
		slog.WarnContext(r.Context(), "logout return url resolution failed", "error", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if logoutURL, err := s.federatedLogoutURL(returnTo); err == nil && strings.TrimSpace(logoutURL) != "" {
		http.Redirect(w, r, logoutURL, http.StatusFound)
		return
	}
	parsed, err := url.Parse(returnTo)
	if err != nil || parsed.Path == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, parsed.RequestURI(), http.StatusFound)
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
	resp := map[string]string{"status": "ok"}
	if returnTo, err := s.logoutReturnURL(r); err == nil {
		if logoutURL, err := s.federatedLogoutURL(returnTo); err == nil && strings.TrimSpace(logoutURL) != "" {
			resp["redirect"] = logoutURL
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
