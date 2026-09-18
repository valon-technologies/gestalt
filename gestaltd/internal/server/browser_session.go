package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// browserSessionCookiePurpose is authenticated as part of the cookie
// envelope. AES-GCM provides integrity, while the purpose prevents a valid
// ciphertext for another server state (login, OAuth, or pending connection)
// from being accepted as browser-session provenance.
const browserSessionCookiePurpose = "gestalt/browser-session"
const browserSessionCookiePrefix = "gst_browser_session."

var (
	errBrowserSessionEncryptionUnavailable = errors.New("browser session encryption is not configured")
	errInvalidBrowserSessionCookie         = errors.New("invalid browser session cookie")
	errExpiredBrowserSessionCookie         = errors.New("browser session cookie expired")
)

type browserSessionCookie struct {
	Purpose   string `json:"purpose"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"exp"`
}

func (s *Server) browserSessionCookie(accessToken string, expiresIn int) (string, int, error) {
	if s == nil || s.encryptor == nil {
		return "", 0, errBrowserSessionEncryptionUnavailable
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", 0, fmt.Errorf("%w: token is empty", errInvalidBrowserSessionCookie)
	}
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if expiresIn > 0 {
		maxAge = expiresIn
	}
	now := s.now()
	encoded, err := encodeEncryptedState(s.encryptor, "browser session cookie", browserSessionCookie{
		Purpose:   browserSessionCookiePurpose,
		Token:     accessToken,
		ExpiresAt: now.Add(time.Duration(maxAge) * time.Second).Unix(),
	})
	if err != nil {
		return "", 0, err
	}
	return browserSessionCookiePrefix + encoded, maxAge, nil
}

func (s *Server) decodeBrowserSessionCookie(encoded string) (string, error) {
	if s == nil || s.encryptor == nil {
		return "", errBrowserSessionEncryptionUnavailable
	}
	cookie, err := decodeEncryptedState[browserSessionCookie](s.encryptor, "browser session cookie", strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidBrowserSessionCookie, err)
	}
	if cookie.Purpose != browserSessionCookiePurpose || strings.TrimSpace(cookie.Token) == "" || cookie.ExpiresAt <= 0 {
		return "", errInvalidBrowserSessionCookie
	}
	now := s.now()
	if now.Unix() >= cookie.ExpiresAt {
		return "", errExpiredBrowserSessionCookie
	}
	return strings.TrimSpace(cookie.Token), nil
}

// sessionCookieCredential returns the upstream identity token represented by
// the session cookie and whether it was minted by the browser login callback.
// Legacy raw cookies remain ordinary bearer credentials; malformed prefixed
// session cookies fail closed.
func (s *Server) sessionCookieCredential(r *http.Request) (token string, browserSession bool, present bool, err error) {
	if r == nil {
		return "", false, false, nil
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	value := strings.TrimSpace(cookie.Value)
	if value == "" {
		return "", false, true, nil
	}
	if strings.HasPrefix(value, browserSessionCookiePrefix) {
		token, decodeErr := s.decodeBrowserSessionCookie(strings.TrimPrefix(value, browserSessionCookiePrefix))
		if decodeErr != nil {
			return "", false, true, decodeErr
		}
		return token, true, true, nil
	}
	return value, false, true, nil
}
