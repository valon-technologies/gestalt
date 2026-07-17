package runtimehost

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
	// DefaultInvocationCapabilityTTL scopes short-lived invocation capabilities.
	DefaultInvocationCapabilityTTL = 5 * time.Minute
)

// PrincipalClaims carries verified caller identity inside a host-service capability.
// These are private signed-token claims, not protobuf or public SDK fields.
type PrincipalClaims struct {
	SubjectID string
	Scopes    []string
	ClientID  string
	Audience  []string
}

type HostServiceRelayTokenManager struct {
	secret          []byte
	now             func() time.Time
	defaultTTL      time.Duration
	maxTTL          time.Duration
	decorateContext CapabilityIngressDecorator
}

type HostServiceRelayTokenRequest struct {
	// Routing authority.
	AppName      string
	SessionID    string
	Service      string
	MethodPrefix string

	Caller *PrincipalClaims

	TTL time.Duration
}

type HostServiceRelayTarget struct {
	AppName      string
	SessionID    string
	Service      string
	MethodPrefix string
	Caller       *PrincipalClaims
}

type hostServiceRelayTokenClaims struct {
	jwt.RegisteredClaims
	AppName      string           `json:"app,omitempty"`
	SessionID    string           `json:"session_id,omitempty"`
	Service      string           `json:"service,omitempty"`
	MethodPrefix string           `json:"method_prefix,omitempty"`
	Caller       *PrincipalClaims `json:"caller,omitempty"`
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
	service, methodPrefix, err := normalizeHostServiceRelayTarget(req.Service, req.MethodPrefix)
	if err != nil {
		return "", err
	}

	now := m.now()
	expiresAt := now.Add(m.tokenTTL(req))
	subject := strings.TrimSpace(req.Service)
	if subject == "" {
		subject = strings.TrimSpace(req.AppName)
	}
	if subject == "" {
		subject = "host-service-relay"
	}

	var caller *PrincipalClaims
	if req.Caller != nil {
		caller = &PrincipalClaims{
			SubjectID: strings.TrimSpace(req.Caller.SubjectID),
			ClientID:  strings.TrimSpace(req.Caller.ClientID),
			Scopes:    append([]string(nil), req.Caller.Scopes...),
			Audience:  append([]string(nil), req.Caller.Audience...),
		}
		if caller.SubjectID == "" {
			caller = nil
		}
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
		AppName:      strings.TrimSpace(req.AppName),
		SessionID:    strings.TrimSpace(req.SessionID),
		Service:      service,
		MethodPrefix: methodPrefix,
		Caller:       caller,
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
	service, methodPrefix, err := normalizeHostServiceRelayTarget(claims.Service, claims.MethodPrefix)
	if err != nil {
		return HostServiceRelayTarget{}, fmt.Errorf("host service relay token is invalid or expired")
	}
	return HostServiceRelayTarget{
		AppName:      strings.TrimSpace(claims.AppName),
		SessionID:    strings.TrimSpace(claims.SessionID),
		Service:      service,
		MethodPrefix: methodPrefix,
		Caller:       claims.Caller,
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

func (m *HostServiceRelayTokenManager) tokenTTL(req HostServiceRelayTokenRequest) time.Duration {
	if req.TTL > 0 {
		ttl := req.TTL
		if ttl > m.maxTTL {
			return m.maxTTL
		}
		return ttl
	}
	if req.Caller != nil {
		return DefaultInvocationCapabilityTTL
	}
	return m.defaultTTL
}

func normalizeHostServiceRelayTarget(service, methodPrefix string) (string, string, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", "", fmt.Errorf("host service relay service is required")
	}
	methodPrefix = strings.TrimSpace(methodPrefix)
	if methodPrefix == "" {
		return "", "", fmt.Errorf("host service relay method prefix is required")
	}
	if !strings.HasPrefix(methodPrefix, "/") {
		methodPrefix = "/" + methodPrefix
	}
	return service, methodPrefix, nil
}
