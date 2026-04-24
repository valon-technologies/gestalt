package agentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

var (
	ErrAgentNotConfigured                = errors.New("agent is not configured")
	ErrAgentProviderRequired             = errors.New("agent provider is required")
	ErrAgentProviderNotAvailable         = errors.New("agent provider is not available")
	ErrAgentSessionMetadataNotConfigured = errors.New("agent session metadata is not configured")
	ErrAgentTurnMetadataNotConfigured    = errors.New("agent turn metadata is not configured")
	ErrAgentSubjectRequired              = errors.New("agent subject is required")
	ErrAgentCallerPluginRequired         = errors.New("agent caller plugin is required for inherited tools")
	ErrAgentInheritedSurfaceTool         = errors.New("agent inherited surface tools are not supported")
	ErrAgentSessionCreationInProgress    = errors.New("agent session creation is already in progress for this idempotency key")
	ErrAgentTurnCreationInProgress       = errors.New("agent turn creation is already in progress for this idempotency key")
	ErrAgentInteractionRequired          = errors.New("agent interaction is required")
	ErrAgentInteractionNotFound          = errors.New("agent interaction is not found")
)

type AgentProviderNotAvailableError struct {
	Name string
}

func (e AgentProviderNotAvailableError) Error() string {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return ErrAgentProviderNotAvailable.Error()
	}
	return fmt.Sprintf("agent provider %q is not available", name)
}

func (e AgentProviderNotAvailableError) Unwrap() error {
	return ErrAgentProviderNotAvailable
}

func NewAgentProviderNotAvailableError(name string) error {
	return AgentProviderNotAvailableError{Name: name}
}

type AgentControl interface {
	ResolveProvider(name string) (coreagent.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreagent.Provider, err error)
	ProviderNames() []string
}

type Service interface {
	Available() bool
	CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error)
	GetSession(ctx context.Context, p *principal.Principal, sessionID string) (*coreagent.Session, error)
	ListSessions(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.Session, error)
	UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error)
	CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error)
	GetTurn(ctx context.Context, p *principal.Principal, turnID string) (*coreagent.Turn, error)
	ListTurns(ctx context.Context, p *principal.Principal, sessionID string) ([]*coreagent.Turn, error)
	CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (*coreagent.Turn, error)
	ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) ([]*coreagent.TurnEvent, error)
	ListInteractions(ctx context.Context, p *principal.Principal, turnID string) ([]*coreagent.Interaction, error)
	ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (*coreagent.Interaction, error)
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Agent             AgentControl
	SessionMetadata   *coredata.AgentSessionMetadataService
	RunMetadata       *coredata.AgentRunMetadataService
	Tokens            *coredata.TokenService
	Invoker           invocation.Invoker
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]config.PluginInvocationDependency
	Now               func() time.Time
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	agent             AgentControl
	sessionMetadata   *coredata.AgentSessionMetadataService
	runMetadata       *coredata.AgentRunMetadataService
	tokens            *coredata.TokenService
	invoker           invocation.Invoker
	authorizer        authorization.RuntimeAuthorizer
	defaultConnection map[string]string
	catalogConnection map[string]string
	pluginInvokes     map[string][]config.PluginInvocationDependency
	now               func() time.Time
}

func New(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pluginInvokes := make(map[string][]config.PluginInvocationDependency, len(cfg.PluginInvokes))
	for pluginName, deps := range cfg.PluginInvokes {
		pluginInvokes[pluginName] = append([]config.PluginInvocationDependency(nil), deps...)
	}
	return &Manager{
		providers:         cfg.Providers,
		agent:             cfg.Agent,
		sessionMetadata:   cfg.SessionMetadata,
		runMetadata:       cfg.RunMetadata,
		tokens:            cfg.Tokens,
		invoker:           cfg.Invoker,
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     pluginInvokes,
		now:               now,
	}
}

func (m *Manager) Available() bool {
	if m == nil || m.agent == nil {
		return false
	}
	return len(m.agent.ProviderNames()) > 0
}

