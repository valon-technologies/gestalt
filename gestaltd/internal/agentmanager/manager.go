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
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"go.opentelemetry.io/otel/attribute"
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
	ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) ([]coreagent.Tool, error)
	SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)
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

func startAgentOperation(ctx context.Context, operation string) (context.Context, func(error)) {
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		observability.AttrAgentOperation.String(operation),
	}
	ctx, span := observability.StartSpan(ctx, "agent.operation", attrs...)
	return ctx, func(err error) {
		observability.EndSpan(span, err)
		observability.RecordAgentOperation(ctx, startedAt, err != nil, attrs...)
	}
}

func (m *Manager) ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) (tools []coreagent.Tool, err error) {
	ctx, finish := startAgentOperation(ctx, "resolve_tools")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrAgentSubjectRequired
	}
	if len(req.ToolRefs) == 0 {
		return nil, nil
	}
	toolSource, err := validateToolSource(req.ToolSource)
	if err != nil {
		return nil, err
	}
	refs, err := normalizeToolRefs(req.ToolRefs)
	if err != nil {
		return nil, err
	}
	refs, err = m.applyCallerInvokeCredentialModes(req.CallerPluginName, refs)
	if err != nil {
		return nil, err
	}
	candidates, err := m.searchToolCandidates(ctx, p, refs, "", false)
	if err != nil {
		return nil, err
	}
	tools, err = m.resolveAgentToolCandidates(ctx, p, candidates, 0, false)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentToolSource.String(string(toolSource)))
	return tools, nil
}

func (m *Manager) CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "create_session")
	defer func() { finish(err) }()

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
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(providerName))
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
	session, err = provider.CreateSession(ctx, coreagent.CreateSessionRequest{
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

func (m *Manager) GetSession(ctx context.Context, p *principal.Principal, sessionID string) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "get_session")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedSessionMetadata(ctx, sessionID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	return m.getSessionByMetadata(ctx, p, ref)
}

func (m *Manager) ListSessions(ctx context.Context, p *principal.Principal, providerName string) (sessions []*coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "list_sessions")
	defer func() { finish(err) }()

	refs, err := m.listOwnedSessionMetadata(ctx, p)
	if err != nil {
		return nil, err
	}
	refsByProvider := sessionRefsByProvider(refs)
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(providerName))
	}
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

