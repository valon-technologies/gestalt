package agentmanager

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	integration "github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrAgentNotConfigured              = errors.New("agent is not configured")
	ErrAgentProviderRequired           = errors.New("agent provider is required")
	ErrAgentProviderNotAvailable       = errors.New("agent provider is not available")
	ErrAgentSubjectRequired            = errors.New("agent subject is required")
	ErrAgentCallerPluginRequired       = errors.New("agent caller plugin is required for inherited tools")
	ErrAgentInheritedSurfaceTool       = errors.New("agent inherited surface tools are not supported")
	ErrAgentInteractionRequired        = errors.New("agent interaction is required")
	ErrAgentInteractionNotFound        = errors.New("agent interaction is not found")
	ErrAgentWorkflowToolsNotConfigured = errors.New("agent workflow tools are not configured")
	ErrAgentBoundedListUnsupported     = errors.New("agent provider does not support bounded list hydration")
	ErrAgentInvalidListRequest         = errors.New("agent list request is invalid")
)

const (
	agentToolSearchAllPlugin          = "*"
	agentToolSearchDefaultMaxResults  = 8
	agentToolSearchAdaptiveMaxResults = 3
	agentToolSearchMaxResults         = 20
	agentToolSearchMaxCandidates      = 20
	agentToolListDefaultPageSize      = 100
	agentToolListMaxPageSize          = 500
	agentToolInputSchemaMaxBytes      = 128 * 1024
	maxAgentRouteCacheEntries         = 20_000
	AgentListSummaryDefaultLimit      = 100
	AgentListMaxLimit                 = 500
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

type WorkflowSystemTools interface {
	Available() bool
	ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error)
	SearchTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error)
	AllowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool
}

type Service interface {
	Available() bool
	ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error)
	ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) ([]coreagent.Tool, error)
	SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)
	ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error)
	CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error)
	GetSession(ctx context.Context, p *principal.Principal, sessionID string) (*coreagent.Session, error)
	ListSessions(ctx context.Context, p *principal.Principal, req coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error)
	UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error)
	CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error)
	GetTurn(ctx context.Context, p *principal.Principal, turnID string) (*coreagent.Turn, error)
	ListTurns(ctx context.Context, p *principal.Principal, req coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error)
	CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (*coreagent.Turn, error)
	ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) ([]*coreagent.TurnEvent, error)
	ListInteractions(ctx context.Context, p *principal.Principal, turnID string) ([]*coreagent.Interaction, error)
	ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (*coreagent.Interaction, error)
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Agent             AgentControl
	WorkflowTools     WorkflowSystemTools
	ToolGrants        *agentgrant.Manager
	Invoker           invocation.Invoker
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]config.PluginInvocationDependency
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	agent             AgentControl
	workflowTools     WorkflowSystemTools
	toolGrants        *agentgrant.Manager
	invoker           invocation.Invoker
	authorizer        authorization.RuntimeAuthorizer
	defaultConnection map[string]string
	catalogConnection map[string]string
	pluginInvokes     map[string][]config.PluginInvocationDependency
	// Route caches are process-local accelerators; providers remain the durable source of truth.
	routeMu       sync.Mutex
	sessionRoutes agentRouteCache
	turnRoutes    agentRouteCache
}

func New(cfg Config) *Manager {
	pluginInvokes := make(map[string][]config.PluginInvocationDependency, len(cfg.PluginInvokes))
	for pluginName, deps := range cfg.PluginInvokes {
		pluginInvokes[pluginName] = append([]config.PluginInvocationDependency(nil), deps...)
	}
	return &Manager{
		providers:         cfg.Providers,
		agent:             cfg.Agent,
		workflowTools:     cfg.WorkflowTools,
		toolGrants:        cfg.ToolGrants,
		invoker:           cfg.Invoker,
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     pluginInvokes,
		sessionRoutes:     newAgentRouteCache(),
		turnRoutes:        newAgentRouteCache(),
	}
}

func (m *Manager) cachedSessionRoute(sessionID string) string {
	if m == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	return m.sessionRoutes.get(sessionID)
}

func (m *Manager) rememberSessionRoute(sessionID, providerName string) {
	if m == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if sessionID == "" || providerName == "" {
		return
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.sessionRoutes.remember(sessionID, providerName)
}

func (m *Manager) forgetSessionRoute(sessionID, providerName string) {
	if m == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if sessionID == "" {
		return
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.sessionRoutes.forget(sessionID, providerName)
}

func (m *Manager) cachedTurnRoute(turnID string) string {
	if m == nil {
		return ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return ""
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	return m.turnRoutes.get(turnID)
}

func (m *Manager) rememberTurnRoute(turnID, providerName string) {
	if m == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" || providerName == "" {
		return
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.turnRoutes.remember(turnID, providerName)
}

func (m *Manager) forgetTurnRoute(turnID, providerName string) {
	if m == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" {
		return
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.turnRoutes.forget(turnID, providerName)
}

type agentRouteCache struct {
	values map[string]*list.Element
	order  *list.List
}

type agentRouteEntry struct {
	key   string
	value string
}

func newAgentRouteCache() agentRouteCache {
	return agentRouteCache{
		values: map[string]*list.Element{},
		order:  list.New(),
	}
}

func (c *agentRouteCache) ensure() {
	if c.values == nil {
		c.values = map[string]*list.Element{}
	}
	if c.order == nil {
		c.order = list.New()
	}
}

func (c *agentRouteCache) get(key string) string {
	if c == nil || c.values == nil {
		return ""
	}
	elem := c.values[key]
	if elem == nil {
		return ""
	}
	c.order.MoveToBack(elem)
	return elem.Value.(agentRouteEntry).value
}

func (c *agentRouteCache) remember(key, value string) {
	c.ensure()
	if elem := c.values[key]; elem != nil {
		elem.Value = agentRouteEntry{key: key, value: value}
		c.order.MoveToBack(elem)
		c.trim(maxAgentRouteCacheEntries)
		return
	}
	c.values[key] = c.order.PushBack(agentRouteEntry{key: key, value: value})
	c.trim(maxAgentRouteCacheEntries)
}

func (c *agentRouteCache) forget(key, value string) {
	if c == nil || c.values == nil {
		return
	}
	elem := c.values[key]
	if elem == nil {
		return
	}
	entry := elem.Value.(agentRouteEntry)
	if value != "" && entry.value != value {
		return
	}
	delete(c.values, key)
	c.order.Remove(elem)
}

func (c *agentRouteCache) trim(maxEntries int) {
	if c == nil || c.values == nil || c.order == nil || maxEntries <= 0 {
		return
	}
	for len(c.values) > maxEntries {
		oldest := c.order.Front()
		if oldest == nil {
			return
		}
		entry := oldest.Value.(agentRouteEntry)
		delete(c.values, entry.key)
		c.order.Remove(oldest)
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
	systemTools, err := m.searchWorkflowSystemTools(ctx, p, refs)
	if err != nil {
		return nil, err
	}
	candidates, err := m.searchToolCandidates(ctx, p, refs, "", false)
	if err != nil {
		return nil, err
	}
	pluginTools, _, err := m.resolveAgentToolCandidates(ctx, p, candidates, 0, false)
	if err != nil {
		return nil, err
	}
	tools = append([]coreagent.Tool(nil), systemTools...)
	tools = append(tools, pluginTools...)
	observability.SetSpanAttributes(ctx, observability.AttrAgentToolSource.String(string(toolSource)))
	return tools, nil
}

func (m *Manager) ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (tool coreagent.Tool, err error) {
	ctx, finish := startAgentOperation(ctx, "resolve_tool")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return coreagent.Tool{}, ErrAgentSubjectRequired
	}
	refs, err := normalizeToolRefs([]coreagent.ToolRef{ref})
	if err != nil {
		return coreagent.Tool{}, err
	}
	if len(refs) == 0 {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool is required", invocation.ErrAuthorizationDenied)
	}
	if err := m.authorizeToolRefs(ctx, p, refs); err != nil {
		return coreagent.Tool{}, err
	}
	return m.resolveTool(ctx, p, refs[0])
}

func (m *Manager) CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "create_session")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
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
	session, err = provider.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:       sessionID,
		IdempotencyKey:  idempotencyKey,
		Model:           strings.TrimSpace(req.Model),
		ClientRef:       strings.TrimSpace(req.ClientRef),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
		CreatedBy:       agentActorFromPrincipal(p),
		Subject:         agentSubjectFromPrincipal(p),
	})
	if err != nil {
		fallback, getErr := provider.GetSession(ctx, coreagent.GetSessionRequest{
			SessionID: sessionID,
			Subject:   agentSubjectFromPrincipal(p),
		})
		if getErr != nil {
			return nil, err
		}
		session = fallback
	}
	normalized, err := normalizeProviderSessionForCreate(providerName, sessionID, idempotencyKey, session)
	if err != nil {
		return nil, err
	}
	if !providerSessionOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	m.rememberSessionRoute(normalized.ID, providerName)
	return normalized, nil
}

