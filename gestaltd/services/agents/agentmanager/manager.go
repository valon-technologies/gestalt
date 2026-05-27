package agentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
	integration "github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/services/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

var (
	ErrAgentNotConfigured               = errors.New("agent is not configured")
	ErrAgentProviderRequired            = errors.New("agent provider is required")
	ErrAgentProviderNotAvailable        = errors.New("agent provider is not available")
	ErrAgentSubjectRequired             = errors.New("agent subject is required")
	ErrAgentCallerAppRequired           = errors.New("agent caller app is required for inherited tools")
	ErrAgentInheritedSurfaceTool        = errors.New("agent inherited surface tools are not supported")
	ErrAgentInteractionRequired         = errors.New("agent interaction is required")
	ErrAgentInteractionNotFound         = errors.New("agent interaction is not found")
	ErrAgentSessionNotFound             = errors.New("agent session is not found")
	ErrAgentWorkflowToolsNotConfigured  = errors.New("agent workflow tools are not configured")
	ErrAgentBoundedListUnsupported      = errors.New("agent provider does not support bounded list hydration")
	ErrAgentSessionStartUnsupported     = errors.New("agent provider does not support session start hooks")
	ErrAgentWorkspaceUnsupported        = errors.New("agent provider does not support workspaces")
	ErrAgentWorkspaceInvalid            = errors.New("agent workspace is invalid")
	ErrAgentSessionMetadataInvalid      = errors.New("agent session metadata is invalid")
	ErrAgentInvalidListRequest          = errors.New("agent list request is invalid")
	ErrAgentStructuredOutputUnsupported = errors.New("agent provider does not support structured output")
)

const (
	agentToolSearchAllApp        = "*"
	agentToolListDefaultPageSize = 100
	agentToolListMaxPageSize     = 1000
	agentToolSchemaMaxBytes      = 128 * 1024
	agentDefaultToolNarrowingK   = 200
	AgentListSummaryDefaultLimit = 100
	AgentListMaxLimit            = 500
)

type callerAppNameContextKey struct{}

func WithCallerAppName(ctx context.Context, callerAppName string) context.Context {
	callerAppName = strings.TrimSpace(callerAppName)
	if callerAppName == "" {
		return ctx
	}
	return context.WithValue(ctx, callerAppNameContextKey{}, callerAppName)
}

func callerAppNameFromContext(ctx context.Context) string {
	value, _ := ctx.Value(callerAppNameContextKey{}).(string)
	return strings.TrimSpace(value)
}

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
	CreateSession(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error)
	GetSession(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error)
	ListSessions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error)
	UpdateSession(ctx context.Context, p *principal.Principal, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error)
	CreateTurn(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error)
	GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error)
	ListTurns(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error)
	CancelTurn(ctx context.Context, p *principal.Principal, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error)
	ListTurnEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error)
	ListInteractions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error)
	ResolveInteraction(ctx context.Context, p *principal.Principal, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error)
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
	AppInvokes        map[string][]invocation.AppInvocationDependency
	AgentConnections  map[string][]string
	SessionStart      map[string]*coreagent.SessionStartConfig
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
	appInvokes                    map[string][]invocation.AppInvocationDependency
	agentConnections              map[string][]string
	sessionStart                  map[string]*coreagent.SessionStartConfig
	defaultToolNarrowingThreshold int
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
		appInvokes:        invocation.CloneAppInvocationDependencyMap(cfg.AppInvokes),
		agentConnections:  cloneStringSliceMap(cfg.AgentConnections),
		sessionStart:      cloneSessionStartConfigMap(cfg.SessionStart),
		defaultToolNarrowingThreshold: effectiveAgentToolNarrowingThreshold(
			cfg.DefaultToolNarrowingThreshold,
		),
	}
}