func (m *Manager) UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "update_session")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedSessionMetadata(ctx, req.SessionID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	session, err = provider.UpdateSession(ctx, coreagent.UpdateSessionRequest{
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

func (m *Manager) CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "create_turn")
	defer func() { finish(err) }()

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
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(sessionRef.ProviderName))
	provider, err := m.resolveProviderByName(sessionRef.ProviderName)
	if err != nil {
		return nil, err
	}
	toolSource, err := validateToolSource(req.ToolSource)
	if err != nil {
		return nil, err
	}
	toolRefs, err := normalizeToolRefs(req.ToolRefs)
	if err != nil {
		return nil, err
	}
	toolRefs, err = m.applyCallerInvokeCredentialModes(req.CallerPluginName, toolRefs)
	if err != nil {
		return nil, err
	}
	if err := m.authorizeToolRefs(ctx, p, toolRefs); err != nil {
		return nil, err
	}
	var tools []coreagent.Tool
	nativeToolSearch, err := agentProviderSupportsNativeToolSearch(ctx, provider)
	if err != nil {
		return nil, err
	}
	if !nativeToolSearch && len(toolRefs) > 0 {
		tools, err = m.resolveExactAgentToolRefs(ctx, p, toolRefs)
		if err != nil {
			return nil, err
		}
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
	metadataStartedAt := time.Now()
	metadataAttrs := []attribute.KeyValue{
		observability.AttrAgentProvider.String(sessionRef.ProviderName),
		observability.AttrAgentOperation.String("create_turn"),
	}
	metadataCtx, metadataSpan := observability.StartSpan(ctx, "agent.run_metadata.write", metadataAttrs...)
	ref, err := m.runMetadata.Put(metadataCtx, &coreagent.ExecutionReference{
		ID:                  turnID,
		SessionID:           sessionRef.ID,
		ProviderName:        sessionRef.ProviderName,
		SubjectID:           subjectID,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		IdempotencyKey:      idempotencyKey,
		Permissions:         principal.PermissionsToAccessPermissions(p.TokenPermissions),
		ToolRefs:            append([]coreagent.ToolRef(nil), toolRefs...),
		ToolSource:          toolSource,
		Tools:               tools,
	})
	observability.EndSpan(metadataSpan, err)
	observability.RecordAgentRunMetadataWrite(metadataCtx, metadataStartedAt, err != nil, metadataAttrs...)
	if err != nil {
		if idempotencyKey != "" {
			_ = m.runMetadata.ReleaseIdempotency(ctx, subjectID, sessionRef.ProviderName, idempotencyKey)
		}
		return nil, err
	}
	turn, err = provider.CreateTurn(ctx, coreagent.CreateTurnRequest{
		TurnID:          turnID,
		SessionID:       sessionRef.ID,
		IdempotencyKey:  idempotencyKey,
		Model:           strings.TrimSpace(req.Model),
		Messages:        append([]coreagent.Message(nil), req.Messages...),
		ToolRefs:        append([]coreagent.ToolRef(nil), toolRefs...),
		ToolSource:      toolSource,
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

func (m *Manager) GetTurn(ctx context.Context, p *principal.Principal, turnID string) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "get_turn")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	return m.getTurnByMetadata(ctx, p, ref)
}

func (m *Manager) ListTurns(ctx context.Context, p *principal.Principal, sessionID string) (turns []*coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turns")
	defer func() { finish(err) }()

	sessionRef, err := m.requireOwnedSessionMetadata(ctx, sessionID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(sessionRef.ProviderName))
	provider, err := m.resolveProviderByName(sessionRef.ProviderName)
	if err != nil {
		return nil, err
	}
	turns, err = provider.ListTurns(ctx, coreagent.ListTurnsRequest{SessionID: sessionRef.ID})
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

func (m *Manager) CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "cancel_turn")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	turn, err = provider.CancelTurn(ctx, coreagent.CancelTurnRequest{
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

func (m *Manager) ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) (events []*coreagent.TurnEvent, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turn_events")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
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

func (m *Manager) ListInteractions(ctx context.Context, p *principal.Principal, turnID string) (out []*coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "list_interactions")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	interactions, err := provider.ListInteractions(ctx, coreagent.ListInteractionsRequest{TurnID: ref.ID})
	if err != nil {
		return nil, err
	}
	out = make([]*coreagent.Interaction, 0, len(interactions))
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

func (m *Manager) ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (interaction *coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "resolve_interaction")
	defer func() { finish(err) }()

	ref, err := m.requireOwnedTurnMetadata(ctx, turnID, p)
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ref.ProviderName))
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return nil, ErrAgentInteractionRequired
	}
	provider, err := m.resolveProviderByName(ref.ProviderName)
	if err != nil {
		return nil, err
	}
	interaction, err = provider.ResolveInteraction(ctx, coreagent.ResolveInteractionRequest{
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

func (m *Manager) SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (resp *coreagent.SearchToolsResponse, err error) {
	ctx, finish := startAgentOperation(ctx, "search_tools")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrAgentSubjectRequired
	}
	return m.searchTools(ctx, p, req)
}

func (m *Manager) searchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (resp *coreagent.SearchToolsResponse, err error) {
	startedAt := time.Now()
	toolSource, err := validateToolSource(req.ToolSource)
	if err != nil {
		return nil, err
	}
	attrs := []attribute.KeyValue{
		observability.AttrAgentToolSource.String(string(toolSource)),
	}
	ctx, span := observability.StartSpan(ctx, "agent.tool.search", attrs...)
	defer func() {
		observability.EndSpan(span, err)
		observability.RecordAgentToolResolve(ctx, startedAt, err != nil, attrs...)
	}()

	if m == nil || m.providers == nil {
		return nil, fmt.Errorf("%w: agent providers are not configured", invocation.ErrInternal)
	}
	refs, err := normalizeToolRefs(req.ToolRefs)
	if err != nil {
		return nil, err
	}
	skipUnavailable := len(refs) == 0
	candidates, err := m.searchToolCandidates(ctx, p, refs, req.Query, skipUnavailable)
	if err != nil {
		return nil, err
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}
	tools, err := m.resolveAgentToolCandidates(ctx, p, candidates, maxResults, skipUnavailable)
	if err != nil {
		return nil, err
	}
	return &coreagent.SearchToolsResponse{Tools: tools}, nil
}

func (m *Manager) resolveAgentToolCandidates(ctx context.Context, p *principal.Principal, candidates []agentToolSearchCandidate, maxResults int, skipUnavailable bool) ([]coreagent.Tool, error) {
	tools := make([]coreagent.Tool, 0, len(candidates))
	if maxResults > 0 {
		tools = make([]coreagent.Tool, 0, min(maxResults, len(candidates)))
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if maxResults > 0 && len(tools) >= maxResults {
			break
		}
		tool, err := m.resolveTool(ctx, p, candidate.ref)
		if err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrProviderNotFound) || errors.Is(err, invocation.ErrOperationNotFound) {
				continue
			}
			if skipUnavailable && agentToolSearchUnavailable(err) {
				continue
			}
			return nil, err
		}
		if _, ok := seen[tool.ID]; ok {
			continue
		}
		seen[tool.ID] = struct{}{}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (m *Manager) resolveExactAgentToolRefs(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]coreagent.Tool, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for i := range refs {
		if strings.TrimSpace(refs[i].Operation) == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d] requires native tool search", invocation.ErrInternal, i)
		}
		tool, err := m.resolveTool(ctx, p, refs[i])
		if err != nil {
			return nil, fmt.Errorf("agent tool_refs[%d]: %w", i, err)
		}
		if _, ok := seen[tool.ID]; ok {
			continue
		}
		seen[tool.ID] = struct{}{}
		out = append(out, tool)
	}
	return out, nil
}

