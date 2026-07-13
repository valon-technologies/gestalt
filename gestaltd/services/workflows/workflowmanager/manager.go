package workflowmanager

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

var (
	ErrWorkflowNotConfigured       = errors.New("workflow is not configured")
	ErrWorkflowSubjectRequired     = errors.New("workflow subject is required")
	ErrDuplicateWorkflowObjects    = errors.New("workflow object matched multiple providers")
	ErrWorkflowEventMatchRequired  = errors.New("workflow trigger match.type is required")
	ErrWorkflowEventSourceRequired = errors.New("workflow event source is required")
	ErrWorkflowEventTypeRequired   = errors.New("workflow event type is required")
	ErrWorkflowKeyRequired         = errors.New("workflow key is required")
	ErrWorkflowSignalNameRequired  = errors.New("workflow signal name is required")
)

const defaultWorkflowEventSpecVersion = "1.0"
const workflowRunListDefaultPageSize = 100
const workflowRunListMaxPageSize = 200

func normalizeWorkflowCaller(caller invocation.CallerProvider) invocation.CallerProvider {
	caller.Kind = invocation.ProviderKind(strings.TrimSpace(string(caller.Kind)))
	caller.Name = strings.TrimSpace(caller.Name)
	if caller.Kind == "" || caller.Name == "" {
		return invocation.CallerProvider{}
	}
	return caller
}

func workflowCaller(ctx context.Context, requested invocation.CallerProvider) invocation.CallerProvider {
	if caller := normalizeWorkflowCaller(requested); caller.Kind != "" && caller.Name != "" {
		return caller
	}
	return normalizeWorkflowCaller(invocation.CallerProviderFromContext(ctx))
}

func withWorkflowCaller(ctx context.Context, caller invocation.CallerProvider) context.Context {
	caller = normalizeWorkflowCaller(caller)
	if caller.Kind == "" || caller.Name == "" {
		return ctx
	}
	return invocation.WithCallerProvider(ctx, caller.Kind, caller.Name)
}

func workflowCallerAuditAppName(caller invocation.CallerProvider) string {
	caller = normalizeWorkflowCaller(caller)
	if caller.Kind != invocation.ProviderKindApp {
		return ""
	}
	return caller.Name
}

func workflowProviderRequestContext(ctx context.Context, p *principal.Principal, caller invocation.CallerProvider) (*proto.RequestContext, error) {
	return appaccessservice.RequestContextProto(principal.WithPrincipal(ctx, p), "", normalizeWorkflowCaller(caller))
}

type WorkflowControl interface {
	ResolveProvider(ctx context.Context, name string) (providerName string, provider coreworkflow.Provider, err error)
	ProviderNames() []string
}

type AgentControl interface {
	ResolveProvider(ctx context.Context, name string) (providerName string, provider coreagent.Provider, err error)
}

type Service interface {
	ApplyDefinition(ctx context.Context, p *principal.Principal, req DefinitionApply) (*ManagedDefinition, error)
	GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error)
	ListDefinitions(ctx context.Context, p *principal.Principal) (*ListDefinitionsResponse, error)
	SetDefinitionPaused(ctx context.Context, p *principal.Principal, definitionID string, paused bool) (*ManagedDefinition, error)
	SetActivationPaused(ctx context.Context, p *principal.Principal, definitionID, activationID string, paused bool) (*ManagedDefinition, error)
	DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) error
	ListRuns(ctx context.Context, p *principal.Principal, req coreworkflow.ListRunsRequest) (*ListRunsResponse, error)
	StartRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error)
	GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error)
	GetRunEvents(ctx context.Context, p *principal.Principal, runID string) (*proto.GetWorkflowProviderRunEventsResponse, error)
	GetRunOutput(ctx context.Context, p *principal.Principal, runID string) (*proto.GetWorkflowProviderRunOutputResponse, error)
	CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*ManagedRun, error)
	SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (*ManagedRunSignal, error)
	SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (*ManagedRunSignal, error)
	DeliverEvent(ctx context.Context, p *principal.Principal, req EventDeliver) (coreworkflow.Event, error)
}