func (m *Manager) GetSession(ctx context.Context, p *principal.Principal, sessionID string) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "get_session")
	defer func() { finish(err) }()

	owned, err := m.findOwnedSession(ctx, p, sessionID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	return owned.session, nil
}

func (m *Manager) ListSessions(ctx context.Context, p *principal.Principal, req coreagent.ManagerListSessionsRequest) (sessions []*coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "list_sessions")
	defer func() { finish(err) }()

	providerName := strings.TrimSpace(req.ProviderName)
	if providerName != "" {
		observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(providerName))
	}
	candidates, err := m.providerCandidates(providerName)
	if err != nil {
		return nil, err
	}
	limit, err := normalizeAgentListLimit(req.Limit, req.SummaryOnly)
	if err != nil {
		return nil, err
	}
	requireBounded := req.SummaryOnly || limit > 0
	out := make([]*coreagent.Session, 0)
	for _, candidate := range candidates {
		if requireBounded {
			if err := requireAgentProviderBoundedListHydration(ctx, candidate.name, candidate.provider); err != nil {
				return nil, err
			}
		}
		sessions, err := candidate.provider.ListSessions(ctx, coreagent.ListSessionsRequest{
			Subject:     agentSubjectFromPrincipal(p),
			State:       req.State,
			Limit:       limit,
			SummaryOnly: req.SummaryOnly,
		})
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if session == nil {
				continue
			}
			if !providerSessionOwnedBy(session, p) {
				continue
			}
			normalized, err := normalizeProviderSession(candidate.name, strings.TrimSpace(session.ID), session)
			if err != nil {
				return nil, err
			}
			if req.State != "" && normalized.State != req.State {
				continue
			}
			m.rememberSessionRoute(normalized.ID, candidate.name)
			if req.SummaryOnly {
				normalized = summarizeAgentSession(normalized)
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
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Manager) UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "update_session")
	defer func() { finish(err) }()

	owned, err := m.findOwnedSession(ctx, p, req.SessionID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	session, err = owned.provider.UpdateSession(ctx, coreagent.UpdateSessionRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		ClientRef: strings.TrimSpace(req.ClientRef),
		State:     req.State,
		Metadata:  maps.Clone(req.Metadata),
		Subject:   agentSubjectFromPrincipal(p),
	})
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderSession(owned.providerName, owned.session.ID, session)
	if err != nil {
		return nil, err
	}
	if !providerSessionOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	m.rememberSessionRoute(normalized.ID, owned.providerName)
	return normalized, nil
}

func (m *Manager) CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "create_turn")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	ownedSession, err := m.findOwnedSession(ctx, p, req.SessionID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
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
	if toolSource == coreagent.ToolSourceModeMCPCatalog {
		if err := validateMCPCatalogToolRefs(toolRefs); err != nil {
			return nil, err
		}
	}
	if err := m.authorizeToolRefs(ctx, p, toolRefs); err != nil {
		return nil, err
	}
	var tools []coreagent.Tool
	if supported, err := agentProviderSupportsToolSource(ctx, ownedSession.provider, toolSource); err != nil {
		return nil, err
	} else if !supported {
		return nil, fmt.Errorf("agent provider %q does not support tool source %q", ownedSession.providerName, toolSource)
	}
	resolvableToolRefs := resolvableAgentToolRefs(toolRefs)
	if len(resolvableToolRefs) > 0 {
		tools, err = m.resolveExactAgentToolRefs(ctx, p, resolvableToolRefs)
		if err != nil {
			return nil, err
		}
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	turnID := newAgentTurnID(ownedSession.session.ID, idempotencyKey)
	toolGrant, err := m.mintToolGrant(ctx, p, ownedSession.providerName, ownedSession.session.ID, turnID, req.CallerPluginName, toolRefs, tools, toolSource)
	if err != nil {
		return nil, err
	}
	turn, err = ownedSession.provider.CreateTurn(ctx, coreagent.CreateTurnRequest{
		TurnID:          turnID,
		SessionID:       ownedSession.session.ID,
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
		Subject:         agentSubjectFromPrincipal(p),
		ToolGrant:       toolGrant,
	})
	if err != nil {
		fallback, getErr := ownedSession.provider.GetTurn(ctx, coreagent.GetTurnRequest{
			TurnID:  turnID,
			Subject: agentSubjectFromPrincipal(p),
		})
		if getErr == nil {
			turn = fallback
		} else {
			return nil, err
		}
	}
	normalized, err := normalizeProviderTurnForCreate(ownedSession.providerName, ownedSession.session.ID, turnID, idempotencyKey, turn)
	if err != nil {
		return nil, err
	}
	if !providerTurnOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	m.rememberTurnRoute(normalized.ID, ownedSession.providerName)
	return normalized, nil
}

func (m *Manager) GetTurn(ctx context.Context, p *principal.Principal, turnID string) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "get_turn")
	defer func() { finish(err) }()

	owned, err := m.findOwnedTurn(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	return owned.turn, nil
}