func cloneSessionStartConfigMap(src map[string]*coreagent.SessionStartConfig) map[string]*coreagent.SessionStartConfig {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]*coreagent.SessionStartConfig, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		dst[key] = cloneSessionStartConfig(value)
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func cloneSessionStartConfig(src *coreagent.SessionStartConfig) *coreagent.SessionStartConfig {
	if src == nil {
		return nil
	}
	dst := &coreagent.SessionStartConfig{Hooks: make([]coreagent.SessionStartHook, len(src.Hooks))}
	for i := range src.Hooks {
		hook := src.Hooks[i]
		hook.Command = append([]string(nil), hook.Command...)
		hook.Env = maps.Clone(hook.Env)
		dst.Hooks[i] = hook
	}
	return dst
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

func cloneAgentRequest[T interface {
	gproto.Message
	comparable
}](req T, empty T) T {
	var zero T
	if req == zero {
		return empty
	}
	return gproto.Clone(req).(T)
}

func agentCallerAppName(ctx context.Context) string {
	return callerAppNameFromContext(ctx)
}

func agentActorToProto(actor coreagent.Actor) *proto.AgentActor {
	if actor == (coreagent.Actor{}) {
		return nil
	}
	return &proto.AgentActor{
		SubjectId:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func agentSubjectToProto(subject core.RunAsSubject) *proto.SubjectContext {
	if subject == (core.RunAsSubject{}) {
		return nil
	}
	return &proto.SubjectContext{
		Id:                  subject.SubjectID,
		Kind:                subject.SubjectKind,
		CredentialSubjectId: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
	}
}

func agentWorkspaceFromProto(workspace *proto.AgentWorkspace) *coreagent.Workspace {
	if workspace == nil {
		return nil
	}
	out := &coreagent.Workspace{
		Checkouts: make([]coreagent.WorkspaceGitCheckout, 0, len(workspace.GetCheckouts())),
		CWD:       workspace.GetCwd(),
	}
	for _, checkout := range workspace.GetCheckouts() {
		if checkout == nil {
			continue
		}
		out.Checkouts = append(out.Checkouts, coreagent.WorkspaceGitCheckout{
			URL:  checkout.GetUrl(),
			Ref:  checkout.GetRef(),
			Path: checkout.GetPath(),
		})
	}
	return out
}

func agentWorkspaceToProto(workspace *coreagent.Workspace) *proto.AgentWorkspace {
	if workspace == nil {
		return nil
	}
	out := &proto.AgentWorkspace{
		Checkouts: make([]*proto.AgentWorkspaceGitCheckout, 0, len(workspace.Checkouts)),
		Cwd:       workspace.CWD,
	}
	for _, checkout := range workspace.Checkouts {
		out.Checkouts = append(out.Checkouts, &proto.AgentWorkspaceGitCheckout{
			Url:  checkout.URL,
			Ref:  checkout.Ref,
			Path: checkout.Path,
		})
	}
	return out
}

func sessionStartConfigToProto(value *coreagent.SessionStartConfig) *proto.AgentSessionStartConfig {
	if value == nil {
		return nil
	}
	out := &proto.AgentSessionStartConfig{Hooks: make([]*proto.AgentSessionStartHook, 0, len(value.Hooks))}
	for _, hook := range value.Hooks {
		out.Hooks = append(out.Hooks, &proto.AgentSessionStartHook{
			Id:      hook.ID,
			Type:    hook.Type,
			Command: append([]string(nil), hook.Command...),
			Cwd:     hook.CWD,
			Timeout: hook.Timeout,
			Env:     maps.Clone(hook.Env),
			Output: &proto.AgentSessionStartHookOutput{
				AdditionalContext: hook.Output.AdditionalContext,
				Metadata:          hook.Output.Metadata,
			},
		})
	}
	return out
}

func agentSessionStateFromProto(state proto.AgentSessionState) (coreagent.SessionState, error) {
	switch state {
	case proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED:
		return "", nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive, nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived, nil
	default:
		return "", fmt.Errorf("unknown agent session state %v", state)
	}
}

func agentExecutionStatusFromProto(status proto.AgentExecutionStatus) (coreagent.ExecutionStatus, error) {
	switch status {
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED:
		return "", nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING:
		return coreagent.ExecutionStatusPending, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING:
		return coreagent.ExecutionStatusRunning, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED:
		return coreagent.ExecutionStatusSucceeded, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED:
		return coreagent.ExecutionStatusFailed, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED:
		return coreagent.ExecutionStatusCanceled, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT:
		return coreagent.ExecutionStatusWaitingForInput, nil
	default:
		return "", fmt.Errorf("unknown agent execution status %v", status)
	}
}

func agentToolSourceModeFromProtoStrict(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	switch mode {
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED:
		return coreagent.ToolSourceModeUnspecified
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG:
		return coreagent.ToolSourceModeMCPCatalog
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE:
		return coreagent.ToolSourceModeNone
	default:
		return coreagent.ToolSourceMode(fmt.Sprintf("unknown:%d", mode))
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
	if toolSource == coreagent.ToolSourceModeNone {
		return nil, fmt.Errorf("%w: toolRefs are not supported with agent tool source %q", invocation.ErrInvalidInvocation, toolSource)
	}
	refs, err := normalizeToolRefs(req.ToolRefs)
	if err != nil {
		return nil, err
	}
	refs, err = m.applyCallerInvokePolicies(req.CallerAppName, refs)
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
	appTools, _, err := m.resolveAgentToolCandidates(ctx, p, candidates, 0, false)
	if err != nil {
		return nil, err
	}
	tools = append([]coreagent.Tool(nil), systemTools...)
	tools = append(tools, appTools...)
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

func (m *Manager) CreateSession(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "create_session")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.CreateAgentProviderSessionRequest{}
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	providerName, provider, err := m.resolveProviderSelection(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(providerName))
	if !m.allowsAgentProvider(ctx, p, providerName) {
		return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
	}
	metadata := protoutil.MapFromStruct(req.GetMetadata())
	if err := validateAgentSessionUserMetadata(metadata); err != nil {
		return nil, err
	}
	workspace, err := coreagent.NormalizeWorkspace(agentWorkspaceFromProto(req.GetWorkspace()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentWorkspaceInvalid, err)
	}
	if workspace != nil {
		workspaceProvider, ok := provider.(coreagent.WorkspaceProvider)
		if !ok || !workspaceProvider.SupportsWorkspaceRequests() {
			return nil, errAgentWorkspaceUnsupported(providerName)
		}
	}
	sessionStart, err := m.sessionStartForProvider(ctx, providerName, provider)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	sessionID := newAgentSessionID(providerName, subjectID, idempotencyKey, workspace != nil)
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderSessionRequest{})
	providerReq.SessionId = sessionID
	providerReq.IdempotencyKey = idempotencyKey
	providerReq.ProviderName = providerName
	providerReq.Model = strings.TrimSpace(req.GetModel())
	providerReq.ClientRef = strings.TrimSpace(req.GetClientRef())
	providerReq.Metadata = req.GetMetadata()
	providerReq.CreatedBy = agentActorToProto(agentActorFromPrincipal(p))
	providerReq.Subject = agentSubjectToProto(agentSubjectFromPrincipal(p))
	providerReq.SessionStart = sessionStartConfigToProto(sessionStart)
	providerReq.Workspace = agentWorkspaceToProto(workspace)
	providerReq.PreparedWorkspace = nil
	providerReq.InvocationToken = ""
	session, err = provider.CreateSession(ctx, providerReq)
	if err != nil {
		if sessionStart != nil || workspace != nil {
			return nil, err
		}
		fallback, getErr := provider.GetSession(ctx, &proto.GetAgentProviderSessionRequest{
			SessionId: sessionID,
			Subject:   agentSubjectToProto(agentSubjectFromPrincipal(p)),
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
	if workspace != nil && idempotencyKey != "" && strings.TrimSpace(normalized.ID) != sessionID {
		return nil, fmt.Errorf("agent provider returned session id %q, want workspace idempotency id %q", normalized.ID, sessionID)
	}
	if !providerSessionOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	return normalized, nil
}

func (m *Manager) GetSession(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "get_session")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.GetAgentProviderSessionRequest{}
	}
	owned, err := m.findAccessibleSession(ctx, p, req.GetSessionId(), "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	return owned.session, nil
}

func (m *Manager) ListSessions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) (sessions []*coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "list_sessions")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ListAgentProviderSessionsRequest{}
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrInvalidInvocation, err)
	}
	providerName := strings.TrimSpace(req.GetProviderName())
	if providerName != "" {
		observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(providerName))
	}
	limit, err := normalizeAgentListLimit(int(req.GetLimit()), req.GetSummaryOnly())
	if err != nil {
		return nil, err
	}
	if len(req.GetSessionIds()) > 0 {
		return m.listExactSessions(ctx, p, providerName, req.GetSessionIds(), state, limit, req.GetSummaryOnly())
	}
	candidates, err := m.authorizedProviderCandidates(ctx, p, providerName)
	if err != nil {
		if !errors.Is(err, invocation.ErrAuthorizationDenied) {
			return nil, err
		}
		candidates = nil
	}
	requireBounded := req.GetSummaryOnly() || limit > 0
	out := make([]*coreagent.Session, 0)
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if requireBounded {
			if err := requireAgentProviderBoundedListHydration(ctx, candidate.name, candidate.provider); err != nil {
				return nil, err
			}
		}
		sessions, err := candidate.provider.ListSessions(ctx, &proto.ListAgentProviderSessionsRequest{
			Subject:     agentSubjectToProto(agentSubjectFromPrincipal(p)),
			State:       req.GetState(),
			Limit:       int32(limit),
			SummaryOnly: req.GetSummaryOnly(),
		})
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if session == nil {
				continue
			}
			normalized, err := normalizeProviderSession(candidate.name, strings.TrimSpace(session.ID), session)
			if err != nil {
				return nil, err
			}
			if state != "" && normalized.State != state {
				continue
			}
			if _, ok := seen[normalized.ID]; ok {
				continue
			}
			seen[normalized.ID] = struct{}{}
			if req.GetSummaryOnly() {
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

func (m *Manager) listExactSessions(ctx context.Context, p *principal.Principal, providerName string, sessionIDs []string, state coreagent.SessionState, limit int, summaryOnly bool) ([]*coreagent.Session, error) {
	out := make([]*coreagent.Session, 0, len(sessionIDs))
	seen := map[string]struct{}{}
	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		owned, err := m.findAccessibleSession(ctx, p, sessionID, providerName)
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			return nil, err
		}
		if state != "" && owned.session.State != state {
			continue
		}
		session := owned.session
		if summaryOnly {
			session = summarizeAgentSession(session)
		}
		out = append(out, session)
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

func (m *Manager) UpdateSession(ctx context.Context, p *principal.Principal, req *proto.UpdateAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "update_session")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.UpdateAgentProviderSessionRequest{}
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrInvalidInvocation, err)
	}
	owned, err := m.findOwnedSession(ctx, p, req.GetSessionId(), "")
	if err != nil {
		return nil, err
	}
	metadata := protoutil.MapFromStruct(req.GetMetadata())
	if err := validateAgentSessionUserMetadata(metadata); err != nil {
		return nil, err
	}
	metadata = mergeReservedLifecycleMetadata(metadata, owned.session.Metadata)
	providerMetadata, err := protoutil.StructFromMap(metadata)
	if err != nil {
		return nil, fmt.Errorf("agent session metadata: %w", err)
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	providerReq := cloneAgentRequest(req, &proto.UpdateAgentProviderSessionRequest{})
	providerReq.SessionId = strings.TrimSpace(req.GetSessionId())
	providerReq.ClientRef = strings.TrimSpace(req.GetClientRef())
	providerReq.State = req.GetState()
	providerReq.Metadata = providerMetadata
	providerReq.Subject = agentSubjectToProto(agentSubjectFromPrincipal(p))
	providerReq.InvocationToken = ""
	session, err = owned.provider.UpdateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	_ = state
	normalized, err := normalizeProviderSession(owned.providerName, owned.session.ID, session)
	if err != nil {
		return nil, err
	}
	if !providerSessionOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	return normalized, nil
}

func (m *Manager) CreateTurn(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "create_turn")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.CreateAgentProviderTurnRequest{}
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrAgentSubjectRequired
	}
	ownedSession, err := m.findOwnedSession(ctx, p, req.GetSessionId(), "")
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %w", ErrAgentSessionNotFound, err)
		}
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	toolRefs, err := normalizeToolRefs(agentwire.ToolRefsFromProto(req.GetToolRefs()))
	if err != nil {
		return nil, err
	}
	callerAppName := agentCallerAppName(ctx)
	toolRefs, err = m.applyCallerInvokePolicies(callerAppName, toolRefs)
	if err != nil {
		return nil, err
	}
	toolSource, err := validateProviderTurnToolSource(agentToolSourceModeFromProtoStrict(req.GetToolSource()))
	if err != nil {
		return nil, err
	}
	if toolSource == coreagent.ToolSourceModeUnspecified && len(toolRefs) > 0 {
		toolSource = coreagent.ToolSourceModeMCPCatalog
	}
	if toolSource == coreagent.ToolSourceModeUnspecified && len(toolRefs) == 0 && !req.GetToolRefsSet() && defaultAgentTurnToolSource(ctx, ownedSession.provider) == coreagent.ToolSourceModeMCPCatalog {
		toolSource = coreagent.ToolSourceModeMCPCatalog
		toolRefs = m.defaultAgentTurnToolRefs(ctx, p, callerAppName, agentwire.MessagesFromProto(req.GetMessages()))
	}
	toolRefsSet := req.GetToolRefsSet() || len(toolRefs) > 0
	responseSchema := protoutil.MapFromStruct(req.GetResponseSchema())
	if req.GetResponseSchema() != nil {
		if err := validateAgentResponseSchema(responseSchema); err != nil {
			return nil, err
		}
		if supported, err := agentProviderSupportsStructuredOutput(ctx, ownedSession.provider); err != nil {
			return nil, err
		} else if !supported {
			return nil, fmt.Errorf("%w: agent provider %q", ErrAgentStructuredOutputUnsupported, ownedSession.providerName)
		}
	}
	var tools []coreagent.Tool
	switch toolSource {
	case coreagent.ToolSourceModeMCPCatalog:
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
	case coreagent.ToolSourceModeNone:
		if len(toolRefs) > 0 {
			return nil, fmt.Errorf("%w: toolRefs are not supported with agent tool source %q", invocation.ErrInvalidInvocation, toolSource)
		}
		if supported, err := agentProviderSupportsToolSource(ctx, ownedSession.provider, toolSource); err != nil {
			return nil, err
		} else if !supported {
			return nil, fmt.Errorf("agent provider %q does not support tool source %q", ownedSession.providerName, toolSource)
		}
	case coreagent.ToolSourceModeUnspecified:
	default:
		return nil, fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInvalidInvocation, toolSource)
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	turnID := newAgentTurnID(ownedSession.session.ID, idempotencyKey)
	runGrant, err := m.mintRunGrant(ctx, p, ownedSession.providerName, ownedSession.session.ID, turnID, callerAppName, toolRefs, toolRefsSet, tools, toolSource)
	if err != nil {
		return nil, err
	}
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderTurnRequest{})
	providerReq.TurnId = turnID
	providerReq.SessionId = ownedSession.session.ID
	providerReq.IdempotencyKey = idempotencyKey
	providerReq.Model = strings.TrimSpace(req.GetModel())
	providerReq.ToolRefs = agentwire.ToolRefsToProto(toolRefs)
	providerReq.ToolRefsSet = toolRefsSet
	providerReq.ToolSource = agentwire.ToolSourceModeToProto(toolSource)
	providerReq.Tools = nil
	providerReq.CreatedBy = agentActorToProto(agentActorFromPrincipal(p))
	providerReq.ExecutionRef = turnID
	providerReq.Subject = agentSubjectToProto(agentSubjectFromPrincipal(p))
	providerReq.RunGrant = runGrant
	providerReq.InvocationToken = ""
	turn, err = ownedSession.provider.CreateTurn(ctx, providerReq)
	if err != nil {
		fallback, getErr := ownedSession.provider.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
			TurnId:  turnID,
			Subject: agentSubjectToProto(agentSubjectFromPrincipal(p)),
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
	return normalized, nil
}

