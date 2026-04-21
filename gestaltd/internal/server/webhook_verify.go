package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

var webhookHeaderTemplatePattern = regexp.MustCompile(`\{header\.([^}]+)\}`)

type verifiedWebhookSender struct {
	Scheme     string
	Subject    string
	DeliveryID string
	Claims     map[string]string
}

type webhookRequestError struct {
	status  int
	message string
	err     error
}

func (e *webhookRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return e.err.Error()
	}
	return e.message
}

func (e *webhookRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func newWebhookRequestError(status int, message string, err error) error {
	return &webhookRequestError{status: status, message: message, err: err}
}

func (s *Server) verifyWebhookRequest(r *http.Request, mounted MountedWebhook, rawBody []byte) (*verifiedWebhookSender, error) {
	if mounted.Operation == nil {
		return nil, newWebhookRequestError(http.StatusInternalServerError, "webhook operation is not initialized", nil)
	}
	if len(mounted.Operation.Security) == 0 {
		return nil, newWebhookRequestError(http.StatusInternalServerError, "webhook security is not configured", nil)
	}

	var lastErr error
	for _, requirement := range mounted.Operation.Security {
		verified := &verifiedWebhookSender{
			Claims: map[string]string{},
		}
		matched := true
		for schemeName := range requirement {
			scheme := mounted.SecuritySchemes[schemeName]
			if scheme == nil {
				lastErr = newWebhookRequestError(http.StatusInternalServerError, "webhook security scheme is not initialized", nil)
				matched = false
				break
			}
			result, err := s.verifyWebhookSecurityScheme(r, mounted, schemeName, scheme, rawBody)
			if err != nil {
				lastErr = err
				matched = false
				break
			}
			if verified.Scheme == "" {
				verified.Scheme = result.Scheme
			}
			if verified.Subject == "" {
				verified.Subject = result.Subject
			}
			if verified.DeliveryID == "" {
				verified.DeliveryID = result.DeliveryID
			}
			for key, value := range result.Claims {
				verified.Claims[key] = value
			}
		}
		if matched {
			if verified.Subject == "" {
				verified.Subject = mounted.PluginName + "/" + mounted.Name
			}
			if verified.Scheme == "" {
				verified.Scheme = "none"
			}
			return verified, nil
		}
	}
	if lastErr == nil {
		lastErr = newWebhookRequestError(http.StatusUnauthorized, "webhook verification failed", nil)
	}
	return nil, lastErr
}

func (s *Server) verifyWebhookSecurityScheme(r *http.Request, mounted MountedWebhook, schemeName string, scheme *providermanifestv1.WebhookSecurityScheme, rawBody []byte) (*verifiedWebhookSender, error) {
	switch scheme.Type {
	case providermanifestv1.WebhookSecuritySchemeTypeNone:
		return &verifiedWebhookSender{
			Scheme:  schemeName,
			Subject: mounted.PluginName + "/" + mounted.Name + "#" + schemeName,
			Claims:  map[string]string{},
		}, nil
	case providermanifestv1.WebhookSecuritySchemeTypeHMAC:
		return s.verifyWebhookHMAC(r, mounted, schemeName, scheme, rawBody)
	case providermanifestv1.WebhookSecuritySchemeTypeAPIKey:
		return s.verifyWebhookAPIKey(r, mounted, schemeName, scheme)
	case providermanifestv1.WebhookSecuritySchemeTypeHTTP:
		return s.verifyWebhookHTTPAuth(r, mounted, schemeName, scheme)
	case providermanifestv1.WebhookSecuritySchemeTypeMutualTLS:
		return s.verifyWebhookMutualTLS(r, mounted, schemeName, scheme)
	default:
		return nil, newWebhookRequestError(http.StatusInternalServerError, "unsupported webhook security scheme", fmt.Errorf("unsupported scheme type %q", scheme.Type))
	}
}

func (s *Server) verifyWebhookHMAC(r *http.Request, mounted MountedWebhook, schemeName string, scheme *providermanifestv1.WebhookSecurityScheme, rawBody []byte) (*verifiedWebhookSender, error) {
	secret, err := resolveWebhookSharedSecret(scheme.Secret)
	if err != nil {
		return nil, newWebhookRequestError(http.StatusInternalServerError, "webhook secret is not configured", err)
	}
	signatureHeader := strings.TrimSpace(scheme.Signature.SignatureHeader)
	signature := strings.TrimSpace(r.Header.Get(signatureHeader))
	if signature == "" {
		return nil, newWebhookRequestError(http.StatusUnauthorized, "missing webhook signature", nil)
	}
	payload := renderWebhookPayloadTemplate(scheme.Signature.PayloadTemplate, r, rawBody)
	if payload == "" {
		payload = string(rawBody)
	}
	switch strings.ToLower(strings.TrimSpace(scheme.Signature.Algorithm)) {
	case "sha256":
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(payload))
		expected := strings.TrimSpace(scheme.Signature.DigestPrefix) + fmt.Sprintf("%x", mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "invalid webhook signature", nil)
		}
	default:
		return nil, newWebhookRequestError(http.StatusInternalServerError, "unsupported webhook signature algorithm", fmt.Errorf("unsupported hmac algorithm %q", scheme.Signature.Algorithm))
	}

	var deliveryID string
	if header := strings.TrimSpace(scheme.Signature.DeliveryIDHeader); header != "" {
		deliveryID = strings.TrimSpace(r.Header.Get(header))
	}
	ttl, err := verifyWebhookReplayWindow(r, scheme)
	if err != nil {
		return nil, err
	}
	replayKey := deliveryID
	if replayKey == "" && scheme.Replay != nil && strings.TrimSpace(scheme.Replay.MaxAge) != "" {
		replayKey = "sig:" + signature
	}
	if replayKey != "" && !s.webhookReplayStore.MarkIfNew(mounted.PluginName+"\x00"+mounted.Name+"\x00"+schemeName+"\x00"+replayKey, ttl) {
		return nil, newWebhookRequestError(http.StatusConflict, "duplicate webhook delivery", nil)
	}
	return &verifiedWebhookSender{
		Scheme:     schemeName,
		Subject:    mounted.PluginName + "/" + mounted.Name + "#" + schemeName,
		DeliveryID: deliveryID,
		Claims: map[string]string{
			"scheme": schemeName,
		},
	}, nil
}