func (m *Manager) ListTurns(ctx context.Context, p *principal.Principal, req coreagent.ManagerListTurnsRequest) (turns []*coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turns")
	defer func() { finish(err) }()

	ownedSession, err := m.findOwnedSession(ctx, p, req.SessionID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	limit, err := normalizeAgentListLimit(req.Limit, req.SummaryOnly)
	if err != nil {
		return nil, err
	}
	if req.SummaryOnly || limit > 0 {
		if err := requireAgentProviderBoundedListHydration(ctx, ownedSession.providerName, ownedSession.provider); err != nil {
			return nil, err
		}
	}
	turns, err = ownedSession.provider.ListTurns(ctx, coreagent.ListTurnsRequest{
		SessionID:   ownedSession.session.ID,
		Subject:     agentSubjectFromPrincipal(p),
		Status:      req.Status,
		Limit:       limit,
		SummaryOnly: req.SummaryOnly,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*coreagent.Turn, 0, len(turns))
	for _, turn := range turns {
		if turn == nil {
			continue
		}
		if !providerTurnOwnedBy(turn, p) {
			continue
		}
		normalized, err := normalizeProviderTurn(ownedSession.providerName, ownedSession.session.ID, strings.TrimSpace(turn.ID), turn)
		if err != nil {
			return nil, err
		}
		if req.Status != "" && normalized.Status != req.Status {
			continue
		}
		m.rememberTurnRoute(normalized.ID, ownedSession.providerName)
		if req.SummaryOnly {
			normalized = summarizeAgentTurn(normalized)
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
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Manager) CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "cancel_turn")
	defer func() { finish(err) }()

	owned, err := m.findOwnedTurn(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	turn, err = owned.provider.CancelTurn(ctx, coreagent.CancelTurnRequest{
		TurnID:  strings.TrimSpace(turnID),
		Reason:  strings.TrimSpace(reason),
		Subject: agentSubjectFromPrincipal(p),
	})
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderTurn(owned.providerName, owned.turn.SessionID, owned.turn.ID, turn)
	if err != nil {
		return nil, err
	}
	if !providerTurnOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	if coreagent.ExecutionStatusIsLive(normalized.Status) {
		return nil, fmt.Errorf("%w: agent provider %q returned live turn %q after cancel", invocation.ErrInternal, owned.providerName, strings.TrimSpace(normalized.ID))
	}
	if m.toolGrants != nil {
		m.toolGrants.RevokeTurn(owned.providerName, normalized.SessionID, normalized.ID)
		if executionRef := strings.TrimSpace(normalized.ExecutionRef); executionRef != "" && executionRef != strings.TrimSpace(normalized.ID) {
			m.toolGrants.RevokeTurn(owned.providerName, normalized.SessionID, executionRef)
		}
	}
	m.rememberTurnRoute(normalized.ID, owned.providerName)
	return normalized, nil
}

func (m *Manager) ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) (events []*coreagent.TurnEvent, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turn_events")
	defer func() { finish(err) }()

	owned, err := m.findOwnedTurn(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	events, err = owned.provider.ListTurnEvents(ctx, coreagent.ListTurnEventsRequest{
		TurnID:   owned.turn.ID,
		AfterSeq: afterSeq,
		Limit:    limit,
		Subject:  agentSubjectFromPrincipal(p),
	})
	if err != nil {
		return nil, err
	}
	return normalizeTurnEventsForDisplay(events), nil
}

func (m *Manager) ListInteractions(ctx context.Context, p *principal.Principal, turnID string) (out []*coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "list_interactions")
	defer func() { finish(err) }()

	owned, err := m.findOwnedTurn(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	interactions, err := owned.provider.ListInteractions(ctx, coreagent.ListInteractionsRequest{
		TurnID:  owned.turn.ID,
		Subject: agentSubjectFromPrincipal(p),
	})
	if err != nil {
		return nil, err
	}
	out = make([]*coreagent.Interaction, 0, len(interactions))
	for _, interaction := range interactions {
		if interaction == nil {
			continue
		}
		if strings.TrimSpace(interaction.TurnID) != owned.turn.ID {
			return nil, fmt.Errorf("agent provider returned interaction %q for turn %q, want %q", strings.TrimSpace(interaction.ID), strings.TrimSpace(interaction.TurnID), owned.turn.ID)
		}
		if strings.TrimSpace(interaction.SessionID) != owned.turn.SessionID {
			return nil, fmt.Errorf("agent provider returned interaction %q for session %q, want %q", strings.TrimSpace(interaction.ID), strings.TrimSpace(interaction.SessionID), owned.turn.SessionID)
		}
		out = append(out, interaction)
	}
	return out, nil
}

func (m *Manager) ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (interaction *coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "resolve_interaction")
	defer func() { finish(err) }()

	owned, err := m.findOwnedTurn(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	interactionID = strings.TrimSpace(interactionID)
	if interactionID == "" {
		return nil, ErrAgentInteractionRequired
	}
	interaction, err = owned.provider.ResolveInteraction(ctx, coreagent.ResolveInteractionRequest{
		InteractionID: interactionID,
		Resolution:    maps.Clone(resolution),
		Subject:       agentSubjectFromPrincipal(p),
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
	if gotTurnID := strings.TrimSpace(interaction.TurnID); gotTurnID != "" && gotTurnID != owned.turn.ID {
		return nil, core.ErrNotFound
	}
	if gotSessionID := strings.TrimSpace(interaction.SessionID); gotSessionID != "" && gotSessionID != owned.turn.SessionID {
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

type namedAgentProvider struct {
	name     string
	provider coreagent.Provider
}

type ownedAgentSession struct {
	providerName string
	provider     coreagent.Provider
	session      *coreagent.Session
}

type ownedAgentTurn struct {
	providerName string
	provider     coreagent.Provider
	turn         *coreagent.Turn
}

func (m *Manager) providerCandidates(providerName string) ([]namedAgentProvider, error) {
	if m == nil || m.agent == nil {
		return nil, ErrAgentNotConfigured
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		return []namedAgentProvider{{name: providerName, provider: provider}}, nil
	}
	names := m.agent.ProviderNames()
	candidates := make([]namedAgentProvider, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		provider, err := m.resolveProviderByName(name)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, namedAgentProvider{name: name, provider: provider})
	}
	return candidates, nil
}

func (m *Manager) findOwnedSession(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*ownedAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		found, err := m.findOwnedSessionInProviders(ctx, p, sessionID, providerName)
		if err != nil {
			return nil, err
		}
		m.rememberSessionRoute(found.session.ID, found.providerName)
		return found, nil
	}
	if cachedProviderName := m.cachedSessionRoute(sessionID); cachedProviderName != "" {
		found, err := m.findOwnedSessionInProviders(ctx, p, sessionID, cachedProviderName)
		if err == nil {
			m.rememberSessionRoute(found.session.ID, found.providerName)
			return found, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
		m.forgetSessionRoute(sessionID, cachedProviderName)
	}
	found, err := m.findOwnedSessionInProviders(ctx, p, sessionID, "")
	if err != nil {
		return nil, err
	}
	m.rememberSessionRoute(found.session.ID, found.providerName)
	return found, nil
}

func (m *Manager) findOwnedSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*ownedAgentSession, error) {
	candidates, err := m.providerCandidates(providerName)
	if err != nil {
		return nil, err
	}
	var found *ownedAgentSession
	for _, candidate := range candidates {
		session, err := candidate.provider.GetSession(ctx, coreagent.GetSessionRequest{
			SessionID: sessionID,
			Subject:   agentSubjectFromPrincipal(p),
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			return nil, err
		}
		normalized, err := normalizeProviderSession(candidate.name, sessionID, session)
		if err != nil {
			return nil, err
		}
		if !providerSessionOwnedBy(normalized, p) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: agent session %q is present in multiple providers", invocation.ErrInternal, sessionID)
		}
		found = &ownedAgentSession{
			providerName: candidate.name,
			provider:     candidate.provider,
			session:      normalized,
		}
	}
	if found == nil {
		return nil, core.ErrNotFound
	}
	return found, nil
}

func (m *Manager) findOwnedTurn(ctx context.Context, p *principal.Principal, turnID, providerName string) (*ownedAgentTurn, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, core.ErrNotFound
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		found, err := m.findOwnedTurnInProviders(ctx, p, turnID, providerName)
		if err != nil {
			return nil, err
		}
		m.rememberTurnRoute(found.turn.ID, found.providerName)
		return found, nil
	}
	if cachedProviderName := m.cachedTurnRoute(turnID); cachedProviderName != "" {
		found, err := m.findOwnedTurnInProviders(ctx, p, turnID, cachedProviderName)
		if err == nil {
			m.rememberTurnRoute(found.turn.ID, found.providerName)
			return found, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
		m.forgetTurnRoute(turnID, cachedProviderName)
	}
	found, err := m.findOwnedTurnInProviders(ctx, p, turnID, "")
	if err != nil {
		return nil, err
	}
	m.rememberTurnRoute(found.turn.ID, found.providerName)
	return found, nil
}

func (m *Manager) findOwnedTurnInProviders(ctx context.Context, p *principal.Principal, turnID, providerName string) (*ownedAgentTurn, error) {
	candidates, err := m.providerCandidates(providerName)
	if err != nil {
		return nil, err
	}
	var found *ownedAgentTurn
	for _, candidate := range candidates {
		turn, err := candidate.provider.GetTurn(ctx, coreagent.GetTurnRequest{
			TurnID:  turnID,
			Subject: agentSubjectFromPrincipal(p),
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			return nil, err
		}
		if turn == nil {
			continue
		}
		sessionID := strings.TrimSpace(turn.SessionID)
		normalized, err := normalizeProviderTurn(candidate.name, sessionID, turnID, turn)
		if err != nil {
			return nil, err
		}
		if !providerTurnOwnedBy(normalized, p) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: agent turn %q is present in multiple providers", invocation.ErrInternal, turnID)
		}
		found = &ownedAgentTurn{
			providerName: candidate.name,
			provider:     candidate.provider,
			turn:         normalized,
		}
	}
	if found == nil {
		return nil, core.ErrNotFound
	}
	return found, nil
}

func agentProviderReturnedNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func (m *Manager) mintToolGrant(ctx context.Context, p *principal.Principal, providerName, sessionID, turnID, callerPluginName string, toolRefs []coreagent.ToolRef, tools []coreagent.Tool, toolSource coreagent.ToolSourceMode) (string, error) {
	if m == nil || m.toolGrants == nil {
		return "", fmt.Errorf("%w: agent tool grants are not configured", invocation.ErrInternal)
	}
	subject := agentSubjectFromPrincipal(p)
	return m.toolGrants.Mint(agentgrant.Grant{
		ProviderName:        providerName,
		SessionID:           sessionID,
		TurnID:              turnID,
		SubjectID:           subject.SubjectID,
		SubjectKind:         subject.SubjectKind,
		CredentialSubjectID: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
		Permissions:         agentRunPermissions(ctx, p, callerPluginName, toolRefs),
		ToolRefs:            append([]coreagent.ToolRef(nil), toolRefs...),
		Tools:               append([]coreagent.Tool(nil), tools...),
		ToolSource:          toolSource,
	})
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
	if toolSource != coreagent.ToolSourceModeNativeSearch {
		return nil, fmt.Errorf("agent tool search requires %q tool source", coreagent.ToolSourceModeNativeSearch)
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
	loadRefs, err := normalizeToolRefs(req.LoadRefs)
	if err != nil {
		return nil, err
	}
	systemTools, err := m.searchWorkflowSystemTools(ctx, p, refs)
	if err != nil {
		return nil, err
	}
	exactLoad := len(loadRefs) > 0
	var candidates []agentToolSearchCandidate
	if exactLoad {
		candidates, err = m.searchToolCandidatesForLoadRefs(ctx, p, refs, loadRefs)
	} else {
		candidates, err = m.searchToolCandidates(ctx, p, refs, req.Query, true)
	}
	if err != nil {
		return nil, err
	}
	maxResults := effectiveAgentToolSearchMaxResults(req.MaxResults, req.CandidateLimit, exactLoad)
	pluginMaxResults := maxResults
	if len(systemTools) >= pluginMaxResults {
		pluginMaxResults = 0
	} else {
		pluginMaxResults -= len(systemTools)
	}
	var tools []coreagent.Tool
	var loadedCandidateKeys []agentToolTargetKey
	if pluginMaxResults > 0 {
		failIfOnlyUnavailable := (len(refs) > 0 || exactLoad) && len(systemTools) == 0
		tools, loadedCandidateKeys, err = m.resolveAgentToolCandidates(ctx, p, candidates, pluginMaxResults, failIfOnlyUnavailable)
		if err != nil {
			return nil, err
		}
	}
	if len(systemTools) > 0 {
		tools = append(systemTools, tools...)
	}
	candidateLimit := effectiveAgentToolSearchCandidateLimit(req.CandidateLimit)
	compactCandidates, hasMore := agentToolCandidates(candidates, tools, loadedCandidateKeys, candidateLimit)
	return &coreagent.SearchToolsResponse{
		Tools:      tools,
		Candidates: compactCandidates,
		HasMore:    hasMore,
	}, nil
}

func (m *Manager) ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (resp *coreagent.ListToolsResponse, err error) {
	ctx, finish := startAgentOperation(ctx, "list_tools")
	defer func() { finish(err) }()

	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrAgentSubjectRequired
	}
	return m.listTools(ctx, p, req)
}

func (m *Manager) listTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (resp *coreagent.ListToolsResponse, err error) {
	startedAt := time.Now()
	toolSource, err := validateToolSource(req.ToolSource)
	if err != nil {
		return nil, err
	}
	if toolSource != coreagent.ToolSourceModeMCPCatalog {
		return nil, fmt.Errorf("agent tool listing requires %q tool source", coreagent.ToolSourceModeMCPCatalog)
	}
	attrs := []attribute.KeyValue{
		observability.AttrAgentToolSource.String(string(toolSource)),
	}
	ctx, span := observability.StartSpan(ctx, "agent.tool.list", attrs...)
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
	if err := validateMCPCatalogToolRefs(refs); err != nil {
		return nil, err
	}
	pageSize, err := effectiveAgentToolListPageSize(req.PageSize)
	if err != nil {
		return nil, err
	}
	pageOffset, err := agentToolListPageOffset(req.PageToken)
	if err != nil {
		return nil, err
	}

	out := make([]coreagent.ListedTool, 0, len(refs))
	seen := map[agentToolTargetKey]struct{}{}
	systemTools, err := m.searchWorkflowSystemTools(ctx, p, refs)
	if err != nil {
		return nil, err
	}
	for i := range systemTools {
		key := agentToolTargetKeyFromTarget(systemTools[i].Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		listed, err := listedAgentSystemTool(systemTools[i])
		if err != nil {
			return nil, err
		}
		out = append(out, listed)
	}

	candidates, err := m.searchToolCandidates(ctx, p, refs, "", true)
	if err != nil {
		return nil, err
	}
	var firstUnavailableErr error
	for i := range candidates {
		candidate := candidates[i]
		tool, err := m.resolveTool(ctx, p, candidate.ref)
		if err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrProviderNotFound) || errors.Is(err, invocation.ErrOperationNotFound) {
				continue
			}
			if candidate.skipUnavailable && agentToolSearchUnavailable(err) {
				if firstUnavailableErr == nil {
					firstUnavailableErr = err
				}
				continue
			}
			return nil, err
		}
		key := agentToolTargetKeyFromTarget(tool.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, listedAgentPluginTool(tool, candidate))
	}
	if len(out) == 0 && len(refs) > 0 && firstUnavailableErr != nil {
		return nil, firstUnavailableErr
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MCPName != out[j].MCPName {
			return out[i].MCPName < out[j].MCPName
		}
		return out[i].ToolID < out[j].ToolID
	})
	assignUniqueListedAgentToolNames(out)
	tools, nextPageToken := paginateListedAgentTools(out, pageSize, pageOffset)
	return &coreagent.ListToolsResponse{
		Tools:         tools,
		NextPageToken: nextPageToken,
	}, nil
}

func (m *Manager) searchWorkflowSystemTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	systemRefs := make([]coreagent.ToolRef, 0)
	for i := range refs {
		if strings.TrimSpace(refs[i].System) != "" {
			systemRefs = append(systemRefs, refs[i])
		}
	}
	if len(systemRefs) == 0 {
		return nil, nil
	}
	if m == nil || m.workflowTools == nil || !m.workflowTools.Available() {
		return nil, ErrAgentWorkflowToolsNotConfigured
	}
	tools, err := m.workflowTools.SearchTools(ctx, p, systemRefs)
	if err != nil {
		return nil, err
	}
	for i := range tools {
		toolID, err := m.mintAgentToolID(tools[i].Target)
		if err != nil {
			return nil, err
		}
		tools[i].ID = toolID
	}
	return tools, nil
}

func (m *Manager) resolveAgentToolCandidates(ctx context.Context, p *principal.Principal, candidates []agentToolSearchCandidate, maxResults int, failIfOnlyUnavailable bool) ([]coreagent.Tool, []agentToolTargetKey, error) {
	tools := make([]coreagent.Tool, 0, len(candidates))
	if maxResults > 0 {
		tools = make([]coreagent.Tool, 0, min(maxResults, len(candidates)))
	}
	loadedCandidateKeys := make([]agentToolTargetKey, 0, cap(tools))
	seen := map[agentToolTargetKey]struct{}{}
	var firstUnavailableErr error
	for i := range candidates {
		candidate := &candidates[i]
		if maxResults > 0 && len(tools) >= maxResults {
			break
		}
		tool, err := m.resolveTool(ctx, p, candidate.ref)
		if err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrProviderNotFound) || errors.Is(err, invocation.ErrOperationNotFound) {
				continue
			}
			if candidate.skipUnavailable && agentToolSearchUnavailable(err) {
				if firstUnavailableErr == nil {
					firstUnavailableErr = err
				}
				continue
			}
			return nil, nil, err
		}
		loadedCandidateKeys = append(loadedCandidateKeys, agentToolTargetKeyFromRef(candidate.ref))
		key := agentToolTargetKeyFromTarget(tool.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tools = append(tools, tool)
	}
	if len(tools) == 0 && failIfOnlyUnavailable && firstUnavailableErr != nil {
		return nil, nil, firstUnavailableErr
	}
	return tools, loadedCandidateKeys, nil
}

func effectiveAgentToolSearchMaxResults(maxResults int, candidateLimit int, exactLoad bool) int {
	if exactLoad {
		if maxResults <= 0 || maxResults > agentToolSearchMaxResults {
			return agentToolSearchMaxResults
		}
		return maxResults
	}
	if maxResults <= 0 {
		if candidateLimit > 0 {
			return agentToolSearchAdaptiveMaxResults
		}
		return agentToolSearchDefaultMaxResults
	}
	if maxResults > agentToolSearchMaxResults {
		return agentToolSearchMaxResults
	}
	return maxResults
}

func effectiveAgentToolSearchCandidateLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > agentToolSearchMaxCandidates {
		return agentToolSearchMaxCandidates
	}
	return limit
}