type ListDefinitionsResponse struct {
	Definitions []*ManagedDefinition
}

type ListRunsResponse struct {
	Runs          []*ManagedRun
	NextPageToken string
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Workflow          WorkflowControl
	Agent             AgentControl
	AgentManager      agentmanager.Service
	Invoker           invocation.Invoker
	Audit             core.AuditSink
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	MCPConnection     map[string]string
	Now               func() time.Time
	Logger            *slog.Logger
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	workflow          WorkflowControl
	agent             AgentControl
	agentManager      agentmanager.Service
	invoker           invocation.Invoker
	audit             core.AuditSink
	defaultConnection map[string]string
	catalogConnection map[string]string
	mcpConnection     map[string]string
	now               func() time.Time
	logger            *slog.Logger
}

type DefinitionApply struct {
	ProviderName   string
	Spec           coreworkflow.DefinitionSpec
	IdempotencyKey string
	Caller         invocation.CallerProvider
}

type RunStart struct {
	ProviderName                 string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	WorkflowKey                  string
	Caller                       invocation.CallerProvider
}

type RunSignal struct {
	RunID  string
	Signal coreworkflow.Signal
}

type RunSignalOrStart struct {
	ProviderName                 string
	WorkflowKey                  string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	Signal                       coreworkflow.Signal
	Caller                       invocation.CallerProvider
}

type EventDeliver struct {
	ProviderName string
	// AppName is trusted owner context. Callers must derive or authorize it before
	// entering the workflow manager; the manager only normalizes and forwards it.
	AppName string
	Event   coreworkflow.Event
}

type ManagedDefinition struct {
	ProviderName string
	Definition   *coreworkflow.Definition
	provider     coreworkflow.Provider
}

type ManagedRun struct {
	ProviderName string
	Run          *coreworkflow.Run
	provider     coreworkflow.Provider
}

type ManagedRunSignal struct {
	ProviderName string
	Run          *coreworkflow.Run
	Signal       coreworkflow.Signal
	StartedRun   bool
	WorkflowKey  string
	provider     coreworkflow.Provider
}

const (
	workflowAuditSource = "workflow_manager"

	workflowAuditOperationDefinitionApply  = "workflow.definition.apply"
	workflowAuditOperationDefinitionPause  = "workflow.definition.pause"
	workflowAuditOperationDefinitionDelete = "workflow.definition.delete"
	workflowAuditOperationActivationPause  = "workflow.activation.pause"
	workflowAuditOperationRunStart         = "workflow.run.start"
	workflowAuditOperationRunSignal        = "workflow.run.signal"
	workflowAuditOperationRunSignalOrStart = "workflow.run.signal_or_start"
	workflowAuditOperationRunCancel        = "workflow.run.cancel"
	workflowAuditOperationEventDeliver     = "workflow.event.deliver"

	workflowAuditTargetDefinition = "workflow_definition"
	workflowAuditTargetRun        = "workflow_run"
	workflowAuditTargetEvent      = "workflow_event"
)

type workflowAuditEvent struct {
	sink  core.AuditSink
	entry core.AuditEntry
}

func New(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		providers:         cfg.Providers,
		workflow:          cfg.Workflow,
		agent:             cfg.Agent,
		agentManager:      cfg.AgentManager,
		invoker:           cfg.Invoker,
		audit:             cfg.Audit,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		mcpConnection:     maps.Clone(cfg.MCPConnection),
		now:               now,
		logger:            cfg.Logger,
	}
}

func (m *Manager) log() *slog.Logger {
	if m != nil && m.logger != nil {
		return m.logger
	}
	return slog.Default()
}

func (m *Manager) beginWorkflowAudit(ctx context.Context, p *principal.Principal, operation string) (context.Context, *workflowAuditEvent) {
	ctx, entry := invocation.BuildAuditEntry(ctx, p, workflowAuditSource, "", operation)
	var sink core.AuditSink
	if m != nil {
		sink = m.audit
	}
	return ctx, &workflowAuditEvent{sink: sink, entry: entry}
}