func (s *Server) verifyWebhookAPIKey(r *http.Request, mounted MountedWebhook, schemeName string, scheme *providermanifestv1.WebhookSecurityScheme) (*verifiedWebhookSender, error) {
	secret, err := resolveWebhookSharedSecret(scheme.Secret)
	if err != nil {
		return nil, newWebhookRequestError(http.StatusInternalServerError, "webhook secret is not configured", err)
	}
	var actual string
	switch scheme.In {
	case providermanifestv1.WebhookInHeader:
		actual = strings.TrimSpace(r.Header.Get(strings.TrimSpace(scheme.Name)))
	case providermanifestv1.WebhookInQuery:
		actual = strings.TrimSpace(r.URL.Query().Get(strings.TrimSpace(scheme.Name)))
	default:
		return nil, newWebhookRequestError(http.StatusInternalServerError, "unsupported apiKey location", fmt.Errorf("unsupported apiKey location %q", scheme.In))
	}
	if actual == "" {
		return nil, newWebhookRequestError(http.StatusUnauthorized, "missing webhook credential", nil)
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(actual)) != 1 {
		return nil, newWebhookRequestError(http.StatusUnauthorized, "invalid webhook credential", nil)
	}
	return &verifiedWebhookSender{
		Scheme:  schemeName,
		Subject: mounted.PluginName + "/" + mounted.Name + "#" + schemeName,
		Claims: map[string]string{
			"scheme": schemeName,
		},
	}, nil
}

func (s *Server) verifyWebhookHTTPAuth(r *http.Request, mounted MountedWebhook, schemeName string, scheme *providermanifestv1.WebhookSecurityScheme) (*verifiedWebhookSender, error) {
	secret, err := resolveWebhookSharedSecret(scheme.Secret)
	if err != nil {
		return nil, newWebhookRequestError(http.StatusInternalServerError, "webhook secret is not configured", err)
	}
	switch scheme.Scheme {
	case providermanifestv1.WebhookHTTPSchemeBearer:
		token, err := requestBearerToken(r)
		if err != nil {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "invalid authorization header format", err)
		}
		if token == "" {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "missing bearer token", nil)
		}
		if subtle.ConstantTimeCompare([]byte(secret), []byte(token)) != 1 {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "invalid bearer token", nil)
		}
	case providermanifestv1.WebhookHTTPSchemeBasic:
		username, password, ok := r.BasicAuth()
		if !ok {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "missing basic authorization", nil)
		}
		if subtle.ConstantTimeCompare([]byte(secret), []byte(username+":"+password)) != 1 {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "invalid basic authorization", nil)
		}
	default:
		return nil, newWebhookRequestError(http.StatusInternalServerError, "unsupported http auth scheme", fmt.Errorf("unsupported http auth scheme %q", scheme.Scheme))
	}
	return &verifiedWebhookSender{
		Scheme:  schemeName,
		Subject: mounted.PluginName + "/" + mounted.Name + "#" + schemeName,
		Claims: map[string]string{
			"scheme": schemeName,
		},
	}, nil
}