func agentToolCandidates(candidates []agentToolSearchCandidate, tools []coreagent.Tool, loadedCandidateKeys []agentToolTargetKey, limit int) ([]coreagent.ToolCandidate, bool) {
	if limit <= 0 || len(candidates) == 0 {
		return nil, false
	}
	seen := make(map[agentToolTargetKey]struct{}, len(tools)+len(loadedCandidateKeys)+limit)
	for i := range tools {
		seen[agentToolTargetKeyFromTarget(tools[i].Target)] = struct{}{}
	}
	for _, key := range loadedCandidateKeys {
		seen[key] = struct{}{}
	}
	out := make([]coreagent.ToolCandidate, 0, min(limit, len(candidates)))
	for i := range candidates {
		key := agentToolTargetKeyFromRef(candidates[i].ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if len(out) >= limit {
			return out, true
		}
		out = append(out, agentToolCandidate(candidates[i], key))
	}
	return out, false
}

func agentToolCandidate(candidate agentToolSearchCandidate, key agentToolTargetKey) coreagent.ToolCandidate {
	op := candidate.operation
	ref := candidate.ref
	name := strings.TrimSpace(op.Title)
	if name == "" {
		name = strings.TrimSpace(ref.Title)
	}
	if name == "" {
		name = ref.Plugin + "." + op.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(op.Description)
	}
	return coreagent.ToolCandidate{
		Ref:         ref,
		ID:          key.String(),
		Name:        name,
		Description: description,
		Parameters:  agentToolCandidateParameterNames(op),
		Score:       candidate.score,
	}
}

func agentToolCandidateParameterNames(op catalog.CatalogOperation) []string {
	if len(op.Parameters) == 0 {
		return nil
	}
	out := make([]string, 0, len(op.Parameters))
	seen := make(map[string]struct{}, len(op.Parameters))
	for _, param := range op.Parameters {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			name = strings.TrimSpace(param.WireName)
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (m *Manager) resolveExactAgentToolRefs(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]coreagent.Tool, 0, len(refs))
	seen := make(map[agentToolTargetKey]struct{}, len(refs))
	for i := range refs {
		if strings.TrimSpace(refs[i].Operation) == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d] requires native tool search", invocation.ErrInternal, i)
		}
		tool, err := m.resolveTool(ctx, p, refs[i])
		if err != nil {
			return nil, fmt.Errorf("agent tool_refs[%d]: %w", i, err)
		}
		key := agentToolTargetKeyFromTarget(tool.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tool)
	}
	return out, nil
}