func (m *Manager) CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	p = principal.Canonicalized(p)
	if m == nil || m.sessionMetadata == nil {
		return nil, ErrAgentSessionMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	providerName, provider, err := m.resolveProviderSelection(req.ProviderName)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	sessionID := uuid.NewString()
	if idempotencyKey != "" {
		for {
			claimedSessionID, claimed, err := m.sessionMetadata.ClaimIdempotency(ctx, subjectID, providerName, idempotencyKey, sessionID, m.now())
			if err != nil {
				return nil, err
			}
			if claimed {
				break
			}
			existing, err := m.sessionMetadata.Get(ctx, claimedSessionID)
			if err != nil {
				if errors.Is(err, indexeddb.ErrNotFound) {
					return nil, ErrAgentSessionCreationInProgress
				}
				return nil, err
			}
			if !sessionRefOwnedBy(existing, p) {
				return nil, core.ErrNotFound
			}
			session, err := m.getSessionByMetadata(ctx, p, existing)
			if err == nil {
				return session, nil
			}
			if !errors.Is(err, core.ErrNotFound) {
				return nil, err
			}
			if deleteErr := m.sessionMetadata.Delete(ctx, existing.ID); deleteErr != nil {
				return nil, deleteErr
			}
			sessionID = uuid.NewString()
		}
	}
	session, err := provider.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:       sessionID,
		IdempotencyKey:  idempotencyKey,
		Model:           strings.TrimSpace(req.Model),
		ClientRef:       strings.TrimSpace(req.ClientRef),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
		CreatedBy:       agentActorFromPrincipal(p),
	})
	if err != nil {
		fallback, getErr := provider.GetSession(ctx, coreagent.GetSessionRequest{SessionID: sessionID})
		if getErr != nil {
			if idempotencyKey != "" {
				_ = m.sessionMetadata.ReleaseIdempotency(ctx, subjectID, providerName, idempotencyKey)
			}
			return nil, err
		}
		session = fallback
	}
	normalized, err := normalizeProviderSession(providerName, sessionID, session)
	if err != nil {
		if idempotencyKey != "" {
			_ = m.sessionMetadata.ReleaseIdempotency(ctx, subjectID, providerName, idempotencyKey)
		}
		return nil, err
	}
	if _, err := m.sessionMetadata.Put(ctx, &coreagent.SessionReference{
		ID:                  normalized.ID,
		ProviderName:        providerName,
		SubjectID:           subjectID,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		IdempotencyKey:      idempotencyKey,
	}); err != nil {
		if idempotencyKey != "" {
			_ = m.sessionMetadata.ReleaseIdempotency(ctx, subjectID, providerName, idempotencyKey)
		}
		return nil, err
	}
	return normalized, nil
}

func (m *Manager) GetSession(ctx context.Context, p *principal.Principal, sessionID string) (*coreagent.Session, error) {
	ref, err := m.requireOwnedSessionMetadata(ctx, sessionID, p)
	if err != nil {
		return nil, err
	}
	return m.getSessionByMetadata(ctx, p, ref)
}

func (m *Manager) ListSessions(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.Session, error) {
	refs, err := m.listOwnedSessionMetadata(ctx, p)
	if err != nil {
		return nil, err
	}
	refsByProvider := sessionRefsByProvider(refs)
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		if providerRefs, ok := refsByProvider[providerName]; ok {
			refsByProvider = map[string][]*coreagent.SessionReference{providerName: providerRefs}
		} else {
			refsByProvider = map[string][]*coreagent.SessionReference{}
		}
	}
	out := make([]*coreagent.Session, 0, len(refs))
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		sessions, err := provider.ListSessions(ctx, coreagent.ListSessionsRequest{})
		if err != nil {
			return nil, err
		}
		refIndex := sessionRefsByID(providerRefs)
		for _, session := range sessions {
			if session == nil {
				continue
			}
			ref := refIndex[strings.TrimSpace(session.ID)]
			if ref == nil || !sessionRefOwnedBy(ref, p) {
				continue
			}
			normalized, err := normalizeProviderSession(providerName, ref.ID, session)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		leftTime := sessionSortTime(left)
		rightTime := sessionSortTime(right)
		if leftTime != nil && rightTime != nil && !leftTime.Equal(*rightTime) {
			return leftTime.After(*rightTime)
		}
		return left.ID < right.ID
	})
	return out, nil
}