func (m *Manager) GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "get_turn")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.GetAgentProviderTurnRequest{}
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), "", "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	return owned.turn, nil
}

func (m *Manager) ListTurns(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) (turns []*coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turns")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ListAgentProviderTurnsRequest{}
	}
	statusFilter, err := agentExecutionStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", invocation.ErrInvalidInvocation, err)
	}
	limit, err := normalizeAgentListLimit(int(req.GetLimit()), req.GetSummaryOnly())
	if err != nil {
		return nil, err
	}
	ownedSession, err := m.findAccessibleSession(ctx, p, req.GetSessionId(), "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	if req.GetSummaryOnly() || limit > 0 {
		if err := requireAgentProviderBoundedListHydration(ctx, ownedSession.providerName, ownedSession.provider); err != nil {
			return nil, err
		}
	}
	turns, err = ownedSession.provider.ListTurns(ctx, &proto.ListAgentProviderTurnsRequest{
		SessionId:   ownedSession.session.ID,
		Subject:     agentSubjectToProto(agentSubjectFromPrincipal(p)),
		TurnIds:     append([]string(nil), req.GetTurnIds()...),
		Status:      req.GetStatus(),
		Limit:       int32(limit),
		SummaryOnly: req.GetSummaryOnly(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*coreagent.Turn, 0, len(turns))
	for _, turn := range turns {
		if turn == nil {
			continue
		}
		normalized, err := normalizeProviderTurn(ownedSession.providerName, ownedSession.session.ID, strings.TrimSpace(turn.ID), turn)
		if err != nil {
			return nil, err
		}
		if statusFilter != "" && normalized.Status != statusFilter {
			continue
		}
		if req.GetSummaryOnly() {
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

func (m *Manager) CancelTurn(ctx context.Context, p *principal.Principal, req *proto.CancelAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "cancel_turn")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.CancelAgentProviderTurnRequest{}
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), "", "")
	if err != nil {
		return nil, err
	}
	if !owned.sessionOwned {
		return nil, core.ErrNotFound
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	providerReq := cloneAgentRequest(req, &proto.CancelAgentProviderTurnRequest{})
	providerReq.TurnId = strings.TrimSpace(req.GetTurnId())
	providerReq.Reason = strings.TrimSpace(req.GetReason())
	providerReq.Subject = agentSubjectToProto(agentSubjectFromPrincipal(p))
	providerReq.InvocationToken = ""
	turn, err = owned.provider.CancelTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderTurn(owned.providerName, owned.turn.SessionID, owned.turn.ID, turn)
	if err != nil {
		return nil, err
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
	return normalized, nil
}

func (m *Manager) ListTurnEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnEventsRequest) (events []*coreagent.TurnEvent, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turn_events")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ListAgentProviderTurnEventsRequest{}
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), "", "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	events, err = owned.provider.ListTurnEvents(ctx, &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   owned.turn.ID,
		AfterSeq: req.GetAfterSeq(),
		Limit:    req.GetLimit(),
		Subject:  agentSubjectToProto(agentSubjectFromPrincipal(p)),
	})
	if err != nil {
		return nil, err
	}
	return normalizeTurnEventsForDisplay(events), nil
}