func (m *Manager) resolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if strings.TrimSpace(ref.System) != "" {
		if m == nil || m.workflowTools == nil || !m.workflowTools.Available() {
			return coreagent.Tool{}, ErrAgentWorkflowToolsNotConfigured
		}
		tool, err := m.workflowTools.ResolveTool(ctx, p, ref)
		if err != nil {
			return coreagent.Tool{}, err
		}
		toolID, err := m.mintAgentToolID(tool.Target)
		if err != nil {
			return coreagent.Tool{}, err
		}
		tool.ID = toolID
		return tool, nil
	}
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
	target := coreagent.ToolTarget{
		Plugin:         pluginName,
		Operation:      opMeta.ID,
		Connection:     connection,
		Instance:       sessionInstance,
		CredentialMode: credentialMode,
	}
	toolID, err := m.mintAgentToolID(target)
	if err != nil {
		return coreagent.Tool{}, err
	}
	return coreagent.Tool{
		ID:               toolID,
		Name:             name,
		Description:      description,
		ParametersSchema: parametersSchema,
		Hidden:           !catalog.OperationVisibleByDefault(opMeta),
		Target:           target,
	}, nil
}

type agentToolSearchCandidate struct {
	ref             coreagent.ToolRef
	catalog         *catalog.Catalog
	operation       catalog.CatalogOperation
	skipUnavailable bool
	score           float64
}

type agentToolSearchCatalog struct {
	ref     coreagent.ToolRef
	catalog *catalog.Catalog
}

type agentToolTargetKey struct {
	system         string
	plugin         string
	operation      string
	connection     string
	instance       string
	credentialMode core.ConnectionMode
}

func agentToolTargetKeyFromRef(ref coreagent.ToolRef) agentToolTargetKey {
	return agentToolTargetKey{
		system:         strings.TrimSpace(ref.System),
		plugin:         strings.TrimSpace(ref.Plugin),
		operation:      strings.TrimSpace(ref.Operation),
		connection:     config.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		instance:       strings.TrimSpace(ref.Instance),
		credentialMode: ref.CredentialMode,
	}
}

func agentToolTargetKeyFromTarget(target coreagent.ToolTarget) agentToolTargetKey {
	return agentToolTargetKey{
		system:         strings.TrimSpace(target.System),
		plugin:         strings.TrimSpace(target.Plugin),
		operation:      strings.TrimSpace(target.Operation),
		connection:     config.ResolveConnectionAlias(strings.TrimSpace(target.Connection)),
		instance:       strings.TrimSpace(target.Instance),
		credentialMode: target.CredentialMode,
	}
}

