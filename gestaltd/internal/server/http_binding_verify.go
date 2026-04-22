package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/httpbinding"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type verifiedHTTPBindingSender struct {
	Scheme  string
	Subject string
	Claims  map[string]string
}

type httpBindingRequestError struct {
	status  int
	message string
	err     error
}

func (e *httpBindingRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return e.err.Error()
	}
	return e.message
}

func (e *httpBindingRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func newHTTPBindingRequestError(status int, message string, err error) error {
	return &httpBindingRequestError{status: status, message: message, err: err}
}

func (s *Server) verifyHTTPBindingRequest(r *http.Request, binding MountedHTTPBinding, rawBody []byte) (*verifiedHTTPBindingSender, error) {
	if binding.Security == nil {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "http binding security is not configured", nil)
	}
	switch binding.Security.Type {
	case providermanifestv1.HTTPSecuritySchemeTypeHMAC:
		return s.verifyHTTPBindingHMACRequest(r, binding, rawBody)
	case providermanifestv1.HTTPSecuritySchemeTypeAPIKey:
		return verifyHTTPBindingAPIKey(r, binding)
	case providermanifestv1.HTTPSecuritySchemeTypeHTTP:
		return verifyHTTPBindingHTTPAuth(r, binding)
	case providermanifestv1.HTTPSecuritySchemeTypeNone:
		return &verifiedHTTPBindingSender{
			Scheme:  binding.SecurityName,
			Subject: binding.PluginName + "/" + binding.Name + "#" + binding.SecurityName,
			Claims: map[string]string{
				"scheme": binding.SecurityName,
			},
		}, nil
	default:
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "unsupported http binding security scheme", fmt.Errorf("unsupported scheme type %q", binding.Security.Type))
	}
}

func (s *Server) verifyHTTPBindingHMACRequest(r *http.Request, binding MountedHTTPBinding, rawBody []byte) (*verifiedHTTPBindingSender, error) {
	secret, err := resolveHTTPBindingSecret(binding.Security.Secret)
	if err != nil {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "http binding secret is not configured", err)
	}
	signature := strings.TrimSpace(r.Header.Get(strings.TrimSpace(binding.Security.SignatureHeader)))
	if signature == "" {
		return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "missing request signature", nil)
	}
	if strings.TrimSpace(binding.Security.TimestampHeader) != "" {
		timestamp := strings.TrimSpace(r.Header.Get(strings.TrimSpace(binding.Security.TimestampHeader)))
		if timestamp == "" {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "missing request timestamp", nil)
		}
		requestTime, err := parseUnixTimestamp(timestamp)
		if err != nil {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid request timestamp", err)
		}
		now := time.Now()
		if s != nil && s.now != nil {
			now = s.now()
		}
		maxAge := time.Duration(binding.Security.MaxAgeSeconds) * time.Second
		if delta := now.Sub(requestTime); delta > maxAge || delta < -maxAge {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "stale request timestamp", nil)
		}
	}
	payload, err := httpbinding.RenderPayloadTemplate(binding.Security.PayloadTemplate, r.Header, rawBody)
	if err != nil {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "invalid http binding payload template", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	expected := strings.TrimSpace(binding.Security.SignaturePrefix) + fmt.Sprintf("%x", mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid request signature", nil)
	}
	if binding.Ack != nil && strings.TrimSpace(binding.Security.TimestampHeader) != "" && binding.Security.MaxAgeSeconds > 0 {
		replayKey := binding.PluginName + "\x00" + binding.Name + "\x00" + binding.SecurityName + "\x00sig:" + signature
		if !s.httpBindingReplayStore.MarkIfNew(replayKey, time.Duration(binding.Security.MaxAgeSeconds)*time.Second) {
			return nil, newHTTPBindingRequestError(http.StatusOK, "duplicate signed delivery", nil)
		}
	}
	return &verifiedHTTPBindingSender{
		Scheme:  binding.SecurityName,
		Subject: binding.PluginName + "/" + binding.Name + "#" + binding.SecurityName,
		Claims: map[string]string{
			"scheme": binding.SecurityName,
		},
	}, nil
}

func verifyHTTPBindingAPIKey(r *http.Request, binding MountedHTTPBinding) (*verifiedHTTPBindingSender, error) {
	secret, err := resolveHTTPBindingSecret(binding.Security.Secret)
	if err != nil {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "http binding secret is not configured", err)
	}
	var actual string
	switch binding.Security.In {
	case providermanifestv1.HTTPInHeader:
		actual = strings.TrimSpace(r.Header.Get(strings.TrimSpace(binding.Security.Name)))
	case providermanifestv1.HTTPInQuery:
		actual = strings.TrimSpace(r.URL.Query().Get(strings.TrimSpace(binding.Security.Name)))
	default:
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "unsupported apiKey location", fmt.Errorf("unsupported apiKey location %q", binding.Security.In))
	}
	if actual == "" {
		return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "missing http binding credential", nil)
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(actual)) != 1 {
		return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid http binding credential", nil)
	}
	return &verifiedHTTPBindingSender{
		Scheme:  binding.SecurityName,
		Subject: binding.PluginName + "/" + binding.Name + "#" + binding.SecurityName,
		Claims: map[string]string{
			"scheme": binding.SecurityName,
		},
	}, nil
}

func verifyHTTPBindingHTTPAuth(r *http.Request, binding MountedHTTPBinding) (*verifiedHTTPBindingSender, error) {
	secret, err := resolveHTTPBindingSecret(binding.Security.Secret)
	if err != nil {
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "http binding secret is not configured", err)
	}
	switch binding.Security.Scheme {
	case providermanifestv1.HTTPAuthSchemeBearer:
		token, err := requestBearerToken(r)
		if err != nil {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid authorization header format", err)
		}
		if token == "" {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "missing bearer token", nil)
		}
		if subtle.ConstantTimeCompare([]byte(secret), []byte(token)) != 1 {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid bearer token", nil)
		}
	case providermanifestv1.HTTPAuthSchemeBasic:
		username, password, ok := r.BasicAuth()
		if !ok {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "missing basic authorization", nil)
		}
		if subtle.ConstantTimeCompare([]byte(secret), []byte(username+":"+password)) != 1 {
			return nil, newHTTPBindingRequestError(http.StatusUnauthorized, "invalid basic authorization", nil)
		}
	default:
		return nil, newHTTPBindingRequestError(http.StatusInternalServerError, "unsupported http auth scheme", fmt.Errorf("unsupported http auth scheme %q", binding.Security.Scheme))
	}
	return &verifiedHTTPBindingSender{
		Scheme:  binding.SecurityName,
		Subject: binding.PluginName + "/" + binding.Name + "#" + binding.SecurityName,
		Claims: map[string]string{
			"scheme": binding.SecurityName,
		},
	}, nil
}

func resolveHTTPBindingSecret(ref *providermanifestv1.HTTPSecretRef) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is required")
	}
	if env := strings.TrimSpace(ref.Env); env != "" {
		value, ok := os.LookupEnv(env)
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("environment variable %q is not set", env)
		}
		return value, nil
	}
	if secret := strings.TrimSpace(ref.Secret); secret != "" {
		return secret, nil
	}
	return "", fmt.Errorf("secret reference is empty")
}

func parseUnixTimestamp(raw string) (time.Time, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}