func (m *Manager) resolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if m == nil || m.providers == nil {
		return coreagent.Tool{}, fmt.Errorf("%w: agent providers are not configured", invocation.ErrInternal)
	}
	pluginName := strings.TrimSpace(ref.Plugin)
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
	credentialMode, err := normalizeAgentToolCredentialMode(ref.CredentialMode)
	if err != nil {
		return coreagent.Tool{}, err
	}
	if credentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, credentialMode)
	}
	if m.authorizer != nil && principal.IsNonUserPrincipal(p) && (connection != "" || instance != "") {
		return coreagent.Tool{}, fmt.Errorf("%w: non-user subjects may not override connection or instance bindings", invocation.ErrAuthorizationDenied)
	}

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, pluginName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	sessionConnections := m.catalogSelectorConfig().SessionCatalogConnections(pluginName, connection)
	sessionInstance := instance
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, pluginName, resolver, p, operation, sessionConnections, sessionInstance)
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
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, pluginName, connection, sessionInstance)
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
		ID:               agentToolID(pluginName, opMeta.ID, connection, sessionInstance, credentialMode),
		Name:             name,
		Description:      description,
		ParametersSchema: parametersSchema,
		Target: coreagent.ToolTarget{
			Plugin:         pluginName,
			Operation:      opMeta.ID,
			Connection:     connection,
			Instance:       sessionInstance,
			CredentialMode: credentialMode,
		},
	}, nil
}

type agentToolSearchCandidate struct {
	ref   coreagent.ToolRef
	score int
}

