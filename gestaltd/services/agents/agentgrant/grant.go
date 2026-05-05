package agentgrant

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

const (
	issuer                      = "gestaltd"
	audience                    = "gestalt-agent-host"
	runGrantTTL                 = 24 * time.Hour
	revokedTurnRetention        = 24 * time.Hour
	maxRevokedTurnTombstoneKeys = 20_000
)

type ConnectionBinding struct {
	Connection string `json:"connection"`
}

type Manager struct {
	mu      sync.Mutex
	secret  []byte
	now     func() time.Time
	revoked map[string]time.Time
}

type Grant struct {
	ID                  string
	ProviderName        string
	SessionID           string
	TurnID              string
	SubjectID           string
	SubjectKind         string
	CredentialSubjectID string
	DisplayName         string
	AuthSource          string
	Permissions         []core.AccessPermission
	ToolRefs            []coreagent.ToolRef
	Tools               []coreagent.Tool
	ToolSource          coreagent.ToolSourceMode
	Connections         []ConnectionBinding
}

type claims struct {
	jwt.RegisteredClaims
	ProviderName        string                  `json:"provider_name,omitempty"`
	SessionID           string                  `json:"session_id,omitempty"`
	TurnID              string                  `json:"turn_id,omitempty"`
	SubjectKind         string                  `json:"subject_kind,omitempty"`
	CredentialSubjectID string                  `json:"credential_subject_id,omitempty"`
	DisplayName         string                  `json:"display_name,omitempty"`
	AuthSource          string                  `json:"auth_source,omitempty"`
	Permissions         []core.AccessPermission `json:"permissions,omitempty"`
	ToolScope           string                  `json:"tool_scope,omitempty"`
}

type toolScope struct {
	ToolRefs    []coreagent.ToolRef      `json:"tool_refs,omitempty"`
	Tools       []coreagent.Tool         `json:"tools,omitempty"`
	ToolSource  coreagent.ToolSourceMode `json:"tool_source,omitempty"`
	Connections []ConnectionBinding      `json:"connections,omitempty"`
}

func NewManager(secret []byte) (*Manager, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate agent run grant secret: %w", err)
		}
	}
	return &Manager{
		secret:  append([]byte(nil), secret...),
		now:     time.Now,
		revoked: map[string]time.Time{},
	}, nil
}

func (m *Manager) Mint(grant Grant) (string, error) {
	if m == nil {
		return "", fmt.Errorf("agent run grants are not available")
	}
	now := m.now()
	grant.ID = strings.TrimSpace(grant.ID)
	if grant.ID == "" {
		grant.ID = uuid.NewString()
	}
	encoded := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        grant.ID,
			Issuer:    issuer,
			Subject:   strings.TrimSpace(grant.SubjectID),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(runGrantTTL)),
		},
		ProviderName:        strings.TrimSpace(grant.ProviderName),
		SessionID:           strings.TrimSpace(grant.SessionID),
		TurnID:              strings.TrimSpace(grant.TurnID),
		SubjectKind:         strings.TrimSpace(grant.SubjectKind),
		CredentialSubjectID: strings.TrimSpace(grant.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(grant.DisplayName),
		AuthSource:          strings.TrimSpace(grant.AuthSource),
		Permissions:         append([]core.AccessPermission(nil), grant.Permissions...),
	}
	scope, err := m.sealValue(sealPurposeToolScope, toolScope{
		ToolRefs:    append([]coreagent.ToolRef(nil), grant.ToolRefs...),
		Tools:       append([]coreagent.Tool(nil), grant.Tools...),
		ToolSource:  grant.ToolSource,
		Connections: append([]ConnectionBinding(nil), grant.Connections...),
	})
	if err != nil {
		return "", err
	}
	encoded.ToolScope = scope
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &encoded)
	return token.SignedString(m.secret)
}