func (m *Manager) UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	ref, err := m.requireOwnedSessionMetadata(ctx, req.SessionID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	session, err := provider.UpdateSession(ctx, coreagent.UpdateSessionRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		ClientRef: strings.TrimSpace(req.ClientRef),
		State:     req.State,
		Metadata:  maps.Clone(req.Metadata),
	})
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderSession(ref.ProviderName, ref.ID, session)
	if err != nil {
		return nil, err
	}
	ref.ArchivedAt = nil
	if normalized.State == coreagent.SessionStateArchived {
		now := m.now()
		ref.ArchivedAt = &now
	}
	if _, err := m.sessionMetadata.Put(ctx, ref); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (m *Manager) CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	p = principal.Canonicalized(p)
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentTurnMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	sessionRef, err := m.requireOwnedSessionMetadata(ctx, req.SessionID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(sessionRef.ProviderName)
	if err != nil {
		return nil, err
	}
	tools, err := m.resolveTools(ctx, p, req.CallerPluginName, req.ToolRefs, req.ToolSource)
	if err != nil {
		return nil, err
	}
	turnID := uuid.NewString()
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		for {
			claimedTurnID, claimed, err := m.runMetadata.ClaimIdempotency(ctx, subjectID, sessionRef.ProviderName, idempotencyKey, turnID, m.now())
			if err != nil {
				return nil, err
			}
			if claimed {
				break
			}
			existing, err := m.runMetadata.Get(ctx, claimedTurnID)
			if err != nil {
				if errors.Is(err, indexeddb.ErrNotFound) {
					return nil, ErrAgentTurnCreationInProgress
				}
				return nil, err
			}
			if !executionRefOwnedBy(existing, p) || existing.SessionID != sessionRef.ID {
				return nil, core.ErrNotFound
			}
			if !m.allowRun(ctx, p, existing) {
				return nil, core.ErrNotFound
			}
			turn, err := m.getTurnByMetadata(ctx, p, existing)
			if err == nil {
				return turn, nil
			}
			if !errors.Is(err, core.ErrNotFound) {
				return nil, err
			}
			if deleteErr := m.runMetadata.Delete(ctx, existing.ID); deleteErr != nil {
				return nil, deleteErr
			}
			turnID = uuid.NewString()
		}
	}
	ref, err := m.runMetadata.Put(ctx, &coreagent.ExecutionReference{
		ID:                  turnID,
		SessionID:           sessionRef.ID,
		ProviderName:        sessionRef.ProviderName,
		SubjectID:           subjectID,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		IdempotencyKey:      idempotencyKey,
		Permissions:         principal.PermissionsToAccessPermissions(p.TokenPermissions),
		Tools:               tools,
	})
	if err != nil {
		if idempotencyKey != "" {
			_ = m.runMetadata.ReleaseIdempotency(ctx, subjectID, sessionRef.ProviderName, idempotencyKey)
		}
		return nil, err
	}
	turn, err := provider.CreateTurn(ctx, coreagent.CreateTurnRequest{
		TurnID:          turnID,
		SessionID:       sessionRef.ID,
		IdempotencyKey:  idempotencyKey,
		Model:           strings.TrimSpace(req.Model),
		Messages:        append([]coreagent.Message(nil), req.Messages...),
		Tools:           append([]coreagent.Tool(nil), tools...),
		ResponseSchema:  maps.Clone(req.ResponseSchema),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
		CreatedBy:       agentActorFromPrincipal(p),
		ExecutionRef:    turnID,
	})
	if err != nil {
		fallback, getErr := provider.GetTurn(ctx, coreagent.GetTurnRequest{TurnID: turnID})
		if getErr == nil {
			turn = fallback
		} else {
			_ = m.runMetadata.Delete(ctx, ref.ID)
			return nil, err
		}
	}
	normalized, err := normalizeProviderTurn(sessionRef.ProviderName, sessionRef.ID, turnID, turn)
	if err != nil {
		_ = m.runMetadata.Delete(ctx, ref.ID)
		return nil, err
	}
	return normalized, nil
}

func (m *Manager) GetTurn(ctx context.Context, p *principal.Principal, turnID string) (*coreagent.Turn, error) {
	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	return m.getTurnByMetadata(ctx, p, ref)
}

func (m *Manager) ListTurns(ctx context.Context, p *principal.Principal, sessionID string) ([]*coreagent.Turn, error) {
	sessionRef, err := m.requireOwnedSessionMetadata(ctx, sessionID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(sessionRef.ProviderName)
	if err != nil {
		return nil, err
	}
	turns, err := provider.ListTurns(ctx, coreagent.ListTurnsRequest{SessionID: sessionRef.ID})
	if err != nil {
		return nil, err
	}
	ownedRefs, err := m.listOwnedTurnMetadata(ctx, p, sessionRef.ID, false)
	if err != nil {
		return nil, err
	}
	refIndex := executionRefsByID(ownedRefs)
	out := make([]*coreagent.Turn, 0, len(turns))
	for _, turn := range turns {
		if turn == nil {
			continue
		}
		ref := refIndex[strings.TrimSpace(turn.ID)]
		if ref == nil {
			continue
		}
		normalized, err := normalizeProviderTurn(sessionRef.ProviderName, sessionRef.ID, ref.ID, turn)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.CreatedAt != nil && right.CreatedAt != nil && !left.CreatedAt.Equal(*right.CreatedAt) {
			return left.CreatedAt.After(*right.CreatedAt)
		}
		return left.ID < right.ID
	})
	return out, nil
}

func (m *Manager) CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (*coreagent.Turn, error) {
	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	turn, err := provider.CancelTurn(ctx, coreagent.CancelTurnRequest{
		TurnID: strings.TrimSpace(turnID),
		Reason: strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, err
	}
	if _, err := m.runMetadata.Revoke(ctx, ref.ID, m.now()); err != nil {
		return nil, err
	}
	return normalizeProviderTurn(ref.ProviderName, ref.SessionID, ref.ID, turn)
}

func (m *Manager) ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) ([]*coreagent.TurnEvent, error) {
	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	return provider.ListTurnEvents(ctx, coreagent.ListTurnEventsRequest{
		TurnID:   ref.ID,
		AfterSeq: afterSeq,
		Limit:    limit,
	})
}

