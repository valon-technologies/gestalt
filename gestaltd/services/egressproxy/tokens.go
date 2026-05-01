// Package egressproxy exposes signed egress proxy token primitives.
package egressproxy

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/internal/egress"
)

const (
	tokenIssuer     = "gestaltd"
	tokenAudience   = "gestalt-egress-proxy"
	defaultTokenTTL = 24 * time.Hour
	maxTokenTTL     = 30 * 24 * time.Hour
)

type TokenManager struct {
	secret     []byte
	now        func() time.Time
	defaultTTL time.Duration
	maxTTL     time.Duration
}

type TokenRequest struct {
	PluginName    string
	SessionID     string
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
	TTL           time.Duration
}

type Target struct {
	PluginName    string
	SessionID     string
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
}

type tokenClaims struct {
	jwt.RegisteredClaims
	PluginName    string              `json:"plugin,omitempty"`
	SessionID     string              `json:"session_id,omitempty"`
	AllowedHosts  []string            `json:"allowed_hosts,omitempty"`
	DefaultAction egress.PolicyAction `json:"default_action,omitempty"`
}

func NewTokenManager(secret []byte) (*TokenManager, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate egress proxy token secret: %w", err)
		}
	}
	return &TokenManager{
		secret:     append([]byte(nil), secret...),
		now:        time.Now,
		defaultTTL: defaultTokenTTL,
		maxTTL:     maxTokenTTL,
	}, nil
}

func (m *TokenManager) MintToken(req TokenRequest) (string, error) {
	if m == nil {
		return "", fmt.Errorf("egress proxy tokens are not available")
	}

	now := m.now()
	expiresAt := now.Add(m.tokenTTL(req.TTL))
	subject := strings.TrimSpace(req.PluginName)
	if subject == "" {
		subject = "egress-proxy"
	}

	return m.signClaims(&tokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    tokenIssuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		PluginName:    strings.TrimSpace(req.PluginName),
		SessionID:     strings.TrimSpace(req.SessionID),
		AllowedHosts:  normalizeAllowedHosts(req.AllowedHosts),
		DefaultAction: normalizeDefaultAction(req.DefaultAction),
	})
}

func (m *TokenManager) ResolveToken(token string) (Target, error) {
	if m == nil {
		return Target{}, fmt.Errorf("egress proxy tokens are not available")
	}
	claims, err := m.parseClaims(token)
	if err != nil {
		return Target{}, err
	}
	return Target{
		PluginName:    strings.TrimSpace(claims.PluginName),
		SessionID:     strings.TrimSpace(claims.SessionID),
		AllowedHosts:  normalizeAllowedHosts(claims.AllowedHosts),
		DefaultAction: normalizeDefaultAction(claims.DefaultAction),
	}, nil
}

func (m *TokenManager) parseClaims(token string) (*tokenClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("egress proxy token is required")
	}
	parsed, err := jwt.ParseWithClaims(token, &tokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithAudience(tokenAudience), jwt.WithIssuer(tokenIssuer), jwt.WithTimeFunc(m.now))
	if err != nil {
		return nil, fmt.Errorf("egress proxy token is invalid or expired")
	}
	claims, ok := parsed.Claims.(*tokenClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("egress proxy token is invalid or expired")
	}
	return claims, nil
}

func (m *TokenManager) signClaims(claims *tokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *TokenManager) tokenTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return m.defaultTTL
	}
	if ttl > m.maxTTL {
		return m.maxTTL
	}
	return ttl
}

func normalizeAllowedHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		out = append(out, host)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeDefaultAction(action egress.PolicyAction) egress.PolicyAction {
	if action == "" {
		return egress.PolicyAllow
	}
	return action
}
