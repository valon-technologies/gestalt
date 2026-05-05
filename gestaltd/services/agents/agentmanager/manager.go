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
	"unicode"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	"github.com/valon-technologies/gestalt/server/services/authorization"
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
	ErrAgentSessionNotFound            = errors.New("agent session is not found")
	ErrAgentWorkflowToolsNotConfigured = errors.New("agent workflow tools are not configured")
	ErrAgentBoundedListUnsupported     = errors.New("agent provider does not support bounded list hydration")
	ErrAgentInvalidListRequest         = errors.New("agent list request is invalid")
)

const (
	agentToolSearchAllPlugin     = "*"
	agentToolListDefaultPageSize = 100
	agentToolListMaxPageSize     = 1000
	agentToolSchemaMaxBytes      = 128 * 1024
	agentDefaultToolNarrowingK   = 200
	maxAgentRouteCacheEntries    = 20_000
	AgentListSummaryDefaultLimit = 100
	AgentListMaxLimit            = 500
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
	ResolveTools(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef) ([]coreagent.Tool, error)
	AllowTool(ctx context.Context, p *principal.Principal, tool coreagent.Tool) bool
}

type Service interface {
	Available() bool
	ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error)
	ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) ([]coreagent.Tool, error)
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
	RunGrants         *agentgrant.Manager
	Invoker           invocation.Invoker
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]invocation.PluginInvocationDependency
	AgentConnections  map[string][]string
	RouteStore        RouteStore
	// DefaultToolNarrowingThreshold controls when implicit default wildcard
	// catalog grants are narrowed to exactly mentioned providers. Nil uses the
	// package default; zero means narrow whenever any visible catalog candidate
	// exists.
	DefaultToolNarrowingThreshold *int
}

type Manager struct {
	providers                     *registry.ProviderMap[core.Provider]
	agent                         AgentControl
	workflowTools                 WorkflowSystemTools
	runGrants                     *agentgrant.Manager
	invoker                       invocation.Invoker
	authorizer                    authorization.RuntimeAuthorizer
	defaultConnection             map[string]string
	catalogConnection             map[string]string
	pluginInvokes                 map[string][]invocation.PluginInvocationDependency
	agentConnections              map[string][]string
	routeStore                    RouteStore
	defaultToolNarrowingThreshold int
	// Route caches are process-local accelerators; the route store is the durable provider index.
	routeMu       sync.Mutex
	sessionRoutes agentRouteCache
	turnRoutes    agentRouteCache
}

func New(cfg Config) *Manager {
	return &Manager{
		providers:         cfg.Providers,
		agent:             cfg.Agent,
		workflowTools:     cfg.WorkflowTools,
		runGrants:         cfg.RunGrants,
		invoker:           cfg.Invoker,
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     invocation.ClonePluginInvocationDependencyMap(cfg.PluginInvokes),
		agentConnections:  cloneStringSliceMap(cfg.AgentConnections),
		routeStore:        cfg.RouteStore,
		defaultToolNarrowingThreshold: effectiveAgentToolNarrowingThreshold(
			cfg.DefaultToolNarrowingThreshold,
		),
		sessionRoutes: newAgentRouteCache(),
		turnRoutes:    newAgentRouteCache(),
	}
}

func effectiveAgentToolNarrowingThreshold(configured *int) int {
	if configured == nil {
		return agentDefaultToolNarrowingK
	}
	if *configured < 0 {
		return 0
	}
	return *configured
}

func cloneStringSliceMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, value := range src {
		dst[key] = append([]string(nil), value...)
	}
	return dst
}