func (m *Manager) searchToolCandidates(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef, query string, skipUnavailable bool) ([]agentToolSearchCandidate, error) {
	scope := newAgentToolSearchScope(refs)
	providerNames := scope.providerNames()
	if len(providerNames) == 0 {
		if !scope.all {
			return nil, nil
		}
		providerNames = m.providers.List()
	}
	query = strings.TrimSpace(query)
	candidates := make([]agentToolSearchCandidate, 0)
	for _, pluginName := range providerNames {
		pluginName = strings.TrimSpace(pluginName)
		if pluginName == "" {
			continue
		}
		prov, err := m.providers.Get(pluginName)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
		}
		if !m.allowProvider(ctx, p, pluginName) {
			continue
		}
		for _, searchRef := range scope.refsForProvider(pluginName) {
			cat, err := m.catalogForAgentToolSearch(ctx, p, prov, pluginName, searchRef)
			if err != nil {
				if skipUnavailable && agentToolSearchUnavailable(err) {
					continue
				}
				return nil, err
			}
			if cat == nil {
				continue
			}
			for i := range cat.Operations {
				op := cat.Operations[i]
				operation := strings.TrimSpace(op.ID)
				if operation == "" || !agentToolSearchRefAllows(searchRef, operation) {
					continue
				}
				if strings.TrimSpace(searchRef.Operation) == "" && !catalog.OperationVisibleByDefault(op) {
					continue
				}
				if !m.allowOperation(ctx, p, pluginName, operation) || !principal.AllowsOperationPermission(p, pluginName, operation) {
					continue
				}
				if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, op) {
					continue
				}
				score := scoreAgentToolSearch(query, pluginName, cat, op)
				if query != "" && score <= 0 {
					continue
				}
				ref := searchRef
				if strings.TrimSpace(ref.Operation) == "" {
					ref.Title = ""
					ref.Description = ""
				}
				ref.Plugin = pluginName
				ref.Operation = operation
				candidates = append(candidates, agentToolSearchCandidate{ref: ref, score: score})
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].ref.Plugin != candidates[j].ref.Plugin {
			return candidates[i].ref.Plugin < candidates[j].ref.Plugin
		}
		return candidates[i].ref.Operation < candidates[j].ref.Operation
	})
	return candidates, nil
}

func agentToolSearchUnavailable(err error) bool {
	return errors.Is(err, invocation.ErrNoCredential) ||
		errors.Is(err, invocation.ErrAmbiguousInstance) ||
		errors.Is(err, invocation.ErrReconnectRequired) ||
		errors.Is(err, invocation.ErrNotAuthenticated) ||
		errors.Is(err, invocation.ErrScopeDenied)
}

func (m *Manager) catalogForAgentToolSearch(ctx context.Context, p *principal.Principal, prov core.Provider, pluginName string, ref coreagent.ToolRef) (*catalog.Catalog, error) {
	connection := strings.TrimSpace(ref.Connection)
	if connection != "" && !config.SafeConnectionValue(connection) {
		return nil, fmt.Errorf("connection name contains invalid characters")
	}
	instance := strings.TrimSpace(ref.Instance)
	if instance != "" && !config.SafeInstanceValue(instance) {
		return nil, fmt.Errorf("instance name contains invalid characters")
	}
	credentialMode, err := normalizeAgentToolCredentialMode(ref.CredentialMode)
	if err != nil {
		return nil, err
	}
	if credentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, credentialMode)
	}
	if m.authorizer != nil && principal.IsNonUserPrincipal(p) && (connection != "" || instance != "") {
		return nil, fmt.Errorf("%w: non-user subjects may not override connection or instance bindings", invocation.ErrAuthorizationDenied)
	}
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	catalogCtx := invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, pluginName))
	cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
		catalogCtx,
		prov,
		pluginName,
		resolver,
		p,
		m.catalogSelectorConfig().SessionCatalogTargets(pluginName, connection, instance),
		core.SupportsSessionCatalog(prov) || connection != "" || instance != "",
	)
	return cat, err
}

type agentToolSearchScope struct {
	all      bool
	plugins  map[string]coreagent.ToolRef
	exactOps map[string]map[string]coreagent.ToolRef
}