func (m *Manager) ListInteractions(ctx context.Context, p *principal.Principal, turnID string) ([]*coreagent.Interaction, error) {
	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	interactions, err := provider.ListInteractions(ctx, coreagent.ListInteractionsRequest{TurnID: ref.ID})
	if err != nil {
		return nil, err
	}
	out := make([]*coreagent.Interaction, 0, len(interactions))
	for _, interaction := range interactions {
		if interaction == nil {
			continue
		}
		if strings.TrimSpace(interaction.TurnID) != ref.ID {
			return nil, fmt.Errorf("agent provider returned interaction %q for turn %q, want %q", strings.TrimSpace(interaction.ID), strings.TrimSpace(interaction.TurnID), ref.ID)
		}
		if strings.TrimSpace(interaction.SessionID) != ref.SessionID {
			return nil, fmt.Errorf("agent provider returned interaction %q for session %q, want %q", strings.TrimSpace(interaction.ID), strings.TrimSpace(interaction.SessionID), ref.SessionID)
		}
		out = append(out, interaction)
	}
	return out, nil
}

func (m *Manager) ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (*coreagent.Interaction, error) {
	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return nil, ErrAgentInteractionRequired
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	interaction, err := provider.ResolveInteraction(ctx, coreagent.ResolveInteractionRequest{
		InteractionID: interactionID,
		Resolution:    maps.Clone(resolution),
	})
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, ErrAgentInteractionNotFound
		}
		return nil, err
	}
	if interaction == nil {
		return nil, ErrAgentInteractionNotFound
	}
	if gotInteractionID := strings.TrimSpace(interaction.ID); gotInteractionID == "" || gotInteractionID != interactionID {
		return nil, ErrAgentInteractionNotFound
	}
	if gotTurnID := strings.TrimSpace(interaction.TurnID); gotTurnID != "" && gotTurnID != ref.ID {
		return nil, core.ErrNotFound
	}
	if gotSessionID := strings.TrimSpace(interaction.SessionID); gotSessionID != "" && gotSessionID != ref.SessionID {
		return nil, core.ErrNotFound
	}
	if strings.TrimSpace(interaction.TurnID) == "" {
		return nil, fmt.Errorf("agent provider returned interaction %q without turn id", interactionID)
	}
	if strings.TrimSpace(interaction.SessionID) == "" {
		return nil, fmt.Errorf("agent provider returned interaction %q without session id", interactionID)
	}
	return interaction, nil
}

func (m *Manager) getSessionByMetadata(ctx context.Context, p *principal.Principal, ref *coreagent.SessionReference) (*coreagent.Session, error) {
	if ref == nil || !sessionRefOwnedBy(ref, p) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	session, err := provider.GetSession(ctx, coreagent.GetSessionRequest{SessionID: ref.ID})
	if err != nil {
		return nil, err
	}
	return normalizeProviderSession(ref.ProviderName, ref.ID, session)
}

func (m *Manager) getTurnByMetadata(ctx context.Context, p *principal.Principal, ref *coreagent.ExecutionReference) (*coreagent.Turn, error) {
	if ref == nil || !executionRefOwnedBy(ref, p) || !m.allowRun(ctx, p, ref) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	turn, err := provider.GetTurn(ctx, coreagent.GetTurnRequest{TurnID: strings.TrimSpace(ref.ID)})
	if err != nil {
		return nil, err
	}
	return normalizeProviderTurn(ref.ProviderName, ref.SessionID, ref.ID, turn)
}

func (m *Manager) resolveProviderSelection(providerName string) (string, coreagent.Provider, error) {
	if m == nil || m.agent == nil {
		return "", nil, ErrAgentNotConfigured
	}
	return m.agent.ResolveProviderSelection(strings.TrimSpace(providerName))
}

func (m *Manager) resolveProviderByName(providerName string) (coreagent.Provider, error) {
	if m == nil || m.agent == nil {
		return nil, ErrAgentNotConfigured
	}
	return m.agent.ResolveProvider(strings.TrimSpace(providerName))
}