func (m *Manager) ListInteractions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderInteractionsRequest) (out []*coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "list_interactions")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ListAgentProviderInteractionsRequest{}
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), "", "")
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	interactions, err := owned.provider.ListInteractions(ctx, &proto.ListAgentProviderInteractionsRequest{
		TurnId:  owned.turn.ID,
		Subject: agentSubjectToProto(agentSubjectFromPrincipal(p)),
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

func (m *Manager) ResolveInteraction(ctx context.Context, p *principal.Principal, req *proto.ResolveAgentProviderInteractionRequest) (interaction *coreagent.Interaction, err error) {
	ctx, finish := startAgentOperation(ctx, "resolve_interaction")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ResolveAgentProviderInteractionRequest{}
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), "", "")
	if err != nil {
		return nil, err
	}
	if !owned.sessionOwned {
		return nil, core.ErrNotFound
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	interactionID := strings.TrimSpace(req.GetInteractionId())
	if interactionID == "" {
		return nil, ErrAgentInteractionRequired
	}
	providerReq := cloneAgentRequest(req, &proto.ResolveAgentProviderInteractionRequest{})
	providerReq.InteractionId = interactionID
	providerReq.TurnId = owned.turn.ID
	providerReq.Subject = agentSubjectToProto(agentSubjectFromPrincipal(p))
	providerReq.InvocationToken = ""
	interaction, err = owned.provider.ResolveInteraction(ctx, providerReq)
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

type accessibleAgentSession struct {
	providerName string
	provider     coreagent.Provider
	session      *coreagent.Session
	owned        bool
}

type accessibleAgentTurn struct {
	providerName string
	provider     coreagent.Provider
	turn         *coreagent.Turn
	session      *coreagent.Session
	sessionOwned bool
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

func (m *Manager) directReadProviderCandidates(providerName string) ([]namedAgentProvider, error, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		candidates, err := m.providerCandidates(providerName)
		return candidates, nil, err
	}
	if m == nil || m.agent == nil {
		return nil, nil, ErrAgentNotConfigured
	}
	names := m.agent.ProviderNames()
	candidates := make([]namedAgentProvider, 0, len(names))
	var retainedErr error
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		provider, err := m.resolveProviderByName(name)
		if err != nil {
			if agentProviderReadFallbackAllowed(err) {
				retainedErr = retainAgentProviderReadError(retainedErr, err)
				continue
			}
			return nil, nil, err
		}
		candidates = append(candidates, namedAgentProvider{name: name, provider: provider})
	}
	return candidates, retainedErr, nil
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

func (m *Manager) findAccessibleSession(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*accessibleAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	return m.findAccessibleSessionInProviders(ctx, p, sessionID, providerName)
}

func (m *Manager) findAccessibleSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*accessibleAgentSession, error) {
	candidates, retainedErr, err := m.directReadProviderCandidates(providerName)
	if err != nil {
		return nil, err
	}
	candidates, err = filterProviderCandidatesByTokenPermission(p, candidates, providerName)
	if err != nil {
		return nil, err
	}
	var found *accessibleAgentSession
	authDenied := false
	for _, candidate := range candidates {
		if !m.allowsAgentProvider(ctx, p, candidate.name) {
			authDenied = true
			continue
		}
		session, err := candidate.provider.GetSession(ctx, &proto.GetAgentProviderSessionRequest{
			SessionId: sessionID,
			Subject:   agentSubjectToProto(agentSubjectFromPrincipal(p)),
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			if strings.TrimSpace(providerName) == "" && agentProviderReadFallbackAllowed(err) {
				retainedErr = retainAgentProviderReadError(retainedErr, err)
				continue
			}
			return nil, err
		}
		normalized, err := normalizeProviderSession(candidate.name, sessionID, session)
		if err != nil {
			return nil, err
		}
		if found != nil {
			return nil, fmt.Errorf("%w: agent session %q is present in multiple providers", invocation.ErrInternal, sessionID)
		}
		found = &accessibleAgentSession{
			providerName: candidate.name,
			provider:     candidate.provider,
			session:      normalized,
			owned:        providerSessionOwnedBy(normalized, p),
		}
	}
	if found == nil {
		if retainedErr != nil {
			return nil, retainedErr
		}
		if authDenied {
			if providerName = strings.TrimSpace(providerName); providerName != "" {
				return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
			}
			return nil, invocation.ErrAuthorizationDenied
		}
		return nil, core.ErrNotFound
	}
	return found, nil
}

func (m *Manager) findAccessibleTurn(ctx context.Context, p *principal.Principal, turnID, providerName, expectedSessionID string) (*accessibleAgentTurn, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, core.ErrNotFound
	}
	return m.findAccessibleTurnInProviders(ctx, p, turnID, providerName, expectedSessionID)
}

func (m *Manager) findAccessibleTurnInProviders(ctx context.Context, p *principal.Principal, turnID, providerName, expectedSessionID string) (*accessibleAgentTurn, error) {
	candidates, retainedErr, err := m.directReadProviderCandidates(providerName)
	if err != nil {
		return nil, err
	}
	candidates, err = filterProviderCandidatesByTokenPermission(p, candidates, providerName)
	if err != nil {
		return nil, err
	}
	var found *accessibleAgentTurn
	authDenied := false
	for _, candidate := range candidates {
		if !m.allowsAgentProvider(ctx, p, candidate.name) {
			authDenied = true
			continue
		}
		turn, err := candidate.provider.GetTurn(ctx, &proto.GetAgentProviderTurnRequest{
			TurnId:  turnID,
			Subject: agentSubjectToProto(agentSubjectFromPrincipal(p)),
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			if strings.TrimSpace(providerName) == "" && agentProviderReadFallbackAllowed(err) {
				retainedErr = retainAgentProviderReadError(retainedErr, err)
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
		session, err := m.findAccessibleSessionInProviders(ctx, p, normalized.SessionID, candidate.name)
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			if strings.TrimSpace(providerName) == "" && agentProviderReadFallbackAllowed(err) {
				retainedErr = retainAgentProviderReadError(retainedErr, err)
				continue
			}
			return nil, err
		}
		if found != nil {
			return nil, fmt.Errorf("%w: agent turn %q is present in multiple providers", invocation.ErrInternal, turnID)
		}
		found = &accessibleAgentTurn{
			providerName: candidate.name,
			provider:     candidate.provider,
			turn:         normalized,
			session:      session.session,
			sessionOwned: session.owned,
		}
	}
	if found == nil {
		if retainedErr != nil {
			return nil, retainedErr
		}
		if authDenied {
			if providerName = strings.TrimSpace(providerName); providerName != "" {
				return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
			}
			return nil, invocation.ErrAuthorizationDenied
		}
		return nil, core.ErrNotFound
	}
	return found, nil
}

func filterProviderCandidatesByTokenPermission(p *principal.Principal, candidates []namedAgentProvider, providerName string) ([]namedAgentProvider, error) {
	filtered := make([]namedAgentProvider, 0, len(candidates))
	denied := false
	for _, candidate := range candidates {
		if principal.AllowsProviderPermission(p, candidate.name) {
			filtered = append(filtered, candidate)
			continue
		}
		denied = true
	}
	if len(filtered) == 0 && denied {
		if providerName = strings.TrimSpace(providerName); providerName != "" {
			return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
		}
		return nil, invocation.ErrAuthorizationDenied
	}
	return filtered, nil
}

func (m *Manager) findOwnedSession(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*ownedAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	return m.findOwnedSessionInProviders(ctx, p, sessionID, providerName)
}

func (m *Manager) findOwnedSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string) (*ownedAgentSession, error) {
	candidates, retainedErr, err := m.directReadProviderCandidates(providerName)
	if err != nil {
		return nil, err
	}
	candidates, err = filterProviderCandidatesByTokenPermission(p, candidates, providerName)
	if err != nil {
		return nil, err
	}
	var found *ownedAgentSession
	authDenied := false
	for _, candidate := range candidates {
		if !m.allowsAgentProvider(ctx, p, candidate.name) {
			authDenied = true
			continue
		}
		session, err := candidate.provider.GetSession(ctx, &proto.GetAgentProviderSessionRequest{
			SessionId: sessionID,
			Subject:   agentSubjectToProto(agentSubjectFromPrincipal(p)),
		})
		if err != nil {
			if agentProviderReturnedNotFound(err) {
				continue
			}
			if strings.TrimSpace(providerName) == "" && agentProviderReadFallbackAllowed(err) {
				retainedErr = retainAgentProviderReadError(retainedErr, err)
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
		if retainedErr != nil {
			return nil, retainedErr
		}
		if authDenied {
			if providerName = strings.TrimSpace(providerName); providerName != "" {
				return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, providerName)
			}
			return nil, invocation.ErrAuthorizationDenied
		}
		return nil, core.ErrNotFound
	}
	return found, nil
}

func agentProviderReturnedNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
}

func retainAgentProviderReadError(existing, err error) error {
	if existing != nil {
		return existing
	}
	return err
}

func agentProviderReadFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAgentProviderNotAvailable) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection error",
		"connection refused",
		"connection reset",
		"dial tcp",
		"i/o timeout",
		"no route to host",
		"pod not found",
		"sandbox pod",
		"transport is closing",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (m *Manager) mintRunGrant(ctx context.Context, p *principal.Principal, providerName, sessionID, turnID, callerAppName string, toolRefs []coreagent.ToolRef, toolRefsSet bool, tools []coreagent.Tool, toolSource coreagent.ToolSourceMode) (string, error) {
	if m == nil || m.runGrants == nil {
		return "", fmt.Errorf("%w: agent run grants are not configured", invocation.ErrInternal)
	}
	subject := agentSubjectFromPrincipal(p)
	permissions := agentRunPermissions(ctx, p, callerAppName, toolRefs)
	connections := m.agentConnectionBindings(providerName)
	if toolSource == coreagent.ToolSourceModeNone {
		connections = nil
	}
	return m.runGrants.Mint(agentgrant.Grant{
		ProviderName:        providerName,
		SessionID:           sessionID,
		TurnID:              turnID,
		CallerAppName:       strings.TrimSpace(callerAppName),
		SubjectID:           subject.SubjectID,
		SubjectKind:         subject.SubjectKind,
		CredentialSubjectID: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
		Permissions:         permissions,
		ToolRefs:            append([]coreagent.ToolRef(nil), toolRefs...),
		ToolRefsSet:         toolRefsSet,
		Tools:               append([]coreagent.Tool(nil), tools...),
		ToolSource:          toolSource,
		Connections:         connections,
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
		listed, err := m.listedAgentAppCandidateTool(candidate)
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
		listed, err := m.listedUnavailableAgentAppTool(unavailable[i])
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
		tools[i] = projectResolvedAgentToolSchema(tools[i])
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
		tool = projectResolvedAgentToolSchema(tool)
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
	appName := strings.TrimSpace(ref.App)
	if appName == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool app is required", invocation.ErrProviderNotFound)
	}
	prov, err := m.providers.Get(appName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreagent.Tool{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, appName)
		}
		return coreagent.Tool{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	operation := strings.TrimSpace(ref.Operation)
	if operation == "" {
		return coreagent.Tool{}, fmt.Errorf("%w: agent tool operation is required", invocation.ErrOperationNotFound)
	}
	if !m.allowProvider(ctx, p, appName) || !m.allowOperation(ctx, p, appName, operation) {
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

	ctx = invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, appName))
	var resolver invocation.TokenResolver
	if tr, ok := m.invoker.(invocation.TokenResolver); ok {
		resolver = tr
	}
	sessionConnections := m.catalogSelectorConfig().SessionCatalogConnections(appName, connection)
	sessionInstance := instance
	opMeta, _, resolvedConnection, err := invocation.ResolveOperation(ctx, prov, appName, resolver, p, operation, sessionConnections, sessionInstance)
	if err != nil {
		return coreagent.Tool{}, err
	}
	if !principal.AllowsOperationPermission(p, appName, opMeta.ID) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, appName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, appName, opMeta) {
		return coreagent.Tool{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, appName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, appName, connection, sessionInstance)
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
		name = appName + "." + opMeta.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(opMeta.Description)
	}
	target := coreagent.ToolTarget{
		App:                   appName,
		Operation:             opMeta.ID,
		Connection:            connection,
		Instance:              sessionInstance,
		CredentialMode:        credentialMode,
		RunAs:                 core.NormalizeRunAsSubject(ref.RunAs),
		RunAsExternalIdentity: core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity),
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
	system                string
	app                   string
	operation             string
	connection            string
	instance              string
	credentialMode        core.ConnectionMode
	runAs                 core.RunAsSubject
	runAsExternalIdentity core.ExternalIdentityRef
}

func agentToolTargetKeyFromRef(ref coreagent.ToolRef) agentToolTargetKey {
	return agentToolTargetKey{
		system:                strings.TrimSpace(ref.System),
		app:                   strings.TrimSpace(ref.App),
		operation:             strings.TrimSpace(ref.Operation),
		connection:            core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		instance:              strings.TrimSpace(ref.Instance),
		credentialMode:        ref.CredentialMode,
		runAs:                 agentToolRunAsKey(ref.RunAs),
		runAsExternalIdentity: agentToolExternalIdentityKey(ref.RunAsExternalIdentity),
	}
}

func agentToolTargetKeyFromTarget(target coreagent.ToolTarget) agentToolTargetKey {
	return agentToolTargetKey{
		system:                strings.TrimSpace(target.System),
		app:                   strings.TrimSpace(target.App),
		operation:             strings.TrimSpace(target.Operation),
		connection:            core.ResolveConnectionAlias(strings.TrimSpace(target.Connection)),
		instance:              strings.TrimSpace(target.Instance),
		credentialMode:        target.CredentialMode,
		runAs:                 agentToolRunAsKey(target.RunAs),
		runAsExternalIdentity: agentToolExternalIdentityKey(target.RunAsExternalIdentity),
	}
}

func (k agentToolTargetKey) String() string {
	if k.system != "" {
		return strings.Join([]string{"system", k.system, k.operation}, "/")
	}
	parts := []string{k.app, k.operation}
	runAsKey := agentToolRunAsKeyString(k.runAs)
	externalIdentityKey := agentToolExternalIdentityKeyString(k.runAsExternalIdentity)
	if k.connection != "" || k.instance != "" || k.credentialMode != "" || runAsKey != "" || externalIdentityKey != "" {
		parts = append(parts, k.connection, k.instance, string(k.credentialMode), runAsKey, externalIdentityKey)
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
	for _, appName := range providerNames {
		appName = strings.TrimSpace(appName)
		if appName == "" {
			continue
		}
		prov, err := m.providers.Get(appName)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			return fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
		}
		if !m.allowProvider(ctx, p, appName) {
			continue
		}
		searchRefs := scope.refsForProvider(appName)
		for i := range searchRefs {
			searchRef := searchRefs[i]
			searchCatalogs, err := m.catalogsForAgentToolSearch(ctx, p, prov, appName, searchRef)
			if err != nil {
				refSkipsUnavailable := skipUnavailable && agentToolSearchRefSkipsUnavailable(searchRef)
				if refSkipsUnavailable && agentToolSearchUnavailable(err) {
					if firstUnavailableErr == nil {
						firstUnavailableErr = err
					}
					if principal.AllowsProviderPermission(p, appName) {
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
					if !m.allowOperation(ctx, p, appName, operation) || !principal.AllowsOperationPermission(p, appName, operation) {
						continue
					}
					if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, appName, op) {
						continue
					}
					ref := searchCatalog.ref
					if strings.TrimSpace(ref.Operation) == "" {
						ref.Title = ""
						ref.Description = ""
					}
					ref.App = appName
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
	ref.App = strings.TrimSpace(ref.App)
	ref.Operation = ""
	ref.Title = ""
	ref.Description = ""
	reason := unavailableAgentToolReason(err)
	return agentToolUnavailableCandidate{
		ref:     ref,
		err:     err,
		reason:  reason,
		message: unavailableAgentToolMessage(ref.App, reason, err),
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

func unavailableAgentToolTitle(appName, reason string) string {
	appName = strings.TrimSpace(appName)
	switch reason {
	case coreagent.ToolUnavailableReasonInstanceRequired:
		return appName + " instance required"
	case coreagent.ToolUnavailableReasonScopeDenied:
		return appName + " scope denied"
	case coreagent.ToolUnavailableReasonNotAuthenticated:
		return appName + " authentication required"
	case coreagent.ToolUnavailableReasonNoCredential:
		return appName + " connection required"
	default:
		return appName + " reconnect required"
	}
}

func unavailableAgentToolMessage(appName, reason string, err error) string {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "this integration"
	}
	switch reason {
	case coreagent.ToolUnavailableReasonInstanceRequired:
		return fmt.Sprintf("%s has multiple matching instances. Ask the user to choose or reconnect a specific instance before using these tools.", appName)
	case coreagent.ToolUnavailableReasonScopeDenied:
		return fmt.Sprintf("%s is connected but is missing required OAuth scopes. Ask the user to reconnect %s with the required scopes before using these tools.", appName, appName)
	case coreagent.ToolUnavailableReasonNotAuthenticated, coreagent.ToolUnavailableReasonNoCredential, coreagent.ToolUnavailableReasonReconnectRequired:
		return fmt.Sprintf("%s is not connected, its credentials expired, or refresh failed. Ask the user to reconnect %s before using these tools.", appName, appName)
	default:
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%s is unavailable.", appName)
	}
}

func agentToolSearchRefSkipsUnavailable(ref coreagent.ToolRef) bool {
	return strings.TrimSpace(ref.Operation) == ""
}

func (m *Manager) catalogsForAgentToolSearch(ctx context.Context, p *principal.Principal, prov core.Provider, appName string, ref coreagent.ToolRef) ([]agentToolSearchCatalog, error) {
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
	catalogCtx := invocation.WithAccessContext(ctx, m.providerAccessContext(ctx, p, appName))
	targets := m.catalogSelectorConfig().SessionCatalogTargets(appName, connection, instance)
	if !shouldExpandAgentToolSearchCatalogTargets(ref, credentialMode) {
		cat, _, err := invocation.ResolveCatalogForTargetsWithMetadata(
			catalogCtx,
			prov,
			appName,
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
			appName,
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
	targets, err = expander.ExpandCatalogTargets(catalogCtx, p, appName, targets)
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
			appName,
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
	apps     map[string][]coreagent.ToolRef
	exactOps map[string]map[string][]coreagent.ToolRef
}

func newAgentToolSearchScope(refs []coreagent.ToolRef) agentToolSearchScope {
	if len(refs) == 0 {
		return agentToolSearchScope{all: true}
	}
	scope := agentToolSearchScope{
		apps:     map[string][]coreagent.ToolRef{},
		exactOps: map[string]map[string][]coreagent.ToolRef{},
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		appName := strings.TrimSpace(ref.App)
		if appName == "" {
			continue
		}
		ref.App = appName
		ref.Operation = strings.TrimSpace(ref.Operation)
		if ref.App == agentToolSearchAllApp {
			scope.all = true
			continue
		}
		if ref.Operation == "" {
			scope.apps[appName] = append(scope.apps[appName], ref)
			continue
		}
		if scope.exactOps[appName] == nil {
			scope.exactOps[appName] = map[string][]coreagent.ToolRef{}
		}
		scope.exactOps[appName][ref.Operation] = append(scope.exactOps[appName][ref.Operation], ref)
	}
	return scope
}

func (s agentToolSearchScope) providerNames() []string {
	if s.all {
		return nil
	}
	set := map[string]struct{}{}
	for appName := range s.apps {
		set[appName] = struct{}{}
	}
	for appName := range s.exactOps {
		set[appName] = struct{}{}
	}
	names := make([]string, 0, len(set))
	for appName := range set {
		names = append(names, appName)
	}
	sort.Strings(names)
	return names
}

func (s agentToolSearchScope) refsForProvider(appName string) []coreagent.ToolRef {
	refs := []coreagent.ToolRef{}
	if s.all {
		refs = append(refs, coreagent.ToolRef{App: appName})
	}
	if ops := s.exactOps[appName]; len(ops) > 0 {
		operations := make([]string, 0, len(ops))
		for operation := range ops {
			operations = append(operations, operation)
		}
		sort.Strings(operations)
		for _, operation := range operations {
			refs = append(refs, ops[operation]...)
		}
	}
	if appRefs := s.apps[appName]; len(appRefs) > 0 {
		refs = append(refs, appRefs...)
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
		appName := strings.TrimSpace(ref.App)
		if appName == "" {
			continue
		}
		if appName == agentToolSearchAllApp {
			continue
		}
		if strings.TrimSpace(ref.Operation) != "" {
			if _, err := m.resolveTool(ctx, p, ref); err != nil {
				return err
			}
			continue
		}
		if _, err := m.providers.Get(appName); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, appName)
			}
			return fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
		}
		if !m.allowsAgentProvider(ctx, p, appName) {
			return fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, appName)
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

func newAgentSessionID(providerName, subjectID, idempotencyKey string, workspace bool) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !workspace || idempotencyKey == "" {
		return uuid.NewString()
	}
	scope := "gestalt:agent-session-workspace:" + strings.TrimSpace(providerName) + ":" + strings.TrimSpace(subjectID) + ":" + idempotencyKey
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String()
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
	raw := projectAgentToolInputSchema(agentToolInputSchema(op), op.Parameters)
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
	return synthesizeAgentToolInputSchema(op.Parameters)
}

func agentToolInputSchemaJSON(op catalog.CatalogOperation) string {
	return string(projectAgentToolInputSchema(agentToolInputSchema(op), op.Parameters))
}

func agentToolOutputSchemaJSON(op catalog.CatalogOperation) string {
	if len(op.OutputSchema) == 0 || len(op.OutputSchema) > agentToolSchemaMaxBytes {
		return ""
	}
	return string(op.OutputSchema)
}

func agentToolSchemaJSON(schema map[string]any) (string, error) {
	if len(schema) == 0 {
		return string(agentToolPermissiveInputSchema()), nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("marshal agent tool input schema: %w", err)
	}
	if len(raw) > agentToolSchemaMaxBytes {
		return string(agentToolPermissiveInputSchema()), nil
	}
	return string(projectAgentToolInputSchema(raw, nil)), nil
}

func projectResolvedAgentToolSchema(tool coreagent.Tool) coreagent.Tool {
	raw, err := json.Marshal(tool.ParametersSchema)
	if err != nil || len(raw) == 0 || len(raw) > agentToolSchemaMaxBytes {
		tool.ParametersSchema = agentToolPermissiveInputSchemaMap()
		return tool
	}
	projected := projectAgentToolInputSchema(raw, nil)
	var out map[string]any
	if err := json.Unmarshal(projected, &out); err != nil || len(out) == 0 {
		tool.ParametersSchema = agentToolPermissiveInputSchemaMap()
		return tool
	}
	tool.ParametersSchema = out
	return tool
}

func projectAgentToolOperationForListing(op catalog.CatalogOperation) catalog.CatalogOperation {
	out := op
	out.ProviderID = ""
	out.Path = ""
	out.Parameters = publicAgentToolParameters(op.Parameters)
	out.InputSchema = projectAgentToolInputSchema(agentToolInputSchema(op), op.Parameters)
	return out
}

func projectAgentToolInputSchema(raw json.RawMessage, params []catalog.CatalogParameter) json.RawMessage {
	if len(raw) == 0 || len(raw) > agentToolSchemaMaxBytes {
		return synthesizeAgentToolInputSchema(params)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return synthesizeAgentToolInputSchema(params)
	}
	projected, ok := projectAgentToolSchemaObject(schema, internalAgentToolSchemaFilter(params))
	if !ok {
		return synthesizeAgentToolInputSchema(params)
	}
	projectedRaw, err := json.Marshal(projected)
	if err != nil || len(projectedRaw) > agentToolSchemaMaxBytes {
		return synthesizeAgentToolInputSchema(params)
	}
	return projectedRaw
}

func synthesizeAgentToolInputSchema(params []catalog.CatalogParameter) json.RawMessage {
	if synthesized := integration.SynthesizeInputSchema(publicAgentToolParameters(params)); len(synthesized) > 0 && len(synthesized) <= agentToolSchemaMaxBytes {
		return synthesized
	}
	return agentToolPermissiveInputSchema()
}

func publicAgentToolParameters(params []catalog.CatalogParameter) []catalog.CatalogParameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]catalog.CatalogParameter, 0, len(params))
	for _, param := range params {
		if param.Internal {
			continue
		}
		out = append(out, param)
	}
	return out
}

type agentToolInternalSchemaFilter struct {
	names   map[string]struct{}
	phrases []string
}

func internalAgentToolSchemaFilter(params []catalog.CatalogParameter) agentToolInternalSchemaFilter {
	var out agentToolInternalSchemaFilter
	for _, param := range params {
		if !param.Internal {
			continue
		}
		out.addName(param.Name)
		out.addName(param.WireName)
		out.addPhrase(param.Description, 8)
	}
	return out
}

func (f *agentToolInternalSchemaFilter) addName(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if f.names == nil {
		f.names = make(map[string]struct{}, 2)
	}
	f.names[value] = struct{}{}
	f.addPhrase(value, 4)
}

func (f *agentToolInternalSchemaFilter) addPhrase(value string, minLength int) {
	value = strings.TrimSpace(value)
	if len(value) < minLength {
		return
	}
	f.phrases = append(f.phrases, strings.ToLower(value))
}

func (f agentToolInternalSchemaFilter) internalName(value string) bool {
	_, ok := f.names[strings.TrimSpace(value)]
	return ok
}

func (f agentToolInternalSchemaFilter) mentionsInternal(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, phrase := range f.phrases {
		if phrase != "" && strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}

func agentToolPermissiveInputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}

func agentToolPermissiveInputSchemaMap() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}

func projectAgentToolSchemaObject(schema map[string]any, filter agentToolInternalSchemaFilter) (map[string]any, bool) {
	if !agentToolSchemaRootSupportsObject(schema) {
		return nil, false
	}
	out := map[string]any{"type": "object"}
	if additionalProperties, ok := schema["additionalProperties"].(bool); ok {
		out["additionalProperties"] = additionalProperties
	}
	properties, ok := agentToolSchemaProperties(schema)
	if !ok {
		return nil, false
	}
	projectedProperties := make(map[string]any, len(properties))
	rootPropertyNames := make(map[string]struct{}, len(properties))
	for name, value := range properties {
		if filter.internalName(name) || filter.mentionsInternal(name) {
			continue
		}
		projectedValue, keep := projectAgentToolSchemaValue(value, filter)
		if !keep {
			continue
		}
		projectedProperties[name] = projectedValue
		rootPropertyNames[name] = struct{}{}
	}
	required := map[string]struct{}{}
	for _, spec := range []struct {
		key           string
		unionRequired bool
	}{
		{key: "allOf", unionRequired: true},
		{key: "oneOf", unionRequired: false},
		{key: "anyOf", unionRequired: false},
	} {
		if _, exists := schema[spec.key]; !exists {
			continue
		}
		if !mergeAgentToolSchemaCombinator(schema[spec.key], projectedProperties, rootPropertyNames, required, filter, spec.unionRequired) {
			return nil, false
		}
	}
	for name := range agentToolSchemaRequiredSet(projectedProperties, filter, schema["required"]) {
		required[name] = struct{}{}
	}
	if len(projectedProperties) > 0 {
		out["properties"] = projectedProperties
	}
	if len(required) > 0 {
		out["required"] = sortedAgentToolRequired(required)
	}
	return out, true
}

func projectAgentToolSchemaValue(value any, filter agentToolInternalSchemaFilter) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if agentToolUnsafeNestedSchemaKey(key) || filter.internalName(key) || filter.mentionsInternal(key) {
				continue
			}
			projected, keep := projectAgentToolSchemaValue(item, filter)
			if !keep {
				continue
			}
			out[key] = projected
		}
		return out, true
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			projected, keep := projectAgentToolSchemaValue(item, filter)
			if keep {
				out = append(out, projected)
			}
		}
		return out, true
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if filter.internalName(item) || filter.mentionsInternal(item) {
				continue
			}
			out = append(out, item)
		}
		return out, true
	case string:
		if filter.mentionsInternal(typed) {
			return nil, false
		}
		return typed, true
	default:
		return value, true
	}
}