func (k agentToolTargetKey) String() string {
	if k.system != "" {
		return strings.Join([]string{"system", k.system, k.operation}, "/")
	}
	parts := []string{k.plugin, k.operation}
	if k.connection != "" || k.instance != "" || k.credentialMode != "" {
		parts = append(parts, k.connection, k.instance, string(k.credentialMode))
	}
	return strings.Join(parts, "/")
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
	if scope.all {
		if mentioned := mentionedAgentToolSearchProviders(query, providerNames); len(mentioned) > 0 {
			providerNames = mentioned
		}
	}
	candidates := make([]agentToolSearchCandidate, 0)
	var firstUnavailableErr error
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
		searchRefs := scope.refsForProvider(pluginName)
		for i := range searchRefs {
			searchRef := searchRefs[i]
			searchCatalogs, err := m.catalogsForAgentToolSearch(ctx, p, prov, pluginName, searchRef)
			if err != nil {
				refSkipsUnavailable := skipUnavailable && agentToolSearchRefSkipsUnavailable(searchRef)
				if refSkipsUnavailable && agentToolSearchUnavailable(err) {
					if firstUnavailableErr == nil {
						firstUnavailableErr = err
					}
					continue
				}
				return nil, err
			}
			for j := range searchCatalogs {
				searchCatalog := searchCatalogs[j]
				cat := searchCatalog.catalog
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
					ref := searchCatalog.ref
					if strings.TrimSpace(ref.Operation) == "" {
						ref.Title = ""
						ref.Description = ""
					}
					ref.Plugin = pluginName
					ref.Operation = operation
					candidates = append(candidates, agentToolSearchCandidate{
						ref:             ref,
						catalog:         cat,
						operation:       op,
						skipUnavailable: skipUnavailable && agentToolSearchRefSkipsUnavailable(searchRef),
					})
				}
			}
		}
	}
	if len(candidates) == 0 && !scope.all && firstUnavailableErr != nil {
		return nil, firstUnavailableErr
	}
	return rankAgentToolSearchCandidates(query, candidates)
}

func (m *Manager) searchToolCandidatesForLoadRefs(ctx context.Context, p *principal.Principal, scopeRefs []coreagent.ToolRef, loadRefs []coreagent.ToolRef) ([]agentToolSearchCandidate, error) {
	if len(loadRefs) == 0 {
		return nil, nil
	}
	out := make([]agentToolSearchCandidate, 0, len(loadRefs))
	seen := map[agentToolTargetKey]struct{}{}
	var eligible []agentToolSearchCandidate
	var eligibleErr error
	eligibleLoaded := false
	for i := range loadRefs {
		loadRef := loadRefs[i]
		if strings.TrimSpace(loadRef.Operation) == "" {
			return nil, fmt.Errorf("%w: agent load_refs[%d].operation is required", invocation.ErrOperationNotFound, i)
		}
		if exactRef, ok := configuredExactAgentLoadRef(loadRef, scopeRefs); ok {
			candidates, err := m.searchToolCandidates(ctx, p, []coreagent.ToolRef{exactRef}, "", true)
			if err != nil {
				return nil, err
			}
			for j := range candidates {
				key := agentToolTargetKeyFromRef(candidates[j].ref)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, candidates[j])
			}
			continue
		}
		if !eligibleLoaded {
			eligible, eligibleErr = m.searchToolCandidates(ctx, p, scopeRefs, "", true)
			eligibleLoaded = true
		}
		if eligibleErr != nil {
			return nil, eligibleErr
		}
		for j := range eligible {
			if !agentLoadRefMatchesCandidate(loadRef, eligible[j].ref) {
				continue
			}
			key := agentToolTargetKeyFromRef(eligible[j].ref)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, eligible[j])
		}
	}
	return out, nil
}

func configuredExactAgentLoadRef(loadRef coreagent.ToolRef, refs []coreagent.ToolRef) (coreagent.ToolRef, bool) {
	loadPlugin := strings.TrimSpace(loadRef.Plugin)
	loadOperation := strings.TrimSpace(loadRef.Operation)
	loadConnection := config.ResolveConnectionAlias(strings.TrimSpace(loadRef.Connection))
	loadInstance := strings.TrimSpace(loadRef.Instance)
	exactRefs := exactAgentToolRefs(refs)
	for i := range exactRefs {
		ref := exactRefs[i]
		if strings.TrimSpace(ref.Plugin) != loadPlugin || strings.TrimSpace(ref.Operation) != loadOperation {
			continue
		}
		if loadConnection != "" && config.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)) != loadConnection {
			continue
		}
		if loadInstance != "" && strings.TrimSpace(ref.Instance) != loadInstance {
			continue
		}
		if loadRef.CredentialMode != "" && ref.CredentialMode != loadRef.CredentialMode {
			continue
		}
		return ref, true
	}
	return coreagent.ToolRef{}, false
}

func agentLoadRefMatchesCandidate(loadRef coreagent.ToolRef, candidateRef coreagent.ToolRef) bool {
	return agentToolTargetKeyFromRef(loadRef) == agentToolTargetKeyFromRef(candidateRef)
}

func agentToolSearchUnavailable(err error) bool {
	return errors.Is(err, invocation.ErrNoCredential) ||
		errors.Is(err, invocation.ErrAmbiguousInstance) ||
		errors.Is(err, invocation.ErrReconnectRequired) ||
		errors.Is(err, invocation.ErrNotAuthenticated) ||
		errors.Is(err, invocation.ErrScopeDenied)
}

func agentToolSearchRefSkipsUnavailable(ref coreagent.ToolRef) bool {
	return strings.TrimSpace(ref.Operation) == ""
}

func (m *Manager) catalogsForAgentToolSearch(ctx context.Context, p *principal.Principal, prov core.Provider, pluginName string, ref coreagent.ToolRef) ([]agentToolSearchCatalog, error) {
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
	targets := m.catalogSelectorConfig().SessionCatalogTargets(pluginName, connection, instance)
	if !shouldExpandAgentToolSearchCatalogTargets(ref, credentialMode) {
		cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
			catalogCtx,
			prov,
			pluginName,
			resolver,
			p,
			targets,
			core.SupportsSessionCatalog(prov) || connection != "" || instance != "",
		)
		if err != nil || cat == nil {
			return nil, err
		}
		return []agentToolSearchCatalog{{ref: ref, catalog: cat}}, nil
	}

	expander, ok := m.invoker.(invocation.CatalogTargetExpander)
	if !ok {
		cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
			catalogCtx,
			prov,
			pluginName,
			resolver,
			p,
			targets,
			core.SupportsSessionCatalog(prov) || connection != "" || instance != "",
		)
		if err != nil || cat == nil {
			return nil, err
		}
		return []agentToolSearchCatalog{{ref: ref, catalog: cat}}, nil
	}
	targets, err = expander.ExpandCatalogTargets(catalogCtx, p, pluginName, targets)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		targets = []invocation.CatalogResolutionTarget{{}}
	}

	out := make([]agentToolSearchCatalog, 0, len(targets))
	var firstErr error
	for _, target := range targets {
		target.Connection = strings.TrimSpace(target.Connection)
		target.Instance = strings.TrimSpace(target.Instance)
		if target.Connection != "" && !config.SafeConnectionValue(target.Connection) {
			return nil, fmt.Errorf("connection name contains invalid characters")
		}
		if target.Instance != "" && !config.SafeInstanceValue(target.Instance) {
			return nil, fmt.Errorf("instance name contains invalid characters")
		}
		cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
			catalogCtx,
			prov,
			pluginName,
			resolver,
			p,
			[]invocation.CatalogResolutionTarget{target},
			core.SupportsSessionCatalog(prov) || target.Connection != "" || target.Instance != "",
		)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cat == nil {
			continue
		}
		targetRef := ref
		targetRef.Connection = target.Connection
		targetRef.Instance = target.Instance
		out = append(out, agentToolSearchCatalog{
			ref:     targetRef,
			catalog: cat,
		})
	}
	if len(out) == 0 {
		return nil, firstErr
	}
	return out, nil
}

func shouldExpandAgentToolSearchCatalogTargets(ref coreagent.ToolRef, credentialMode core.ConnectionMode) bool {
	return strings.TrimSpace(ref.Operation) == "" &&
		strings.TrimSpace(ref.Instance) == "" &&
		credentialMode != core.ConnectionModeNone
}

type agentToolSearchScope struct {
	all      bool
	plugins  map[string][]coreagent.ToolRef
	exactOps map[string]map[string][]coreagent.ToolRef
}

