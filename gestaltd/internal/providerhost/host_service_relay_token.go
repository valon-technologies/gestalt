package providerhost

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	HostServiceRelayTokenHeader     = "x-gestalt-host-service-relay-token"
	hostServiceRelayTokenIssuer     = "gestaltd"
	hostServiceRelayTokenAudience   = "gestalt-host-service-relay"
	defaultHostServiceRelayTokenTTL = 24 * time.Hour
	maxHostServiceRelayTokenTTL     = 30 * 24 * time.Hour
)

type HostServiceRelayTokenManager struct {
	secret     []byte
	now        func() time.Time
	defaultTTL time.Duration
	maxTTL     time.Duration
}

type HostServiceRelayTokenRequest struct {
	PluginName   string
	SessionID    string
	Service      string
	EnvVar       string
	MethodPrefix string
	TTL          time.Duration
}

type HostServiceRelayTarget struct {
	PluginName   string
	SessionID    string
	Service      string
	EnvVar       string
	MethodPrefix string
}

type hostServiceRelayTokenClaims struct {
	jwt.RegisteredClaims
	PluginName   string `json:"plugin,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Service      string `json:"service,omitempty"`
	EnvVar       string `json:"env_var,omitempty"`
	MethodPrefix string `json:"method_prefix,omitempty"`
}

func NewHostServiceRelayTokenManager(secret []byte) (*HostServiceRelayTokenManager, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate host service relay token secret: %w", err)
		}
	}
	return &HostServiceRelayTokenManager{
		secret:     append([]byte(nil), secret...),
		now:        time.Now,
		defaultTTL: defaultHostServiceRelayTokenTTL,
		maxTTL:     maxHostServiceRelayTokenTTL,
	}, nil
}

func (m *HostServiceRelayTokenManager) MintToken(req HostServiceRelayTokenRequest) (string, error) {
	if m == nil {
		return "", fmt.Errorf("host service relay tokens are not available")
	}
	service, envVar, methodPrefix, err := normalizeHostServiceRelayTarget(req.Service, req.EnvVar, req.MethodPrefix)
	if err != nil {
		return "", err
	}

	now := m.now()
	expiresAt := now.Add(m.tokenTTL(req.TTL))
	subject := strings.TrimSpace(req.Service)
	if subject == "" {
		subject = strings.TrimSpace(req.PluginName)
	}
	if subject == "" {
		subject = "host-service-relay"
	}

	return m.signClaims(&hostServiceRelayTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    hostServiceRelayTokenIssuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{hostServiceRelayTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		PluginName:   strings.TrimSpace(req.PluginName),
		SessionID:    strings.TrimSpace(req.SessionID),
		Service:      service,
		EnvVar:       envVar,
		MethodPrefix: methodPrefix,
	})
}

func (m *HostServiceRelayTokenManager) ResolveToken(token string) (HostServiceRelayTarget, error) {
	if m == nil {
		return HostServiceRelayTarget{}, fmt.Errorf("host service relay tokens are not available")
	}
	claims, err := m.parseClaims(token)
	if err != nil {
		return HostServiceRelayTarget{}, err
	}
	service, envVar, methodPrefix, err := normalizeHostServiceRelayTarget(claims.Service, claims.EnvVar, claims.MethodPrefix)
	if err != nil {
		return HostServiceRelayTarget{}, fmt.Errorf("host service relay token is invalid or expired")
	}
	return HostServiceRelayTarget{
		PluginName:   strings.TrimSpace(claims.PluginName),
		SessionID:    strings.TrimSpace(claims.SessionID),
		Service:      service,
		EnvVar:       envVar,
		MethodPrefix: methodPrefix,
	}, nil
}

func (m *HostServiceRelayTokenManager) parseClaims(token string) (*hostServiceRelayTokenClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("host service relay token is required")
	}
	parsed, err := jwt.ParseWithClaims(token, &hostServiceRelayTokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithAudience(hostServiceRelayTokenAudience), jwt.WithIssuer(hostServiceRelayTokenIssuer), jwt.WithTimeFunc(m.now))
	if err != nil {
		return nil, fmt.Errorf("host service relay token is invalid or expired")
	}
	claims, ok := parsed.Claims.(*hostServiceRelayTokenClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("host service relay token is invalid or expired")
	}
	return claims, nil
}

func (m *HostServiceRelayTokenManager) signClaims(claims *hostServiceRelayTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *HostServiceRelayTokenManager) tokenTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return m.defaultTTL
	}
	if ttl > m.maxTTL {
		return m.maxTTL
	}
	return ttl
}

func normalizeHostServiceRelayTarget(service, envVar, methodPrefix string) (string, string, string, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", "", "", fmt.Errorf("host service relay service is required")
	}
	envVar = strings.TrimSpace(envVar)
	if envVar == "" {
		return "", "", "", fmt.Errorf("host service relay env var is required")
	}
	methodPrefix = strings.TrimSpace(methodPrefix)
	if methodPrefix == "" {
		return "", "", "", fmt.Errorf("host service relay method prefix is required")
	}
	if !strings.HasPrefix(methodPrefix, "/") {
		methodPrefix = "/" + methodPrefix
	}
	return service, envVar, methodPrefix, nil
}