func agentToolUnsafeNestedSchemaKey(key string) bool {
	if strings.HasPrefix(key, "$") {
		return true
	}
	switch key {
	case "allOf", "anyOf", "oneOf", "not", "if", "then", "else", "dependentRequired", "dependentSchemas", "patternProperties", "definitions":
		return true
	default:
		return false
	}
}

func mergeAgentToolSchemaCombinator(value any, properties map[string]any, rootPropertyNames map[string]struct{}, required map[string]struct{}, filter agentToolInternalSchemaFilter, unionRequired bool) bool {
	branches, ok := value.([]any)
	if !ok {
		return false
	}
	for _, branchValue := range branches {
		branch, ok := branchValue.(map[string]any)
		if !ok {
			return false
		}
		projected, ok := projectAgentToolSchemaObject(branch, filter)
		if !ok {
			return false
		}
		branchProperties, ok := agentToolSchemaProperties(projected)
		if !ok {
			return false
		}
		for name, value := range branchProperties {
			if _, rootProperty := rootPropertyNames[name]; rootProperty {
				if existing, exists := properties[name]; !exists || !reflect.DeepEqual(existing, value) {
					return false
				}
				continue
			}
			if existing, exists := properties[name]; exists && !reflect.DeepEqual(existing, value) {
				return false
			}
			properties[name] = value
		}
		if unionRequired {
			for name := range agentToolSchemaRequiredSet(properties, filter, projected["required"]) {
				if _, ok := properties[name]; ok {
					required[name] = struct{}{}
				}
			}
		}
	}
	return true
}