func (m *Manager) Resolve(token string) (Grant, error) {
	if m == nil {
		return Grant{}, fmt.Errorf("agent run grants are not available")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Grant{}, fmt.Errorf("agent run grant is required")
	}
	parsed, err := jwt.ParseWithClaims(token, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithAudience(audience), jwt.WithIssuer(issuer), jwt.WithTimeFunc(m.now))
	if err != nil {
		return Grant{}, fmt.Errorf("agent run grant is invalid or expired")
	}
	decoded, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid {
		return Grant{}, fmt.Errorf("agent run grant is invalid or expired")
	}
	var scope toolScope
	if err := m.openValueAny([]string{sealPurposeToolScope, legacySealPurposeToolScope}, strings.TrimSpace(decoded.ToolScope), &scope); err != nil {
		return Grant{}, fmt.Errorf("agent run grant is invalid or expired")
	}
	grant := Grant{
		ID:                  strings.TrimSpace(decoded.ID),
		ProviderName:        strings.TrimSpace(decoded.ProviderName),
		SessionID:           strings.TrimSpace(decoded.SessionID),
		TurnID:              strings.TrimSpace(decoded.TurnID),
		SubjectID:           strings.TrimSpace(decoded.Subject),
		SubjectKind:         strings.TrimSpace(decoded.SubjectKind),
		CredentialSubjectID: strings.TrimSpace(decoded.CredentialSubjectID),
		DisplayName:         strings.TrimSpace(decoded.DisplayName),
		AuthSource:          strings.TrimSpace(decoded.AuthSource),
		Permissions:         append([]core.AccessPermission(nil), decoded.Permissions...),
		ToolRefs:            append([]coreagent.ToolRef(nil), scope.ToolRefs...),
		Tools:               append([]coreagent.Tool(nil), scope.Tools...),
		ToolSource:          scope.ToolSource,
		Connections:         append([]ConnectionBinding(nil), scope.Connections...),
	}
	if m.revokedTurn(grant) {
		return Grant{}, fmt.Errorf("agent run grant is revoked")
	}
	return grant, nil
}

func (m *Manager) RevokeTurn(providerName, sessionID, turnID string) {
	if m == nil {
		return
	}
	turnKey := revokeTurnKey(providerName, turnID)
	exactKey := revokeKey(providerName, sessionID, turnID)
	if turnKey == "" && exactKey == "" {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if turnKey != "" {
		m.revoked[turnKey] = now
	}
	if exactKey != "" {
		m.revoked[exactKey] = now
	}
	m.pruneRevokedLocked(now)
}

func (m *Manager) revokedTurn(grant Grant) bool {
	turnKey := revokeTurnKey(grant.ProviderName, grant.TurnID)
	exactKey := revokeKey(grant.ProviderName, grant.SessionID, grant.TurnID)
	if turnKey == "" && exactKey == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.revokedKeyLocked(now, turnKey) {
		return true
	}
	return m.revokedKeyLocked(now, exactKey)
}

func (m *Manager) revokedKeyLocked(now time.Time, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	revokedAt, ok := m.revoked[key]
	if !ok {
		return false
	}
	if revokedTurnExpired(now, revokedAt) {
		delete(m.revoked, key)
		return false
	}
	return true
}

func (m *Manager) pruneRevokedLocked(now time.Time) {
	for key, revokedAt := range m.revoked {
		if revokedTurnExpired(now, revokedAt) {
			delete(m.revoked, key)
		}
	}
	overflow := len(m.revoked) - maxRevokedTurnTombstoneKeys
	if overflow <= 0 {
		return
	}
	type revokedEntry struct {
		key       string
		revokedAt time.Time
	}
	entries := make([]revokedEntry, 0, len(m.revoked))
	for key, revokedAt := range m.revoked {
		entries = append(entries, revokedEntry{key: key, revokedAt: revokedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].revokedAt.Before(entries[j].revokedAt)
	})
	for i := 0; i < overflow; i++ {
		delete(m.revoked, entries[i].key)
	}
}

func revokedTurnExpired(now, revokedAt time.Time) bool {
	if revokedAt.IsZero() || now.IsZero() || revokedAt.After(now) {
		return false
	}
	return now.Sub(revokedAt) > revokedTurnRetention
}

func revokeKey(providerName, sessionID, turnID string) string {
	providerName = strings.TrimSpace(providerName)
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if providerName == "" || sessionID == "" || turnID == "" {
		return ""
	}
	return providerName + "\x00" + sessionID + "\x00" + turnID
}

func revokeTurnKey(providerName, turnID string) string {
	providerName = strings.TrimSpace(providerName)
	turnID = strings.TrimSpace(turnID)
	if providerName == "" || turnID == "" {
		return ""
	}
	return providerName + "\x00" + turnID
}