func (s *Server) verifyWebhookMutualTLS(r *http.Request, mounted MountedWebhook, schemeName string, scheme *providermanifestv1.WebhookSecurityScheme) (*verifiedWebhookSender, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil, newWebhookRequestError(http.StatusUnauthorized, "client certificate is required", nil)
	}
	if len(r.TLS.VerifiedChains) == 0 {
		return nil, newWebhookRequestError(http.StatusUnauthorized, "verified client certificate chain is required", nil)
	}
	peer := r.TLS.PeerCertificates[0]
	subject := peer.Subject.String()
	expected := ""
	if scheme.MTLS != nil {
		expected = strings.TrimSpace(scheme.MTLS.SubjectAltName)
	}
	if expected != "" {
		if !certificateHasSubjectAltName(peer, expected) {
			return nil, newWebhookRequestError(http.StatusUnauthorized, "client certificate subject alt name mismatch", nil)
		}
		subject = expected
	}
	return &verifiedWebhookSender{
		Scheme:  schemeName,
		Subject: subject,
		Claims: map[string]string{
			"scheme": schemeName,
		},
	}, nil
}

func resolveWebhookSharedSecret(ref *providermanifestv1.WebhookSecretRef) (string, error) {
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

func verifyWebhookReplayWindow(r *http.Request, scheme *providermanifestv1.WebhookSecurityScheme) (time.Duration, error) {
	if scheme == nil || scheme.Replay == nil || strings.TrimSpace(scheme.Replay.MaxAge) == "" {
		return 24 * time.Hour, nil
	}
	maxAge, err := time.ParseDuration(strings.TrimSpace(scheme.Replay.MaxAge))
	if err != nil {
		return 0, newWebhookRequestError(http.StatusInternalServerError, "invalid replay configuration", err)
	}
	timestampHeader := ""
	if scheme.Signature != nil {
		timestampHeader = strings.TrimSpace(scheme.Signature.TimestampHeader)
	}
	if timestampHeader == "" {
		return maxAge, nil
	}
	raw := strings.TrimSpace(r.Header.Get(timestampHeader))
	if raw == "" {
		return 0, newWebhookRequestError(http.StatusUnauthorized, "missing webhook timestamp", nil)
	}
	timestamp, err := parseWebhookTimestamp(raw)
	if err != nil {
		return 0, newWebhookRequestError(http.StatusUnauthorized, "invalid webhook timestamp", err)
	}
	now := time.Now()
	if now.Sub(timestamp) > maxAge || timestamp.Sub(now) > maxAge {
		return 0, newWebhookRequestError(http.StatusUnauthorized, "stale webhook timestamp", nil)
	}
	return maxAge, nil
}

func parseWebhookTimestamp(raw string) (time.Time, error) {
	if unixSeconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(unixSeconds, 0).UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format %q", raw)
}

func renderWebhookPayloadTemplate(template string, r *http.Request, rawBody []byte) string {
	template = strings.TrimSpace(template)
	if template == "" {
		return ""
	}
	rendered := strings.ReplaceAll(template, "{raw_body}", string(rawBody))
	return webhookHeaderTemplatePattern.ReplaceAllStringFunc(rendered, func(match string) string {
		groups := webhookHeaderTemplatePattern.FindStringSubmatch(match)
		if len(groups) != 2 {
			return ""
		}
		return r.Header.Get(strings.TrimSpace(groups[1]))
	})
}

func certificateHasSubjectAltName(cert *x509.Certificate, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" || cert == nil {
		return false
	}
	for _, value := range cert.DNSNames {
		if subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 {
			return true
		}
	}
	for _, value := range cert.EmailAddresses {
		if subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 {
			return true
		}
	}
	for _, value := range cert.URIs {
		if value != nil && subtle.ConstantTimeCompare([]byte(value.String()), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}