func newAgentToolSearchScope(refs []coreagent.ToolRef) agentToolSearchScope {
	if len(refs) == 0 {
		return agentToolSearchScope{all: true}
	}
	scope := agentToolSearchScope{
		plugins:  map[string]coreagent.ToolRef{},
		exactOps: map[string]map[string]coreagent.ToolRef{},
	}
	for _, ref := range refs {
		pluginName := strings.TrimSpace(ref.Plugin)
		if pluginName == "" {
			continue
		}
		ref.Plugin = pluginName
		ref.Operation = strings.TrimSpace(ref.Operation)
		if ref.Operation == "" {
			scope.plugins[pluginName] = ref
			continue
		}
		if scope.exactOps[pluginName] == nil {
			scope.exactOps[pluginName] = map[string]coreagent.ToolRef{}
		}
		scope.exactOps[pluginName][ref.Operation] = ref
	}
	return scope
}

func (s agentToolSearchScope) providerNames() []string {
	if s.all {
		return nil
	}
	set := map[string]struct{}{}
	for pluginName := range s.plugins {
		set[pluginName] = struct{}{}
	}
	for pluginName := range s.exactOps {
		set[pluginName] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for pluginName := range set {
		names = append(names, pluginName)
	}
	sort.Strings(names)
	return names
}

func (s agentToolSearchScope) refsForProvider(pluginName string) []coreagent.ToolRef {
	if s.all {
		return []coreagent.ToolRef{{Plugin: pluginName}}
	}
	refs := []coreagent.ToolRef{}
	if ops := s.exactOps[pluginName]; len(ops) > 0 {
		operations := make([]string, 0, len(ops))
		for operation := range ops {
			operations = append(operations, operation)
		}
		sort.Strings(operations)
		for _, operation := range operations {
			refs = append(refs, ops[operation])
		}
	}
	if ref, ok := s.plugins[pluginName]; ok {
		refs = append(refs, ref)
	}
	return refs
}

func agentToolSearchRefAllows(ref coreagent.ToolRef, operation string) bool {
	refOperation := strings.TrimSpace(ref.Operation)
	return refOperation == "" || refOperation == strings.TrimSpace(operation)
}

func scoreAgentToolSearch(query, pluginName string, cat *catalog.Catalog, op catalog.CatalogOperation) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 1
	}
	haystacks := []struct {
		value  string
		weight int
	}{
		{pluginName, 8},
		{op.ID, 8},
		{op.Title, 6},
		{cat.DisplayName, 4},
		{cat.Description, 2},
		{op.Description, 2},
	}
	score := 0
	for _, term := range strings.Fields(query) {
		for _, haystack := range haystacks {
			if strings.Contains(strings.ToLower(haystack.value), term) {
				score += haystack.weight
			}
		}
	}
	if strings.Contains(strings.ToLower(pluginName+" "+op.ID+" "+op.Title), query) {
		score += 12
	}
	return score
}

func (m *Manager) authorizeToolRefs(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) error {
	if len(refs) == 0 {
		return nil
	}
	for _, ref := range refs {
		pluginName := strings.TrimSpace(ref.Plugin)
		if pluginName == "" {
			continue
		}
		if strings.TrimSpace(ref.Operation) != "" {
			if _, err := m.resolveTool(ctx, p, ref); err != nil {
				return err
			}
			continue
		}
		if _, err := m.providers.Get(pluginName); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
			}
			return fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
		}
		if !m.allowProvider(ctx, p, pluginName) || !principal.AllowsProviderPermission(p, pluginName) {
			return fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, pluginName)
		}
		connection := strings.TrimSpace(ref.Connection)
		if connection != "" && !config.SafeConnectionValue(connection) {
			return fmt.Errorf("connection name contains invalid characters")
		}
		instance := strings.TrimSpace(ref.Instance)
		if instance != "" && !config.SafeInstanceValue(instance) {
			return fmt.Errorf("instance name contains invalid characters")
		}
	}
	return nil
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
	for i := range ref.Tools {
		if !m.allowTool(ctx, p, ref.Tools[i]) {
			return false
		}
	}
	return true
}

func (m *Manager) allowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool {
	pluginName := strings.TrimSpace(tool.Target.Plugin)
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

func agentToolID(pluginName, operation, connection, instance string, credentialMode core.ConnectionMode) string {
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
	if credentialMode != "" {
		sep := "?"
		if strings.Contains(id, "?") {
			sep = "&"
		}
		id += sep + "credentialMode=" + string(credentialMode)
	}
	return id
}

func validateToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = normalizeToolSource(source)
	if source != coreagent.ToolSourceModeNativeSearch {
		return "", fmt.Errorf("unsupported agent tool source %q", source)
	}
	return source, nil
}

