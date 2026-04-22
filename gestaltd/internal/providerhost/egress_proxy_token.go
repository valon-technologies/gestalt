package providerhost

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
	egressProxyTokenIssuer     = "gestaltd"
	egressProxyTokenAudience   = "gestalt-egress-proxy"
	defaultEgressProxyTokenTTL = 24 * time.Hour
	maxEgressProxyTokenTTL     = 30 * 24 * time.Hour
)

type EgressProxyTokenManager struct {
	secret     []byte
	now        func() time.Time
	defaultTTL time.Duration
	maxTTL     time.Duration
}

type EgressProxyTokenRequest struct {
	PluginName    string
	SessionID     string
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
	TTL           time.Duration
}

type EgressProxyTarget struct {
	PluginName    string
	SessionID     string
	AllowedHosts  []string
	DefaultAction egress.PolicyAction
}

type egressProxyTokenClaims struct {
	jwt.RegisteredClaims
	PluginName    string              `json:"plugin,omitempty"`
	SessionID     string              `json:"session_id,omitempty"`
	AllowedHosts  []string            `json:"allowed_hosts,omitempty"`
	DefaultAction egress.PolicyAction `json:"default_action,omitempty"`
}

func NewEgressProxyTokenManager(secret []byte) (*EgressProxyTokenManager, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate egress proxy token secret: %w", err)
		}
	}
	return &EgressProxyTokenManager{
		secret:     append([]byte(nil), secret...),
		now:        time.Now,
		defaultTTL: defaultEgressProxyTokenTTL,
		maxTTL:     maxEgressProxyTokenTTL,
	}, nil
}

func (m *EgressProxyTokenManager) MintToken(req EgressProxyTokenRequest) (string, error) {
	if m == nil {
		return "", fmt.Errorf("egress proxy tokens are not available")
	}

	now := m.now()
	expiresAt := now.Add(m.tokenTTL(req.TTL))
	subject := strings.TrimSpace(req.PluginName)
	if subject == "" {
		subject = "egress-proxy"
	}

	return m.signClaims(&egressProxyTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    egressProxyTokenIssuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{egressProxyTokenAudience},
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

func (m *EgressProxyTokenManager) ResolveToken(token string) (EgressProxyTarget, error) {
	if m == nil {
		return EgressProxyTarget{}, fmt.Errorf("egress proxy tokens are not available")
	}
	claims, err := m.parseClaims(token)
	if err != nil {
		return EgressProxyTarget{}, err
	}
	return EgressProxyTarget{
		PluginName:    strings.TrimSpace(claims.PluginName),
		SessionID:     strings.TrimSpace(claims.SessionID),
		AllowedHosts:  normalizeAllowedHosts(claims.AllowedHosts),
		DefaultAction: normalizeDefaultAction(claims.DefaultAction),
	}, nil
}

func (m *EgressProxyTokenManager) parseClaims(token string) (*egressProxyTokenClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("egress proxy token is required")
	}
	parsed, err := jwt.ParseWithClaims(token, &egressProxyTokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithAudience(egressProxyTokenAudience), jwt.WithIssuer(egressProxyTokenIssuer), jwt.WithTimeFunc(m.now))
	if err != nil {
		return nil, fmt.Errorf("egress proxy token is invalid or expired")
	}
	claims, ok := parsed.Claims.(*egressProxyTokenClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("egress proxy token is invalid or expired")
	}
	return claims, nil
}

func (m *EgressProxyTokenManager) signClaims(claims *egressProxyTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *EgressProxyTokenManager) tokenTTL(ttl time.Duration) time.Duration {
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