func (m *Manager) listOwnedSessionMetadata(ctx context.Context, p *principal.Principal) ([]*coreagent.SessionReference, error) {
	if m == nil || m.sessionMetadata == nil {
		return nil, ErrAgentSessionMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	refs, err := m.sessionMetadata.ListBySubject(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]*coreagent.SessionReference, 0, len(refs))
	for _, ref := range refs {
		if !sessionRefOwnedBy(ref, p) {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) requireOwnedSessionMetadata(ctx context.Context, sessionID string, p *principal.Principal) (*coreagent.SessionReference, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	if m == nil || m.sessionMetadata == nil {
		return nil, ErrAgentSessionMetadataNotConfigured
	}
	ref, err := m.sessionMetadata.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, err
	}
	if !sessionRefOwnedBy(ref, p) {
		return nil, core.ErrNotFound
	}
	return ref, nil
}

func (m *Manager) listOwnedTurnMetadata(ctx context.Context, p *principal.Principal, sessionID string, activeOnly bool) ([]*coreagent.ExecutionReference, error) {
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentTurnMetadataNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	refs, err := m.runMetadata.ListBySubject(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	out := make([]*coreagent.ExecutionReference, 0, len(refs))
	for _, ref := range refs {
		if !executionRefOwnedBy(ref, p) || (activeOnly && !executionRefActive(ref)) {
			continue
		}
		if sessionID != "" && strings.TrimSpace(ref.SessionID) != sessionID {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) requireOwnedTurnMetadata(ctx context.Context, turnID string, p *principal.Principal) (*coreagent.ExecutionReference, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, core.ErrNotFound
	}
	if m == nil || m.runMetadata == nil {
		return nil, ErrAgentTurnMetadataNotConfigured
	}
	ref, err := m.runMetadata.Get(ctx, turnID)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, err
	}
	if !executionRefOwnedBy(ref, p) || !m.allowRun(ctx, p, ref) {
		return nil, core.ErrNotFound
	}
	return ref, nil
}

func (m *Manager) resolveTools(ctx context.Context, p *principal.Principal, callerPluginName string, explicit []coreagent.ToolRef, source coreagent.ToolSourceMode) ([]coreagent.Tool, error) {
	refs := make([]coreagent.ToolRef, 0, len(explicit)+4)
	refs = append(refs, explicit...)
	if source == coreagent.ToolSourceModeInheritInvokes {
		callerPluginName = strings.TrimSpace(callerPluginName)
		if callerPluginName == "" {
			return nil, ErrAgentCallerPluginRequired
		}
		for _, invoke := range m.pluginInvokes[callerPluginName] {
			if strings.TrimSpace(invoke.Surface) != "" {
				return nil, fmt.Errorf("%w: %s.%s", ErrAgentInheritedSurfaceTool, callerPluginName, invoke.Surface)
			}
			refs = append(refs, coreagent.ToolRef{
				PluginName: invoke.Plugin,
				Operation:  invoke.Operation,
			})
		}
	}
	explicitTools, err := m.resolveExplicitTools(ctx, p, refs)
	if err != nil {
		return nil, err
	}
	if source != coreagent.ToolSourceModeUnspecified || strings.TrimSpace(callerPluginName) != "" {
		return explicitTools, nil
	}
	defaultTools := m.resolveDefaultTools(ctx, p)
	if len(defaultTools) == 0 {
		return explicitTools, nil
	}
	return mergeTools(explicitTools, defaultTools), nil
}

func (m *Manager) resolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if m == nil || m.providers == nil {
		return coreagent.Tool{}, fmt.Errorf("%w: agent providers are not configured", invocation.ErrInternal)
	}
	pluginName := strings.TrimSpace(ref.PluginName)
	if pluginName == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool plugin is required", invocation.ErrProviderNotFound)
	}
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreagent.Tool{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return coreagent.Tool{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	operation := strings.TrimSpace(ref.Operation)
	if operation == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool operation is required", invocation.ErrOperationNotFound)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return coreagent.Tool{}, invocation.ErrAuthorizationDenied
	}

	connection := strings.TrimSpace(ref.Connection)
	if connection != "" && !config.SafeConnectionValue(connection) {
		return coreagent.Tool{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = config.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(ref.Instance)
	if instance != "" && !config.SafeInstanceValue(instance) {
		return coreagent.Tool{}, fmt.Errorf("instance name contains invalid characters")
	}
	if m.authorizer != nil && principal.IsWorkloadPrincipal(p) && (connection != "" || instance != "") {
		return coreagent.Tool{}, fmt.Errorf("%w: workloads may not override connection or instance bindings", invocation.ErrAuthorizationDenied)
	}

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, pluginName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	boundCredential := invocation.CredentialBindingResolution{}
	if bindingResolver, ok := m.invoker.(invocation.EffectiveCredentialBindingResolver); ok {
		boundCredential, err = bindingResolver.ResolveEffectiveCredentialBinding(p, pluginName, connection, instance)
		if err != nil {
			return coreagent.Tool{}, err
		}
	}
	boundConnections, sessionInstance := m.catalogSelectorConfig().BoundSessionCatalogConnections(pluginName, p, connection, instance)
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, pluginName, resolver, p, operation, boundConnections, sessionInstance)
	if err != nil {
		return coreagent.Tool{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := invocation.ResolveTokenForBinding(ctx, resolver, p, pluginName, connection, sessionInstance, boundCredential)
		if err != nil {
			return coreagent.Tool{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}

	parametersSchema, err := operationInputSchema(opMeta)
	if err != nil {
		return coreagent.Tool{}, err
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = strings.TrimSpace(opMeta.Title)
	}
	if name == "" {
		name = pluginName + "." + opMeta.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(opMeta.Description)
	}
	return coreagent.Tool{
		ID:               agentToolID(pluginName, opMeta.ID, connection, sessionInstance),
		Name:             name,
		Description:      description,
		ParametersSchema: parametersSchema,
		Target: coreagent.ToolTarget{
			PluginName: pluginName,
			Operation:  opMeta.ID,
			Connection: connection,
			Instance:   sessionInstance,
		},
	}, nil
}

func (m *Manager) resolveExplicitTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]coreagent.Tool, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		tool, err := m.resolveTool(ctx, p, ref)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[tool.ID]; ok {
			continue
		}
		seen[tool.ID] = struct{}{}
		out = append(out, tool)
	}
	return out, nil
}

func (m *Manager) resolveDefaultTools(ctx context.Context, p *principal.Principal) []coreagent.Tool {
	if m == nil || m.providers == nil {
		return nil
	}
	out := make([]coreagent.Tool, 0, 8)
	for _, providerName := range m.defaultToolProviders(ctx, p) {
		providerTools, err := m.resolveDefaultToolsForProvider(ctx, p, providerName)
		if err != nil {
			continue
		}
		out = append(out, providerTools...)
	}
	return out
}

func (m *Manager) defaultToolProviders(ctx context.Context, p *principal.Principal) []string {
	if m == nil || m.providers == nil {
		return nil
	}
	providerNames := m.providers.List()
	if len(providerNames) == 0 {
		return nil
	}

	out := make([]string, 0, len(providerNames))
	credentialAvailability := make(map[string]bool, len(providerNames))
	for _, providerName := range providerNames {
		if !m.allowProvider(ctx, p, providerName) {
			continue
		}
		prov, err := m.providers.Get(providerName)
		if err != nil {
			continue
		}
		if core.NormalizeConnectionMode(prov.ConnectionMode()) == core.ConnectionModeNone {
			out = append(out, providerName)
			continue
		}
		if !m.hasDefaultToolCredential(ctx, p, providerName, credentialAvailability) {
			continue
		}
		out = append(out, providerName)
	}
	return out
}

func (m *Manager) hasDefaultToolCredential(ctx context.Context, p *principal.Principal, providerName string, credentialAvailability map[string]bool) bool {
	if m == nil || m.tokens == nil {
		return true
	}

	bindingResolver, _ := m.invoker.(invocation.EffectiveCredentialBindingResolver)
	targets := m.catalogSelectorConfig().BoundSessionCatalogTargets(providerName, p, "", "")
	if len(targets) == 0 {
		targets = []invocation.CatalogResolutionTarget{{}}
	}
	for _, target := range targets {
		subjectID := m.defaultToolCredentialSubjectID(p, providerName)
		connection := strings.TrimSpace(target.Connection)
		instance := strings.TrimSpace(target.Instance)
		if bindingResolver != nil {
			boundCredential, err := bindingResolver.ResolveEffectiveCredentialBinding(p, providerName, connection, instance)
			if err != nil {
				continue
			}
			if subject := strings.TrimSpace(boundCredential.CredentialSubjectID); subject != "" {
				subjectID = subject
			}
			if conn := strings.TrimSpace(boundCredential.Connection); conn != "" {
				connection = conn
			}
			if inst := strings.TrimSpace(boundCredential.Instance); inst != "" {
				instance = inst
			}
		}
		if subjectID == "" {
			continue
		}

		cacheKey := subjectID + "\x00" + providerName + "\x00" + connection + "\x00" + instance
		if ok, found := credentialAvailability[cacheKey]; found {
			if ok {
				return true
			}
			continue
		}

		ok := m.hasStoredDefaultToolCredential(ctx, subjectID, providerName, connection, instance)
		credentialAvailability[cacheKey] = ok
		if ok {
			return true
		}
	}
	return false
}

func (m *Manager) defaultToolCredentialSubjectID(p *principal.Principal, providerName string) string {
	if m != nil && m.authorizer != nil {
		boundCredential, err := invocation.ResolveEffectiveCredentialBinding(m.authorizer, p, providerName, "", "")
		if err == nil && boundCredential.HasBinding {
			if subjectID := strings.TrimSpace(boundCredential.CredentialSubjectID); subjectID != "" {
				return subjectID
			}
		}
	}
	return principal.EffectiveCredentialSubjectID(p)
}

func (m *Manager) hasStoredDefaultToolCredential(ctx context.Context, subjectID, providerName, connection, instance string) bool {
	if m == nil || m.tokens == nil {
		return false
	}
	subjectID = strings.TrimSpace(subjectID)
	providerName = strings.TrimSpace(providerName)
	connection = strings.TrimSpace(connection)
	instance = strings.TrimSpace(instance)
	if subjectID == "" || providerName == "" {
		return false
	}
	if instance != "" {
		_, err := m.tokens.Token(ctx, subjectID, providerName, connection, instance)
		return err == nil
	}
	tokens, err := m.tokens.ListTokensForConnection(ctx, subjectID, providerName, connection)
	return err == nil && len(tokens) > 0
}

func (m *Manager) resolveDefaultToolsForProvider(ctx context.Context, p *principal.Principal, providerName string) ([]coreagent.Tool, error) {
	prov, err := m.providers.Get(providerName)
	if err != nil {
		return nil, err
	}
	connection, instance, cat, ok := m.resolveDefaultToolCatalog(ctx, p, providerName, prov)
	if !ok || cat == nil {
		return nil, nil
	}

	out := make([]coreagent.Tool, 0, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if !defaultToolAllowed(*op) {
			continue
		}
		if !m.allowOperation(ctx, p, providerName, op.ID) {
			continue
		}
		if !principal.AllowsOperationPermission(p, providerName, op.ID) {
			continue
		}
		if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, providerName, *op) {
			continue
		}

		tool, err := m.resolveTool(ctx, p, coreagent.ToolRef{
			PluginName: providerName,
			Operation:  op.ID,
			Connection: connection,
			Instance:   instance,
		})
		if err != nil {
			continue
		}
		out = append(out, tool)
	}
	return out, nil
}

func (m *Manager) resolveDefaultToolCatalog(ctx context.Context, p *principal.Principal, providerName string, prov core.Provider) (string, string, *catalog.Catalog, bool) {
	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, providerName))
	resolver, _ := m.invoker.(invocation.TokenResolver)
	bindingResolver, _ := m.invoker.(invocation.EffectiveCredentialBindingResolver)
	requiresCredential := core.NormalizeConnectionMode(prov.ConnectionMode()) != core.ConnectionModeNone
	targets := m.catalogSelectorConfig().BoundSessionCatalogTargets(providerName, p, "", "")
	if len(targets) == 0 {
		targets = []invocation.CatalogResolutionTarget{{}}
	}
	for _, target := range targets {
		connection := strings.TrimSpace(target.Connection)
		instance := strings.TrimSpace(target.Instance)
		boundCredential := invocation.CredentialBindingResolution{}
		if bindingResolver != nil {
			resolvedBinding, err := bindingResolver.ResolveEffectiveCredentialBinding(p, providerName, connection, instance)
			if err != nil {
				continue
			}
			boundCredential = resolvedBinding
		}

		catalogCtx := ctx
		if requiresCredential {
			if resolver == nil {
				continue
			}
			resolvedCtx, _, err := invocation.ResolveTokenForBinding(ctx, resolver, p, providerName, connection, instance, boundCredential)
			if err != nil {
				continue
			}
			cred := invocation.CredentialContextFromContext(resolvedCtx)
			if cred.Connection != "" {
				connection = cred.Connection
			}
			if cred.Instance != "" {
				instance = cred.Instance
			}
			catalogCtx = resolvedCtx
		}

		var cat *catalog.Catalog
		var err error
		if requiresCredential {
			cat, _, err = invocation.ResolveCatalogStrictWithMetadata(catalogCtx, prov, providerName, resolver, p, connection, instance)
		} else {
			cat, _, err = invocation.ResolveCatalogWithMetadata(catalogCtx, prov, providerName, resolver, p, connection, instance)
		}
		if err != nil || cat == nil {
			continue
		}
		return connection, instance, cat, true
	}
	return "", "", nil, false
}