func (m *Manager) cachedSessionRoute(sessionID string) (AgentRoute, bool) {
	if m == nil {
		return AgentRoute{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AgentRoute{}, false
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	return m.sessionRoutes.get(sessionID)
}

func (m *Manager) storedSessionRoute(ctx context.Context, sessionID string) (AgentRoute, bool, error) {
	if m == nil {
		return AgentRoute{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || m.routeStore == nil {
		return AgentRoute{}, false, nil
	}
	route, ok, err := m.routeStore.LookupSession(ctx, sessionID)
	if err != nil || !ok {
		return AgentRoute{}, false, err
	}
	route.SessionID = sessionID
	return route, true, nil
}

func (m *Manager) rememberSessionRoute(ctx context.Context, sessionID, providerName string) error {
	if m == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if sessionID == "" || providerName == "" {
		return nil
	}
	if m.routeStore != nil {
		if err := m.routeStore.RememberSession(ctx, sessionID, providerName); err != nil {
			return err
		}
	}
	m.rememberCachedSessionRoute(sessionID, providerName)
	return nil
}

func (m *Manager) rememberSessionRouteBestEffort(ctx context.Context, sessionID, providerName string) {
	if m == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if sessionID == "" || providerName == "" {
		return
	}
	if m.routeStore == nil {
		m.rememberCachedSessionRoute(sessionID, providerName)
		return
	}
	if existing, ok, err := m.routeStore.LookupSession(ctx, sessionID); err != nil {
		return
	} else if ok {
		if existing.ProviderName != providerName {
			m.forgetCachedSessionRoute(sessionID, providerName)
			return
		}
	} else if err := m.routeStore.RememberSession(ctx, sessionID, providerName); err != nil {
		return
	}
	m.rememberCachedSessionRoute(sessionID, providerName)
}

func (m *Manager) rememberCachedSessionRoute(sessionID, providerName string) {
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
	m.sessionRoutes.remember(sessionID, AgentRoute{ProviderName: providerName, SessionID: sessionID})
}

func (m *Manager) forgetCachedSessionRoute(sessionID, providerName string) {
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

func (m *Manager) forgetSessionRoute(ctx context.Context, sessionID, providerName string) error {
	if m == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if sessionID == "" {
		return nil
	}
	if m.routeStore != nil {
		if err := m.routeStore.ForgetSession(ctx, sessionID, providerName); err != nil {
			return err
		}
	}
	m.forgetCachedSessionRoute(sessionID, providerName)
	return nil
}

func (m *Manager) cachedTurnRoute(turnID string) (AgentRoute, bool) {
	if m == nil {
		return AgentRoute{}, false
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return AgentRoute{}, false
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	return m.turnRoutes.get(turnID)
}

func (m *Manager) storedTurnRoute(ctx context.Context, turnID string) (AgentRoute, bool, error) {
	if m == nil {
		return AgentRoute{}, false, nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" || m.routeStore == nil {
		return AgentRoute{}, false, nil
	}
	route, ok, err := m.routeStore.LookupTurn(ctx, turnID)
	if err != nil || !ok {
		return AgentRoute{}, false, err
	}
	return route, true, nil
}

func (m *Manager) rememberTurnRoute(ctx context.Context, turnID, sessionID, providerName string) error {
	if m == nil {
		return nil
	}
	turnID = strings.TrimSpace(turnID)
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" || sessionID == "" || providerName == "" {
		return nil
	}
	if m.routeStore != nil {
		if err := m.routeStore.RememberTurn(ctx, turnID, sessionID, providerName); err != nil {
			return err
		}
	}
	m.rememberCachedTurnRoute(turnID, sessionID, providerName)
	return nil
}

func (m *Manager) rememberTurnRouteBestEffort(ctx context.Context, turnID, sessionID, providerName string) {
	if m == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" || sessionID == "" || providerName == "" {
		return
	}
	if m.routeStore == nil {
		m.rememberCachedTurnRoute(turnID, sessionID, providerName)
		return
	}
	if existing, ok, err := m.routeStore.LookupTurn(ctx, turnID); err != nil {
		return
	} else if ok {
		if existing.ProviderName != providerName || existing.SessionID != sessionID {
			m.forgetCachedTurnRoute(turnID, providerName)
			return
		}
	} else if err := m.routeStore.RememberTurn(ctx, turnID, sessionID, providerName); err != nil {
		return
	}
	m.rememberCachedTurnRoute(turnID, sessionID, providerName)
}

func (m *Manager) rememberCachedTurnRoute(turnID, sessionID, providerName string) {
	if m == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	sessionID = strings.TrimSpace(sessionID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" || sessionID == "" || providerName == "" {
		return
	}
	m.routeMu.Lock()
	defer m.routeMu.Unlock()
	m.turnRoutes.remember(turnID, AgentRoute{ProviderName: providerName, SessionID: sessionID})
}

func (m *Manager) forgetCachedTurnRoute(turnID, providerName string) {
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

func (m *Manager) forgetTurnRoute(ctx context.Context, turnID, providerName string) error {
	if m == nil {
		return nil
	}
	turnID = strings.TrimSpace(turnID)
	providerName = strings.TrimSpace(providerName)
	if turnID == "" {
		return nil
	}
	if m.routeStore != nil {
		if err := m.routeStore.ForgetTurn(ctx, turnID, providerName); err != nil {
			return err
		}
	}
	m.forgetCachedTurnRoute(turnID, providerName)
	return nil
}

type agentRouteCache struct {
	values map[string]*list.Element
	order  *list.List
}

type agentRouteEntry struct {
	key   string
	value AgentRoute
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

func (c *agentRouteCache) get(key string) (AgentRoute, bool) {
	if c == nil || c.values == nil {
		return AgentRoute{}, false
	}
	elem := c.values[key]
	if elem == nil {
		return AgentRoute{}, false
	}
	c.order.MoveToBack(elem)
	return elem.Value.(agentRouteEntry).value, true
}

func (c *agentRouteCache) remember(key string, value AgentRoute) {
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
	if value != "" && strings.TrimSpace(entry.value.ProviderName) != value {
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
	candidates, _, err := m.searchToolCandidates(ctx, p, refs, "", false)
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
	if !m.allowsAgentProvider(ctx, p, providerName) {
		return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	sessionID := uuid.NewString()
	session, err = provider.CreateSession(ctx, coreagent.CreateSessionRequest{
		SessionID:      sessionID,
		IdempotencyKey: idempotencyKey,
		Model:          strings.TrimSpace(req.Model),
		ClientRef:      strings.TrimSpace(req.ClientRef),
		Metadata:       maps.Clone(req.Metadata),
		CreatedBy:      agentActorFromPrincipal(p),
		Subject:        agentSubjectFromPrincipal(p),
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
	if err := m.rememberSessionRoute(ctx, normalized.ID, providerName); err != nil {
		return nil, err
	}
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
	candidates, err := m.authorizedProviderCandidates(ctx, p, providerName)
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
			m.rememberSessionRouteBestEffort(ctx, normalized.ID, candidate.name)
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
	m.rememberSessionRouteBestEffort(ctx, normalized.ID, owned.providerName)
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
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %w", ErrAgentSessionNotFound, err)
		}
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	toolRefs, err := normalizeToolRefs(req.ToolRefs)
	if err != nil {
		return nil, err
	}
	toolRefs, err = m.applyCallerInvokeCredentialModes(req.CallerPluginName, toolRefs)
	if err != nil {
		return nil, err
	}
	toolSource, err := validateProviderTurnToolSource(req.ToolSource)
	if err != nil {
		return nil, err
	}
	if toolSource == coreagent.ToolSourceModeUnspecified && len(toolRefs) > 0 {
		toolSource = coreagent.ToolSourceModeMCPCatalog
	}
	if toolSource == coreagent.ToolSourceModeUnspecified && len(toolRefs) == 0 && !req.ToolRefsSet && defaultAgentTurnToolSource(ctx, ownedSession.provider) == coreagent.ToolSourceModeMCPCatalog {
		toolSource = coreagent.ToolSourceModeMCPCatalog
		toolRefs = m.defaultAgentTurnToolRefs(ctx, p, req)
	}
	var tools []coreagent.Tool
	if toolSource == coreagent.ToolSourceModeMCPCatalog {
		if err := validateMCPCatalogToolRefs(toolRefs); err != nil {
			return nil, err
		}
		if err := m.authorizeToolRefs(ctx, p, toolRefs); err != nil {
			return nil, err
		}
		if supported, err := agentProviderSupportsToolSource(ctx, ownedSession.provider, toolSource); err != nil {
			return nil, err
		} else if !supported {
			return nil, fmt.Errorf("agent provider %q does not support tool source %q", ownedSession.providerName, toolSource)
		}
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	turnID := newAgentTurnID(ownedSession.session.ID, idempotencyKey)
	runGrant, err := m.mintRunGrant(ctx, p, ownedSession.providerName, ownedSession.session.ID, turnID, req.CallerPluginName, toolRefs, tools, toolSource)
	if err != nil {
		return nil, err
	}
	turn, err = ownedSession.provider.CreateTurn(ctx, coreagent.CreateTurnRequest{
		TurnID:         turnID,
		SessionID:      ownedSession.session.ID,
		IdempotencyKey: idempotencyKey,
		Model:          strings.TrimSpace(req.Model),
		Messages:       append([]coreagent.Message(nil), req.Messages...),
		ToolRefs:       append([]coreagent.ToolRef(nil), toolRefs...),
		ToolSource:     toolSource,
		Tools:          append([]coreagent.Tool(nil), tools...),
		ResponseSchema: maps.Clone(req.ResponseSchema),
		Metadata:       maps.Clone(req.Metadata),
		ModelOptions:   maps.Clone(req.ModelOptions),
		CreatedBy:      agentActorFromPrincipal(p),
		ExecutionRef:   turnID,
		Subject:        agentSubjectFromPrincipal(p),
		RunGrant:       runGrant,
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
	if err := m.rememberTurnRoute(ctx, normalized.ID, normalized.SessionID, ownedSession.providerName); err != nil {
		return nil, err
	}
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
		m.rememberTurnRouteBestEffort(ctx, normalized.ID, normalized.SessionID, ownedSession.providerName)
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
	if m.runGrants != nil {
		m.runGrants.RevokeTurn(owned.providerName, normalized.SessionID, normalized.ID)
		if executionRef := strings.TrimSpace(normalized.ExecutionRef); executionRef != "" && executionRef != strings.TrimSpace(normalized.ID) {
			m.runGrants.RevokeTurn(owned.providerName, normalized.SessionID, executionRef)
		}
	}
	m.rememberTurnRouteBestEffort(ctx, normalized.ID, normalized.SessionID, owned.providerName)
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

func (m *Manager) authorizedProviderCandidates(ctx context.Context, p *principal.Principal, providerName string) ([]namedAgentProvider, error) {
	candidates, err := m.providerCandidates(providerName)
	if err != nil {
		return nil, err
	}
	authorized := make([]namedAgentProvider, 0, len(candidates))
	for _, candidate := range candidates {
		if m.allowsAgentProvider(ctx, p, candidate.name) {
			authorized = append(authorized, candidate)
		}
	}
	if len(authorized) == 0 {
		if providerName = strings.TrimSpace(providerName); providerName != "" {
			return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
		}
		return nil, invocation.ErrAuthorizationDenied
	}
	return authorized, nil
}

func (m *Manager) agentProviderConfigured(providerName string) bool {
	if m == nil || m.agent == nil {
		return false
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return false
	}
	for _, name := range m.agent.ProviderNames() {
		if strings.TrimSpace(name) == providerName {
			return true
		}
	}
	return false
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
		m.rememberSessionRouteBestEffort(ctx, found.session.ID, found.providerName)
		return found, nil
	}
	if storedRoute, ok, err := m.storedSessionRoute(ctx, sessionID); err != nil {
		return nil, err
	} else if ok {
		found, findErr := m.findOwnedSessionInProviders(ctx, p, sessionID, storedRoute.ProviderName)
		if findErr == nil {
			m.rememberSessionRouteBestEffort(ctx, found.session.ID, found.providerName)
			return found, nil
		}
		if errors.Is(findErr, ErrAgentProviderNotAvailable) {
			if m.agentProviderConfigured(storedRoute.ProviderName) {
				return nil, findErr
			}
			if err := m.forgetSessionRoute(ctx, sessionID, storedRoute.ProviderName); err != nil {
				return nil, err
			}
		} else if !errors.Is(findErr, core.ErrNotFound) {
			return nil, findErr
		}
	}
	if cachedRoute, ok := m.cachedSessionRoute(sessionID); ok {
		found, err := m.findOwnedSessionInProviders(ctx, p, sessionID, cachedRoute.ProviderName)
		if err == nil {
			m.rememberSessionRouteBestEffort(ctx, found.session.ID, found.providerName)
			return found, nil
		}
		m.forgetCachedSessionRoute(sessionID, cachedRoute.ProviderName)
		if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, ErrAgentProviderNotAvailable) {
			return nil, err
		}
		if errors.Is(err, ErrAgentProviderNotAvailable) && !m.agentProviderConfigured(cachedRoute.ProviderName) {
			_ = m.forgetSessionRoute(ctx, sessionID, cachedRoute.ProviderName)
		}
	}
	found, err := m.findOwnedSessionInProviders(ctx, p, sessionID, "")
	if err != nil {
		return nil, err
	}
	m.rememberSessionRouteBestEffort(ctx, found.session.ID, found.providerName)
	return found, nil
}

func (m *Manager) findOwnedSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*ownedAgentSession, error) {
	candidates, err := m.authorizedProviderCandidates(ctx, p, providerName)
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
		found, err := m.findOwnedTurnInProviders(ctx, p, turnID, providerName, "")
		if err != nil {
			return nil, err
		}
		m.rememberTurnRouteBestEffort(ctx, found.turn.ID, found.turn.SessionID, found.providerName)
		return found, nil
	}
	if storedRoute, ok, err := m.storedTurnRoute(ctx, turnID); err != nil {
		return nil, err
	} else if ok {
		found, findErr := m.findOwnedTurnInProviders(ctx, p, turnID, storedRoute.ProviderName, storedRoute.SessionID)
		if findErr == nil {
			m.rememberTurnRouteBestEffort(ctx, found.turn.ID, found.turn.SessionID, found.providerName)
			return found, nil
		}
		if errors.Is(findErr, ErrAgentProviderNotAvailable) {
			if m.agentProviderConfigured(storedRoute.ProviderName) {
				return nil, findErr
			}
			if err := m.forgetTurnRoute(ctx, turnID, storedRoute.ProviderName); err != nil {
				return nil, err
			}
		} else if !errors.Is(findErr, core.ErrNotFound) {
			return nil, findErr
		}
	}
	if cachedRoute, ok := m.cachedTurnRoute(turnID); ok {
		found, err := m.findOwnedTurnInProviders(ctx, p, turnID, cachedRoute.ProviderName, cachedRoute.SessionID)
		if err == nil {
			m.rememberTurnRouteBestEffort(ctx, found.turn.ID, found.turn.SessionID, found.providerName)
			return found, nil
		}
		m.forgetCachedTurnRoute(turnID, cachedRoute.ProviderName)
		if !errors.Is(err, core.ErrNotFound) && !errors.Is(err, ErrAgentProviderNotAvailable) {
			return nil, err
		}
		if errors.Is(err, ErrAgentProviderNotAvailable) && !m.agentProviderConfigured(cachedRoute.ProviderName) {
			_ = m.forgetTurnRoute(ctx, turnID, cachedRoute.ProviderName)
		}
	}
	found, err := m.findOwnedTurnInProviders(ctx, p, turnID, "", "")
	if err != nil {
		return nil, err
	}
	m.rememberTurnRouteBestEffort(ctx, found.turn.ID, found.turn.SessionID, found.providerName)
	return found, nil
}

func (m *Manager) findOwnedTurnInProviders(ctx context.Context, p *principal.Principal, turnID, providerName, expectedSessionID string) (*ownedAgentTurn, error) {
	candidates, err := m.authorizedProviderCandidates(ctx, p, providerName)
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
		if expectedSessionID = strings.TrimSpace(expectedSessionID); expectedSessionID != "" {
			sessionID = expectedSessionID
		}
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

func (m *Manager) mintRunGrant(ctx context.Context, p *principal.Principal, providerName, sessionID, turnID, callerPluginName string, toolRefs []coreagent.ToolRef, tools []coreagent.Tool, toolSource coreagent.ToolSourceMode) (string, error) {
	if m == nil || m.runGrants == nil {
		return "", fmt.Errorf("%w: agent run grants are not configured", invocation.ErrInternal)
	}
	subject := agentSubjectFromPrincipal(p)
	return m.runGrants.Mint(agentgrant.Grant{
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
		Connections:         m.agentConnectionBindings(providerName),
	})
}

func (m *Manager) agentConnectionBindings(providerName string) []agentgrant.ConnectionBinding {
	if m == nil {
		return nil
	}
	names := append([]string(nil), m.agentConnections[strings.TrimSpace(providerName)]...)
	if len(names) == 0 {
		return nil
	}
	out := make([]agentgrant.ConnectionBinding, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, agentgrant.ConnectionBinding{Connection: name})
		}
	}
	return out
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
	if len(refs) == 0 {
		return &coreagent.ListToolsResponse{}, nil
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

	query := strings.TrimSpace(req.Query)
	candidates, unavailable, err := m.searchToolCandidates(ctx, p, refs, query, true)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		candidate := candidates[i]
		listed, err := m.listedAgentPluginCandidateTool(candidate)
		if err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrProviderNotFound) || errors.Is(err, invocation.ErrOperationNotFound) {
				continue
			}
			if candidate.skipUnavailable && agentToolSearchUnavailable(err) {
				continue
			}
			return nil, err
		}
		key := agentToolTargetKeyFromTarget(listed.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, listed)
	}
	for i := range unavailable {
		listed, err := m.listedUnavailableAgentPluginTool(unavailable[i])
		if err != nil {
			return nil, err
		}
		key := agentToolTargetKeyFromTarget(listed.Target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, listed)
	}

	if query == "" {
		sort.SliceStable(out, func(i, j int) bool {
			return listedAgentToolSortLess(out[i], out[j])
		})
	}
	assignStableUniqueListedAgentToolNames(out)
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
	tools, err := m.workflowTools.ResolveTools(ctx, p, systemRefs)
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
	if connection != "" && !core.SafeConnectionValue(connection) {
		return coreagent.Tool{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = core.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(ref.Instance)
	if instance != "" && !core.SafeInstanceValue(instance) {
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

type agentToolUnavailableCandidate struct {
	ref     coreagent.ToolRef
	err     error
	reason  string
	message string
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
		connection:     core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		instance:       strings.TrimSpace(ref.Instance),
		credentialMode: ref.CredentialMode,
	}
}

func agentToolTargetKeyFromTarget(target coreagent.ToolTarget) agentToolTargetKey {
	return agentToolTargetKey{
		system:         strings.TrimSpace(target.System),
		plugin:         strings.TrimSpace(target.Plugin),
		operation:      strings.TrimSpace(target.Operation),
		connection:     core.ResolveConnectionAlias(strings.TrimSpace(target.Connection)),
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

func (m *Manager) searchToolCandidates(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef, query string, skipUnavailable bool) ([]agentToolSearchCandidate, []agentToolUnavailableCandidate, error) {
	candidates := make([]agentToolSearchCandidate, 0)
	unavailable := make([]agentToolUnavailableCandidate, 0)
	err := m.visitToolSearchCandidates(ctx, p, refs, query, skipUnavailable, true,
		func(candidate agentToolSearchCandidate) (bool, error) {
			candidates = append(candidates, candidate)
			return true, nil
		},
		func(candidate agentToolUnavailableCandidate) (bool, error) {
			unavailable = append(unavailable, candidate)
			return true, nil
		},
	)
	if err != nil {
		return nil, nil, err
	}
	ranked, err := rankAgentToolSearchCandidates(query, candidates)
	if err != nil {
		return nil, nil, err
	}
	return ranked, unavailable, nil
}

func (m *Manager) visitToolSearchCandidates(
	ctx context.Context,
	p *principal.Principal,
	refs []coreagent.ToolRef,
	query string,
	skipUnavailable bool,
	allowQueryProviderNarrowing bool,
	visitCandidate func(agentToolSearchCandidate) (bool, error),
	visitUnavailable func(agentToolUnavailableCandidate) (bool, error),
) error {
	scope := newAgentToolSearchScope(refs)
	providerNames := scope.providerNames()
	if len(providerNames) == 0 {
		if !scope.all {
			return nil
		}
		providerNames = m.providers.List()
	}
	query = strings.TrimSpace(query)
	if scope.all && allowQueryProviderNarrowing {
		if mentioned := mentionedAgentToolSearchProviders(query, providerNames); len(mentioned) > 0 {
			providerNames = mentioned
		}
	}
	seenCandidates := false
	seenUnavailable := false
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
			return fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
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
					if principal.AllowsProviderPermission(p, pluginName) {
						seenUnavailable = true
						if visitUnavailable != nil {
							keepGoing, visitErr := visitUnavailable(unavailableAgentToolCandidate(searchRef, err))
							if visitErr != nil {
								return visitErr
							}
							if !keepGoing {
								return nil
							}
						}
					}
					continue
				}
				return err
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
					seenCandidates = true
					if visitCandidate == nil {
						continue
					}
					keepGoing, visitErr := visitCandidate(agentToolSearchCandidate{
						ref:             ref,
						catalog:         cat,
						operation:       op,
						skipUnavailable: skipUnavailable && agentToolSearchRefSkipsUnavailable(searchRef),
					})
					if visitErr != nil {
						return visitErr
					}
					if !keepGoing {
						return nil
					}
				}
			}
		}
	}
	if !seenCandidates && !seenUnavailable && !scope.all && firstUnavailableErr != nil {
		return firstUnavailableErr
	}
	return nil
}

func agentToolSearchUnavailable(err error) bool {
	return errors.Is(err, invocation.ErrNoCredential) ||
		errors.Is(err, invocation.ErrAmbiguousInstance) ||
		errors.Is(err, invocation.ErrReconnectRequired) ||
		errors.Is(err, invocation.ErrNotAuthenticated) ||
		errors.Is(err, invocation.ErrScopeDenied)
}

func unavailableAgentToolCandidate(ref coreagent.ToolRef, err error) agentToolUnavailableCandidate {
	ref.Plugin = strings.TrimSpace(ref.Plugin)
	ref.Operation = ""
	ref.Title = ""
	ref.Description = ""
	reason := unavailableAgentToolReason(err)
	return agentToolUnavailableCandidate{
		ref:     ref,
		err:     err,
		reason:  reason,
		message: unavailableAgentToolMessage(ref.Plugin, reason, err),
	}
}

func listedAgentToolUnavailable(tool coreagent.ListedTool) bool {
	return tool.Target.Unavailable != nil
}

func unavailableAgentToolReason(err error) string {
	switch {
	case errors.Is(err, invocation.ErrAmbiguousInstance):
		return coreagent.ToolUnavailableReasonInstanceRequired
	case errors.Is(err, invocation.ErrScopeDenied):
		return coreagent.ToolUnavailableReasonScopeDenied
	case errors.Is(err, invocation.ErrNotAuthenticated):
		return coreagent.ToolUnavailableReasonNotAuthenticated
	case errors.Is(err, invocation.ErrNoCredential):
		return coreagent.ToolUnavailableReasonNoCredential
	default:
		return coreagent.ToolUnavailableReasonReconnectRequired
	}
}

func unavailableAgentToolTitle(pluginName, reason string) string {
	pluginName = strings.TrimSpace(pluginName)
	switch reason {
	case coreagent.ToolUnavailableReasonInstanceRequired:
		return pluginName + " instance required"
	case coreagent.ToolUnavailableReasonScopeDenied:
		return pluginName + " scope denied"
	case coreagent.ToolUnavailableReasonNotAuthenticated:
		return pluginName + " authentication required"
	case coreagent.ToolUnavailableReasonNoCredential:
		return pluginName + " connection required"
	default:
		return pluginName + " reconnect required"
	}
}

func unavailableAgentToolMessage(pluginName, reason string, err error) string {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		pluginName = "this integration"
	}
	switch reason {
	case coreagent.ToolUnavailableReasonInstanceRequired:
		return fmt.Sprintf("%s has multiple matching instances. Ask the user to choose or reconnect a specific instance before using these tools.", pluginName)
	case coreagent.ToolUnavailableReasonScopeDenied:
		return fmt.Sprintf("%s is connected but is missing required OAuth scopes. Ask the user to reconnect %s with the required scopes before using these tools.", pluginName, pluginName)
	case coreagent.ToolUnavailableReasonNotAuthenticated, coreagent.ToolUnavailableReasonNoCredential, coreagent.ToolUnavailableReasonReconnectRequired:
		return fmt.Sprintf("%s is not connected, its credentials expired, or refresh failed. Ask the user to reconnect %s before using these tools.", pluginName, pluginName)
	default:
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%s is unavailable.", pluginName)
	}
}

func agentToolSearchRefSkipsUnavailable(ref coreagent.ToolRef) bool {
	return strings.TrimSpace(ref.Operation) == ""
}

func (m *Manager) catalogsForAgentToolSearch(ctx context.Context, p *principal.Principal, prov core.Provider, pluginName string, ref coreagent.ToolRef) ([]agentToolSearchCatalog, error) {
	connection := strings.TrimSpace(ref.Connection)
	if connection != "" && !core.SafeConnectionValue(connection) {
		return nil, fmt.Errorf("connection name contains invalid characters")
	}
	instance := strings.TrimSpace(ref.Instance)
	if instance != "" && !core.SafeInstanceValue(instance) {
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
		if target.Connection != "" && !core.SafeConnectionValue(target.Connection) {
			return nil, fmt.Errorf("connection name contains invalid characters")
		}
		if target.Instance != "" && !core.SafeInstanceValue(target.Instance) {
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
	refs := []coreagent.ToolRef{}
	if s.all {
		refs = append(refs, coreagent.ToolRef{Plugin: pluginName})
	}
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
		if !m.allowsAgentProvider(ctx, p, pluginName) {
			return fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, pluginName)
		}
		connection := strings.TrimSpace(ref.Connection)
		if connection != "" && !core.SafeConnectionValue(connection) {
			return fmt.Errorf("connection name contains invalid characters")
		}
		instance := strings.TrimSpace(ref.Instance)
		if instance != "" && !core.SafeInstanceValue(instance) {
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

func (m *Manager) allowsAgentProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	return m.allowProvider(ctx, p, provider) && principal.AllowsProviderPermission(p, provider)
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
	if len(op.InputSchema) <= agentToolSchemaMaxBytes {
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

func agentToolOutputSchemaJSON(op catalog.CatalogOperation) string {
	if len(op.OutputSchema) == 0 || len(op.OutputSchema) > agentToolSchemaMaxBytes {
		return ""
	}
	return string(op.OutputSchema)
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

func (m *Manager) listedAgentPluginCandidateTool(candidate agentToolSearchCandidate) (coreagent.ListedTool, error) {
	ref := candidate.ref
	target := coreagent.ToolTarget{
		Plugin:         strings.TrimSpace(ref.Plugin),
		Operation:      strings.TrimSpace(ref.Operation),
		Connection:     core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		Instance:       strings.TrimSpace(ref.Instance),
		CredentialMode: ref.CredentialMode,
	}
	toolID, err := m.mintAgentToolID(target)
	if err != nil {
		return coreagent.ListedTool{}, err
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = strings.TrimSpace(candidate.operation.Title)
	}
	if name == "" {
		name = target.Plugin + "." + candidate.operation.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(candidate.operation.Description)
	}
	ref.Connection = target.Connection
	ref.Instance = target.Instance
	ref.CredentialMode = target.CredentialMode
	return coreagent.ListedTool{
		ToolID:           toolID,
		MCPName:          agentToolMCPName(target),
		Title:            name,
		Description:      description,
		Tags:             append([]string(nil), candidate.operation.Tags...),
		SearchText:       agentToolSearchMetadataText(candidate),
		InputSchemaJSON:  agentToolInputSchemaJSON(candidate.operation),
		OutputSchemaJSON: agentToolOutputSchemaJSON(candidate.operation),
		Annotations:      capabilityAnnotationsFromCatalog(candidate.operation.Annotations),
		Ref:              ref,
		Target:           target,
		Hidden:           !catalog.OperationVisibleByDefault(candidate.operation),
	}, nil
}

func (m *Manager) listedUnavailableAgentPluginTool(candidate agentToolUnavailableCandidate) (coreagent.ListedTool, error) {
	ref := candidate.ref
	ref.Plugin = strings.TrimSpace(ref.Plugin)
	ref.Operation = ""
	ref.Connection = core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection))
	ref.Instance = strings.TrimSpace(ref.Instance)
	ref.Title = ""
	ref.Description = ""
	target := coreagent.ToolTarget{
		Plugin:         ref.Plugin,
		Connection:     ref.Connection,
		Instance:       ref.Instance,
		CredentialMode: ref.CredentialMode,
		Unavailable: &coreagent.UnavailableToolTarget{
			Reason:  candidate.reason,
			Message: candidate.message,
		},
	}
	toolID, err := m.mintAgentToolID(target)
	if err != nil {
		return coreagent.ListedTool{}, err
	}
	return coreagent.ListedTool{
		ToolID:          toolID,
		MCPName:         agentUnavailableToolMCPName(target),
		Title:           unavailableAgentToolTitle(ref.Plugin, candidate.reason),
		Description:     candidate.message,
		InputSchemaJSON: `{"type":"object","properties":{},"additionalProperties":false}`,
		Annotations: core.CapabilityAnnotations{
			ReadOnlyHint:    agentToolBoolPtr(true),
			DestructiveHint: agentToolBoolPtr(false),
			OpenWorldHint:   agentToolBoolPtr(false),
		},
		Ref:    ref,
		Target: target,
	}, nil
}

func agentToolBoolPtr(value bool) *bool {
	return &value
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
	order := make([]int, len(tools))
	for i := range tools {
		order[i] = i
	}
	assignUniqueListedAgentToolNamesInOrder(tools, order)
}

func assignStableUniqueListedAgentToolNames(tools []coreagent.ListedTool) {
	order := make([]int, len(tools))
	for i := range tools {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return listedAgentToolSortLess(tools[order[i]], tools[order[j]])
	})
	assignUniqueListedAgentToolNamesInOrder(tools, order)
}

func listedAgentToolSortLess(a, b coreagent.ListedTool) bool {
	if leftUnavailable, rightUnavailable := listedAgentToolUnavailable(a), listedAgentToolUnavailable(b); leftUnavailable != rightUnavailable {
		return !leftUnavailable
	}
	if a.MCPName != b.MCPName {
		return a.MCPName < b.MCPName
	}
	if a.Target.System != b.Target.System {
		return a.Target.System < b.Target.System
	}
	if a.Target.Plugin != b.Target.Plugin {
		return a.Target.Plugin < b.Target.Plugin
	}
	if a.Target.Operation != b.Target.Operation {
		return a.Target.Operation < b.Target.Operation
	}
	if a.Target.Connection != b.Target.Connection {
		return a.Target.Connection < b.Target.Connection
	}
	if a.Target.Instance != b.Target.Instance {
		return a.Target.Instance < b.Target.Instance
	}
	if a.Target.CredentialMode != b.Target.CredentialMode {
		return a.Target.CredentialMode < b.Target.CredentialMode
	}
	return a.ToolID < b.ToolID
}

func assignUniqueListedAgentToolNamesInOrder(tools []coreagent.ListedTool, order []int) {
	used := make(map[string]struct{}, len(tools))
	nextSuffix := make(map[string]int, len(tools))
	for _, i := range order {
		if i < 0 || i >= len(tools) {
			continue
		}
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
	if target.Unavailable != nil {
		return agentUnavailableToolMCPName(target)
	}
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

func agentUnavailableToolMCPName(target coreagent.ToolTarget) string {
	reason := coreagent.ToolUnavailableReasonReconnectRequired
	if target.Unavailable != nil && strings.TrimSpace(target.Unavailable.Reason) != "" {
		reason = strings.TrimSpace(target.Unavailable.Reason)
	}
	return agentToolMCPName(coreagent.ToolTarget{
		Plugin:     strings.TrimSpace(target.Plugin),
		Operation:  reason,
		Connection: core.ResolveConnectionAlias(strings.TrimSpace(target.Connection)),
		Instance:   strings.TrimSpace(target.Instance),
	})
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
	if m == nil || m.runGrants == nil {
		return "", fmt.Errorf("%w: agent run grants are not configured", invocation.ErrInternal)
	}
	id, err := m.runGrants.MintToolID(target)
	if err != nil {
		return "", fmt.Errorf("%w: mint agent tool id: %v", invocation.ErrInternal, err)
	}
	return id, nil
}

func validateToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = normalizeToolSource(source)
	switch source {
	case coreagent.ToolSourceModeMCPCatalog:
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", source)
	}
	return source, nil
}

func validateProviderTurnToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = coreagent.ToolSourceMode(strings.TrimSpace(string(source)))
	switch source {
	case coreagent.ToolSourceModeUnspecified, coreagent.ToolSourceModeMCPCatalog:
		return source, nil
	default:
		return "", fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInvalidInvocation, source)
	}
}

func defaultAgentTurnToolSource(ctx context.Context, provider coreagent.Provider) coreagent.ToolSourceMode {
	if provider == nil {
		return coreagent.ToolSourceModeUnspecified
	}
	caps, err := provider.GetCapabilities(ctx, coreagent.GetCapabilitiesRequest{})
	if err != nil {
		return coreagent.ToolSourceModeUnspecified
	}
	if agentProviderCapabilitiesSupportToolSource(caps, coreagent.ToolSourceModeMCPCatalog) {
		return coreagent.ToolSourceModeMCPCatalog
	}
	return coreagent.ToolSourceModeUnspecified
}

func (m *Manager) defaultAgentTurnToolRefs(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) []coreagent.ToolRef {
	broadRefs := []coreagent.ToolRef{{Plugin: agentToolSearchAllPlugin}}
	if m == nil || m.providers == nil {
		return broadRefs
	}
	if strings.TrimSpace(req.CallerPluginName) != "" {
		return broadRefs
	}
	latestUserText := latestAgentUserMessageText(req.Messages)
	if strings.TrimSpace(latestUserText) == "" {
		return broadRefs
	}
	mentionedProviders := m.exactMentionedAgentToolProviders(ctx, p, latestUserText)
	if len(mentionedProviders) == 0 {
		return broadRefs
	}
	largeCatalog, err := m.agentToolCandidateCountExceeds(ctx, p, broadRefs, m.defaultToolNarrowingThreshold)
	if err != nil || !largeCatalog {
		return broadRefs
	}

	narrowedRefs := make([]coreagent.ToolRef, 0, len(mentionedProviders))
	for _, pluginName := range mentionedProviders {
		hasCandidate, err := m.agentToolVisibleCandidateCountExceeds(ctx, p, []coreagent.ToolRef{{Plugin: pluginName}}, 0)
		if err != nil {
			return broadRefs
		}
		if hasCandidate {
			narrowedRefs = append(narrowedRefs, coreagent.ToolRef{Plugin: pluginName})
		}
	}
	if len(narrowedRefs) == 0 {
		return broadRefs
	}
	return narrowedRefs
}

func latestAgentUserMessageText(messages []coreagent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		parts := make([]string, 0, 1+len(msg.Parts))
		if text := strings.TrimSpace(msg.Text); text != "" {
			parts = append(parts, text)
		}
		for j := range msg.Parts {
			part := msg.Parts[j]
			if part.Type != coreagent.MessagePartTypeText {
				continue
			}
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func (m *Manager) exactMentionedAgentToolProviders(ctx context.Context, p *principal.Principal, text string) []string {
	normalizedText := normalizeAgentToolMentionText(text)
	if normalizedText == "" || m == nil || m.providers == nil {
		return nil
	}
	out := make([]string, 0)
	for _, pluginName := range m.providers.List() {
		pluginName = strings.TrimSpace(pluginName)
		if pluginName == "" {
			continue
		}
		prov, err := m.providers.Get(pluginName)
		if err != nil {
			continue
		}
		if !m.allowsAgentProvider(ctx, p, pluginName) {
			continue
		}
		aliases := []string{pluginName}
		if displayName := strings.TrimSpace(prov.DisplayName()); displayName != "" {
			aliases = append(aliases, displayName)
		}
		for _, alias := range aliases {
			if exactAgentToolMention(normalizedText, alias) {
				out = append(out, pluginName)
				break
			}
		}
	}
	return out
}

func exactAgentToolMention(normalizedText, alias string) bool {
	normalizedAlias := normalizeAgentToolMentionText(alias)
	if normalizedAlias == "" {
		return false
	}
	return strings.Contains(" "+normalizedText+" ", " "+normalizedAlias+" ")
}

func normalizeAgentToolMentionText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSeparator := true
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastSeparator = false
		default:
			if !lastSeparator {
				b.WriteByte(' ')
				lastSeparator = true
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (m *Manager) agentToolCandidateCountExceeds(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef, threshold int) (bool, error) {
	if threshold < 0 {
		threshold = 0
	}
	count := 0
	exceeded := false
	visit := func() (bool, error) {
		count++
		if count > threshold {
			exceeded = true
			return false, nil
		}
		return true, nil
	}
	err := m.visitToolSearchCandidates(ctx, p, refs, "", true, false,
		func(agentToolSearchCandidate) (bool, error) {
			return visit()
		},
		func(agentToolUnavailableCandidate) (bool, error) {
			return visit()
		},
	)
	if err != nil {
		return false, err
	}
	return exceeded, nil
}

func (m *Manager) agentToolVisibleCandidateCountExceeds(ctx context.Context, p *principal.Principal, refs []coreagent.ToolRef, threshold int) (bool, error) {
	if threshold < 0 {
		threshold = 0
	}
	count := 0
	exceeded := false
	err := m.visitToolSearchCandidates(ctx, p, refs, "", true, false,
		func(agentToolSearchCandidate) (bool, error) {
			count++
			if count > threshold {
				exceeded = true
				return false, nil
			}
			return true, nil
		},
		nil,
	)
	if err != nil {
		return false, err
	}
	return exceeded, nil
}

func agentRunPermissions(ctx context.Context, p *principal.Principal, callerPluginName string, refs []coreagent.ToolRef) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	if permissions, ok := compactAgentRunPermissionsForRefs(p, refs); ok {
		return permissions
	}
	if shouldUseResolvedUserToolScope(ctx, p, callerPluginName, refs) {
		return nil
	}
	return principal.PermissionsToAccessPermissions(p.TokenPermissions)
}

func compactAgentRunPermissionsForRefs(p *principal.Principal, refs []coreagent.ToolRef) ([]core.AccessPermission, bool) {
	if len(refs) == 0 {
		return nil, false
	}
	operationsByPlugin := map[string]map[string]struct{}{}
	providerWide := map[string]struct{}{}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		plugin := strings.TrimSpace(ref.Plugin)
		if plugin == "" || plugin == agentToolSearchAllPlugin || strings.Contains(plugin, "*") {
			return nil, false
		}
		operation := strings.TrimSpace(ref.Operation)
		if strings.Contains(operation, "*") {
			return nil, false
		}
		if operation == "" {
			providerWide[plugin] = struct{}{}
			delete(operationsByPlugin, plugin)
			continue
		}
		if _, ok := providerWide[plugin]; ok {
			continue
		}
		ops := operationsByPlugin[plugin]
		if ops == nil {
			ops = map[string]struct{}{}
			operationsByPlugin[plugin] = ops
		}
		ops[operation] = struct{}{}
	}
	if len(providerWide) == 0 && len(operationsByPlugin) == 0 {
		return nil, false
	}
	plugins := make([]string, 0, len(providerWide)+len(operationsByPlugin))
	for plugin := range providerWide {
		plugins = append(plugins, plugin)
	}
	for plugin := range operationsByPlugin {
		if _, ok := providerWide[plugin]; !ok {
			plugins = append(plugins, plugin)
		}
	}
	sort.Strings(plugins)
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, plugin := range plugins {
		if _, ok := providerWide[plugin]; ok {
			if p != nil && p.TokenPermissions != nil {
				tokenOps, ok := p.TokenPermissions[plugin]
				if !ok {
					return nil, false
				}
				perm := core.AccessPermission{Plugin: plugin}
				if len(tokenOps) > 0 {
					ops := make([]string, 0, len(tokenOps))
					for operation := range tokenOps {
						ops = append(ops, operation)
					}
					sort.Strings(ops)
					perm.Operations = ops
				}
				out = append(out, perm)
				continue
			}
			out = append(out, core.AccessPermission{Plugin: plugin})
			continue
		}
		ops := make([]string, 0, len(operationsByPlugin[plugin]))
		for operation := range operationsByPlugin[plugin] {
			ops = append(ops, operation)
		}
		sort.Strings(ops)
		out = append(out, core.AccessPermission{
			Plugin:     plugin,
			Operations: ops,
		})
	}
	return out, true
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
		return coreagent.ToolSourceModeMCPCatalog
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
			if ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" || ref.Title != "" || ref.Description != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] system refs cannot include connection, instance, credential mode, title, or description", invocation.ErrInvalidInvocation, idx)
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
			mode, err := normalizeAgentToolCredentialMode(invoke.CredentialMode)
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
	return false
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
	return errAgentBoundedListUnsupported(providerName)
}

func errAgentBoundedListUnsupported(providerName string) error {
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
