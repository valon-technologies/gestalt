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

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agenttoolid"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	integration "github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	ErrAgentNotConfigured              = errors.New("agent is not configured")
	ErrAgentProviderRequired           = errors.New("agent provider is required")
	ErrAgentProviderNotAvailable       = errors.New("agent provider is not available")
	ErrAgentSubjectRequired            = errors.New("agent subject is required")
	ErrAgentInheritedSurfaceTool       = errors.New("agent inherited surface tools are not supported")
	ErrAgentInteractionRequired        = errors.New("agent interaction is required")
	ErrAgentInteractionNotFound        = errors.New("agent interaction is not found")
	ErrAgentSessionNotFound            = errors.New("agent session is not found")
	ErrAgentWorkflowToolsNotConfigured = errors.New("agent workflow tools are not configured")
	ErrAgentBoundedListUnsupported     = errors.New("agent provider does not support bounded list hydration")
	ErrAgentSessionStartUnsupported    = errors.New("agent provider does not support session start hooks")
	ErrAgentWorkspaceUnsupported       = errors.New("agent provider does not support workspaces")
	ErrAgentWorkspaceInvalid           = errors.New("agent workspace is invalid")
	ErrAgentSessionMetadataInvalid     = errors.New("agent session metadata is invalid")
	ErrAgentInvalidListRequest         = errors.New("agent list request is invalid")
)

const (
	agentToolSearchAllApp        = "*"
	agentToolListDefaultPageSize = 100
	agentToolListMaxPageSize     = 1000
	agentToolSchemaMaxBytes      = 128 * 1024
	AgentListSummaryDefaultLimit = 100
	AgentListMaxLimit            = 500
)

const (
	agentSessionToolScopeMetadataKey           = "__gestalt.lifecycle.agent.tools"
	agentSessionToolScopeMetadataSourceCatalog = "catalog"
	agentSessionToolScopeMetadataSourceNone    = "none"
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
	ResolveProvider(ctx context.Context, name string) (providerName string, provider coreagent.Provider, err error)
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
	AuthorizeAppInvocation(context.Context, invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error)
	AuthorizeWorkflowInvocation(context.Context, invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error)
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Agent             AgentControl
	WorkflowTools     WorkflowSystemTools
	TurnScopes        *agentturnscope.Store
	ToolIDs           *agenttoolid.Codec
	Invoker           invocation.Invoker
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	MCPConnection     map[string]string
	AgentConnections  map[string][]string
	SessionStart      map[string]*coreagent.SessionStartConfig
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	agent             AgentControl
	workflowTools     WorkflowSystemTools
	turnScopes        *agentturnscope.Store
	toolIDs           *agenttoolid.Codec
	invoker           invocation.Invoker
	defaultConnection map[string]string
	catalogConnection map[string]string
	mcpConnection     map[string]string
	agentConnections  map[string][]string
	sessionStart      map[string]*coreagent.SessionStartConfig
}

func New(cfg Config) *Manager {
	return &Manager{
		providers:         cfg.Providers,
		agent:             cfg.Agent,
		workflowTools:     cfg.WorkflowTools,
		turnScopes:        cfg.TurnScopes,
		toolIDs:           cfg.ToolIDs,
		invoker:           cfg.Invoker,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		mcpConnection:     maps.Clone(cfg.MCPConnection),
		agentConnections:  cloneStringSliceMap(cfg.AgentConnections),
		sessionStart:      cloneSessionStartConfigMap(cfg.SessionStart),
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

func agentCaller(ctx context.Context, reqCtx *proto.RequestContext, providerName string) (invocation.ProviderKind, string) {
	if caller := reqCtx.GetCaller(); caller != nil {
		kind := invocation.ProviderKind(strings.TrimSpace(caller.GetKind()))
		name := strings.TrimSpace(caller.GetName())
		if kind != "" && name != "" {
			return kind, name
		}
	}
	caller := invocation.CallerProviderFromContext(ctx)
	if caller.Kind != "" && caller.Name != "" {
		return caller.Kind, caller.Name
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return "", ""
	}
	return invocation.ProviderKindAgent, providerName
}

func agentRequestContext(ctx context.Context, p *principal.Principal, existing *proto.RequestContext, callerKind invocation.ProviderKind, callerName string) (*proto.RequestContext, error) {
	callerKind = invocation.ProviderKind(strings.TrimSpace(string(callerKind)))
	callerName = strings.TrimSpace(callerName)
	base, err := appaccessservice.RequestContextProto(principal.WithPrincipal(ctx, p), "", invocation.CallerProvider{
		Kind: callerKind,
		Name: callerName,
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return appaccessservice.MergeRequestContext(existing, base), nil
	}
	if callerKind == "" || callerName == "" {
		return base, nil
	}
	return base, nil
}

func agentProviderRequestContext(ctx context.Context, p *principal.Principal, existing *proto.RequestContext, providerName string) (context.Context, *proto.RequestContext, error) {
	callerKind, callerName := agentCaller(ctx, existing, providerName)
	if callerKind != "" && callerName != "" {
		ctx = invocation.WithCallerProvider(ctx, callerKind, callerName)
	}
	reqCtx, err := agentRequestContext(ctx, p, existing, callerKind, callerName)
	return ctx, reqCtx, err
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
	providerName, provider, err := m.resolveProvider(ctx, req.GetProviderName())
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
	callerKind, callerName := agentCaller(ctx, req.GetContext(), providerName)
	if callerKind != "" && callerName != "" {
		ctx = invocation.WithCallerProvider(ctx, callerKind, callerName)
	}
	if _, _, err := workflowTurnScope(ctx, req.GetContext(), callerKind, providerName); err != nil {
		return nil, err
	}
	ctx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), providerName)
	if err != nil {
		return nil, err
	}
	sessionTools, err := m.agentSessionTools(ctx, p, providerName, provider, req.GetTools())
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	providerMetadata := req.GetMetadata()
	if sessionTools.set {
		providerMetadata, err = agentSessionMetadataWithToolScope(metadata, sessionTools)
		if err != nil {
			return nil, err
		}
	}
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderSessionRequest{})
	providerReq.IdempotencyKey = idempotencyKey
	providerReq.ProviderName = providerName
	providerReq.Model = strings.TrimSpace(req.GetModel())
	providerReq.ClientRef = strings.TrimSpace(req.GetClientRef())
	providerReq.Metadata = providerMetadata
	providerReq.SessionStart = sessionStartConfigToProto(sessionStart)
	providerReq.Workspace = agentWorkspaceToProto(workspace)
	providerReq.PreparedWorkspace = nil
	providerReq.Tools = sessionTools.config
	providerReq.Context = providerReqContext
	session, err = provider.CreateSession(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderSessionForCreate(providerName, session)
	if err != nil {
		return nil, err
	}
	if !providerSessionOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	if sessionTools.set {
		if persistedScope, ok, err := agentSessionScopeFromMetadata(providerName, normalized); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("%w: agent session is missing tool scope metadata", invocation.ErrInvalidInvocation)
		} else if !agentSessionScopeMatchesTools(persistedScope, sessionTools) {
			return nil, fmt.Errorf("%w: agent session tool scope does not match existing session", invocation.ErrInvalidInvocation)
		}
		if err := m.storeSessionScope(ctx, req.GetContext(), p, providerName, normalized.ID, callerKind, callerName, sessionTools.refs, sessionTools.listed, sessionTools.toolSource); err != nil {
			return nil, err
		}
	} else {
		if persistedScope, ok, err := agentSessionScopeFromMetadata(providerName, normalized); err != nil {
			return nil, err
		} else if ok && !agentSessionScopeIsNoTools(persistedScope) {
			return nil, fmt.Errorf("%w: agent session has tool scope metadata", invocation.ErrInvalidInvocation)
		}
		m.deleteSessionScope(providerName, normalized.ID)
	}
	return normalized, nil
}