func newAgentToolSearchScope(refs []coreagent.ToolRef) agentToolSearchScope {
	if len(refs) == 0 {
		return agentToolSearchScope{all: true}
	}
	scope := agentToolSearchScope{
		plugins:  map[string][]coreagent.ToolRef{},
		exactOps: map[string]map[string][]coreagent.ToolRef{},
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		pluginName := strings.TrimSpace(ref.Plugin)
		if pluginName == "" {
			continue
		}
		ref.Plugin = pluginName
		ref.Operation = strings.TrimSpace(ref.Operation)
		if ref.Plugin == agentToolSearchAllPlugin {
			scope.all = true
			continue
		}
		if ref.Operation == "" {
			scope.plugins[pluginName] = append(scope.plugins[pluginName], ref)
			continue
		}
		if scope.exactOps[pluginName] == nil {
			scope.exactOps[pluginName] = map[string][]coreagent.ToolRef{}
		}
		scope.exactOps[pluginName][ref.Operation] = append(scope.exactOps[pluginName][ref.Operation], ref)
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
			refs = append(refs, ops[operation]...)
		}
	}
	if pluginRefs := s.plugins[pluginName]; len(pluginRefs) > 0 {
		refs = append(refs, pluginRefs...)
	}
	return refs
}

func agentToolSearchRefAllows(ref coreagent.ToolRef, operation string) bool {
	refOperation := strings.TrimSpace(ref.Operation)
	return refOperation == "" || refOperation == strings.TrimSpace(operation)
}

func (m *Manager) authorizeToolRefs(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) error {
	if len(refs) == 0 {
		return nil
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			if _, err := m.resolveTool(ctx, p, ref); err != nil {
				return err
			}
			continue
		}
		pluginName := strings.TrimSpace(ref.Plugin)
		if pluginName == "" {
			continue
		}
		if pluginName == agentToolSearchAllPlugin {
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

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func providerSessionOwnedBy(session *coreagent.Session, p *principal.Principal) bool {
	if session == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(session.CreatedBy.SubjectID) == subjectID
}

func providerTurnOwnedBy(turn *coreagent.Turn, p *principal.Principal) bool {
	if turn == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(turn.CreatedBy.SubjectID) == subjectID
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

func normalizeProviderSessionForCreate(providerName, sessionID, idempotencyKey string, session *coreagent.Session) (*coreagent.Session, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return normalizeProviderSession(providerName, sessionID, session)
	}
	if session == nil {
		return nil, core.ErrNotFound
	}
	cloned := *session
	if strings.TrimSpace(cloned.ID) == "" {
		return nil, fmt.Errorf("agent provider returned session without id")
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

func normalizeProviderTurnForCreate(providerName, sessionID, turnID, idempotencyKey string, turn *coreagent.Turn) (*coreagent.Turn, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return normalizeProviderTurn(providerName, sessionID, turnID, turn)
	}
	if turn == nil {
		return nil, core.ErrNotFound
	}
	cloned := *turn
	if strings.TrimSpace(cloned.ID) == "" {
		return nil, fmt.Errorf("agent provider returned turn without id")
	}
	if strings.TrimSpace(cloned.SessionID) == "" {
		return nil, fmt.Errorf("agent provider returned turn %q without session id", strings.TrimSpace(cloned.ID))
	}
	if strings.TrimSpace(cloned.SessionID) != strings.TrimSpace(sessionID) {
		return nil, fmt.Errorf("agent provider returned turn session id %q, want %q", cloned.SessionID, sessionID)
	}
	if strings.TrimSpace(cloned.ProviderName) == "" {
		cloned.ProviderName = strings.TrimSpace(providerName)
	}
	return &cloned, nil
}

func newAgentTurnID(sessionID, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt:agent-turn:"+strings.TrimSpace(sessionID)+":"+idempotencyKey)).String()
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
	raw := agentToolInputSchema(op)
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s input schema: %w", op.ID, err)
	}
	return out, nil
}

func validateMCPCatalogToolRefs(refs []coreagent.ToolRef) error {
	if err := coreagent.ValidateMCPCatalogToolRefs(refs, "tool_refs"); err != nil {
		return fmt.Errorf("%w: %w", invocation.ErrInvalidInvocation, err)
	}
	return nil
}

func effectiveAgentToolListPageSize(pageSize int) (int, error) {
	if pageSize < 0 {
		return 0, fmt.Errorf("%w: page_size must be non-negative", invocation.ErrInvalidInvocation)
	}
	if pageSize == 0 {
		return agentToolListDefaultPageSize, nil
	}
	if pageSize > agentToolListMaxPageSize {
		return agentToolListMaxPageSize, nil
	}
	return pageSize, nil
}

func agentToolListPageOffset(pageToken string) (int, error) {
	pageToken = strings.TrimSpace(pageToken)
	if pageToken == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	return offset, nil
}

func paginateListedAgentTools(tools []coreagent.ListedTool, pageSize, offset int) ([]coreagent.ListedTool, string) {
	if offset >= len(tools) {
		return nil, ""
	}
	end := offset + pageSize
	if end >= len(tools) {
		return append([]coreagent.ListedTool(nil), tools[offset:]...), ""
	}
	return append([]coreagent.ListedTool(nil), tools[offset:end]...), strconv.Itoa(end)
}

func agentToolInputSchema(op catalog.CatalogOperation) json.RawMessage {
	if len(op.InputSchema) <= agentToolInputSchemaMaxBytes {
		return op.InputSchema
	}
	if synthesized := integration.SynthesizeInputSchema(op.Parameters); len(synthesized) > 0 {
		return synthesized
	}
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}

func agentToolInputSchemaJSON(op catalog.CatalogOperation) string {
	raw := agentToolInputSchema(op)
	if len(raw) == 0 {
		return `{"type":"object","additionalProperties":true}`
	}
	return string(raw)
}

func agentToolSchemaJSON(schema map[string]any) (string, error) {
	if len(schema) == 0 {
		return `{"type":"object","additionalProperties":true}`, nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("marshal agent tool input schema: %w", err)
	}
	return string(raw), nil
}

func listedAgentSystemTool(tool coreagent.Tool) (coreagent.ListedTool, error) {
	inputSchema, err := agentToolSchemaJSON(tool.ParametersSchema)
	if err != nil {
		return coreagent.ListedTool{}, err
	}
	return coreagent.ListedTool{
		ToolID:          tool.ID,
		MCPName:         agentToolMCPName(tool.Target),
		Title:           tool.Name,
		Description:     tool.Description,
		InputSchemaJSON: inputSchema,
		Ref:             agentToolRefFromTarget(tool.Target),
		Target:          tool.Target,
		Hidden:          tool.Hidden,
	}, nil
}

func listedAgentPluginTool(tool coreagent.Tool, candidate agentToolSearchCandidate) coreagent.ListedTool {
	ref := candidate.ref
	ref.Connection = tool.Target.Connection
	ref.Instance = tool.Target.Instance
	ref.CredentialMode = tool.Target.CredentialMode
	return coreagent.ListedTool{
		ToolID:           tool.ID,
		MCPName:          agentToolMCPName(tool.Target),
		Title:            tool.Name,
		Description:      tool.Description,
		InputSchemaJSON:  agentToolInputSchemaJSON(candidate.operation),
		OutputSchemaJSON: string(candidate.operation.OutputSchema),
		Annotations:      capabilityAnnotationsFromCatalog(candidate.operation.Annotations),
		Ref:              ref,
		Target:           tool.Target,
		Hidden:           tool.Hidden,
	}
}

func capabilityAnnotationsFromCatalog(value catalog.OperationAnnotations) core.CapabilityAnnotations {
	return core.CapabilityAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		IdempotentHint:  value.IdempotentHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
	}
}

func agentToolRefFromTarget(target coreagent.ToolTarget) coreagent.ToolRef {
	return coreagent.ToolRef{
		System:         target.System,
		Plugin:         target.Plugin,
		Operation:      target.Operation,
		Connection:     target.Connection,
		Instance:       target.Instance,
		CredentialMode: target.CredentialMode,
	}
}

func assignUniqueListedAgentToolNames(tools []coreagent.ListedTool) {
	used := make(map[string]struct{}, len(tools))
	nextSuffix := make(map[string]int, len(tools))
	for i := range tools {
		base := strings.TrimSpace(tools[i].MCPName)
		if base == "" {
			base = "tool"
		}
		name := base
		if _, exists := used[name]; exists {
			suffix := nextSuffix[base]
			if suffix < 2 {
				suffix = 2
			}
			for {
				candidate := fmt.Sprintf("%s_%d", base, suffix)
				suffix++
				if _, usedCandidate := used[candidate]; usedCandidate {
					continue
				}
				name = candidate
				nextSuffix[base] = suffix
				break
			}
		}
		tools[i].MCPName = name
		used[name] = struct{}{}
	}
}

func agentToolMCPName(target coreagent.ToolTarget) string {
	var parts []string
	if strings.TrimSpace(target.System) != "" {
		parts = []string{"system", target.System, target.Operation}
	} else {
		parts = []string{target.Plugin, target.Operation}
		if strings.TrimSpace(target.Connection) != "" || strings.TrimSpace(target.Instance) != "" || target.CredentialMode != "" {
			parts = append(parts, target.Connection, target.Instance, string(target.CredentialMode))
		}
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeMCPNamePart(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return "tool"
	}
	return strings.Join(out, "__")
}

func sanitizeMCPNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastSeparator := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSeparator = false
		case r == '_' || r == '-':
			if !lastSeparator {
				b.WriteRune(r)
				lastSeparator = true
			}
		default:
			if !lastSeparator {
				b.WriteByte('_')
				lastSeparator = true
			}
		}
	}
	return strings.Trim(b.String(), "_-")
}

