package server

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

const (
	loginDeniedPath           = browserLoginPath + "/denied"
	loginDeniedReasonCookie   = "login_denied_reason"
	loginDeniedCookieMaxAge   = 300
	loginFailureReasonDomain  = "domain_not_allowed"
	loginFailureReasonEmail   = "email_not_verified"
	loginFailureReasonOAuth   = "oauth_error"
	loginFailureReasonGeneric = "generic"
)

func classifyLoginFailure(err error) string {
	if err == nil {
		return loginFailureReasonGeneric
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "is not allowed"):
		return loginFailureReasonDomain
	case strings.Contains(msg, "is not verified"):
		return loginFailureReasonEmail
	default:
		return loginFailureReasonGeneric
	}
}

func loginFailureFromOAuthError(oauthErr, description string) error {
	oauthErr = strings.TrimSpace(oauthErr)
	description = strings.TrimSpace(description)
	if description != "" {
		return fmt.Errorf("%s: %s", oauthErr, description)
	}
	if oauthErr != "" {
		return errors.New(oauthErr)
	}
	return errors.New("oauth authorization failed")
}

func (s *Server) loginDeniedURL(r *http.Request, reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = loginFailureReasonGeneric
	}
	deniedPath := loginDeniedPath + "?reason=" + url.QueryEscape(reason)
	return s.resolvePublicURL(r, deniedPath)
}

func (s *Server) setLoginDeniedReasonCookie(w http.ResponseWriter, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     loginDeniedReasonCookie,
		Value:    reason,
		Path:     loginDeniedPath,
		MaxAge:   loginDeniedCookieMaxAge,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) loginDeniedReasonFromRequest(r *http.Request) string {
	if reason := strings.TrimSpace(r.URL.Query().Get("reason")); reason != "" {
		return reason
	}
	cookie, err := r.Cookie(loginDeniedReasonCookie)
	if err != nil {
		return loginFailureReasonGeneric
	}
	reason := strings.TrimSpace(cookie.Value)
	if reason == "" {
		return loginFailureReasonGeneric
	}
	return reason
}

func (s *Server) clearLoginDeniedReasonCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginDeniedReasonCookie,
		Value:    "",
		Path:     loginDeniedPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) failBrowserLogin(w http.ResponseWriter, r *http.Request, auth authRuntime, cause error) {
	s.clearLoginStateCookie(w)
	reason := classifyLoginFailure(cause)
	if strings.TrimSpace(r.URL.Query().Get("error")) != "" {
		reason = loginFailureReasonOAuth
	}
	s.setLoginDeniedReasonCookie(w, reason)

	deniedURL, err := s.loginDeniedURL(r, reason)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}
	if logoutURL, err := s.federatedLogoutURL(auth, deniedURL); err == nil && strings.TrimSpace(logoutURL) != "" {
		http.Redirect(w, r, logoutURL, http.StatusFound)
		return
	}
	http.Redirect(w, r, deniedURL, http.StatusFound)
}

func loginDeniedCopy(reason string) (title, message string) {
	switch strings.TrimSpace(reason) {
	case loginFailureReasonDomain:
		return "Sign-in not allowed", "That email domain is not allowed for this site. Sign in with an approved work account, or contact your administrator."
	case loginFailureReasonEmail:
		return "Email not verified", "Your email address must be verified before you can sign in."
	case loginFailureReasonOAuth:
		return "Sign-in canceled", "Sign-in did not complete. You can try again with a different account."
	default:
		return "Sign-in failed", "We could not complete sign-in. You can try again with a different account."
	}
}

func (s *Server) loginDenied(w http.ResponseWriter, r *http.Request) {
	reason := s.loginDeniedReasonFromRequest(r)
	s.clearLoginDeniedReasonCookie(w)

	title, message := loginDeniedCopy(reason)
	retryURL, err := s.resolvePublicURL(r, browserLoginPath+"?next=/")
	if err != nil {
		retryURL = browserLoginPath + "?next=/"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f8fafc; color: #0f172a; }
    main { max-width: 32rem; padding: 2rem; background: #fff; border: 1px solid #e2e8f0; border-radius: 12px; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.08); }
    h1 { margin: 0 0 0.75rem; font-size: 1.5rem; }
    p { margin: 0 0 1.5rem; line-height: 1.5; color: #334155; }
    a { display: inline-block; padding: 0.625rem 1rem; border-radius: 8px; background: #0f172a; color: #fff; text-decoration: none; font-weight: 600; }
    a:hover { background: #1e293b; }
  </style>
</head>
<body>
  <main>
    <h1>%s</h1>
    <p>%s</p>
    <a href="%s">Try again</a>
  </main>
</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(title),
		html.EscapeString(message),
		html.EscapeString(retryURL),
	)
}