func (m *Manager) GetSession(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, finish := startAgentOperation(ctx, "get_session")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.GetAgentProviderSessionRequest{}
	}
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleSession(ctx, p, req.GetSessionId(), providerName, req.GetContext())
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
		return m.listExactSessions(ctx, p, providerName, req.GetSessionIds(), state, limit, req.GetSummaryOnly(), req.GetContext())
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
		callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), candidate.name)
		if err != nil {
			return nil, err
		}
		sessions, err := candidate.provider.ListSessions(callCtx, &proto.ListAgentProviderSessionsRequest{
			Context:     providerReqContext,
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

func (m *Manager) listExactSessions(ctx context.Context, p *principal.Principal, providerName string, sessionIDs []string, state coreagent.SessionState, limit int, summaryOnly bool, reqContext *proto.RequestContext) ([]*coreagent.Session, error) {
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
		owned, err := m.findAccessibleSession(ctx, p, sessionID, providerName, reqContext)
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findOwnedSession(ctx, p, req.GetSessionId(), providerName, req.GetContext())
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
	providerReq.ProviderName = owned.providerName
	providerReq.ClientRef = strings.TrimSpace(req.GetClientRef())
	providerReq.State = req.GetState()
	providerReq.Metadata = providerMetadata
	ctx, providerReq.Context, err = agentProviderRequestContext(ctx, p, req.GetContext(), owned.providerName)
	if err != nil {
		return nil, err
	}
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
	if normalized.State == coreagent.SessionStateArchived {
		m.deleteSessionScope(owned.providerName, normalized.ID)
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	ownedSession, err := m.findOwnedSession(ctx, p, req.GetSessionId(), providerName, req.GetContext())
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %w", ErrAgentSessionNotFound, err)
		}
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	callerKind, callerName := agentCaller(ctx, req.GetContext(), ownedSession.providerName)
	if callerKind != "" && callerName != "" {
		ctx = invocation.WithCallerProvider(ctx, callerKind, callerName)
	}
	toolSource := coreagent.ToolSourceModeNone
	var toolRefs []coreagent.ToolRef
	if sessionScope, ok, err := m.sessionScopeForSession(ctx, p, ownedSession.providerName, ownedSession.session); err != nil {
		return nil, err
	} else if ok {
		toolSource = normalizeAgentToolSource(sessionScope.ToolSource)
		toolRefs = append([]coreagent.ToolRef(nil), sessionScope.ToolRefs...)
	}
	requestedOutput := agentOutputFromProto(req.GetOutput())
	if err := validateAgentOutput(requestedOutput); err != nil {
		return nil, err
	}
	if req.GetTimeoutSeconds() < 0 {
		return nil, fmt.Errorf("%w: timeout_seconds must not be negative", invocation.ErrInvalidInvocation)
	}
	switch toolSource {
	case coreagent.ToolSourceModeCatalog:
		if err := validateCatalogToolRefs(toolRefs); err != nil {
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
	case coreagent.ToolSourceModeUnspecified:
	default:
		return nil, fmt.Errorf("%w: unsupported agent tool source %q", invocation.ErrInvalidInvocation, toolSource)
	}
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	turnID := newAgentTurnID(ownedSession.session.ID, idempotencyKey)
	workflowRunID, workflowStepID, err := workflowTurnScope(ctx, req.GetContext(), callerKind, ownedSession.providerName)
	if err != nil {
		return nil, err
	}
	if m.turnScopes != nil {
		if err := m.turnScopes.PutTurnBinding(ownedSession.providerName, ownedSession.session.ID, turnID, agentturnscope.TurnBinding{
			CallerKind:     callerKind,
			CallerName:     strings.TrimSpace(callerName),
			WorkflowRunID:  workflowRunID,
			WorkflowStepID: workflowStepID,
		}); err != nil {
			return nil, fmt.Errorf("%w: store agent turn binding: %v", invocation.ErrInternal, err)
		}
	}
	bindingCommitted := false
	defer func() {
		if !bindingCommitted && m.turnScopes != nil {
			m.turnScopes.DeleteTurnBinding(ownedSession.providerName, ownedSession.session.ID, turnID)
		}
	}()
	providerReq := cloneAgentRequest(req, &proto.CreateAgentProviderTurnRequest{})
	providerReq.TurnId = turnID
	providerReq.SessionId = ownedSession.session.ID
	providerReq.ProviderName = ownedSession.providerName
	providerReq.IdempotencyKey = idempotencyKey
	providerReq.Model = strings.TrimSpace(req.GetModel())
	providerReq.ExecutionRef = turnID
	ctx = invocation.WithAgentInvocationContext(ctx, invocation.AgentInvocationContext{
		ProviderName: ownedSession.providerName,
		SessionID:    ownedSession.session.ID,
		TurnID:       turnID,
	})
	providerReq.Context, err = agentRequestContext(ctx, p, req.GetContext(), callerKind, callerName)
	if err != nil {
		return nil, err
	}
	providerReq.Context.Agent = &proto.AgentInvocationContext{
		ProviderName: ownedSession.providerName,
		SessionId:    ownedSession.session.ID,
		TurnId:       turnID,
	}
	turn, err = ownedSession.provider.CreateTurn(ctx, providerReq)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeProviderTurnForCreate(ownedSession.providerName, ownedSession.session.ID, turnID, idempotencyKey, turn)
	if err != nil {
		return nil, err
	}
	if !providerTurnOwnedBy(normalized, p) {
		return nil, core.ErrNotFound
	}
	if err := validateAgentTurnOutput(requestedOutput, normalized); err != nil {
		return nil, err
	}
	if m.turnScopes != nil {
		if executionRef := strings.TrimSpace(normalized.ExecutionRef); executionRef != "" {
			if err := m.turnScopes.BindProviderTurnID(ownedSession.providerName, normalized.SessionID, executionRef, normalized.ID); err != nil {
				return nil, fmt.Errorf("%w: bind agent provider turn binding: %v", invocation.ErrInternal, err)
			}
		}
	}
	bindingCommitted = true
	return normalized, nil
}

func (m *Manager) GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, finish := startAgentOperation(ctx, "get_turn")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.GetAgentProviderTurnRequest{}
	}
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), providerName, req.GetSessionId(), req.GetContext())
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	if err := m.authorizeWorkflowTurnAccess(ctx, owned, req.GetContext()); err != nil {
		return nil, err
	}
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	ownedSession, err := m.findAccessibleSession(ctx, p, req.GetSessionId(), providerName, req.GetContext())
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(ownedSession.providerName))
	if req.GetSummaryOnly() || limit > 0 {
		if err := requireAgentProviderBoundedListHydration(ctx, ownedSession.providerName, ownedSession.provider); err != nil {
			return nil, err
		}
	}
	callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), ownedSession.providerName)
	if err != nil {
		return nil, err
	}
	turns, err = ownedSession.provider.ListTurns(callCtx, &proto.ListAgentProviderTurnsRequest{
		SessionId:    ownedSession.session.ID,
		ProviderName: ownedSession.providerName,
		Context:      providerReqContext,
		TurnIds:      append([]string(nil), req.GetTurnIds()...),
		Status:       req.GetStatus(),
		Limit:        int32(limit),
		SummaryOnly:  req.GetSummaryOnly(),
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), providerName, req.GetSessionId(), req.GetContext())
	if err != nil {
		return nil, err
	}
	if !owned.sessionOwned {
		return nil, core.ErrNotFound
	}
	if err := m.authorizeWorkflowTurnAccess(ctx, owned, req.GetContext()); err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	providerReq := cloneAgentRequest(req, &proto.CancelAgentProviderTurnRequest{})
	providerReq.TurnId = strings.TrimSpace(req.GetTurnId())
	providerReq.ProviderName = owned.providerName
	providerReq.Reason = strings.TrimSpace(req.GetReason())
	ctx, providerReq.Context, err = agentProviderRequestContext(ctx, p, req.GetContext(), owned.providerName)
	if err != nil {
		return nil, err
	}
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
	return normalized, nil
}