func normalizeToolSource(source coreagent.ToolSourceMode) coreagent.ToolSourceMode {
	if strings.TrimSpace(string(source)) == "" {
		return coreagent.ToolSourceModeNativeSearch
	}
	return source
}

func normalizeToolRefs(refs []coreagent.ToolRef) ([]coreagent.ToolRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for idx, ref := range refs {
		ref.Plugin = strings.TrimSpace(ref.Plugin)
		ref.Operation = strings.TrimSpace(ref.Operation)
		ref.Connection = strings.TrimSpace(ref.Connection)
		ref.Instance = strings.TrimSpace(ref.Instance)
		ref.Title = strings.TrimSpace(ref.Title)
		ref.Description = strings.TrimSpace(ref.Description)
		credentialMode, err := normalizeAgentToolCredentialMode(ref.CredentialMode)
		if err != nil {
			return nil, err
		}
		ref.CredentialMode = credentialMode
		if ref.Plugin == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].plugin is required", invocation.ErrProviderNotFound, idx)
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) applyCallerInvokeCredentialModes(callerPluginName string, refs []coreagent.ToolRef) ([]coreagent.ToolRef, error) {
	callerPluginName = strings.TrimSpace(callerPluginName)
	if len(refs) == 0 || m == nil {
		return refs, nil
	}
	modes := make(map[string]core.ConnectionMode)
	if callerPluginName != "" {
		for _, invoke := range m.pluginInvokes[callerPluginName] {
			if strings.TrimSpace(invoke.Surface) != "" {
				continue
			}
			pluginName := strings.TrimSpace(invoke.Plugin)
			operation := strings.TrimSpace(invoke.Operation)
			if pluginName == "" || operation == "" {
				continue
			}
			mode, err := normalizeAgentToolCredentialMode(core.ConnectionMode(invoke.CredentialMode))
			if err != nil {
				return nil, err
			}
			if mode != "" {
				modes[agentToolInvokeKey(pluginName, operation)] = mode
			}
		}
	}
	out := append([]coreagent.ToolRef(nil), refs...)
	for i := range out {
		operation := strings.TrimSpace(out[i].Operation)
		if out[i].CredentialMode != "" && callerPluginName == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires a caller plugin declaration", invocation.ErrAuthorizationDenied, i)
		}
		if operation == "" {
			if out[i].CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires an exact operation", invocation.ErrAuthorizationDenied, i)
			}
			continue
		}
		mode, ok := modes[agentToolInvokeKey(out[i].Plugin, operation)]
		if !ok {
			if out[i].CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires a declared invoke mode", invocation.ErrAuthorizationDenied, i)
			}
			continue
		}
		if out[i].CredentialMode != "" && out[i].CredentialMode != mode {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode %q exceeds declared invoke mode %q", invocation.ErrAuthorizationDenied, i, out[i].CredentialMode, mode)
		}
		out[i].CredentialMode = mode
	}
	return out, nil
}

func agentToolInvokeKey(pluginName, operation string) string {
	return strings.TrimSpace(pluginName) + "\x00" + strings.TrimSpace(operation)
}

func agentProviderSupportsNativeToolSearch(ctx context.Context, provider coreagent.Provider) (bool, error) {
	if provider == nil {
		return false, ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, coreagent.GetCapabilitiesRequest{})
	if err != nil {
		return false, err
	}
	return caps != nil && caps.NativeToolSearch, nil
}

func normalizeAgentToolCredentialMode(mode core.ConnectionMode) (core.ConnectionMode, error) {
	switch core.ConnectionMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case "":
		return "", nil
	case core.ConnectionModeNone:
		return core.ConnectionModeNone, nil
	case core.ConnectionModeUser:
		return core.ConnectionModeUser, nil
	default:
		return "", fmt.Errorf("unsupported agent tool credential mode %q", mode)
	}
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