func defaultToolAllowed(op catalog.CatalogOperation) bool {
	if op.Visible != nil && !*op.Visible {
		return false
	}
	if op.Annotations.DestructiveHint != nil && *op.Annotations.DestructiveHint {
		return false
	}
	if op.ReadOnly {
		return true
	}
	return op.Annotations.ReadOnlyHint != nil && *op.Annotations.ReadOnlyHint
}

func mergeTools(groups ...[]coreagent.Tool) []coreagent.Tool {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total == 0 {
		return nil
	}
	out := make([]coreagent.Tool, 0, total)
	seen := make(map[string]struct{}, total)
	for _, group := range groups {
		for _, tool := range group {
			if _, ok := seen[tool.ID]; ok {
				continue
			}
			seen[tool.ID] = struct{}{}
			out = append(out, tool)
		}
	}
	return out
}

func (m *Manager) allowProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowProvider(ctx, p, provider)
}

func (m *Manager) allowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	if m == nil || m.authorizer == nil {
		return true
	}
	return m.authorizer.AllowOperation(ctx, p, provider, operation)
}

func (m *Manager) providerAccessContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	if m == nil || m.authorizer == nil {
		return invocation.AccessContext{}
	}
	access, _ := m.authorizer.ResolveAccess(ctx, p, provider)
	return access
}