func (m *Manager) ListTurnEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnEventsRequest) (events []*coreagent.TurnEvent, err error) {
	ctx, finish := startAgentOperation(ctx, "list_turn_events")
	defer func() { finish(err) }()

	if req == nil {
		req = &proto.ListAgentProviderTurnEventsRequest{}
	}
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), providerName, req.GetSessionId(), req.GetContext())
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	if err := m.authorizeWorkflowTurnAccess(ctx, owned, req.GetContext()); err != nil {
		return nil, err
	}
	callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.providerName)
	if err != nil {
		return nil, err
	}
	events, err = owned.provider.ListTurnEvents(callCtx, &proto.ListAgentProviderTurnEventsRequest{
		TurnId:       owned.turn.ID,
		ProviderName: owned.providerName,
		AfterSeq:     req.GetAfterSeq(),
		Limit:        req.GetLimit(),
		Context:      providerReqContext,
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), providerName, "", req.GetContext())
	if err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	if err := m.authorizeWorkflowTurnAccess(ctx, owned, req.GetContext()); err != nil {
		return nil, err
	}
	callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, req.GetContext(), owned.providerName)
	if err != nil {
		return nil, err
	}
	interactions, err := owned.provider.ListInteractions(callCtx, &proto.ListAgentProviderInteractionsRequest{
		TurnId:       owned.turn.ID,
		ProviderName: owned.providerName,
		Context:      providerReqContext,
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
	providerName, err := requireAgentProviderName(req.GetProviderName())
	if err != nil {
		return nil, err
	}
	owned, err := m.findAccessibleTurn(ctx, p, req.GetTurnId(), providerName, "", req.GetContext())
	if err != nil {
		return nil, err
	}
	if !owned.sessionOwned {
		return nil, core.ErrNotFound
	}
	if err := m.authorizeWorkflowTurnAccess(ctx, owned, req.GetContext()); err != nil {
		return nil, err
	}
	observability.SetSpanAttributes(ctx, observability.AttrAgentProvider.String(owned.providerName))
	interactionID := strings.TrimSpace(req.GetInteractionId())
	if interactionID == "" {
		return nil, ErrAgentInteractionRequired
	}
	providerReq := cloneAgentRequest(req, &proto.ResolveAgentProviderInteractionRequest{})
	providerReq.InteractionId = interactionID
	providerReq.TurnId = owned.turn.ID
	providerReq.ProviderName = owned.providerName
	ctx, providerReq.Context, err = agentProviderRequestContext(ctx, p, req.GetContext(), owned.providerName)
	if err != nil {
		return nil, err
	}
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

func (m *Manager) resolveProvider(ctx context.Context, providerName string) (string, coreagent.Provider, error) {
	if m == nil || m.agent == nil {
		return "", nil, ErrAgentNotConfigured
	}
	return m.agent.ResolveProvider(ctx, strings.TrimSpace(providerName))
}

func requireAgentProviderName(providerName string) (string, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return "", ErrAgentProviderRequired
	}
	return providerName, nil
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

type agentSessionTools struct {
	config     *proto.AgentToolConfig
	refs       []coreagent.ToolRef
	listed     []coreagent.ListedTool
	toolSource coreagent.ToolSourceMode
	set        bool
}

type agentSessionToolScopeMetadata struct {
	Version int                           `json:"version"`
	Source  string                        `json:"source"`
	Refs    []agentSessionToolRefMetadata `json:"refs,omitempty"`
}

type agentSessionToolRefMetadata struct {
	System         string                     `json:"system,omitempty"`
	App            string                     `json:"app,omitempty"`
	Operation      string                     `json:"operation,omitempty"`
	Connection     string                     `json:"connection,omitempty"`
	Instance       string                     `json:"instance,omitempty"`
	CredentialMode string                     `json:"credentialMode,omitempty"`
	RunAs          *agentSessionRunAsMetadata `json:"runAs,omitempty"`
}

type agentSessionRunAsMetadata struct {
	SubjectID string `json:"subjectId,omitempty"`
}

func (m *Manager) providerCandidates(ctx context.Context, providerName string) ([]namedAgentProvider, error) {
	if m == nil || m.agent == nil {
		return nil, ErrAgentNotConfigured
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		_, provider, err := m.resolveProvider(ctx, providerName)
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
		_, provider, err := m.resolveProvider(ctx, name)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, namedAgentProvider{name: name, provider: provider})
	}
	return candidates, nil
}

func (m *Manager) authorizedProviderCandidates(ctx context.Context, p *principal.Principal, providerName string) ([]namedAgentProvider, error) {
	candidates, err := m.providerCandidates(ctx, providerName)
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

func (m *Manager) findAccessibleSession(ctx context.Context, p *principal.Principal, sessionID, providerName string, reqContext *proto.RequestContext) (*accessibleAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	var err error
	providerName, err = requireAgentProviderName(providerName)
	if err != nil {
		return nil, err
	}
	return m.findAccessibleSessionInProviders(ctx, p, sessionID, providerName, reqContext)
}

func (m *Manager) findAccessibleSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string, reqContext *proto.RequestContext) (*accessibleAgentSession, error) {
	candidates, err := m.providerCandidates(ctx, providerName)
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
		callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, reqContext, candidate.name)
		if err != nil {
			return nil, err
		}
		session, err := candidate.provider.GetSession(callCtx, &proto.GetAgentProviderSessionRequest{
			SessionId:    sessionID,
			ProviderName: candidate.name,
			Context:      providerReqContext,
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

func (m *Manager) findAccessibleTurn(ctx context.Context, p *principal.Principal, turnID, providerName, expectedSessionID string, reqContext *proto.RequestContext) (*accessibleAgentTurn, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil, core.ErrNotFound
	}
	var err error
	providerName, err = requireAgentProviderName(providerName)
	if err != nil {
		return nil, err
	}
	return m.findAccessibleTurnInProviders(ctx, p, turnID, providerName, expectedSessionID, reqContext)
}

func (m *Manager) findAccessibleTurnInProviders(ctx context.Context, p *principal.Principal, turnID, providerName, expectedSessionID string, reqContext *proto.RequestContext) (*accessibleAgentTurn, error) {
	candidates, err := m.providerCandidates(ctx, providerName)
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
		callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, reqContext, candidate.name)
		if err != nil {
			return nil, err
		}
		turn, err := candidate.provider.GetTurn(callCtx, &proto.GetAgentProviderTurnRequest{
			TurnId:       turnID,
			ProviderName: candidate.name,
			Context:      providerReqContext,
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
		if expectedSessionID = strings.TrimSpace(expectedSessionID); expectedSessionID != "" {
			if strings.TrimSpace(turn.SessionID) != expectedSessionID {
				continue
			}
		}
		sessionID := strings.TrimSpace(turn.SessionID)
		normalized, err := normalizeProviderTurn(candidate.name, sessionID, turnID, turn)
		if err != nil {
			return nil, err
		}
		session, err := m.findAccessibleSessionInProviders(ctx, p, normalized.SessionID, candidate.name, reqContext)
		if err != nil {
			if agentProviderReturnedNotFound(err) {
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

func (m *Manager) findOwnedSession(ctx context.Context, p *principal.Principal, sessionID, providerName string, reqContext *proto.RequestContext) (*ownedAgentSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, core.ErrNotFound
	}
	var err error
	providerName, err = requireAgentProviderName(providerName)
	if err != nil {
		return nil, err
	}
	return m.findOwnedSessionInProviders(ctx, p, sessionID, providerName, reqContext)
}

func (m *Manager) findOwnedSessionInProviders(ctx context.Context, p *principal.Principal, sessionID, providerName string, reqContext *proto.RequestContext) (*ownedAgentSession, error) {
	candidates, err := m.providerCandidates(ctx, providerName)
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
		callCtx, providerReqContext, err := agentProviderRequestContext(ctx, p, reqContext, candidate.name)
		if err != nil {
			return nil, err
		}
		session, err := candidate.provider.GetSession(callCtx, &proto.GetAgentProviderSessionRequest{
			SessionId:    sessionID,
			ProviderName: candidate.name,
			Context:      providerReqContext,
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

func (m *Manager) storeSessionScope(ctx context.Context, reqContext *proto.RequestContext, p *principal.Principal, providerName, sessionID string, callerKind invocation.ProviderKind, callerName string, toolRefs []coreagent.ToolRef, listedTools []coreagent.ListedTool, toolSource coreagent.ToolSourceMode) error {
	if m == nil || m.turnScopes == nil {
		return fmt.Errorf("%w: agent turn scopes are not configured", invocation.ErrInternal)
	}
	workflowRunID, workflowStepID, err := workflowTurnScope(ctx, reqContext, callerKind, providerName)
	if err != nil {
		return err
	}
	subject := agentSubjectFromPrincipal(p)
	permissions := agentTurnPermissions(ctx, p, callerKind, callerName, toolRefs)
	connections := m.agentConnectionBindings(providerName)
	if toolSource == coreagent.ToolSourceModeNone {
		connections = nil
	}
	return m.turnScopes.PutSession(agentturnscope.Scope{
		ProviderName:   providerName,
		SessionID:      sessionID,
		CallerKind:     callerKind,
		CallerName:     callerName,
		WorkflowRunID:  workflowRunID,
		WorkflowStepID: workflowStepID,
		SubjectID:      subject.SubjectID,
		Permissions:    permissions,
		ToolRefs:       append([]coreagent.ToolRef(nil), toolRefs...),
		ToolRefsSet:    true,
		ListedTools:    append([]coreagent.ListedTool(nil), listedTools...),
		ToolSource:     toolSource,
		Connections:    connections,
	})
}

func (m *Manager) sessionScope(providerName, sessionID string) (agentturnscope.Scope, bool) {
	if m == nil || m.turnScopes == nil {
		return agentturnscope.Scope{}, false
	}
	return m.turnScopes.GetSession(providerName, sessionID)
}

func (m *Manager) sessionScopeForSession(ctx context.Context, p *principal.Principal, providerName string, session *coreagent.Session) (agentturnscope.Scope, bool, error) {
	if session == nil {
		return agentturnscope.Scope{}, false, nil
	}
	sessionID := strings.TrimSpace(session.ID)
	if scope, ok := m.sessionScope(providerName, sessionID); ok {
		var err error
		scope, err = m.hydrateSessionScopeListedTools(ctx, p, providerName, scope)
		if err != nil {
			return agentturnscope.Scope{}, true, err
		}
		if m != nil && m.turnScopes != nil {
			_ = m.turnScopes.PutSession(scope)
		}
		return scope, true, nil
	}
	scope, ok, err := agentSessionScopeFromMetadata(providerName, session)
	if err != nil || !ok {
		return agentturnscope.Scope{}, ok, err
	}
	scope, err = m.hydrateSessionScopeListedTools(ctx, p, providerName, scope)
	if err != nil {
		return agentturnscope.Scope{}, true, err
	}
	if m != nil && m.turnScopes != nil {
		_ = m.turnScopes.PutSession(scope)
	}
	return scope, true, nil
}

func (m *Manager) hydrateSessionScopeListedTools(ctx context.Context, p *principal.Principal, providerName string, scope agentturnscope.Scope) (agentturnscope.Scope, error) {
	if normalizeAgentToolSource(scope.ToolSource) != coreagent.ToolSourceModeCatalog || len(scope.ToolRefs) == 0 || len(scope.ListedTools) > 0 {
		return scope, nil
	}
	listed, err := m.listAllCatalogTools(ctx, p, providerName, scope.ToolRefs)
	if err != nil {
		return agentturnscope.Scope{}, err
	}
	scope.ListedTools = listed
	return scope, nil
}

func (m *Manager) deleteSessionScope(providerName, sessionID string) {
	if m == nil || m.turnScopes == nil {
		return
	}
	m.turnScopes.DeleteSession(providerName, sessionID)
}

func workflowTurnScope(ctx context.Context, reqContext *proto.RequestContext, callerKind invocation.ProviderKind, providerName string) (string, string, error) {
	if callerKind != invocation.ProviderKindWorkflow {
		return "", "", nil
	}
	step, err := appaccessservice.WorkflowStepInvocationFromContext(agentWorkflowContext(ctx, reqContext))
	if err != nil {
		return "", "", err
	}
	if step.Kind != "agent" {
		return "", "", fmt.Errorf("%w: workflow step %q is not an agent step", invocation.ErrAuthorizationDenied, step.ID)
	}
	if step.AgentProvider != strings.TrimSpace(providerName) {
		return "", "", fmt.Errorf("%w: workflow step %q may not invoke agent provider %q", invocation.ErrAuthorizationDenied, step.ID, strings.TrimSpace(providerName))
	}
	return step.RunID, step.ID, nil
}

func (m *Manager) authorizeWorkflowTurnAccess(ctx context.Context, owned *accessibleAgentTurn, reqContext *proto.RequestContext) error {
	caller := reqContext.GetCaller()
	if invocation.ProviderKind(strings.TrimSpace(caller.GetKind())) != invocation.ProviderKindWorkflow {
		return nil
	}
	callerName := strings.TrimSpace(caller.GetName())
	step, err := appaccessservice.WorkflowStepInvocationFromContext(agentWorkflowContext(ctx, reqContext))
	if err != nil {
		return err
	}
	if callerName == "" || step.ProviderName != callerName {
		return fmt.Errorf("%w: workflow caller %q does not match workflow context provider %q", invocation.ErrAuthorizationDenied, callerName, step.ProviderName)
	}
	if step.Kind != "agent" || step.AgentProvider != owned.providerName {
		return fmt.Errorf("%w: workflow %q step %q may not access agent turn %q", invocation.ErrAuthorizationDenied, callerName, step.ID, strings.TrimSpace(owned.turn.ID))
	}
	if model := strings.TrimSpace(step.Model); model != "" {
		turnModel := strings.TrimSpace(owned.turn.Model)
		sessionModel := strings.TrimSpace(owned.session.Model)
		if turnModel != "" && turnModel != model {
			return fmt.Errorf("%w: workflow %q step %q may not access agent model %q", invocation.ErrAuthorizationDenied, callerName, step.ID, turnModel)
		}
		if sessionModel != "" && sessionModel != model {
			return fmt.Errorf("%w: workflow %q step %q may not access agent model %q", invocation.ErrAuthorizationDenied, callerName, step.ID, sessionModel)
		}
	}
	if m == nil || m.turnScopes == nil {
		return fmt.Errorf("%w: agent turn bindings are not configured", invocation.ErrInternal)
	}
	binding, ok := m.turnScopes.GetTurnBinding(owned.providerName, owned.turn.SessionID, owned.turn.ID)
	if !ok {
		return fmt.Errorf("%w: workflow %q step %q may not access agent turn %q", invocation.ErrAuthorizationDenied, callerName, step.ID, strings.TrimSpace(owned.turn.ID))
	}
	if binding.CallerKind != invocation.ProviderKindWorkflow ||
		strings.TrimSpace(binding.CallerName) != callerName ||
		strings.TrimSpace(binding.WorkflowRunID) != step.RunID ||
		strings.TrimSpace(binding.WorkflowStepID) != step.ID {
		return fmt.Errorf("%w: workflow %q step %q may not access agent turn %q", invocation.ErrAuthorizationDenied, callerName, step.ID, strings.TrimSpace(owned.turn.ID))
	}
	return nil
}

func agentWorkflowContext(ctx context.Context, reqContext *proto.RequestContext) map[string]any {
	if workflow := invocation.WorkflowContextFromContext(ctx); workflow != nil {
		return workflow
	}
	if workflow := reqContext.GetWorkflow(); workflow != nil {
		return workflow.AsMap()
	}
	return nil
}

func (m *Manager) agentConnectionBindings(providerName string) []agentturnscope.ConnectionBinding {
	if m == nil {
		return nil
	}
	names := append([]string(nil), m.agentConnections[strings.TrimSpace(providerName)]...)
	if len(names) == 0 {
		return nil
	}
	out := make([]agentturnscope.ConnectionBinding, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, agentturnscope.ConnectionBinding{Connection: name})
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
	if toolSource != coreagent.ToolSourceModeCatalog {
		return nil, fmt.Errorf("agent tool listing requires %q tool source", coreagent.ToolSourceModeCatalog)
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
	if err := validateCatalogToolRefs(refs); err != nil {
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

func (m *Manager) agentSessionTools(ctx context.Context, p *principal.Principal, providerName string, provider coreagent.Provider, config *proto.AgentToolConfig) (agentSessionTools, error) {
	if config == nil || config.GetSource() == nil {
		return agentSessionTools{}, nil
	}
	switch source := config.GetSource().(type) {
	case *proto.AgentToolConfig_None:
		return agentSessionTools{}, nil
	case *proto.AgentToolConfig_Catalog:
		if supported, err := agentProviderSupportsToolSource(ctx, provider, coreagent.ToolSourceModeCatalog); err != nil {
			return agentSessionTools{}, err
		} else if !supported {
			return agentSessionTools{}, fmt.Errorf("agent provider %q does not support tool source %q", providerName, coreagent.ToolSourceModeCatalog)
		}
		catalog := source.Catalog
		if catalog == nil {
			catalog = &proto.AgentCatalogToolConfig{}
		}
		refs, err := normalizeToolRefs(agentwire.ToolRefsFromProto(catalog.GetRefs()))
		if err != nil {
			return agentSessionTools{}, err
		}
		if err := validateCatalogToolRefs(refs); err != nil {
			return agentSessionTools{}, err
		}
		if err := m.authorizeToolRefs(ctx, p, refs); err != nil {
			return agentSessionTools{}, err
		}
		var listed []coreagent.ListedTool
		if len(refs) > 0 {
			resolved, err := m.listAllCatalogTools(ctx, p, providerName, refs)
			if err != nil {
				return agentSessionTools{}, err
			}
			listed = executableListedTools(resolved)
		}
		return agentSessionTools{
			config: &proto.AgentToolConfig{Source: &proto.AgentToolConfig_Catalog{Catalog: &proto.AgentCatalogToolConfig{
				Refs:  agentwire.ToolRefsToProto(refs),
				Tools: listedAgentToolsToProto(listed),
			}}},
			refs:       refs,
			listed:     listed,
			toolSource: coreagent.ToolSourceModeCatalog,
			set:        true,
		}, nil
	default:
		return agentSessionTools{}, fmt.Errorf("%w: agent session tools source is required", invocation.ErrInvalidInvocation)
	}
}

func (m *Manager) listAllCatalogTools(ctx context.Context, p *principal.Principal, providerName string, refs []coreagent.ToolRef) ([]coreagent.ListedTool, error) {
	var tools []coreagent.ListedTool
	pageToken := ""
	for {
		listResp, err := m.listTools(ctx, p, coreagent.ListToolsRequest{
			ProviderName: providerName,
			PageSize:     agentToolListMaxPageSize,
			PageToken:    pageToken,
			ToolRefs:     refs,
			ToolSource:   coreagent.ToolSourceModeCatalog,
		})
		if err != nil {
			return nil, err
		}
		if listResp == nil {
			return tools, nil
		}
		tools = append(tools, listResp.Tools...)
		nextPageToken := strings.TrimSpace(listResp.NextPageToken)
		if nextPageToken == "" {
			return tools, nil
		}
		if nextPageToken == pageToken {
			return nil, fmt.Errorf("%w: agent tool listing returned duplicate next page token", invocation.ErrInternal)
		}
		pageToken = nextPageToken
	}
}

func executableListedTools(tools []coreagent.ListedTool) []coreagent.ListedTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]coreagent.ListedTool, 0, len(tools))
	for i := range tools {
		if tools[i].Target.Unavailable != nil {
			continue
		}
		out = append(out, tools[i])
	}
	return out
}

func agentSessionMetadataWithToolScope(metadata map[string]any, tools agentSessionTools) (*structpb.Struct, error) {
	withScope := maps.Clone(metadata)
	if withScope == nil {
		withScope = map[string]any{}
	}
	scope, err := agentSessionToolScopeMetadataFromTools(tools)
	if err != nil {
		return nil, err
	}
	scopeValue, err := agentSessionToolScopeMetadataValue(scope)
	if err != nil {
		return nil, err
	}
	withScope[agentSessionToolScopeMetadataKey] = scopeValue
	return protoutil.StructFromMap(withScope)
}

func agentSessionToolScopeMetadataValue(scope agentSessionToolScopeMetadata) (map[string]any, error) {
	encoded, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func agentSessionToolScopeMetadataFromTools(tools agentSessionTools) (agentSessionToolScopeMetadata, error) {
	source, err := agentSessionToolScopeMetadataSource(tools.toolSource)
	if err != nil {
		return agentSessionToolScopeMetadata{}, err
	}
	return agentSessionToolScopeMetadata{
		Version: 1,
		Source:  source,
		Refs:    agentSessionToolRefsMetadataFromCore(tools.refs),
	}, nil
}

func agentSessionScopeFromMetadata(providerName string, session *coreagent.Session) (agentturnscope.Scope, bool, error) {
	if session == nil || session.Metadata == nil {
		return agentturnscope.Scope{}, false, nil
	}
	value, ok := session.Metadata[agentSessionToolScopeMetadataKey]
	if !ok {
		return agentturnscope.Scope{}, false, nil
	}
	var metadata agentSessionToolScopeMetadata
	encoded, err := json.Marshal(value)
	if err != nil {
		return agentturnscope.Scope{}, true, fmt.Errorf("%w: invalid agent session tool metadata", invocation.ErrInvalidInvocation)
	}
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return agentturnscope.Scope{}, true, fmt.Errorf("%w: invalid agent session tool metadata", invocation.ErrInvalidInvocation)
	}
	source, err := agentSessionToolScopeMetadataCoreSource(metadata.Source)
	if err != nil {
		return agentturnscope.Scope{}, true, err
	}
	refs := agentSessionToolRefsMetadataToCore(metadata.Refs)
	if source == coreagent.ToolSourceModeNone {
		refs = nil
	}
	if source == coreagent.ToolSourceModeCatalog {
		if err := validateCatalogToolRefs(refs); err != nil {
			return agentturnscope.Scope{}, true, err
		}
	}
	return agentturnscope.Scope{
		ProviderName: strings.TrimSpace(providerName),
		SessionID:    strings.TrimSpace(session.ID),
		ToolRefs:     refs,
		ToolRefsSet:  true,
		ToolSource:   source,
	}, true, nil
}

func agentSessionToolScopeMetadataSource(source coreagent.ToolSourceMode) (string, error) {
	switch normalizeAgentToolSource(source) {
	case coreagent.ToolSourceModeCatalog:
		return agentSessionToolScopeMetadataSourceCatalog, nil
	case coreagent.ToolSourceModeNone:
		return agentSessionToolScopeMetadataSourceNone, nil
	default:
		return "", fmt.Errorf("%w: unsupported agent session tool source %q", invocation.ErrInvalidInvocation, source)
	}
}

func agentSessionToolScopeMetadataCoreSource(source string) (coreagent.ToolSourceMode, error) {
	switch strings.TrimSpace(source) {
	case agentSessionToolScopeMetadataSourceCatalog:
		return coreagent.ToolSourceModeCatalog, nil
	case agentSessionToolScopeMetadataSourceNone:
		return coreagent.ToolSourceModeNone, nil
	default:
		return "", fmt.Errorf("%w: unsupported agent session tool source %q", invocation.ErrInvalidInvocation, source)
	}
}

func agentSessionToolRefsMetadataFromCore(refs []coreagent.ToolRef) []agentSessionToolRefMetadata {
	if len(refs) == 0 {
		return nil
	}
	out := make([]agentSessionToolRefMetadata, 0, len(refs))
	for i := range refs {
		ref := normalizeToolRefForCompare(refs[i])
		out = append(out, agentSessionToolRefMetadata{
			System:         ref.System,
			App:            ref.App,
			Operation:      ref.Operation,
			Connection:     ref.Connection,
			Instance:       ref.Instance,
			CredentialMode: string(ref.CredentialMode),
			RunAs:          agentSessionRunAsMetadataFromCore(ref.RunAs),
		})
	}
	return out
}

func agentSessionToolRefsMetadataToCore(refs []agentSessionToolRefMetadata) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for i := range refs {
		out = append(out, normalizeToolRefForCompare(coreagent.ToolRef{
			System:         refs[i].System,
			App:            refs[i].App,
			Operation:      refs[i].Operation,
			Connection:     refs[i].Connection,
			Instance:       refs[i].Instance,
			CredentialMode: core.ConnectionMode(refs[i].CredentialMode),
			RunAs:          agentSessionRunAsMetadataToCore(refs[i].RunAs),
		}))
	}
	return out
}

func agentSessionRunAsMetadataFromCore(runAs *core.RunAsSubject) *agentSessionRunAsMetadata {
	normalized := core.NormalizeRunAsSubject(runAs)
	if normalized == nil {
		return nil
	}
	return &agentSessionRunAsMetadata{
		SubjectID: normalized.SubjectID,
	}
}

func agentSessionRunAsMetadataToCore(runAs *agentSessionRunAsMetadata) *core.RunAsSubject {
	if runAs == nil {
		return nil
	}
	return core.NormalizeRunAsSubject(&core.RunAsSubject{
		SubjectID: runAs.SubjectID,
	})
}

func agentSessionScopeMatchesTools(scope agentturnscope.Scope, tools agentSessionTools) bool {
	if normalizeAgentToolSource(scope.ToolSource) != normalizeAgentToolSource(tools.toolSource) {
		return false
	}
	return agentToolRefsEqual(scope.ToolRefs, tools.refs)
}

func agentSessionScopeIsNoTools(scope agentturnscope.Scope) bool {
	return normalizeAgentToolSource(scope.ToolSource) == coreagent.ToolSourceModeNone && len(scope.ToolRefs) == 0
}

func agentToolRefsEqual(left, right []coreagent.ToolRef) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[agentToolRefCompareKey]int, len(left))
	for i := range left {
		counts[agentToolRefCompareKeyFromRef(left[i])]++
	}
	for i := range right {
		key := agentToolRefCompareKeyFromRef(right[i])
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}

type agentToolRefCompareKey struct {
	System         string
	App            string
	Operation      string
	Connection     string
	Instance       string
	CredentialMode core.ConnectionMode
	RunAsSubject   string
}

func agentToolRefCompareKeyFromRef(ref coreagent.ToolRef) agentToolRefCompareKey {
	normalized := normalizeToolRefForCompare(ref)
	runAs := core.NormalizeRunAsSubject(normalized.RunAs)
	key := agentToolRefCompareKey{
		System:         normalized.System,
		App:            normalized.App,
		Operation:      normalized.Operation,
		Connection:     normalized.Connection,
		Instance:       normalized.Instance,
		CredentialMode: normalized.CredentialMode,
	}
	if runAs != nil {
		key.RunAsSubject = runAs.SubjectID
	}
	return key
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
	prov, err := m.providers.GetWithContext(ctx, appName)
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
	if principal.IsNonUserPrincipal(p) && (connection != "" || instance != "") {
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
		App:            appName,
		Operation:      opMeta.ID,
		Connection:     connection,
		Instance:       sessionInstance,
		CredentialMode: credentialMode,
		RunAs:          core.NormalizeRunAsSubject(ref.RunAs),
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
	app            string
	operation      string
	connection     string
	instance       string
	credentialMode core.ConnectionMode
	runAs          core.RunAsSubject
}

func agentToolTargetKeyFromRef(ref coreagent.ToolRef) agentToolTargetKey {
	return agentToolTargetKey{
		system:         strings.TrimSpace(ref.System),
		app:            strings.TrimSpace(ref.App),
		operation:      strings.TrimSpace(ref.Operation),
		connection:     core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		instance:       strings.TrimSpace(ref.Instance),
		credentialMode: ref.CredentialMode,
		runAs:          agentToolRunAsKey(ref.RunAs),
	}
}

func agentToolTargetKeyFromTarget(target coreagent.ToolTarget) agentToolTargetKey {
	return agentToolTargetKey{
		system:         strings.TrimSpace(target.System),
		app:            strings.TrimSpace(target.App),
		operation:      strings.TrimSpace(target.Operation),
		connection:     core.ResolveConnectionAlias(strings.TrimSpace(target.Connection)),
		instance:       strings.TrimSpace(target.Instance),
		credentialMode: target.CredentialMode,
		runAs:          agentToolRunAsKey(target.RunAs),
	}
}

func (k agentToolTargetKey) String() string {
	if k.system != "" {
		return strings.Join([]string{"system", k.system, k.operation}, "/")
	}
	parts := []string{k.app, k.operation}
	runAsKey := agentToolRunAsKeyString(k.runAs)
	if k.connection != "" || k.instance != "" || k.credentialMode != "" || runAsKey != "" {
		parts = append(parts, k.connection, k.instance, string(k.credentialMode), runAsKey)
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
		prov, err := m.providers.GetWithContext(ctx, appName)
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

func listedAgentToolsToProto(tools []coreagent.ListedTool) []*proto.ListedAgentTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]*proto.ListedAgentTool, 0, len(tools))
	for i := range tools {
		out = append(out, listedAgentToolToProto(tools[i]))
	}
	return out
}

func listedAgentToolToProto(tool coreagent.ListedTool) *proto.ListedAgentTool {
	return &proto.ListedAgentTool{
		Id:           strings.TrimSpace(tool.ToolID),
		McpName:      strings.TrimSpace(tool.MCPName),
		Title:        strings.TrimSpace(tool.Title),
		Description:  strings.TrimSpace(tool.Description),
		InputSchema:  strings.TrimSpace(tool.InputSchemaJSON),
		OutputSchema: strings.TrimSpace(tool.OutputSchemaJSON),
		Annotations:  operationAnnotationsToProto(tool.Annotations),
		Ref:          agentwire.ToolRefToProto(tool.Ref),
		Tags:         append([]string(nil), tool.Tags...),
		SearchText:   strings.TrimSpace(tool.SearchText),
	}
}

func operationAnnotationsToProto(annotations core.CapabilityAnnotations) *proto.OperationAnnotations {
	if annotations.ReadOnlyHint == nil &&
		annotations.IdempotentHint == nil &&
		annotations.DestructiveHint == nil &&
		annotations.OpenWorldHint == nil {
		return nil
	}
	return &proto.OperationAnnotations{
		ReadOnlyHint:    annotations.ReadOnlyHint,
		IdempotentHint:  annotations.IdempotentHint,
		DestructiveHint: annotations.DestructiveHint,
		OpenWorldHint:   annotations.OpenWorldHint,
	}
}

func normalizeToolRefForCompare(ref coreagent.ToolRef) coreagent.ToolRef {
	ref.System = strings.TrimSpace(ref.System)
	ref.App = strings.TrimSpace(ref.App)
	ref.Operation = strings.TrimSpace(ref.Operation)
	ref.Connection = strings.TrimSpace(ref.Connection)
	ref.Instance = strings.TrimSpace(ref.Instance)
	ref.CredentialMode = core.NormalizeOptionalConnectionMode(ref.CredentialMode)
	ref.RunAs = core.NormalizeRunAsSubject(ref.RunAs)
	return ref
}

func normalizeAgentToolSource(source coreagent.ToolSourceMode) coreagent.ToolSourceMode {
	source = coreagent.ToolSourceMode(strings.TrimSpace(string(source)))
	if source == "" {
		return coreagent.ToolSourceModeUnspecified
	}
	return source
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
	if principal.IsNonUserPrincipal(p) && (connection != "" || instance != "") {
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
		if _, err := m.providers.GetWithContext(ctx, appName); err != nil {
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
	return true
}

func (m *Manager) allowsAgentProvider(ctx context.Context, p *principal.Principal, provider string) bool {
	return m.allowProvider(ctx, p, provider) && principal.AllowsProviderPermission(p, provider)
}

func (m *Manager) allowOperation(ctx context.Context, p *principal.Principal, provider, operation string) bool {
	return true
}

func (m *Manager) providerAccessContext(ctx context.Context, p *principal.Principal, provider string) invocation.AccessContext {
	return invocation.AccessContext{}
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		MCPConnection:     m.mcpConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func providerSessionOwnedBy(session *coreagent.Session, p *principal.Principal) bool {
	if session == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(session.CreatedBySubjectID) == subjectID
}

func providerTurnOwnedBy(turn *coreagent.Turn, p *principal.Principal) bool {
	if turn == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(turn.CreatedBySubjectID) == subjectID
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

func normalizeProviderSessionForCreate(providerName string, session *coreagent.Session) (*coreagent.Session, error) {
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
	if err := validateSuccessfulAgentTurnOutputVariant(&cloned); err != nil {
		return nil, err
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
	if err := validateSuccessfulAgentTurnOutputVariant(&cloned); err != nil {
		return nil, err
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
	raw := projectAgentToolInputSchema(agentToolInputSchema(op), op.Parameters)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s input schema: %w", op.ID, err)
	}
	return out, nil
}

func validateCatalogToolRefs(refs []coreagent.ToolRef) error {
	if err := coreagent.ValidateCatalogToolRefs(refs, "tool_refs"); err != nil {
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
	// Prefer the legacy OutputSchema field; fall back to the unary response
	// schema introduced by the OperationResponseSpec change.
	schema := op.OutputSchema
	if len(schema) == 0 && op.Response != nil && op.Response.Unary != nil {
		schema = op.Response.Unary.Schema
	}
	if len(schema) == 0 || len(schema) > agentToolSchemaMaxBytes {
		return ""
	}
	return string(schema)
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
		App:            strings.TrimSpace(ref.App),
		Operation:      strings.TrimSpace(ref.Operation),
		Connection:     core.ResolveConnectionAlias(strings.TrimSpace(ref.Connection)),
		Instance:       strings.TrimSpace(ref.Instance),
		CredentialMode: ref.CredentialMode,
		RunAs:          core.NormalizeRunAsSubject(ref.RunAs),
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
		System:         target.System,
		App:            target.App,
		Operation:      target.Operation,
		Connection:     target.Connection,
		Instance:       target.Instance,
		CredentialMode: target.CredentialMode,
		RunAs:          core.NormalizeRunAsSubject(target.RunAs),
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
	if m == nil || m.toolIDs == nil {
		return "", fmt.Errorf("%w: agent tool ids are not configured", invocation.ErrInternal)
	}
	id, err := m.toolIDs.Mint(target)
	if err != nil {
		return "", fmt.Errorf("%w: mint agent tool id: %v", invocation.ErrInternal, err)
	}
	return id, nil
}

func validateToolSource(source coreagent.ToolSourceMode) (coreagent.ToolSourceMode, error) {
	source = normalizeToolSource(source)
	switch source {
	case coreagent.ToolSourceModeCatalog, coreagent.ToolSourceModeNone:
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", source)
	}
	return source, nil
}

func agentOutputFromProto(output *proto.AgentOutput) coreagent.Output {
	if output == nil {
		return coreagent.Output{}
	}
	if structured := output.GetStructured(); structured != nil {
		return coreagent.Output{
			Structured: &coreagent.StructuredOutput{
				Schema: protoutil.MapFromStruct(structured.GetSchema()),
			},
		}
	}
	if output.GetText() != nil {
		return coreagent.Output{Text: &coreagent.TextOutput{}}
	}
	return coreagent.Output{}
}

func validateAgentOutput(output coreagent.Output) error {
	textSet := output.Text != nil
	structuredSet := output.Structured != nil
	if textSet == structuredSet {
		return fmt.Errorf("%w: exactly one of output.text or output.structured is required", invocation.ErrInvalidInvocation)
	}
	if output.Structured != nil {
		if err := validateAgentSchema(output.Structured.Schema); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("%w: output.structured.schema must be a non-empty JSON schema object with type %q", invocation.ErrInvalidInvocation, "object")
	}
	rawType, ok := schema["type"]
	if !ok {
		return fmt.Errorf("%w: output.structured.schema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	typeValue, ok := rawType.(string)
	if !ok || strings.TrimSpace(typeValue) != "object" {
		return fmt.Errorf("%w: output.structured.schema.type must be %q", invocation.ErrInvalidInvocation, "object")
	}
	return nil
}

func validateAgentTurnOutput(requested coreagent.Output, turn *coreagent.Turn) error {
	if turn == nil || turn.Status != coreagent.ExecutionStatusSucceeded {
		return nil
	}
	if err := validateSuccessfulAgentTurnOutputVariant(turn); err != nil {
		return err
	}
	if requested.Text != nil {
		if turn.Output.Text == nil {
			return fmt.Errorf("agent provider returned successful turn without text output")
		}
		return nil
	}
	if requested.Structured != nil {
		if turn.Output.Structured == nil {
			return fmt.Errorf("agent provider returned successful turn without structured output")
		}
		if err := validateAgentStructuredValue(requested.Structured.Schema, turn.Output.Structured.Value); err != nil {
			return fmt.Errorf("agent provider returned invalid structured output: %w", err)
		}
	}
	return nil
}

func validateSuccessfulAgentTurnOutputVariant(turn *coreagent.Turn) error {
	if turn == nil || turn.Status != coreagent.ExecutionStatusSucceeded {
		return nil
	}
	textSet := turn.Output.Text != nil
	structuredSet := turn.Output.Structured != nil
	if textSet == structuredSet {
		return fmt.Errorf("agent provider returned successful turn without exactly one of output.text or output.structured")
	}
	return nil
}

func validateAgentStructuredValue(schema map[string]any, value map[string]any) error {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("agent-output.schema", schema); err != nil {
		return fmt.Errorf("invalid response schema: %w", err)
	}
	compiled, err := compiler.Compile("agent-output.schema")
	if err != nil {
		return fmt.Errorf("compile response schema: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return err
	}
	return nil
}

func agentTurnPermissions(ctx context.Context, p *principal.Principal, callerKind invocation.ProviderKind, callerName string, refs []coreagent.ToolRef) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	if permissions, ok := compactAgentRunPermissionsForRefs(p, refs); ok {
		return permissions
	}
	if shouldUseResolvedUserToolScope(ctx, p, callerKind, callerName, refs) {
		return nil
	}
	return principal.PermissionsToAccessPermissions(p.EffectivePermissions())
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
		perms := p.EffectivePermissions()
		if _, ok := providerWide[app]; ok {
			if p != nil && perms != nil {
				tokenOps, ok := perms[app]
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

func shouldUseResolvedUserToolScope(ctx context.Context, p *principal.Principal, callerKind invocation.ProviderKind, callerName string, refs []coreagent.ToolRef) bool {
	if callerKind != invocation.ProviderKindApp || strings.TrimSpace(callerName) == "" {
		return false
	}
	if invocation.InvocationSurfaceFromContext(ctx) != invocation.InvocationSurfaceHTTP {
		return false
	}
	if p == nil || p.Kind != principal.KindUser || len(p.Scopes) == 0 {
		return false
	}
	perms := p.EffectivePermissions()
	if perms == nil {
		return false
	}
	if _, ok := perms[callerName]; !ok {
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
	source = coreagent.ToolSourceMode(strings.TrimSpace(string(source)))
	if source == "" {
		return coreagent.ToolSourceModeCatalog
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
			if ref.Connection != "" || ref.Instance != "" || ref.CredentialMode != "" || ref.RunAs != nil || ref.Title != "" || ref.Description != "" {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] system refs cannot include connection, instance, credential mode, runAs, title, or description", invocation.ErrInvalidInvocation, idx)
			}
			out = append(out, ref)
			continue
		}
		if ref.App == "" {
			return nil, fmt.Errorf("%w: agent tool_refs[%d].app is required", invocation.ErrProviderNotFound, idx)
		}
		if ref.App == agentToolSearchAllApp {
			if ref.Operation != "" || ref.Connection != "" || ref.Instance != "" || ref.Title != "" || ref.Description != "" || ref.CredentialMode != "" || ref.RunAs != nil {
				return nil, fmt.Errorf("%w: agent tool_refs[%d] global search ref cannot include operation, connection, instance, credential mode, runAs, title, or description", invocation.ErrProviderNotFound, idx)
			}
		}
		if ref.RunAs != nil {
			return nil, fmt.Errorf("%w: agent tool_refs[%d] runAs delegation is not supported for provider tool refs", invocation.ErrAuthorizationDenied, idx)
		}
		out = append(out, ref)
	}
	return out, nil
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
		SubjectID: normalized.SubjectID,
	}
}

func agentToolRunAsKeyString(subject core.RunAsSubject) string {
	if subject == (core.RunAsSubject{}) {
		return ""
	}
	return strings.TrimSpace(subject.SubjectID)
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

func agentProviderCapabilitiesSupportToolSource(caps *coreagent.ProviderCapabilities, source coreagent.ToolSourceMode) bool {
	if caps == nil {
		return false
	}
	for _, supported := range caps.SupportedToolSources {
		if normalizeAgentToolSource(supported) == source {
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
	cloned.Output = coreagent.TurnOutput{}
	return &cloned
}

func normalizeAgentToolCredentialMode(mode core.ConnectionMode) (core.ConnectionMode, error) {
	switch core.NormalizeOptionalConnectionMode(mode) {
	case "":
		return "", nil
	case core.ConnectionModeNone:
		return core.ConnectionModeNone, nil
	case core.ConnectionModeSubject:
		return core.ConnectionModeSubject, nil
	default:
		return "", fmt.Errorf("unsupported agent tool credential mode %q", mode)
	}
}

func agentSubjectFromPrincipal(p *principal.Principal) core.RunAsSubject {
	p = principal.Canonicalized(p)
	if p == nil {
		return core.RunAsSubject{}
	}
	return core.RunAsSubject{
		SubjectID: strings.TrimSpace(p.SubjectID),
	}
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

var _ Service = (*Manager)(nil)