func (a *workflowAuditEvent) setProvider(providerName string) {
	if a == nil {
		return
	}
	a.entry.Provider = strings.TrimSpace(providerName)
}

func (a *workflowAuditEvent) setCallerApp(callerApp string) {
	if a == nil {
		return
	}
	a.entry.CallerApp = strings.TrimSpace(callerApp)
}

func (a *workflowAuditEvent) setWorkflowKey(workflowKey string) {
	if a == nil {
		return
	}
	a.entry.WorkflowKeySHA256 = workflowManagerSHA256(workflowKey)
}

func (a *workflowAuditEvent) setObjectTarget(kind, id, name string) {
	if a == nil {
		return
	}
	a.entry.TargetKind = strings.TrimSpace(kind)
	a.entry.TargetID = strings.TrimSpace(id)
	a.entry.TargetName = strings.TrimSpace(name)
}

func (a *workflowAuditEvent) setWorkflowTarget(target coreworkflow.Target) {
	if a == nil {
		return
	}
	a.entry.WorkflowTargetKind = workflowAuditTargetKind(target)
	for i := range target.Steps {
		if target.Steps[i].App == nil {
			continue
		}
		appName := strings.TrimSpace(target.Steps[i].App.Name)
		operation := strings.TrimSpace(target.Steps[i].App.Operation)
		if appName == "" || operation == "" {
			continue
		}
		a.entry.WorkflowTargetProvider = appName
		a.entry.WorkflowTargetOperation = operation
		return
	}
}

func (a *workflowAuditEvent) setWorkflowTargetAuthorizationFailure(target coreworkflow.Target, failure targetAuthorizationFailure) {
	if a == nil {
		return
	}
	a.entry.AuthorizationDecision = workflowAuditTargetAuthorizationDecision(failure)
	a.entry.WorkflowTargetKind = workflowAuditTargetKind(target)
	a.entry.WorkflowTargetComponent = strings.TrimSpace(failure.component)
	a.entry.WorkflowTargetProvider = strings.TrimSpace(failure.provider)
	a.entry.WorkflowTargetOperation = strings.TrimSpace(failure.operation)
}

func (a *workflowAuditEvent) clone() *workflowAuditEvent {
	if a == nil {
		return nil
	}
	copied := *a
	return &copied
}

func (a *workflowAuditEvent) finish(ctx context.Context, err error) {
	if a == nil || a.sink == nil {
		return
	}
	a.entry.Allowed = err == nil
	if err != nil {
		a.entry.Error = workflowManagerErrorType(err)
	}
	a.sink.Log(ctx, a.entry)
}

func workflowAuditTargetKind(target coreworkflow.Target) string {
	if len(target.Steps) > 0 {
		return "steps"
	}
	return ""
}

func workflowAuditTargetAuthorizationDecision(failure targetAuthorizationFailure) string {
	reason := strings.TrimSpace(failure.reason)
	if reason == "" {
		return ""
	}
	return "workflow_target_" + reason
}

func workflowRunID(run *coreworkflow.Run) string {
	if run == nil {
		return ""
	}
	return strings.TrimSpace(run.ID)
}