func agentToolSchemaRootSupportsObject(schema map[string]any) bool {
	value, exists := schema["type"]
	if !exists {
		return true
	}
	switch typed := value.(type) {
	case string:
		return typed == "object"
	case []any:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	}
	return false
}

func agentToolSchemaProperties(schema map[string]any) (map[string]any, bool) {
	value, exists := schema["properties"]
	if !exists || value == nil {
		return nil, true
	}
	properties, ok := value.(map[string]any)
	return properties, ok
}

func agentToolSchemaRequiredSet(properties map[string]any, filter agentToolInternalSchemaFilter, values any) map[string]struct{} {
	out := map[string]struct{}{}
	for _, name := range agentToolSchemaRequired(values) {
		if filter.internalName(name) || filter.mentionsInternal(name) {
			continue
		}
		if _, exists := properties[name]; exists {
			out[name] = struct{}{}
		}
	}
	return out
}

func agentToolSchemaRequired(values any) []string {
	switch typed := values.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			name, ok := item.(string)
			if !ok || name == "" {
				continue
			}
			out = append(out, name)
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, name := range typed {
			if name == "" {
				continue
			}
			out = append(out, name)
		}
		return out
	default:
		return nil
	}
}

func sortedAgentToolRequired(required map[string]struct{}) []string {
	out := make([]string, 0, len(required))
	for name := range required {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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

func (m *Manager) listedAgentAppCandidateTool(candidate agentToolSearchCandidate) (coreagent.ListedTool, error) {
	projectedOperation := projectAgentToolOperationForListing(candidate.operation)
	projectedCandidate := candidate
	projectedCandidate.operation = projectedOperation
	ref := candidate.ref
	target := coreagent.ToolTarget{
		App:                   strings.TrimSpace(ref.App),
		Operation:             strings.TrimSpace(ref.Operation),
		Connection:            core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		Instance:              strings.TrimSpace(ref.Instance),
		CredentialMode:        ref.CredentialMode,
		RunAs:                 core.NormalizeRunAsSubject(ref.RunAs),
		RunAsExternalIdentity: core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity),
	}
	toolID, err := m.mintAgentToolID(target)
	if err != nil {
		return coreagent.ListedTool{}, err
	}
	name := strings.TrimSpace(ref.Title)
	if name == "" {
		name = strings.TrimSpace(projectedOperation.Title)
	}
	if name == "" {
		name = target.App + "." + projectedOperation.ID
	}
	description := strings.TrimSpace(ref.Description)
	if description == "" {
		description = strings.TrimSpace(projectedOperation.Description)
	}
	ref.Connection = target.Connection
	ref.Instance = target.Instance
	ref.CredentialMode = target.CredentialMode
	ref.RunAsExternalIdentity = target.RunAsExternalIdentity
	return coreagent.ListedTool{
		ToolID:           toolID,
		MCPName:          agentToolMCPName(target),
		Title:            name,
		Description:      description,
		Tags:             append([]string(nil), projectedOperation.Tags...),
		SearchText:       agentToolSearchMetadataText(projectedCandidate),
		InputSchemaJSON:  agentToolInputSchemaJSON(projectedOperation),
		OutputSchemaJSON: agentToolOutputSchemaJSON(projectedOperation),
		Annotations:      projectedOperation.Annotations,
		Ref:              ref,
		Target:           target,
		Hidden:           !catalog.OperationVisibleByDefault(projectedOperation),
	}, nil
}

func (m *Manager) listedUnavailableAgentAppTool(candidate agentToolUnavailableCandidate) (coreagent.ListedTool, error) {
	ref := candidate.ref
	ref.App = strings.TrimSpace(ref.App)
	ref.Operation = ""
	ref.Connection = core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection))
	ref.Instance = strings.TrimSpace(ref.Instance)
	ref.Title = ""
	ref.Description = ""
	target := coreagent.ToolTarget{
		App:            ref.App,
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
		Title:           unavailableAgentToolTitle(ref.App, candidate.reason),
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

func agentToolRefFromTarget(target coreagent.ToolTarget) coreagent.ToolRef {
	return coreagent.ToolRef{
		System:                target.System,
		App:                   target.App,
		Operation:             target.Operation,
		Connection:            target.Connection,
		Instance:              target.Instance,
		CredentialMode:        target.CredentialMode,
		RunAs:                 core.NormalizeRunAsSubject(target.RunAs),
		RunAsExternalIdentity: core.NormalizeExternalIdentityRef(target.RunAsExternalIdentity),
	}
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
	if a.Target.App != b.Target.App {
		return a.Target.App < b.Target.App
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
		parts = []string{target.App, target.Operation}
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
		App:        strings.TrimSpace(target.App),
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
	case coreagent.ToolSourceModeMCPCatalog, coreagent.ToolSourceModeNone:
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", source)
	}
	return source, nil
}

func validateProviderTurnToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = coreagent.ToolSourceMode(strings.TrimSpace(string(source)))
	switch source {
	case coreagent.ToolSourceModeUnspecified, coreagent.ToolSourceModeMCPCatalog, coreagent.ToolSourceModeNone:
		return source, nil
	default:
		return "", fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInvalidInvocation, source)
	}
}

func validateAgentResponseSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("%w: responseSchema must be a non-empty JSON schema object with type %q", invocation.ErrInvalidInvocation, "object")
	}
	rawType, ok := schema["type"]
	if !ok {
		return fmt.Errorf("%w: responseSchema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	typeValue, ok := rawType.(string)
	if !ok || strings.TrimSpace(typeValue) != "object" {
		return fmt.Errorf("%w: responseSchema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	return nil
}

func defaultAgentTurnToolSource(ctx context.Context, provider coreagent.Provider) coreagent.ToolSourceMode {
	if provider == nil {
		return coreagent.ToolSourceModeUnspecified
	}
	caps, err := provider.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return coreagent.ToolSourceModeUnspecified
	}
	if agentProviderCapabilitiesSupportToolSource(caps, coreagent.ToolSourceModeMCPCatalog) {
		return coreagent.ToolSourceModeMCPCatalog
	}
	return coreagent.ToolSourceModeUnspecified
}

func (m *Manager) defaultAgentTurnToolRefs(ctx context.Context, p *principal.Principal, callerAppName string, messages []coreagent.Message) []coreagent.ToolRef {
	broadRefs := []coreagent.ToolRef{{App: agentToolSearchAllApp}}
	if m == nil || m.providers == nil {
		return broadRefs
	}
	if strings.TrimSpace(callerAppName) != "" {
		return broadRefs
	}
	latestUserText := latestAgentUserMessageText(messages)
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
	for _, appName := range mentionedProviders {
		hasCandidate, err := m.agentToolVisibleCandidateCountExceeds(ctx, p, []coreagent.ToolRef{{App: appName}}, 0)
		if err != nil {
			return broadRefs
		}
		if hasCandidate {
			narrowedRefs = append(narrowedRefs, coreagent.ToolRef{App: appName})
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
	for _, appName := range m.providers.List() {
		appName = strings.TrimSpace(appName)
		if appName == "" {
			continue
		}
		prov, err := m.providers.Get(appName)
		if err != nil {
			continue
		}
		if !m.allowsAgentProvider(ctx, p, appName) {
			continue
		}
		aliases := []string{appName}
		if displayName := strings.TrimSpace(prov.DisplayName()); displayName != "" {
			aliases = append(aliases, displayName)
		}
		for _, alias := range aliases {
			if exactAgentToolMention(normalizedText, alias) {
				out = append(out, appName)
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

func agentRunPermissions(ctx context.Context, p *principal.Principal, callerAppName string, refs []coreagent.ToolRef) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	if permissions, ok := compactAgentRunPermissionsForRefs(p, refs); ok {
		return permissions
	}
	if shouldUseResolvedUserToolScope(ctx, p, callerAppName, refs) {
		return nil
	}
	return principal.PermissionsToAccessPermissions(p.TokenPermissions)
}

func compactAgentRunPermissionsForRefs(p *principal.Principal, refs []coreagent.ToolRef) ([]core.AccessPermission, bool) {
	if len(refs) == 0 {
		return nil, false
	}
	operationsByApp := map[string]map[string]struct{}{}
	providerWide := map[string]struct{}{}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		app := strings.TrimSpace(ref.App)
		if app == "" || app == agentToolSearchAllApp || strings.Contains(app, "*") {
			return nil, false
		}
		operation := strings.TrimSpace(ref.Operation)
		if strings.Contains(operation, "*") {
			return nil, false
		}
		if operation == "" {
			providerWide[app] = struct{}{}
			delete(operationsByApp, app)
			continue
		}
		if _, ok := providerWide[app]; ok {
			continue
		}
		ops := operationsByApp[app]
		if ops == nil {
			ops = map[string]struct{}{}
			operationsByApp[app] = ops
		}
		ops[operation] = struct{}{}
	}
	if len(providerWide) == 0 && len(operationsByApp) == 0 {
		return nil, false
	}
	apps := make([]string, 0, len(providerWide)+len(operationsByApp))
	for app := range providerWide {
		apps = append(apps, app)
	}
	for app := range operationsByApp {
		if _, ok := providerWide[app]; !ok {
			apps = append(apps, app)
		}
	}
	sort.Strings(apps)
	out := make([]core.AccessPermission, 0, len(apps))
	for _, app := range apps {
		if _, ok := providerWide[app]; ok {
			if p != nil && p.TokenPermissions != nil {
				tokenOps, ok := p.TokenPermissions[app]
				if !ok {
					return nil, false
				}
				perm := core.AccessPermission{App: app}
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
			out = append(out, core.AccessPermission{App: app})
			continue
		}
		ops := make([]string, 0, len(operationsByApp[app]))
		for operation := range operationsByApp[app] {
			ops = append(ops, operation)
		}
		sort.Strings(ops)
		out = append(out, core.AccessPermission{
			App:        app,
			Operations: ops,
		})
	}
	return out, true
}

func shouldUseResolvedUserToolScope(ctx context.Context, p *principal.Principal, callerAppName string, refs []coreagent.ToolRef) bool {
	if strings.TrimSpace(callerAppName) == "" {
		return false
	}
	if invocation.InvocationSurfaceFromContext(ctx) != invocation.InvocationSurfaceHTTP {
		return false
	}
	if p == nil || p.Kind != principal.KindUser || p.Source == principal.SourceAPIToken {
		return false
	}
	for i := range refs {
		if strings.TrimSpace(refs[i].App) == agentToolSearchAllApp && strings.TrimSpace(refs[i].Operation) == "" {
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
		ref.App = strings.TrimSpace(ref.App)
		ref.Operation = strings.TrimSpace(ref.Operation)
		ref.Connection = strings.TrimSpace(ref.Connection)
		ref.Instance = strings.TrimSpace(ref.Instance)
		ref.Title = strings.TrimSpace(ref.Title)
		ref.Description = strings.TrimSpace(ref.Description)
		ref.RunAs = core.NormalizeRunAsSubject(ref.RunAs)
		ref.RunAsExternalIdentity = core.NormalizeExternalIdentityRef(ref.RunAsExternalIdentity)
		credentialMode, err := normalizeAgentToolCredentialMode(ref.CredentialMode)
		if err != nil {
			return nil, err
		}
		ref.CredentialMode = credentialMode
		if ref.System != "" {
			if ref.App != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] must set exactly one of app or system", invocation.ErrInvalidInvocation, idx)
			}
			if ref.System != coreagent.SystemToolWorkflow {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].system %q is not supported", invocation.ErrInvalidInvocation, idx, ref.System)
			}
			if ref.Operation == "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].operation is required for system tool refs", invocation.ErrOperationNotFound, idx)
			}
			if ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil || ref.Title != "" || ref.Description != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] system refs cannot include connection, instance, credential mode, runAs, runAs external identity, title, or description", invocation.ErrInvalidInvocation, idx)
			}
			out = append(out, ref)
			continue
		}
		if ref.App == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].app is required", invocation.ErrProviderNotFound, idx)
		}
		if ref.App == agentToolSearchAllApp {
			if ref.Operation != "" || ref.Connection != "" || ref.Instance != "" || ref.Title != "" || ref.Description != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.RunAsExternalIdentity != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] global search ref cannot include operation, connection, instance, credential mode, runAs, runAs external identity, title, or description", invocation.ErrProviderNotFound, idx)
			}
		}
		if ref.RunAsExternalIdentity != nil && ref.RunAs == nil {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity requires runAs.subject", invocation.ErrInvalidInvocation, idx)
		}
		out = append(out, ref)
	}
	return out, nil
}