func (m *Manager) allowRun(ctx context.Context, p *principal.Principal, ref *coreagent.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	if !executionRefOwnedBy(ref, p) {
		return false
	}
	if len(ref.Tools) == 0 {
		return true
	}
	for _, tool := range ref.Tools {
		if !m.allowTool(ctx, p, tool) {
			return false
		}
	}
	return true
}

func (m *Manager) allowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool {
	pluginName := strings.TrimSpace(tool.Target.PluginName)
	operation := strings.TrimSpace(tool.Target.Operation)
	if pluginName == "" || operation == "" {
		return false
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return false
	}
	return principal.AllowsOperationPermission(p, pluginName, operation)
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Authorizer:        m.authorizer,
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func executionRefOwnedBy(ref *coreagent.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func sessionRefOwnedBy(ref *coreagent.SessionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func executionRefActive(ref *coreagent.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

func sessionRefsByProvider(refs []*coreagent.SessionReference) map[string][]*coreagent.SessionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string][]*coreagent.SessionReference)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		providerName := strings.TrimSpace(ref.ProviderName)
		if providerName == "" {
			continue
		}
		out[providerName] = append(out[providerName], ref)
	}
	return out
}

func sessionRefsByID(refs []*coreagent.SessionReference) map[string]*coreagent.SessionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreagent.SessionReference, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		id := strings.TrimSpace(ref.ID)
		if id == "" {
			continue
		}
		out[id] = ref
	}
	return out
}