func (m *Manager) ListRuns(ctx context.Context, p *principal.Principal, req coreworkflow.ListRunsRequest) (*ListRunsResponse, error) {
	pageSize, err := effectiveWorkflowRunListPageSize(req.PageSize)
	if err != nil {
		return nil, err
	}
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{})
	if err != nil {
		return nil, err
	}
	providerNames := m.providerNames()
	states, err := decodeWorkflowRunListPageToken(req.PageToken, providerNames, req, pageSize)
	if err != nil {
		return nil, err
	}
	nextStates := cloneWorkflowRunProviderPageStates(states)
	candidates := make([]workflowRunListCandidate, 0, pageSize)
	providerCandidateTotals := map[int]int{}
	providerSourcesExhausted := map[int]bool{}
	for providerIndex, providerName := range providerNames {
		state := states[providerIndex]
		if state.Exhausted {
			providerSourcesExhausted[providerIndex] = true
			continue
		}
		_, provider, err := m.resolveProvider(ctx, providerName)
		if err != nil {
			return nil, err
		}
		providerPageToken := state.ProviderToken
		providerOffset := state.ProviderOffset
		seenProviderTokens := map[string]struct{}{}
		seenProviderRunIDs := map[string]struct{}{}
		skipProviderRunIDs := workflowRunProviderSkipSet(state.SkipRunIDs)
		providerCandidates := 0
		providerStateSet := false
		providerSourceExhausted := false
		for providerCandidates < pageSize {
			if _, ok := seenProviderTokens[providerPageToken]; ok {
				if !providerStateSet {
					nextStates[providerIndex] = workflowRunProviderPageState{
						ProviderName: providerName,
						Exhausted:    true,
					}
				}
				providerSourceExhausted = true
				break
			}
			seenProviderTokens[providerPageToken] = struct{}{}
			resp, err := provider.ListRuns(ctx, &proto.ListWorkflowProviderRunsRequest{
				PageSize:  int32(pageSize),
				PageToken: providerPageToken,
				Status:    workflowwire.RunStatusToProto(req.Status),
				TargetApp: strings.TrimSpace(req.TargetApp),
				Context:   reqContext,
			})
			if err != nil {
				return nil, err
			}
			if resp == nil {
				if !providerStateSet {
					nextStates[providerIndex] = workflowRunProviderPageState{
						ProviderName: providerName,
						Exhausted:    true,
					}
				}
				providerSourceExhausted = true
				break
			}

			nextProviderToken := strings.TrimSpace(resp.GetNextPageToken())
			for rawIndex, runProto := range resp.GetRuns() {
				if rawIndex < providerOffset {
					continue
				}
				if runProto == nil {
					continue
				}
				run, err := workflowwire.RunFromProto(runProto)
				if err != nil {
					return nil, err
				}
				runID := strings.TrimSpace(run.ID)
				if runID != "" {
					if _, ok := skipProviderRunIDs[runID]; ok {
						continue
					}
					if _, ok := seenProviderRunIDs[runID]; ok {
						continue
					}
					seenProviderRunIDs[runID] = struct{}{}
				}
				if !runMatchesListFilters(run, req) {
					continue
				}
				managed := &ManagedRun{
					ProviderName: providerName,
					Run:          run,
					provider:     provider,
				}
				if !m.runAccessible(ctx, p, managed) {
					continue
				}
				if !providerStateSet {
					nextStates[providerIndex] = workflowRunProviderPageState{
						ProviderName:   providerName,
						ProviderToken:  strings.TrimSpace(providerPageToken),
						ProviderOffset: rawIndex,
						SkipRunIDs:     cloneWorkflowRunSkipIDs(state.SkipRunIDs),
					}
					providerStateSet = true
				}
				resume := workflowRunProviderPageState{
					ProviderName:   providerName,
					ProviderToken:  strings.TrimSpace(providerPageToken),
					ProviderOffset: rawIndex + 1,
					SkipRunIDs:     cloneWorkflowRunSkipIDs(state.SkipRunIDs),
				}
				if rawIndex+1 >= len(resp.GetRuns()) {
					if nextProviderToken == "" {
						resume = workflowRunProviderPageState{ProviderName: providerName, Exhausted: true}
					} else {
						resume = workflowRunProviderPageState{ProviderName: providerName, ProviderToken: nextProviderToken}
					}
				}
				candidates = append(candidates, workflowRunListCandidate{
					ProviderIndex: providerIndex,
					ProviderOrder: providerCandidates,
					RunID:         runID,
					ResumeState:   resume,
					Run:           managed,
				})
				providerCandidates++
				if providerCandidates >= pageSize && nextProviderToken != "" {
					break
				}
			}

			if nextProviderToken == "" {
				if !providerStateSet {
					nextStates[providerIndex] = workflowRunProviderPageState{
						ProviderName: providerName,
						Exhausted:    true,
					}
				}
				providerSourceExhausted = true
				break
			}
			if providerCandidates >= pageSize {
				break
			}
			if providerCandidates > 0 {
				break
			}
			providerPageToken = nextProviderToken
			providerOffset = 0
		}
		providerCandidateTotals[providerIndex] = providerCandidates
		providerSourcesExhausted[providerIndex] = providerSourceExhausted || nextStates[providerIndex].Exhausted
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return workflowRunListCandidateLess(candidates[i], candidates[j])
	})
	out := make([]*ManagedRun, 0, pageSize)
	selectedByProvider := map[int][]workflowRunListCandidate{}
	for _, candidate := range candidates {
		if len(out) >= pageSize {
			break
		}
		out = append(out, candidate.Run)
		selectedByProvider[candidate.ProviderIndex] = append(selectedByProvider[candidate.ProviderIndex], candidate)
	}
	for providerIndex, selected := range selectedByProvider {
		nextStates[providerIndex] = workflowRunProviderResumeState(nextStates[providerIndex], selected, providerCandidateTotals[providerIndex], providerSourcesExhausted[providerIndex])
	}
	if len(out) == 0 || workflowRunProviderPageStatesExhausted(nextStates) {
		return &ListRunsResponse{Runs: out}, nil
	}
	return &ListRunsResponse{
		Runs:          out,
		NextPageToken: workflowRunListNextPageToken(providerNames, req, pageSize, nextStates),
	}, nil
}