func (m *Manager) applyCallerInvokePolicies(callerAppName string, refs []coreagent.ToolRef) ([]coreagent.ToolRef, error) {
	callerAppName = strings.TrimSpace(callerAppName)
	if len(refs) == 0 || m == nil {
		return refs, nil
	}
	modes := make(map[string]core.ConnectionMode)
	runAsSubjects := make(map[string]*core.RunAsSubject)
	runAsExternalIdentities := make(map[string]*core.ExternalIdentityRef)
	runAsExplicitOnly := make(map[string]bool)
	if callerAppName != "" {
		for _, invoke := range m.appInvokes[callerAppName] {
			if strings.TrimSpace(invoke.Surface) != "" {
				continue
			}
			appName := strings.TrimSpace(invoke.App)
			operation := strings.TrimSpace(invoke.Operation)
			if appName == "" || operation == "" {
				continue
			}
			mode, err := normalizeAgentToolCredentialMode(invoke.CredentialMode)
			if err != nil {
				return nil, err
			}
			if mode != "" {
				modes[agentToolInvokeKey(appName, operation)] = mode
			}
			if runAs := core.NormalizeRunAsSubject(invoke.RunAs); runAs != nil {
				key := agentToolInvokeKey(appName, operation)
				runAsSubjects[key] = runAs
				runAsExplicitOnly[key] = invoke.RunAsExplicitOnly
				if identity := core.NormalizeExternalIdentityRef(invoke.RunAsExternalIdentity); identity != nil {
					runAsExternalIdentities[key] = identity
				}
			}
		}
	}
	out := append([]coreagent.ToolRef(nil), refs...)
	for i := range out {
		operation := strings.TrimSpace(out[i].Operation)
		if out[i].CredentialMode != "" && callerAppName == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires a caller app declaration", invocation.ErrAuthorizationDenied, i)
		}
		if out[i].RunAs != nil && callerAppName == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs requires a caller app declaration", invocation.ErrAuthorizationDenied, i)
		}
		if out[i].RunAsExternalIdentity != nil && callerAppName == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity requires a caller app declaration", invocation.ErrAuthorizationDenied, i)
		}
		if operation == "" {
			if out[i].CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires an exact operation", invocation.ErrAuthorizationDenied, i)
			}
			if out[i].RunAs != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs requires an exact operation", invocation.ErrAuthorizationDenied, i)
			}
			if out[i].RunAsExternalIdentity != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity requires an exact operation", invocation.ErrAuthorizationDenied, i)
			}
			continue
		}
		key := agentToolInvokeKey(out[i].App, operation)
		mode, hasMode := modes[key]
		runAs, hasRunAs := runAsSubjects[key]
		externalIdentity, hasExternalIdentity := runAsExternalIdentities[key]
		explicitOnly := runAsExplicitOnly[key]
		requestedRunAs := out[i].RunAs != nil
		requestedExternalIdentity := out[i].RunAsExternalIdentity != nil
		requestedDelegation := requestedRunAs || requestedExternalIdentity
		if !hasMode && !hasRunAs && !hasExternalIdentity {
			if out[i].CredentialMode != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires a declared invoke mode", invocation.ErrAuthorizationDenied, i)
			}
			if out[i].RunAs != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs requires a declared invoke delegation", invocation.ErrAuthorizationDenied, i)
			}
			if out[i].RunAsExternalIdentity != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity requires a declared invoke delegation", invocation.ErrAuthorizationDenied, i)
			}
			continue
		}
		if hasMode && out[i].CredentialMode != "" && out[i].CredentialMode != mode {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode %q exceeds declared invoke mode %q", invocation.ErrAuthorizationDenied, i, out[i].CredentialMode, mode)
		}
		if !hasMode && out[i].CredentialMode != "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].credentialMode requires a declared invoke mode", invocation.ErrAuthorizationDenied, i)
		}
		if hasRunAs && out[i].RunAs != nil && !core.RunAsSubjectsMatchIdentity(out[i].RunAs, runAs) {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs exceeds declared invoke delegation", invocation.ErrAuthorizationDenied, i)
		}
		if !hasRunAs && out[i].RunAs != nil {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs requires a declared invoke delegation", invocation.ErrAuthorizationDenied, i)
		}
		if hasExternalIdentity && out[i].RunAsExternalIdentity != nil && !core.ExternalIdentityRefsEqual(out[i].RunAsExternalIdentity, externalIdentity) {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity exceeds declared invoke delegation", invocation.ErrAuthorizationDenied, i)
		}
		if !hasExternalIdentity && out[i].RunAsExternalIdentity != nil {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].runAs.externalIdentity requires a declared invoke delegation", invocation.ErrAuthorizationDenied, i)
		}
		if hasMode {
			out[i].CredentialMode = mode
		}
		if hasRunAs && (!explicitOnly || requestedDelegation) {
			out[i].RunAs = runAs
		}
		if hasExternalIdentity && (!explicitOnly || requestedDelegation) {
			out[i].RunAsExternalIdentity = externalIdentity
		}
	}
	return out, nil
}

func agentToolInvokeKey(appName, operation string) string {
	return strings.TrimSpace(appName) + "\x00" + strings.TrimSpace(operation)
}

func agentToolRunAsKey(subject *core.RunAsSubject) core.RunAsSubject {
	if subject == nil {
		return core.RunAsSubject{}
	}
	normalized := core.NormalizeRunAsSubject(subject)
	if normalized == nil {
		return core.RunAsSubject{}
	}
	return core.RunAsSubject{
		SubjectID:           normalized.SubjectID,
		SubjectKind:         normalized.SubjectKind,
		CredentialSubjectID: normalized.CredentialSubjectID,
	}
}

func agentToolRunAsKeyString(subject core.RunAsSubject) string {
	if subject == (core.RunAsSubject{}) {
		return ""
	}
	return strings.Join([]string{
		subject.SubjectID,
		subject.SubjectKind,
		subject.CredentialSubjectID,
	}, "\x00")
}

func agentToolExternalIdentityKey(identity *core.ExternalIdentityRef) core.ExternalIdentityRef {
	if identity == nil {
		return core.ExternalIdentityRef{}
	}
	normalized := core.NormalizeExternalIdentityRef(identity)
	if normalized == nil {
		return core.ExternalIdentityRef{}
	}
	return *normalized
}

func agentToolExternalIdentityKeyString(identity core.ExternalIdentityRef) string {
	if identity == (core.ExternalIdentityRef{}) {
		return ""
	}
	return strings.Join([]string{identity.Type, identity.ID}, "\x00")
}

func agentProviderSupportsToolSource(ctx context.Context, provider coreagent.Provider, source coreagent.ToolSourceMode) (bool, error) {
	if provider == nil {
		return false, ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return false, err
	}
	return agentProviderCapabilitiesSupportToolSource(caps, source), nil
}

func agentProviderSupportsStructuredOutput(ctx context.Context, provider coreagent.Provider) (bool, error) {
	if provider == nil {
		return false, ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return false, err
	}
	return caps != nil && caps.StructuredOutput, nil
}

func agentProviderCapabilitiesSupportToolSource(caps *coreagent.ProviderCapabilities, source coreagent.ToolSourceMode) bool {
	if caps == nil {
		return false
	}
	for _, supported := range caps.SupportedToolSources {
		if coreagent.ToolSourceMode(strings.TrimSpace(string(supported))) == source {
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
	caps, err := provider.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return err
	}
	if caps != nil && caps.BoundedListHydration {
		return nil
	}
	return errAgentBoundedListUnsupported(providerName)
}

func (m *Manager) sessionStartForProvider(ctx context.Context, providerName string, provider coreagent.Provider) (*coreagent.SessionStartConfig, error) {
	if m == nil || len(m.sessionStart) == 0 {
		return nil, nil
	}
	cfg := m.sessionStart[strings.TrimSpace(providerName)]
	if cfg == nil || len(cfg.Hooks) == 0 {
		return nil, nil
	}
	if provider == nil {
		return nil, ErrAgentProviderNotAvailable
	}
	caps, err := provider.GetCapabilities(ctx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		return nil, err
	}
	if caps == nil || !caps.SupportsSessionStart {
		return nil, errAgentSessionStartUnsupported(providerName)
	}
	return cloneSessionStartConfig(cfg), nil
}

func errAgentSessionStartUnsupported(providerName string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return ErrAgentSessionStartUnsupported
	}
	return fmt.Errorf("%w: provider %q", ErrAgentSessionStartUnsupported, providerName)
}

func errAgentWorkspaceUnsupported(providerName string) error {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return ErrAgentWorkspaceUnsupported
	}
	return fmt.Errorf("%w: provider %q", ErrAgentWorkspaceUnsupported, providerName)
}

func validateAgentSessionUserMetadata(metadata map[string]any) error {
	for key := range metadata {
		if isReservedLifecycleMetadataKey(key) {
			return fmt.Errorf("%w: key %q is reserved for Gestalt lifecycle data", ErrAgentSessionMetadataInvalid, key)
		}
		if isReservedWorkspaceMetadataKey(key) {
			return fmt.Errorf("%w: key %q is reserved for Gestalt workspace data", ErrAgentSessionMetadataInvalid, key)
		}
	}
	return nil
}

func mergeReservedLifecycleMetadata(metadata map[string]any, existing map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	merged := maps.Clone(metadata)
	for key, value := range existing {
		if isReservedLifecycleMetadataKey(key) {
			merged[key] = value
		}
	}
	return merged
}

func isReservedLifecycleMetadataKey(key string) bool {
	key = strings.TrimSpace(key)
	return key == "__gestalt.lifecycle" || strings.HasPrefix(key, "__gestalt.lifecycle.")
}

func isReservedWorkspaceMetadataKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "cwd", "workspacePath", "worktreePath", "__gestalt.workspace":
		return true
	default:
		return strings.HasPrefix(strings.TrimSpace(key), "__gestalt.workspace.")
	}
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
	switch core.NormalizeOptionalConnectionMode(mode) {
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

func agentSubjectFromPrincipal(p *principal.Principal) core.RunAsSubject {
	p = principal.Canonicalized(p)
	if p == nil {
		return core.RunAsSubject{}
	}
	return core.RunAsSubject{
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