func (m *Manager) mintAgentToolID(target coreagent.ToolTarget) (string, error) {
	if m == nil || m.toolGrants == nil {
		return "", fmt.Errorf("%w: agent tool grants are not configured", invocation.ErrInternal)
	}
	id, err := m.toolGrants.MintToolID(target)
	if err != nil {
		return "", fmt.Errorf("%w: mint agent tool id: %v", invocation.ErrInternal, err)
	}
	return id, nil
}

func validateToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = normalizeToolSource(source)
	switch source {
	case coreagent.ToolSourceModeNativeSearch, coreagent.ToolSourceModeMCPCatalog:
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", source)
	}
	return source, nil
}

func agentRunPermissions(ctx context.Context, p *principal.Principal, callerPluginName string, refs []coreagent.ToolRef) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	if shouldUseResolvedUserToolScope(ctx, p, callerPluginName, refs) {
		return nil
	}
	return principal.PermissionsToAccessPermissions(p.TokenPermissions)
}

func shouldUseResolvedUserToolScope(ctx context.Context, p *principal.Principal, callerPluginName string, refs []coreagent.ToolRef) bool {
	if strings.TrimSpace(callerPluginName) == "" {
		return false
	}
	if invocation.InvocationSurfaceFromContext(ctx) != invocation.InvocationSurfaceHTTP {
		return false
	}
	if p == nil || p.Kind != principal.KindUser || p.Source == principal.SourceAPIToken {
		return false
	}
	for i := range refs {
		if strings.TrimSpace(refs[i].Plugin) == agentToolSearchAllPlugin && strings.TrimSpace(refs[i].Operation) == "" {
			return true
		}
	}
	return false
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
	for idx := range refs {
		ref := refs[idx]
		ref.System = strings.TrimSpace(ref.System)
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
		if ref.System != "" {
			if ref.Plugin != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] must set exactly one of plugin or system", invocation.ErrInvalidInvocation, idx)
			}
			if ref.System != coreagent.SystemToolWorkflow {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].system %q is not supported", invocation.ErrInvalidInvocation, idx, ref.System)
			}
			if ref.Operation == "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].operation is required for system tool refs", invocation.ErrOperationNotFound, idx)
			}
			if ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] system refs cannot include connection, instance, or credential mode", invocation.ErrInvalidInvocation, idx)
			}
			out = append(out, ref)
			continue
		}
		if ref.Plugin == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].plugin is required", invocation.ErrProviderNotFound, idx)
		}
		if ref.Plugin == agentToolSearchAllPlugin {
			if ref.Operation != "" || ref.Connection != "" || ref.Instance != "" || ref.Title != "" || ref.Description != "" || ref.CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] global search ref cannot include operation, connection, instance, credential mode, title, or description", invocation.ErrProviderNotFound, idx)
			}
		}
		out = append(out, ref)
	}
	return out, nil
}

func resolvableAgentToolRefs(refs []coreagent.ToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			if strings.TrimSpace(ref.Operation) == "" {
				continue
			}
			out = append(out, ref)
			continue
		}
		if strings.TrimSpace(ref.Plugin) == agentToolSearchAllPlugin {
			continue
		}
		if strings.TrimSpace(ref.Operation) == "" {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func exactAgentToolRefs(refs []coreagent.ToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		if strings.TrimSpace(ref.Plugin) == agentToolSearchAllPlugin {
			continue
		}
		if strings.TrimSpace(ref.Operation) == "" {
			continue
		}
		out = append(out, ref)
	}
	return out
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

func agentProviderSupportsToolSource(ctx context.Context, provider coreagent.Provider, source coreagent.ToolSourceMode) (bool, error) {
	if provider == nil {
		return false, ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, coreagent.GetCapabilitiesRequest{})
	if err != nil {
		return false, err
	}
	return agentProviderCapabilitiesSupportToolSource(caps, source), nil
}

func agentProviderCapabilitiesSupportToolSource(caps *coreagent.ProviderCapabilities, source coreagent.ToolSourceMode) bool {
	if source == coreagent.ToolSourceModeNativeSearch && caps == nil {
		return true
	}
	if caps == nil {
		return false
	}
	for _, supported := range caps.SupportedToolSources {
		if normalizeToolSource(supported) == source {
			return true
		}
	}
	if len(caps.SupportedToolSources) > 0 {
		return false
	}
	return source == coreagent.ToolSourceModeNativeSearch
}

func requireAgentProviderBoundedListHydration(ctx context.Context, providerName string, provider coreagent.Provider) error {
	if provider == nil {
		return ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, coreagent.GetCapabilitiesRequest{})
	if err != nil {
		return err
	}
	if caps != nil && caps.BoundedListHydration {
		return nil
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return ErrAgentBoundedListUnsupported
	}
	return fmt.Errorf("%w: provider %q", ErrAgentBoundedListUnsupported, providerName)
}

func normalizeAgentListLimit(limit int, summaryOnly bool) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("%w: limit must be non-negative", ErrAgentInvalidListRequest)
	}
	if summaryOnly && limit == 0 {
		return AgentListSummaryDefaultLimit, nil
	}
	if limit > AgentListMaxLimit {
		return AgentListMaxLimit, nil
	}
	return limit, nil
}

func summarizeAgentSession(session *coreagent.Session) *coreagent.Session {
	if session == nil {
		return nil
	}
	cloned := *session
	cloned.Metadata = nil
	return &cloned
}

func summarizeAgentTurn(turn *coreagent.Turn) *coreagent.Turn {
	if turn == nil {
		return nil
	}
	cloned := *turn
	cloned.Messages = nil
	cloned.OutputText = ""
	cloned.StructuredOutput = nil
	return &cloned
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

func agentSubjectFromPrincipal(p *principal.Principal) coreagent.SubjectContext {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreagent.SubjectContext{}
	}
	return coreagent.SubjectContext{
		SubjectID:           strings.TrimSpace(p.SubjectID),
		SubjectKind:         string(p.Kind),
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		DisplayName:         agentActorDisplayName(p),
		AuthSource:          p.AuthSource(),
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