type workflowRunListCandidate struct {
	ProviderIndex int
	ProviderOrder int
	RunID         string
	ResumeState   workflowRunProviderPageState
	Run           *ManagedRun
}

func workflowRunListCandidateLess(left, right workflowRunListCandidate) bool {
	if managedRunLatestFirst(left.Run, right.Run) {
		return true
	}
	if managedRunLatestFirst(right.Run, left.Run) {
		return false
	}
	return left.ProviderIndex < right.ProviderIndex
}

func managedRunLatestFirst(left, right *ManagedRun) bool {
	leftRun := workflowManagedRunValue(left)
	rightRun := workflowManagedRunValue(right)
	if leftRun == nil {
		return false
	}
	if rightRun == nil {
		return true
	}
	if leftRun.CreatedAt != nil && rightRun.CreatedAt != nil && !leftRun.CreatedAt.Equal(*rightRun.CreatedAt) {
		return leftRun.CreatedAt.After(*rightRun.CreatedAt)
	}
	leftID := strings.TrimSpace(leftRun.ID)
	rightID := strings.TrimSpace(rightRun.ID)
	if leftID != rightID {
		return leftID < rightID
	}
	leftProvider := ""
	rightProvider := ""
	if left != nil {
		leftProvider = strings.TrimSpace(left.ProviderName)
	}
	if right != nil {
		rightProvider = strings.TrimSpace(right.ProviderName)
	}
	return leftProvider < rightProvider
}

func workflowManagedRunValue(managed *ManagedRun) *coreworkflow.Run {
	if managed == nil {
		return nil
	}
	return managed.Run
}

func effectiveWorkflowRunListPageSize(pageSize int) (int, error) {
	if pageSize < 0 {
		return 0, fmt.Errorf("%w: page_size must be non-negative", invocation.ErrInvalidInvocation)
	}
	if pageSize == 0 {
		return workflowRunListDefaultPageSize, nil
	}
	if pageSize > workflowRunListMaxPageSize {
		return workflowRunListMaxPageSize, nil
	}
	return pageSize, nil
}

const workflowRunListPageTokenVersion = 1

type workflowRunListPageToken struct {
	Version             int                            `json:"v"`
	ProviderFingerprint string                         `json:"providerFingerprint"`
	Providers           []workflowRunProviderPageState `json:"providers"`
	PageSize            int                            `json:"pageSize"`
	TargetApp           string                         `json:"targetApp,omitempty"`
	Status              coreworkflow.RunStatus         `json:"status,omitempty"`
}