func executionRefsByID(refs []*coreagent.ExecutionReference) map[string]*coreagent.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreagent.ExecutionReference, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		id := strings.TrimSpace(ref.ID)
		if id == "" {
			continue
		}
		out[id] = ref
	}
	return out
}

func normalizeProviderSession(providerName, sessionID string, session *coreagent.Session) (*coreagent.Session, error) {
	if session == nil {
		return nil, core.ErrNotFound
	}
	cloned := *session
	if strings.TrimSpace(cloned.ID) == "" {
		return nil, fmt.Errorf("agent provider returned session without id")
	}
	if strings.TrimSpace(cloned.ID) != strings.TrimSpace(sessionID) {
		return nil, fmt.Errorf("agent provider returned session id %q, want %q", cloned.ID, sessionID)
	}
	if strings.TrimSpace(cloned.ProviderName) == "" {
		cloned.ProviderName = strings.TrimSpace(providerName)
	}
	return &cloned, nil
}

func normalizeProviderTurn(providerName, sessionID, turnID string, turn *coreagent.Turn) (*coreagent.Turn, error) {
	if turn == nil {
		return nil, core.ErrNotFound
	}
	cloned := *turn
	if strings.TrimSpace(cloned.ID) == "" {
		return nil, fmt.Errorf("agent provider returned turn without id")
	}
	if strings.TrimSpace(cloned.ID) != strings.TrimSpace(turnID) {
		return nil, fmt.Errorf("agent provider returned turn id %q, want %q", cloned.ID, turnID)
	}
	if strings.TrimSpace(cloned.SessionID) == "" {
		return nil, fmt.Errorf("agent provider returned turn %q without session id", turnID)
	}
	if strings.TrimSpace(cloned.SessionID) != strings.TrimSpace(sessionID) {
		return nil, fmt.Errorf("agent provider returned turn session id %q, want %q", cloned.SessionID, sessionID)
	}
	if strings.TrimSpace(cloned.ProviderName) == "" {
		cloned.ProviderName = strings.TrimSpace(providerName)
	}
	return &cloned, nil
}

func sessionSortTime(session *coreagent.Session) *time.Time {
	if session == nil {
		return nil
	}
	if session.LastTurnAt != nil && !session.LastTurnAt.IsZero() {
		return session.LastTurnAt
	}
	if session.UpdatedAt != nil && !session.UpdatedAt.IsZero() {
		return session.UpdatedAt
	}
	return session.CreatedAt
}

func operationInputSchema(op catalog.CatalogOperation) (map[string]any, error) {
	if len(op.InputSchema) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(op.InputSchema, &out); err != nil {
		return nil, fmt.Errorf("decode %s input schema: %w", op.ID, err)
	}
	return out, nil
}

func agentToolID(pluginName, operation, connection, instance string) string {
	id := strings.TrimSpace(pluginName) + "/" + strings.TrimSpace(operation)
	if strings.TrimSpace(connection) != "" {
		id += "?connection=" + strings.TrimSpace(connection)
	}
	if strings.TrimSpace(instance) != "" {
		sep := "?"
		if strings.Contains(id, "?") {
			sep = "&"
		}
		id += sep + "instance=" + strings.TrimSpace(instance)
	}
	return id
}

func agentActorFromPrincipal(p *principal.Principal) coreagent.Actor {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreagent.Actor{}
	}
	return coreagent.Actor{
		SubjectID:   strings.TrimSpace(p.SubjectID),
		SubjectKind: string(p.Kind),
		DisplayName: agentActorDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func agentActorDisplayName(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if value := strings.TrimSpace(p.DisplayName); value != "" {
		return value
	}
	if p.Identity != nil {
		return strings.TrimSpace(p.Identity.DisplayName)
	}
	return ""
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

var _ Service = (*Manager)(nil)