type workflowRunProviderPageState struct {
	ProviderName   string   `json:"providerName"`
	ProviderToken  string   `json:"providerToken,omitempty"`
	ProviderOffset int      `json:"providerOffset,omitempty"`
	Exhausted      bool     `json:"exhausted,omitempty"`
	SkipRunIDs     []string `json:"skipRunIds,omitempty"`
}

func decodeWorkflowRunListPageToken(raw string, providerNames []string, req coreworkflow.ListRunsRequest, pageSize int) ([]workflowRunProviderPageState, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return initialWorkflowRunProviderPageStates(providerNames), nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	var token workflowRunListPageToken
	if err := json.Unmarshal(decoded, &token); err != nil {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	if token.Version != workflowRunListPageTokenVersion {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	if token.ProviderFingerprint != workflowRunProviderListFingerprint(providerNames) {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	if len(token.Providers) != len(providerNames) {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	if token.PageSize != pageSize {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	if token.TargetApp != strings.TrimSpace(req.TargetApp) || token.Status != req.Status {
		return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
	}
	for i := range token.Providers {
		state := &token.Providers[i]
		if strings.TrimSpace(state.ProviderName) != providerNames[i] || state.ProviderOffset < 0 {
			return nil, fmt.Errorf("%w: page_token is invalid", invocation.ErrInvalidInvocation)
		}
		state.ProviderName = providerNames[i]
		state.ProviderToken = strings.TrimSpace(state.ProviderToken)
		if state.Exhausted {
			state.ProviderToken = ""
			state.ProviderOffset = 0
			state.SkipRunIDs = nil
			continue
		}
		state.SkipRunIDs = normalizeWorkflowRunSkipIDs(state.SkipRunIDs)
	}
	return token.Providers, nil
}

func workflowRunListNextPageToken(providerNames []string, req coreworkflow.ListRunsRequest, pageSize int, states []workflowRunProviderPageState) string {
	token := workflowRunListPageToken{
		Version:             workflowRunListPageTokenVersion,
		ProviderFingerprint: workflowRunProviderListFingerprint(providerNames),
		Providers:           cloneWorkflowRunProviderPageStates(states),
		PageSize:            pageSize,
		TargetApp:           strings.TrimSpace(req.TargetApp),
		Status:              req.Status,
	}
	return encodeWorkflowRunListPageToken(token)
}

func initialWorkflowRunProviderPageStates(providerNames []string) []workflowRunProviderPageState {
	states := make([]workflowRunProviderPageState, len(providerNames))
	for i, name := range providerNames {
		states[i] = workflowRunProviderPageState{ProviderName: name}
	}
	return states
}

func cloneWorkflowRunProviderPageStates(states []workflowRunProviderPageState) []workflowRunProviderPageState {
	out := make([]workflowRunProviderPageState, len(states))
	for i, state := range states {
		out[i] = state
		out[i].SkipRunIDs = cloneWorkflowRunSkipIDs(state.SkipRunIDs)
	}
	return out
}

func workflowRunProviderPageStatesExhausted(states []workflowRunProviderPageState) bool {
	for _, state := range states {
		if !state.Exhausted {
			return false
		}
	}
	return true
}

func workflowRunProviderResumeState(base workflowRunProviderPageState, selected []workflowRunListCandidate, candidateTotal int, sourceExhausted bool) workflowRunProviderPageState {
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].ProviderOrder < selected[j].ProviderOrder
	})
	if sourceExhausted && len(selected) >= candidateTotal {
		return workflowRunProviderPageState{ProviderName: base.ProviderName, Exhausted: true}
	}
	prefix := true
	for i, candidate := range selected {
		if candidate.ProviderOrder != i {
			prefix = false
			break
		}
	}
	if prefix {
		return selected[len(selected)-1].ResumeState
	}
	base.SkipRunIDs = appendWorkflowRunSkipIDs(base.SkipRunIDs, selected)
	return base
}

func appendWorkflowRunSkipIDs(existing []string, selected []workflowRunListCandidate) []string {
	out := cloneWorkflowRunSkipIDs(existing)
	seen := workflowRunProviderSkipSet(out)
	for _, candidate := range selected {
		runID := strings.TrimSpace(candidate.RunID)
		if runID == "" {
			continue
		}
		if _, ok := seen[runID]; ok {
			continue
		}
		seen[runID] = struct{}{}
		out = append(out, runID)
	}
	return out
}

func workflowRunProviderSkipSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func normalizeWorkflowRunSkipIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func cloneWorkflowRunSkipIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

func encodeWorkflowRunListPageToken(token workflowRunListPageToken) string {
	encoded, err := json.Marshal(token)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func workflowRunProviderListFingerprint(providerNames []string) string {
	h := sha256.New()
	for _, name := range providerNames {
		_, _ = h.Write([]byte(strings.TrimSpace(name)))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func runMatchesListFilters(run *coreworkflow.Run, req coreworkflow.ListRunsRequest) bool {
	if run == nil {
		return false
	}
	if app := strings.TrimSpace(req.TargetApp); app != "" {
		if !workflowTargetHasApp(run.Target, app) {
			return false
		}
	}
	if req.Status != "" && run.Status != req.Status {
		return false
	}
	return true
}

func workflowTargetHasApp(target coreworkflow.Target, appName string) bool {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return false
	}
	for i := range target.Steps {
		if target.Steps[i].App != nil && strings.TrimSpace(target.Steps[i].App.Name) == appName {
			return true
		}
	}
	return false
}

func (m *Manager) DeliverEvent(ctx context.Context, p *principal.Principal, req EventDeliver) (out coreworkflow.Event, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventDeliver)
	appName := strings.TrimSpace(req.AppName)
	audit.setCallerApp(appName)
	finishAudit := true
	defer func() {
		eventType := out.Type
		if eventType == "" {
			eventType = req.Event.Type
		}
		audit.setObjectTarget(workflowAuditTargetEvent, "", eventType)
		if finishAudit {
			audit.finish(ctx, err)
		}
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return coreworkflow.Event{}, ErrWorkflowSubjectRequired
	}
	if m == nil || m.workflow == nil {
		return coreworkflow.Event{}, ErrWorkflowNotConfigured
	}

	providerSelection := strings.TrimSpace(req.ProviderName)
	if appName == "" {
		return coreworkflow.Event{}, ErrWorkflowEventSourceRequired
	}
	event := req.Event
	event.Source = appName
	event = normalizePublishedEvent(event, m.now())
	if strings.TrimSpace(event.Type) == "" {
		return coreworkflow.Event{}, ErrWorkflowEventTypeRequired
	}
	reqContext, err := workflowProviderRequestContext(ctx, p, invocation.CallerProvider{
		Kind: invocation.ProviderKindApp,
		Name: appName,
	})
	if err != nil {
		return coreworkflow.Event{}, err
	}
	if _, err := workflowCallerSubjectID(ctx, reqContext, p); err != nil {
		return coreworkflow.Event{}, err
	}
	event.Source = appName

	providerName, provider, err := m.resolveProvider(ctx, providerSelection)
	if err != nil {
		return coreworkflow.Event{}, err
	}
	audit.setProvider(providerName)
	eventProto, err := workflowwire.EventToProto(event)
	if err != nil {
		return coreworkflow.Event{}, err
	}
	deliveredProto, err := provider.DeliverEvent(ctx, &proto.DeliverWorkflowProviderEventRequest{
		Event:   eventProto,
		Context: reqContext,
	})
	if err != nil {
		return coreworkflow.Event{}, err
	}
	if deliveredProto != nil {
		delivered, err := workflowwire.EventFromProto(deliveredProto)
		if err != nil {
			return coreworkflow.Event{}, err
		}
		return delivered, nil
	}
	return event, nil
}
