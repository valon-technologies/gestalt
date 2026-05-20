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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/jsonvalue"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowprincipal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrWorkflowNotConfigured      = errors.New("workflow is not configured")
	ErrExecutionRefsNotConfigured = errors.New("workflow execution refs are not configured")
	ErrWorkflowSubjectRequired    = errors.New("workflow subject is required")
	ErrWorkflowScheduleSubject    = ErrWorkflowSubjectRequired
	ErrDuplicateExecutionRefs     = errors.New("workflow object matched multiple execution references")
	ErrWorkflowEventMatchRequired = errors.New("workflow trigger match.type is required")
	ErrWorkflowEventTypeRequired  = errors.New("workflow event type is required")
	ErrWorkflowKeyRequired        = errors.New("workflow key is required")
	ErrWorkflowSignalNameRequired = errors.New("workflow signal name is required")
)

const workflowScheduleExecutionRefBasePrefix = "workflow_schedule:"
const workflowEventTriggerExecutionRefBasePrefix = "workflow_event_trigger:"
const workflowRunExecutionRefBasePrefix = "workflow_run:"
const workflowDefinitionExecutionRefBasePrefix = "workflow_definition:"
const (
	workflowNoProviderPermissionsPlugin = "__gestalt.workflow.no_provider_permissions__"
	workflowStepsSemanticsVersion       = "workflow_steps_v1"
	workflowTargetCanonicalizationV1    = "target_fingerprint_v1"
)
const defaultWorkflowEventSpecVersion = "1.0"
const workflowRunListDefaultPageSize = 100
const workflowRunListMaxPageSize = 200

type signalTargetPrincipalSource uint8

const (
	signalTargetPrincipalCaller signalTargetPrincipalSource = iota
	signalTargetPrincipalExecutionRef
)

type WorkflowControl interface {
	ResolveProvider(name string) (coreworkflow.Provider, error)
	ResolveProviderSelection(name string) (providerName string, provider coreworkflow.Provider, err error)
	ProviderNames() []string
}

type AgentControl interface {
	ResolveProviderSelection(name string) (providerName string, provider coreagent.Provider, err error)
}

type Service interface {
	ApplyDefinition(ctx context.Context, p *principal.Principal, req DefinitionApply) (*ManagedDefinition, error)
	GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error)
	ListDefinitions(ctx context.Context, p *principal.Principal) ([]*ManagedDefinition, error)
	DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) error
	SetDefinitionPaused(ctx context.Context, p *principal.Principal, definitionID string, paused bool) (*ManagedDefinition, error)
	SetActivationPaused(ctx context.Context, p *principal.Principal, definitionID, activationID string, paused bool) (*ManagedDefinition, error)
	ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error)
	CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (*ManagedSchedule, error)
	GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
	UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (*ManagedSchedule, error)
	DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) error
	PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
	ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error)
	ListEventTriggers(ctx context.Context, p *principal.Principal) ([]*ManagedEventTrigger, error)
	CreateEventTrigger(ctx context.Context, p *principal.Principal, req EventTriggerUpsert) (*ManagedEventTrigger, error)
	GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error)
	UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req EventTriggerUpsert) (*ManagedEventTrigger, error)
	DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) error
	PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error)
	ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error)
	ListRuns(ctx context.Context, p *principal.Principal, req coreworkflow.ListRunsRequest) (*ListRunsResponse, error)
	StartRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error)
	GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error)
	CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*ManagedRun, error)
	SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (*ManagedRunSignal, error)
	SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (*ManagedRunSignal, error)
	DeliverEvent(ctx context.Context, p *principal.Principal, req EventPublish) (*coreworkflow.DeliverEventResponse, error)
	PublishEvent(ctx context.Context, p *principal.Principal, req EventPublish) (coreworkflow.Event, error)
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
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]invocation.PluginInvocationDependency
	Now               func() time.Time
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	workflow          WorkflowControl
	agent             AgentControl
	agentManager      agentmanager.Service
	invoker           invocation.Invoker
	audit             core.AuditSink
	authorizer        authorization.RuntimeAuthorizer
	defaultConnection map[string]string
	catalogConnection map[string]string
	pluginInvokes     map[string][]invocation.PluginInvocationDependency
	now               func() time.Time
}

type ScheduleUpsert struct {
	ProviderName       string
	Cron               string
	Timezone           string
	Target             coreworkflow.Target
	DefinitionID       string
	SourceDefinitionID string
	Paused             bool
	IdempotencyKey     string
	CallerPluginName   string
	Permissions        []core.AccessPermission
}

type DefinitionApply struct {
	ProviderName     string
	Spec             coreworkflow.DefinitionSpec
	IdempotencyKey   string
	CallerPluginName string
}

type EventTriggerUpsert struct {
	ProviderName     string
	Match            coreworkflow.EventMatch
	Target           coreworkflow.Target
	DefinitionID     string
	Paused           bool
	IdempotencyKey   string
	CallerPluginName string
	Permissions      []core.AccessPermission
}

type RunStart struct {
	ProviderName         string
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	Target               coreworkflow.Target
	Input                map[string]any
	IdempotencyKey       string
	WorkflowKey          string
	CallerPluginName     string
	Permissions          []core.AccessPermission
}

type RunSignal struct {
	RunID  string
	Signal coreworkflow.Signal
}

type RunSignalOrStart struct {
	ProviderName         string
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	WorkflowKey          string
	Target               coreworkflow.Target
	Input                map[string]any
	IdempotencyKey       string
	Signal               coreworkflow.Signal
	CallerPluginName     string
	Permissions          []core.AccessPermission
}

type EventPublish struct {
	ProviderName string
	// PluginName is trusted owner context. Callers must derive or authorize it before
	// entering the workflow manager; the manager only normalizes and forwards it.
	PluginName     string
	Event          coreworkflow.Event
	IdempotencyKey string
}

type ManagedDefinition struct {
	ProviderName string
	Definition   *coreworkflow.Definition
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type ManagedSchedule struct {
	ProviderName string
	Schedule     *coreworkflow.Schedule
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type ManagedEventTrigger struct {
	ProviderName string
	Trigger      *coreworkflow.EventTrigger
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type ManagedRun struct {
	ProviderName string
	Run          *coreworkflow.Run
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type ManagedRunSignal struct {
	ProviderName string
	Run          *coreworkflow.Run
	Signal       coreworkflow.Signal
	StartedRun   bool
	WorkflowKey  string
	ExecutionRef *coreworkflow.ExecutionReference
	provider     coreworkflow.Provider
}

type workflowTargetPlan struct {
	SpecDigest        string
	TargetDigest      string
	ActionTableDigest string
	PermissionsDigest string
	SemanticsVersion  string
}

const (
	workflowAuditSource = "workflow_manager"

	workflowAuditOperationDefinitionCreate   = "workflow.definition.create"
	workflowAuditOperationDefinitionUpdate   = "workflow.definition.update"
	workflowAuditOperationDefinitionDelete   = "workflow.definition.delete"
	workflowAuditOperationScheduleCreate     = "workflow.schedule.create"
	workflowAuditOperationScheduleUpdate     = "workflow.schedule.update"
	workflowAuditOperationScheduleDelete     = "workflow.schedule.delete"
	workflowAuditOperationSchedulePause      = "workflow.schedule.pause"
	workflowAuditOperationScheduleResume     = "workflow.schedule.resume"
	workflowAuditOperationEventTriggerCreate = "workflow.event_trigger.create"
	workflowAuditOperationEventTriggerUpdate = "workflow.event_trigger.update"
	workflowAuditOperationEventTriggerDelete = "workflow.event_trigger.delete"
	workflowAuditOperationEventTriggerPause  = "workflow.event_trigger.pause"
	workflowAuditOperationEventTriggerResume = "workflow.event_trigger.resume"
	workflowAuditOperationRunStart           = "workflow.run.start"
	workflowAuditOperationRunSignal          = "workflow.run.signal"
	workflowAuditOperationRunSignalOrStart   = "workflow.run.signal_or_start"
	workflowAuditOperationRunCancel          = "workflow.run.cancel"
	workflowAuditOperationEventPublish       = "workflow.event.publish"

	workflowAuditTargetDefinition   = "workflow_definition"
	workflowAuditTargetSchedule     = "workflow_schedule"
	workflowAuditTargetEventTrigger = "workflow_event_trigger"
	workflowAuditTargetRun          = "workflow_run"
	workflowAuditTargetEvent        = "workflow_event"
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
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     invocation.ClonePluginInvocationDependencyMap(cfg.PluginInvokes),
		now:               now,
	}
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

func (a *workflowAuditEvent) setCallerPlugin(callerPlugin string) {
	if a == nil {
		return
	}
	a.entry.CallerPlugin = strings.TrimSpace(callerPlugin)
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
		if target.Steps[i].Plugin == nil {
			continue
		}
		pluginName := strings.TrimSpace(target.Steps[i].Plugin.Name)
		operation := strings.TrimSpace(target.Steps[i].Plugin.Operation)
		if pluginName == "" || operation == "" {
			continue
		}
		a.entry.WorkflowTargetProvider = pluginName
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
	switch {
	case len(target.Steps) > 0:
		return "steps"
	default:
		return ""
	}
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

func (m *Manager) ApplyDefinition(ctx context.Context, p *principal.Principal, req DefinitionApply) (*ManagedDefinition, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	spec := workflowDefinitionSpecForApply(req.Spec, workflowCreateIdempotencyScope(p, req.CallerPluginName, req.IdempotencyKey))
	target, err := m.resolveTarget(ctx, p, spec.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	spec.Target = target
	plan, err := m.compileWorkflowDefinitionPlan(ctx, provider, spec)
	if err != nil {
		return nil, err
	}
	executionRefID := newDefinitionExecutionRefID(spec.ID, spec.Generation)
	ref, err := m.buildExecutionRefWithPermissionsAndPlan(executionRefID, providerName, target, p, req.CallerPluginName, "", spec.Permissions, plan)
	if err != nil {
		return nil, err
	}
	ref.SourceDefinitionID = spec.ID
	ref.SourceDefinitionGeneration = spec.Generation
	ref.Generation = spec.Generation
	binding := workflowDefinitionBindingForDefinition(ref, plan, strings.TrimSpace(req.IdempotencyKey), spec.ID, spec.Generation)
	deployment, err := provider.ApplyWorkflowDefinition(ctx, coreworkflow.ApplyDefinitionRequest{
		Spec:         spec,
		Binding:      binding,
		ExecutionRef: ref,
		RequestID:    strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedDefinition{ProviderName: providerName, Definition: deployment, ExecutionRef: ref, provider: provider}, nil
}

func (m *Manager) GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error) {
	ref, providerName, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, definitionID, p)
	if err != nil {
		return nil, err
	}
	deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: strings.TrimSpace(definitionID)})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedDefinition{ProviderName: providerName, Definition: deployment, ExecutionRef: ref, provider: provider}, nil
}

func (m *Manager) ListDefinitions(ctx context.Context, p *principal.Principal) ([]*ManagedDefinition, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := []*ManagedDefinition{}
	for _, ref := range refs {
		definitionID := definitionIDFromExecutionRefID(ref.ID)
		if definitionID == "" || !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: definitionID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
		if !definitionMatchesExecutionRef(ref.ProviderName, deployment, ref) {
			continue
		}
		out = append(out, &ManagedDefinition{ProviderName: strings.TrimSpace(ref.ProviderName), Definition: deployment, ExecutionRef: ref, provider: provider})
	}
	return out, nil
}

func (m *Manager) DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) error {
	_, _, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, definitionID, p)
	if err != nil {
		return err
	}
	if err := provider.DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{DefinitionID: strings.TrimSpace(definitionID)}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) SetDefinitionPaused(ctx context.Context, p *principal.Principal, definitionID string, paused bool) (*ManagedDefinition, error) {
	ref, providerName, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, definitionID, p)
	if err != nil {
		return nil, err
	}
	deployment, err := provider.SetWorkflowDefinitionPaused(ctx, coreworkflow.SetDefinitionPausedRequest{DefinitionID: strings.TrimSpace(definitionID), Paused: paused})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedDefinition{ProviderName: providerName, Definition: deployment, ExecutionRef: ref, provider: provider}, nil
}

func (m *Manager) SetActivationPaused(ctx context.Context, p *principal.Principal, definitionID, activationID string, paused bool) (*ManagedDefinition, error) {
	ref, providerName, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, definitionID, p)
	if err != nil {
		return nil, err
	}
	deployment, err := provider.SetWorkflowActivationPaused(ctx, coreworkflow.SetActivationPausedRequest{
		DefinitionID: strings.TrimSpace(definitionID),
		ActivationID: strings.TrimSpace(activationID),
		Paused:       paused,
	})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedDefinition{ProviderName: providerName, Definition: deployment, ExecutionRef: ref, provider: provider}, nil
}

func (m *Manager) ListRuns(ctx context.Context, p *principal.Principal, req coreworkflow.ListRunsRequest) (*ListRunsResponse, error) {
	pageSize, err := effectiveWorkflowRunListPageSize(req.PageSize)
	if err != nil {
		return nil, err
	}
	if m == nil || m.workflow == nil {
		return nil, ErrExecutionRefsNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	providerNames := m.workflow.ProviderNames()
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
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		store, err := workflowExecutionReferenceStore(providerName, provider)
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
			resp, err := provider.ListRuns(ctx, coreworkflow.ListRunsRequest{
				PageSize:  pageSize,
				PageToken: providerPageToken,
				Status:    req.Status,
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

			nextProviderToken := strings.TrimSpace(resp.NextPageToken)
			for rawIndex, run := range resp.Runs {
				if rawIndex < providerOffset {
					continue
				}
				if run == nil {
					continue
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
				ref, err := m.executionRefForRun(ctx, store, providerName, run)
				if err != nil {
					return nil, err
				}
				if ref == nil ||
					!executionRefOwnedBy(ref, p) ||
					!m.allowTarget(ctx, p, ref.Target) ||
					!runMatchesExecutionRef(providerName, run, ref) ||
					!runMatchesListFilters(run, req) {
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
				if rawIndex+1 >= len(resp.Runs) {
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
					Run: &ManagedRun{
						ProviderName: providerName,
						Run:          run,
						ExecutionRef: ref,
						provider:     provider,
					},
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

func (m *Manager) executionRefForRun(ctx context.Context, store coreworkflow.ExecutionReferenceStore, providerName string, run *coreworkflow.Run) (*coreworkflow.ExecutionReference, error) {
	executionRefID := ""
	if run != nil {
		executionRefID = strings.TrimSpace(run.ExecutionRef)
	}
	if executionRefID == "" {
		return nil, nil
	}
	ref, err := store.GetExecutionReference(ctx, executionRefID)
	if errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return workflowExecutionRefForProvider(ref, providerName), nil
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
	TargetPlugin        string                         `json:"targetPlugin,omitempty"`
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
	if token.TargetPlugin != strings.TrimSpace(req.TargetPlugin) || token.Status != req.Status {
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
		TargetPlugin:        strings.TrimSpace(req.TargetPlugin),
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
	if plugin := strings.TrimSpace(req.TargetPlugin); plugin != "" {
		if !workflowTargetHasPlugin(run.Target, plugin) {
			return false
		}
	}
	if req.Status != "" && run.Status != req.Status {
		return false
	}
	return true
}

func workflowTargetHasPlugin(target coreworkflow.Target, pluginName string) bool {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return false
	}
	for i := range target.Steps {
		if target.Steps[i].Plugin != nil && strings.TrimSpace(target.Steps[i].Plugin.Name) == pluginName {
			return true
		}
	}
	return false
}

func (m *Manager) StartRun(ctx context.Context, p *principal.Principal, req RunStart) (out *ManagedRun, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunStart)
	audit.setCallerPlugin(req.CallerPluginName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	if strings.TrimSpace(req.DefinitionID) != "" {
		return m.startDefinitionRun(ctx, p, req)
	}
	return nil, fmt.Errorf("%w: workflow runs must reference a workflow definition", invocation.ErrInvalidInvocation)
}

func (m *Manager) startDefinitionRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error) {
	ref, providerName, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, req.DefinitionID, p)
	if err != nil {
		return nil, err
	}
	if requestedProvider := strings.TrimSpace(req.ProviderName); requestedProvider != "" && requestedProvider != providerName {
		return nil, invocation.ErrInvalidInvocation
	}
	deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: strings.TrimSpace(req.DefinitionID)})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	generation := req.DefinitionGeneration
	if generation == 0 {
		generation = deployment.Spec.Generation
	}
	plan := workflowTargetPlanFromDefinition(deployment, ref)
	binding := workflowDefinitionBindingForDefinition(ref, plan, req.IdempotencyKey, deployment.Spec.ID, generation)
	run, err := provider.StartRun(ctx, coreworkflow.StartRunRequest{
		DefinitionID:         strings.TrimSpace(deployment.Spec.ID),
		DefinitionGeneration: generation,
		ActivationID:         strings.TrimSpace(req.ActivationID),
		Target:               ref.Target,
		Input:                maps.Clone(req.Input),
		IdempotencyKey:       strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:          strings.TrimSpace(req.WorkflowKey),
		CreatedBy:            workflowActorFromPrincipal(p),
		ExecutionRef:         strings.TrimSpace(ref.ID),
		DefinitionBinding:    binding,
	})
	if err != nil {
		return nil, err
	}
	if !runMatchesExecutionRef(providerName, run, ref) || strings.TrimSpace(ref.ID) != strings.TrimSpace(run.ExecutionRef) {
		return nil, core.ErrNotFound
	}
	return &ManagedRun{ProviderName: providerName, Run: run, ExecutionRef: ref, provider: provider}, nil
}

func (m *Manager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, core.ErrNotFound
	}
	refs, err := m.listOwnedExecutionRefs(ctx, p, false)
	if err != nil {
		return nil, err
	}
	refsByProvider := executionRefsByProvider(refs)
	var firstErr error
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{RunID: runID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ref := executionRefsByID(providerRefs)[strings.TrimSpace(run.ExecutionRef)]
		if ref == nil || !m.allowTarget(ctx, p, ref.Target) || !runMatchesExecutionRef(providerName, run, ref) {
			continue
		}
		return &ManagedRun{
			ProviderName: providerName,
			Run:          run,
			ExecutionRef: ref,
			provider:     provider,
		}, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, core.ErrNotFound
}

func (m *Manager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (out *ManagedRun, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunCancel)
	audit.setObjectTarget(workflowAuditTargetRun, runID, "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.GetRun(ctx, p, runID)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	run, err := existingRunProvider(value).CancelRun(ctx, coreworkflow.CancelRunRequest{
		RunID:  strings.TrimSpace(runID),
		Reason: strings.TrimSpace(reason),
	})
	if err != nil {
		return nil, err
	}
	if !runMatchesExecutionRef(value.ProviderName, run, value.ExecutionRef) {
		return nil, core.ErrNotFound
	}
	value.Run = run
	return value, nil
}

func (m *Manager) SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (out *ManagedRunSignal, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunSignal)
	audit.setObjectTarget(workflowAuditTargetRun, req.RunID, "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			audit.setWorkflowKey(out.WorkflowKey)
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.GetRun(ctx, p, req.RunID)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}
	resp, err := existingRunProvider(value).SignalRun(ctx, coreworkflow.SignalRunRequest{
		RunID:  strings.TrimSpace(req.RunID),
		Signal: signal,
	})
	if err != nil {
		return nil, err
	}
	return m.managedSignalResponse(ctx, p, value.ProviderName, existingRunProvider(value), resp, value.ExecutionRef, signalTargetPrincipalCaller)
}

func (m *Manager) SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (out *ManagedRunSignal, err error) {
	phase := "validate_subject"
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationRunSignalOrStart)
	audit.setCallerPlugin(req.CallerPluginName)
	audit.setWorkflowKey(req.WorkflowKey)
	audit.setObjectTarget(workflowAuditTargetRun, "", "")
	defer func() {
		if out != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetRun, workflowRunID(out.Run), "")
			audit.setWorkflowKey(out.WorkflowKey)
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
		if err != nil {
			logWorkflowSignalOrStartFailure(ctx, req, phase, nil, err)
		}
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	phase = "validate_workflow_key"
	workflowKey := strings.TrimSpace(req.WorkflowKey)
	if workflowKey == "" {
		return nil, ErrWorkflowKeyRequired
	}
	if strings.TrimSpace(req.DefinitionID) != "" {
		phase = "normalize_signal"
		signal, err := m.normalizeSignal(req.Signal, p)
		if err != nil {
			return nil, err
		}
		return m.signalOrStartDefinitionRun(ctx, p, req, signal)
	}
	phase = "validate_definition"
	return nil, fmt.Errorf("%w: signal-or-start must reference a workflow definition", invocation.ErrInvalidInvocation)
}

func (m *Manager) signalOrStartDefinitionRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart, signal coreworkflow.Signal) (*ManagedRunSignal, error) {
	ref, providerName, provider, err := m.requireOwnedDefinitionExecutionRef(ctx, req.DefinitionID, p)
	if err != nil {
		return nil, err
	}
	if requestedProvider := strings.TrimSpace(req.ProviderName); requestedProvider != "" && requestedProvider != providerName {
		return nil, core.ErrNotFound
	}
	deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: strings.TrimSpace(req.DefinitionID)})
	if err != nil {
		return nil, err
	}
	if !definitionMatchesExecutionRef(providerName, deployment, ref) {
		return nil, core.ErrNotFound
	}
	generation := req.DefinitionGeneration
	if generation == 0 {
		generation = deployment.Spec.Generation
	}
	binding := workflowDefinitionBindingForDefinition(ref, workflowTargetPlanFromDefinition(deployment, ref), req.IdempotencyKey, deployment.Spec.ID, generation)
	resp, err := provider.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{
		DefinitionID:         strings.TrimSpace(deployment.Spec.ID),
		DefinitionGeneration: generation,
		ActivationID:         strings.TrimSpace(req.ActivationID),
		WorkflowKey:          strings.TrimSpace(req.WorkflowKey),
		Target:               ref.Target,
		Input:                maps.Clone(req.Input),
		IdempotencyKey:       strings.TrimSpace(req.IdempotencyKey),
		CreatedBy:            workflowActorFromPrincipal(p),
		ExecutionRef:         strings.TrimSpace(ref.ID),
		Signal:               signal,
		DefinitionBinding:    binding,
	})
	if err != nil {
		return nil, err
	}
	return m.managedSignalResponse(ctx, p, providerName, provider, resp, ref, signalTargetPrincipalExecutionRef)
}

func logWorkflowSignalOrStartFailure(ctx context.Context, req RunSignalOrStart, phase string, targetAuthFailure *targetAuthorizationFailure, err error) {
	if err == nil {
		return
	}
	attrs := []any{
		"phase", strings.TrimSpace(phase),
		"workflow_key_sha256", workflowManagerSHA256(req.WorkflowKey),
		"error_type", workflowManagerErrorType(err),
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil && strings.TrimSpace(meta.RequestID) != "" {
		attrs = append(attrs, "request_id", strings.TrimSpace(meta.RequestID))
	}
	if errorCode := workflowManagerErrorCode(err); errorCode != "" {
		attrs = append(attrs, "error_code", errorCode)
	}
	if targetAuthFailure != nil {
		attrs = appendTargetAuthorizationFailureAttrs(attrs, *targetAuthFailure)
	}
	slog.WarnContext(ctx, "workflow manager signal-or-start failed", attrs...)
}

func appendTargetAuthorizationFailureAttrs(attrs []any, failure targetAuthorizationFailure) []any {
	if component := strings.TrimSpace(failure.component); component != "" {
		attrs = append(attrs, "workflow_target_component", component)
	}
	if decision := workflowAuditTargetAuthorizationDecision(failure); decision != "" {
		attrs = append(attrs, "authorization_decision", decision)
	}
	return attrs
}

func workflowManagerSHA256(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func workflowManagerErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return observability.WorkflowGRPCCodeName(st.Code())
	}
	return ""
}

func workflowManagerErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, ErrWorkflowNotConfigured):
		return "workflow_not_configured"
	case errors.Is(err, ErrExecutionRefsNotConfigured):
		return "workflow_execution_ref_store_not_configured"
	case errors.Is(err, ErrWorkflowSubjectRequired):
		return "workflow_subject_required"
	case errors.Is(err, ErrDuplicateExecutionRefs):
		return "duplicate_execution_refs"
	case errors.Is(err, ErrWorkflowEventMatchRequired):
		return "workflow_event_match_required"
	case errors.Is(err, ErrWorkflowEventTypeRequired):
		return "workflow_event_type_required"
	case errors.Is(err, ErrWorkflowKeyRequired):
		return "workflow_key_required"
	case errors.Is(err, ErrWorkflowSignalNameRequired):
		return "workflow_signal_name_required"
	case errors.Is(err, invocation.ErrNotAuthenticated):
		return "not_authenticated"
	case errors.Is(err, invocation.ErrAuthorizationDenied):
		return "authorization_denied"
	case errors.Is(err, invocation.ErrInvalidInvocation):
		return "invalid_invocation"
	case errors.Is(err, core.ErrNotFound):
		return "not_found"
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		return "grpc_status"
	}
	return "unknown"
}

func (m *Manager) PublishEvent(ctx context.Context, p *principal.Principal, req EventPublish) (coreworkflow.Event, error) {
	_, event, err := m.deliverEvent(ctx, p, req)
	return event, err
}

func (m *Manager) DeliverEvent(ctx context.Context, p *principal.Principal, req EventPublish) (*coreworkflow.DeliverEventResponse, error) {
	resp, _, err := m.deliverEvent(ctx, p, req)
	return resp, err
}

func (m *Manager) deliverEvent(ctx context.Context, p *principal.Principal, req EventPublish) (resp *coreworkflow.DeliverEventResponse, out coreworkflow.Event, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventPublish)
	audit.setCallerPlugin(req.PluginName)
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
		return nil, coreworkflow.Event{}, ErrWorkflowSubjectRequired
	}
	if m == nil || m.workflow == nil {
		return nil, coreworkflow.Event{}, ErrWorkflowNotConfigured
	}

	providerSelection := strings.TrimSpace(req.ProviderName)
	pluginName := strings.TrimSpace(req.PluginName)
	event := req.Event
	event = normalizePublishedEvent(event, m.now())
	if strings.TrimSpace(event.Type) == "" {
		return nil, coreworkflow.Event{}, ErrWorkflowEventTypeRequired
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = event.ID
	}
	publishedBy := workflowActorFromPrincipal(p)

	if providerSelection != "" {
		providerName, provider, err := m.resolveProviderSelection(providerSelection)
		if err != nil {
			return nil, coreworkflow.Event{}, err
		}
		audit.setProvider(providerName)
		resp, err := provider.DeliverWorkflowEvent(ctx, coreworkflow.PublishEventRequest{
			PluginName:     pluginName,
			DeliveryID:     event.ID,
			Event:          event,
			PublishedBy:    publishedBy,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return nil, coreworkflow.Event{}, err
		}
		if resp == nil {
			resp = &coreworkflow.DeliverEventResponse{}
		}
		return resp, event, nil
	}

	providerNames := m.workflow.ProviderNames()
	if len(providerNames) > 0 {
		finishAudit = false
	}
	outResp := &coreworkflow.DeliverEventResponse{}
	for _, providerName := range providerNames {
		providerAudit := audit.clone()
		providerAudit.setProvider(providerName)
		providerAudit.setObjectTarget(workflowAuditTargetEvent, "", event.Type)
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			providerAudit.finish(ctx, err)
			return nil, coreworkflow.Event{}, err
		}
		providerResp, err := provider.DeliverWorkflowEvent(ctx, coreworkflow.PublishEventRequest{
			PluginName:     pluginName,
			DeliveryID:     event.ID,
			Event:          event,
			PublishedBy:    publishedBy,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			providerAudit.finish(ctx, err)
			return nil, coreworkflow.Event{}, err
		}
		if providerResp != nil {
			outResp.Results = append(outResp.Results, providerResp.Results...)
		}
		providerAudit.finish(ctx, nil)
	}
	return outResp, event, nil
}

func (m *Manager) ListSchedules(ctx context.Context, p *principal.Principal) ([]*ManagedSchedule, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedSchedule, 0, len(refs))
	for _, ref := range refs {
		if !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		scheduleID := scheduleIDFromExecutionRefID(ref.ID)
		if scheduleID == "" {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: scheduleID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
		schedule := workflowScheduleFromDefinition(deployment, ref)
		if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
			continue
		}
		out = append(out, &ManagedSchedule{
			ProviderName: strings.TrimSpace(ref.ProviderName),
			Schedule:     schedule,
			ExecutionRef: ref,
			provider:     provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Schedule != nil && right.Schedule != nil && left.Schedule.CreatedAt != nil && right.Schedule.CreatedAt != nil && !left.Schedule.CreatedAt.Equal(*right.Schedule.CreatedAt) {
			return left.Schedule.CreatedAt.Before(*right.Schedule.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Schedule != nil {
			leftID = left.Schedule.ID
		}
		if right.Schedule != nil {
			rightID = right.Schedule.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleCreate)
	audit.setCallerPlugin(req.CallerPluginName)
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerPluginName, idempotencyKey)
	scheduleID := newScheduleID(idempotencyScope)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	var existing *ManagedSchedule
	if idempotencyKey != "" {
		var err error
		existing, err = m.requireOwnedSchedule(ctx, scheduleID, p)
		if err == nil {
			audit.setProvider(existing.ProviderName)
			if strings.TrimSpace(req.DefinitionID) != "" {
				if workflowTargetIsSet(req.Target) {
					return nil, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
				}
				if err := m.validateExistingProviderSelection(req.ProviderName, existing.ProviderName); err != nil {
					return nil, err
				}
				if !managedScheduleMatchesDefinitionBackedSchedule(existing, req) {
					return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
				}
				return existing, nil
			}
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}

	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if existing != nil {
		if !managedScheduleMatchesUpsert(existing, providerName, target, req) {
			return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
		}
		return existing, nil
	}
	plan, err := m.compileWorkflowTargetPlan(ctx, provider, target)
	if err != nil {
		return nil, err
	}
	executionRefID := newScheduleExecutionRefID(scheduleID, idempotencyScope)
	ref, err := m.buildExecutionRefWithPermissionsAndPlan(executionRefID, providerName, target, p, req.CallerPluginName, scheduleUpsertSourceDefinitionID(req), req.Permissions, plan)
	if err != nil {
		return nil, err
	}
	binding := workflowDefinitionBinding(ref, plan, req.IdempotencyKey, false)
	deployment, err := provider.ApplyWorkflowDefinition(ctx, coreworkflow.ApplyDefinitionRequest{
		Spec:         workflowScheduleDefinitionSpec(scheduleID, target, req),
		Binding:      binding,
		ExecutionRef: ref,
		RequestID:    strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return nil, err
	}
	return &ManagedSchedule{
		ProviderName: providerName,
		Schedule:     workflowScheduleFromDefinition(deployment, ref),
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func managedScheduleMatchesUpsert(existing *ManagedSchedule, providerName string, target coreworkflow.Target, req ScheduleUpsert) bool {
	if existing == nil || existing.Schedule == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Cron) != strings.TrimSpace(req.Cron) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Timezone) != strings.TrimSpace(req.Timezone) {
		return false
	}
	if existing.Schedule.Paused != req.Paused {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Schedule.Target, target)
}

func managedScheduleMatchesDefinitionBackedSchedule(existing *ManagedSchedule, req ScheduleUpsert) bool {
	if existing == nil || existing.Schedule == nil || existing.ExecutionRef == nil {
		return false
	}
	if strings.TrimSpace(existing.ExecutionRef.SourceDefinitionID) != strings.TrimSpace(req.DefinitionID) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Cron) != strings.TrimSpace(req.Cron) {
		return false
	}
	if strings.TrimSpace(existing.Schedule.Timezone) != strings.TrimSpace(req.Timezone) {
		return false
	}
	return existing.Schedule.Paused == req.Paused
}

func scheduleUpsertSourceDefinitionID(req ScheduleUpsert) string {
	if definitionID := strings.TrimSpace(req.DefinitionID); definitionID != "" {
		return definitionID
	}
	return strings.TrimSpace(req.SourceDefinitionID)
}

func (m *Manager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	return m.requireOwnedSchedule(ctx, scheduleID, p)
}

func (m *Manager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleUpdate)
	audit.setCallerPlugin(req.CallerPluginName)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)
	plan, err := m.compileWorkflowTargetPlan(ctx, nextProvider, target)
	if err != nil {
		return nil, err
	}

	executionRefID := scheduleExecutionRefID(strings.TrimSpace(existing.Schedule.ID))
	nextRef, err := m.buildExecutionRefWithPermissionsAndPlan(executionRefID, nextProviderName, target, p, req.CallerPluginName, scheduleUpsertSourceDefinitionID(req), req.Permissions, plan)
	if err != nil {
		return nil, err
	}
	binding := workflowDefinitionBinding(nextRef, plan, req.IdempotencyKey, false)
	deployment, err := nextProvider.ApplyWorkflowDefinition(ctx, coreworkflow.ApplyDefinitionRequest{
		Spec:         workflowScheduleDefinitionSpec(strings.TrimSpace(existing.Schedule.ID), target, req),
		Binding:      binding,
		ExecutionRef: nextRef,
		RequestID:    strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingProvider(existing).DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
			DefinitionID: strings.TrimSpace(existing.Schedule.ID),
			RequestID:    strings.TrimSpace(req.IdempotencyKey),
		}); err != nil {
			_ = nextProvider.DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
				DefinitionID: strings.TrimSpace(existing.Schedule.ID),
				RequestID:    strings.TrimSpace(req.IdempotencyKey),
			})
			return nil, err
		}
	}
	return &ManagedSchedule{
		ProviderName: nextProviderName,
		Schedule:     workflowScheduleFromDefinition(deployment, nextRef),
		ExecutionRef: nextRef,
		provider:     nextProvider,
	}, nil
}

func (m *Manager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleDelete)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	if err := existingProvider(value).DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
		DefinitionID: strings.TrimSpace(value.Schedule.ID),
	}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationSchedulePause)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	deployment, err := existingProvider(value).SetWorkflowDefinitionPaused(ctx, coreworkflow.SetDefinitionPausedRequest{
		DefinitionID: strings.TrimSpace(value.Schedule.ID),
		Paused:       true,
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = workflowScheduleFromDefinition(deployment, value.ExecutionRef)
	return value, nil
}

func (m *Manager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (out *ManagedSchedule, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationScheduleResume)
	audit.setObjectTarget(workflowAuditTargetSchedule, scheduleID, "")
	defer func() {
		if out != nil && out.Schedule != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetSchedule, out.Schedule.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	deployment, err := existingProvider(value).SetWorkflowDefinitionPaused(ctx, coreworkflow.SetDefinitionPausedRequest{
		DefinitionID: strings.TrimSpace(value.Schedule.ID),
		Paused:       false,
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = workflowScheduleFromDefinition(deployment, value.ExecutionRef)
	return value, nil
}

func (m *Manager) ListEventTriggers(ctx context.Context, p *principal.Principal) ([]*ManagedEventTrigger, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	out := make([]*ManagedEventTrigger, 0, len(refs))
	for _, ref := range refs {
		if !m.allowTarget(ctx, p, ref.Target) {
			continue
		}
		triggerID := eventTriggerIDFromExecutionRefID(ref.ID)
		if triggerID == "" {
			continue
		}
		provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
		if err != nil {
			return nil, err
		}
		deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: triggerID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
		trigger := workflowEventTriggerFromDefinition(deployment, ref)
		if !eventTriggerMatchesExecutionRef(ref.ProviderName, trigger, ref) {
			continue
		}
		out = append(out, &ManagedEventTrigger{
			ProviderName: strings.TrimSpace(ref.ProviderName),
			Trigger:      trigger,
			ExecutionRef: ref,
			provider:     provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Trigger != nil && right.Trigger != nil && left.Trigger.CreatedAt != nil && right.Trigger.CreatedAt != nil && !left.Trigger.CreatedAt.Equal(*right.Trigger.CreatedAt) {
			return left.Trigger.CreatedAt.Before(*right.Trigger.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Trigger != nil {
			leftID = left.Trigger.ID
		}
		if right.Trigger != nil {
			rightID = right.Trigger.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) CreateEventTrigger(ctx context.Context, p *principal.Principal, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerCreate)
	audit.setCallerPlugin(req.CallerPluginName)
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerPluginName, idempotencyKey)
	triggerID := newEventTriggerID(idempotencyScope)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	var existing *ManagedEventTrigger
	if idempotencyKey != "" {
		var err error
		existing, err = m.requireOwnedEventTrigger(ctx, triggerID, p)
		if err == nil {
			audit.setProvider(existing.ProviderName)
			if strings.TrimSpace(req.DefinitionID) != "" {
				if workflowTargetIsSet(req.Target) {
					return nil, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
				}
				if err := m.validateExistingProviderSelection(req.ProviderName, existing.ProviderName); err != nil {
					return nil, err
				}
				if !managedEventTriggerMatchesDefinitionBackedTrigger(existing, match, req) {
					return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
				}
				return existing, nil
			}
		} else if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}

	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if existing != nil {
		if !managedEventTriggerMatchesUpsert(existing, providerName, target, match, req) {
			return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
		}
		return existing, nil
	}
	plan, err := m.compileWorkflowTargetPlan(ctx, provider, target)
	if err != nil {
		return nil, err
	}
	executionRefID := newEventTriggerExecutionRefID(triggerID, idempotencyScope)
	ref, err := m.buildExecutionRefWithPermissionsAndPlan(executionRefID, providerName, target, p, req.CallerPluginName, req.DefinitionID, req.Permissions, plan)
	if err != nil {
		return nil, err
	}
	binding := workflowDefinitionBinding(ref, plan, req.IdempotencyKey, false)
	deployment, err := provider.ApplyWorkflowDefinition(ctx, coreworkflow.ApplyDefinitionRequest{
		Spec:         workflowEventTriggerDefinitionSpec(triggerID, target, match, req),
		Binding:      binding,
		ExecutionRef: ref,
		RequestID:    strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return nil, err
	}
	return &ManagedEventTrigger{
		ProviderName: providerName,
		Trigger:      workflowEventTriggerFromDefinition(deployment, ref),
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func managedEventTriggerMatchesUpsert(existing *ManagedEventTrigger, providerName string, target coreworkflow.Target, match coreworkflow.EventMatch, req EventTriggerUpsert) bool {
	if existing == nil || existing.Trigger == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	if existing.Trigger.Paused != req.Paused {
		return false
	}
	if normalizeEventMatch(existing.Trigger.Match) != match {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Trigger.Target, target)
}

func managedEventTriggerMatchesDefinitionBackedTrigger(existing *ManagedEventTrigger, match coreworkflow.EventMatch, req EventTriggerUpsert) bool {
	if existing == nil || existing.Trigger == nil || existing.ExecutionRef == nil {
		return false
	}
	if strings.TrimSpace(existing.ExecutionRef.SourceDefinitionID) != strings.TrimSpace(req.DefinitionID) {
		return false
	}
	if existing.Trigger.Paused != req.Paused {
		return false
	}
	return normalizeEventMatch(existing.Trigger.Match) == match
}

func (m *Manager) GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	return m.requireOwnedEventTrigger(ctx, triggerID, p)
}

func (m *Manager) UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req EventTriggerUpsert) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerUpdate)
	audit.setCallerPlugin(req.CallerPluginName)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	nextProviderName, nextProvider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(nextProviderName)
	audit.setWorkflowTarget(target)
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}
	plan, err := m.compileWorkflowTargetPlan(ctx, nextProvider, target)
	if err != nil {
		return nil, err
	}

	executionRefID := eventTriggerExecutionRefID(strings.TrimSpace(existing.Trigger.ID))
	nextRef, err := m.buildExecutionRefWithPermissionsAndPlan(executionRefID, nextProviderName, target, p, req.CallerPluginName, req.DefinitionID, req.Permissions, plan)
	if err != nil {
		return nil, err
	}
	binding := workflowDefinitionBinding(nextRef, plan, req.IdempotencyKey, false)
	deployment, err := nextProvider.ApplyWorkflowDefinition(ctx, coreworkflow.ApplyDefinitionRequest{
		Spec:         workflowEventTriggerDefinitionSpec(strings.TrimSpace(existing.Trigger.ID), target, match, req),
		Binding:      binding,
		ExecutionRef: nextRef,
		RequestID:    strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingEventTriggerProvider(existing).DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
			DefinitionID: strings.TrimSpace(existing.Trigger.ID),
			RequestID:    strings.TrimSpace(req.IdempotencyKey),
		}); err != nil {
			_ = nextProvider.DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
				DefinitionID: strings.TrimSpace(existing.Trigger.ID),
				RequestID:    strings.TrimSpace(req.IdempotencyKey),
			})
			return nil, err
		}
	}
	return &ManagedEventTrigger{
		ProviderName: nextProviderName,
		Trigger:      workflowEventTriggerFromDefinition(deployment, nextRef),
		ExecutionRef: nextRef,
		provider:     nextProvider,
	}, nil
}

func (m *Manager) DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerDelete)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	if err := existingEventTriggerProvider(value).DeleteWorkflowDefinition(ctx, coreworkflow.DeleteDefinitionRequest{
		DefinitionID: strings.TrimSpace(value.Trigger.ID),
	}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerPause)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	deployment, err := existingEventTriggerProvider(value).SetWorkflowDefinitionPaused(ctx, coreworkflow.SetDefinitionPausedRequest{
		DefinitionID: strings.TrimSpace(value.Trigger.ID),
		Paused:       true,
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = workflowEventTriggerFromDefinition(deployment, value.ExecutionRef)
	return value, nil
}

func (m *Manager) ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (out *ManagedEventTrigger, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationEventTriggerResume)
	audit.setObjectTarget(workflowAuditTargetEventTrigger, triggerID, "")
	defer func() {
		if out != nil && out.Trigger != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetEventTrigger, out.Trigger.ID, "")
			if out.ExecutionRef != nil {
				audit.setWorkflowTarget(out.ExecutionRef.Target)
			}
		}
		audit.finish(ctx, err)
	}()
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(value.ProviderName)
	if value.ExecutionRef != nil {
		audit.setWorkflowTarget(value.ExecutionRef.Target)
	}
	deployment, err := existingEventTriggerProvider(value).SetWorkflowDefinitionPaused(ctx, coreworkflow.SetDefinitionPausedRequest{
		DefinitionID: strings.TrimSpace(value.Trigger.ID),
		Paused:       false,
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = workflowEventTriggerFromDefinition(deployment, value.ExecutionRef)
	return value, nil
}

func (m *Manager) resolveProviderSelection(providerName string) (string, coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return "", nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProviderSelection(strings.TrimSpace(providerName))
}

func (m *Manager) resolveProviderByName(providerName string) (coreworkflow.Provider, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrWorkflowNotConfigured
	}
	return m.workflow.ResolveProvider(strings.TrimSpace(providerName))
}

func (m *Manager) resolveRequestProviderTarget(ctx context.Context, p *principal.Principal, providerSelection string, target coreworkflow.Target, definitionID, callerPluginName string) (string, coreworkflow.Provider, coreworkflow.Target, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(providerSelection))
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		resolvedTarget, err := m.resolveTarget(ctx, p, target, callerPluginName)
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		return providerName, provider, resolvedTarget, nil
	}
	if workflowTargetIsSet(target) {
		return "", nil, coreworkflow.Target{}, fmt.Errorf("%w: workflow request must set either target or definition_id, not both", invocation.ErrInvalidInvocation)
	}
	definition, err := m.GetDefinition(ctx, p, definitionID)
	if err != nil {
		return "", nil, coreworkflow.Target{}, err
	}
	if strings.TrimSpace(providerSelection) != "" {
		selectedProviderName, _, err := m.resolveProviderSelection(strings.TrimSpace(providerSelection))
		if err != nil {
			return "", nil, coreworkflow.Target{}, err
		}
		if selectedProviderName != definition.ProviderName {
			return "", nil, coreworkflow.Target{}, fmt.Errorf("%w: workflow definition %s belongs to provider %q, not %q", invocation.ErrInvalidInvocation, definitionID, definition.ProviderName, selectedProviderName)
		}
	}
	resolvedTarget, err := m.resolveTarget(ctx, p, definition.Definition.Spec.Target, callerPluginName)
	if err != nil {
		return "", nil, coreworkflow.Target{}, err
	}
	return definition.ProviderName, definition.provider, resolvedTarget, nil
}

func (m *Manager) validateExistingProviderSelection(providerSelection, existingProviderName string) error {
	providerSelection = strings.TrimSpace(providerSelection)
	if providerSelection == "" {
		return nil
	}
	selectedProviderName, _, err := m.resolveProviderSelection(providerSelection)
	if err != nil {
		return err
	}
	if selectedProviderName != strings.TrimSpace(existingProviderName) {
		return fmt.Errorf("%w: workflow idempotency key reused with different provider", invocation.ErrInvalidInvocation)
	}
	return nil
}

func workflowTargetIsSet(target coreworkflow.Target) bool {
	return len(target.Steps) > 0
}

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target, callerPluginName string) (coreworkflow.Target, error) {
	if len(target.Steps) == 0 {
		return coreworkflow.Target{}, fmt.Errorf("workflow target.steps is required")
	}
	return m.resolveStepsTarget(ctx, p, target.Steps, callerPluginName)
}

func (m *Manager) resolveStepsTarget(ctx context.Context, p *principal.Principal, steps []coreworkflow.Step, callerPluginName string) (coreworkflow.Target, error) {
	if len(steps) == 0 {
		return coreworkflow.Target{}, fmt.Errorf("workflow target.steps is required")
	}
	resolved := make([]coreworkflow.Step, 0, len(steps))
	seen := map[string]struct{}{}
	for i := range steps {
		step := steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if !coreworkflow.ValidStepID(step.ID) {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id must contain only letters, numbers, '.', '_', or '-'", i)
		}
		if _, exists := seen[step.ID]; exists {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id duplicates %q", i, step.ID)
		}
		if step.TimeoutSeconds < 0 {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].timeout_seconds must not be negative", i)
		}
		actionCount := 0
		if step.Plugin != nil {
			actionCount++
		}
		if step.Agent != nil {
			actionCount++
		}
		if actionCount != 1 {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d] must set exactly one of plugin or agent", i)
		}
		step.Inputs = cloneWorkflowValueMap(step.Inputs)
		step.Metadata = maps.Clone(step.Metadata)
		step.When = cloneWorkflowStepWhen(step.When)
		step.OutputDelivery = coreworkflow.CloneStepDelivery(step.OutputDelivery)
		if err := validateWorkflowStepValueRefs(fmt.Sprintf("target.steps[%d].inputs", i), step.Inputs, seen); err != nil {
			return coreworkflow.Target{}, err
		}
		if step.When != nil {
			if workflowValueKindCount(step.When.Value) != 1 {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].when.value is required", i)
			}
			if !step.When.EqualsSet {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].when.equals is required", i)
			}
			if !jsonvalue.IsScalar(step.When.Equals) {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].when.equals must be a scalar JSON value", i)
			}
			if err := validateWorkflowStepValueRef(fmt.Sprintf("target.steps[%d].when.value", i), step.When.Value, seen); err != nil {
				return coreworkflow.Target{}, err
			}
		}
		if step.Plugin != nil {
			if err := validateWorkflowStepValueRef(fmt.Sprintf("target.steps[%d].plugin.input", i), step.Plugin.Input, seen); err != nil {
				return coreworkflow.Target{}, err
			}
			plugin, err := m.resolveWorkflowStepPlugin(ctx, p, *step.Plugin, callerPluginName, fmt.Sprintf("target.steps[%d].plugin", i))
			if err != nil {
				return coreworkflow.Target{}, err
			}
			step.Plugin = plugin
		}
		if step.Agent != nil {
			agent, err := m.resolveWorkflowStepAgent(ctx, p, *step.Agent, callerPluginName, fmt.Sprintf("target.steps[%d].agent", i))
			if err != nil {
				return coreworkflow.Target{}, err
			}
			step.Agent = agent
		}
		if step.OutputDelivery != nil {
			if step.OutputDelivery.Plugin == nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].output_delivery.plugin is required", i)
			}
			if err := validateWorkflowStepValueRef(fmt.Sprintf("target.steps[%d].output_delivery.plugin.input", i), step.OutputDelivery.Plugin.Input, workflowStepSeenWithCurrent(seen, step.ID)); err != nil {
				return coreworkflow.Target{}, err
			}
			plugin, err := m.resolveWorkflowStepPlugin(ctx, p, *step.OutputDelivery.Plugin, callerPluginName, fmt.Sprintf("target.steps[%d].output_delivery.plugin", i))
			if err != nil {
				return coreworkflow.Target{}, err
			}
			step.OutputDelivery.Plugin = plugin
		}
		seen[step.ID] = struct{}{}
		resolved = append(resolved, step)
	}
	return coreworkflow.Target{Steps: resolved}, nil
}

func workflowStepSeenWithCurrent(seen map[string]struct{}, stepID string) map[string]struct{} {
	out := make(map[string]struct{}, len(seen)+1)
	for key := range seen {
		out[key] = struct{}{}
	}
	out[strings.TrimSpace(stepID)] = struct{}{}
	return out
}

func (m *Manager) resolveWorkflowStepPlugin(ctx context.Context, p *principal.Principal, call coreworkflow.PluginCall, callerPluginName, fieldName string) (*coreworkflow.PluginCall, error) {
	pluginName := strings.TrimSpace(call.Name)
	operation := strings.TrimSpace(call.Operation)
	if pluginName == "" {
		return nil, fmt.Errorf("workflow %s.name is required", fieldName)
	}
	if operation == "" {
		return nil, fmt.Errorf("workflow %s.operation is required", fieldName)
	}
	if !workflowValueIsObjectOrUnset(call.Input) {
		return nil, fmt.Errorf("workflow %s.input must be an object value", fieldName)
	}
	credentialMode, err := m.normalizeWorkflowPluginTargetCredentialMode(call.CredentialMode, callerPluginName, pluginName, operation)
	if err != nil {
		return nil, err
	}
	if m == nil || m.providers == nil {
		return nil, fmt.Errorf("%w: workflow providers are not configured", invocation.ErrInternal)
	}
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return nil, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return nil, invocation.ErrAuthorizationDenied
	}
	if credentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, credentialMode)
	}

	connection := strings.TrimSpace(call.Connection)
	if connection != "" && !core.SafeConnectionValue(connection) {
		return nil, fmt.Errorf("connection name contains invalid characters")
	}
	connection = core.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(call.Instance)
	if instance != "" && !core.SafeInstanceValue(instance) {
		return nil, fmt.Errorf("instance name contains invalid characters")
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
		return nil, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) && !m.callerPluginDeclaresInvoke(callerPluginName, pluginName, opMeta.ID) {
		return nil, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return nil, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, pluginName, connection, sessionInstance)
		if err != nil {
			return nil, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}
	out := call
	out.Name = pluginName
	out.Operation = opMeta.ID
	out.Connection = connection
	out.Instance = sessionInstance
	out.CredentialMode = credentialMode
	out.Input = coreworkflow.CloneValue(call.Input)
	return &out, nil
}

func (m *Manager) resolveWorkflowStepAgent(ctx context.Context, p *principal.Principal, agent coreworkflow.AgentTurn, callerPluginName, fieldName string) (*coreworkflow.AgentTurn, error) {
	if m == nil || m.agent == nil || m.agentManager == nil {
		return nil, fmt.Errorf("%w: agent workflows are not configured", invocation.ErrInternal)
	}
	providerName, _, err := m.agent.ResolveProviderSelection(agent.ProviderName)
	if err != nil {
		return nil, err
	}
	agent.ProviderName = strings.TrimSpace(providerName)
	if !m.allowProvider(ctx, p, agent.ProviderName) || !principal.AllowsProviderPermission(p, agent.ProviderName) {
		return nil, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, agent.ProviderName)
	}
	agent.Model = strings.TrimSpace(agent.Model)
	agent.SessionKey = strings.TrimSpace(agent.SessionKey)
	agent.Prompt.Template = strings.TrimSpace(agent.Prompt.Template)
	hasMessage := false
	for i := range agent.Messages {
		agent.Messages[i].Text.Template = strings.TrimSpace(agent.Messages[i].Text.Template)
		if agent.Messages[i].Text.Template != "" {
			hasMessage = true
		}
	}
	if agent.Prompt.Template == "" && !hasMessage {
		return nil, fmt.Errorf("workflow target agent prompt or messages is required")
	}
	agent.ToolRefs = append([]coreagent.ToolRef(nil), agent.ToolRefs...)
	agent.ResponseSchema = maps.Clone(agent.ResponseSchema)
	agent.ModelOptions = maps.Clone(agent.ModelOptions)
	if err := validateWorkflowStepAgentToolRefs(agent.ToolRefs, fieldName+".tools"); err != nil {
		return nil, err
	}
	for i := range agent.ToolRefs {
		tool := agent.ToolRefs[i]
		if strings.TrimSpace(tool.System) != "" {
			continue
		}
		pluginName := strings.TrimSpace(tool.Plugin)
		operation := strings.TrimSpace(tool.Operation)
		if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
			return nil, invocation.ErrAuthorizationDenied
		}
		if !principal.AllowsOperationPermission(p, pluginName, operation) && !m.callerPluginDeclaresInvoke(callerPluginName, pluginName, operation) {
			return nil, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, operation)
		}
	}
	return &agent, nil
}

func validateWorkflowStepAgentToolRefs(refs []coreagent.ToolRef, fieldName string) error {
	if err := validateWorkflowAgentToolRefs(refs); err != nil {
		return err
	}
	for i := range refs {
		ref := refs[i]
		if strings.TrimSpace(ref.System) != "" {
			continue
		}
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" || pluginName == "*" || operation == "" {
			return fmt.Errorf("workflow %s[%d] must name an exact plugin operation", fieldName, i)
		}
	}
	return nil
}

func (m *Manager) normalizeWorkflowPluginTargetCredentialMode(mode core.ConnectionMode, callerPluginName, pluginName, operation string) (core.ConnectionMode, error) {
	mode = core.NormalizeOptionalConnectionMode(mode)
	switch mode {
	case "":
		return "", nil
	case core.ConnectionModeNone, core.ConnectionModeUser:
	default:
		return "", fmt.Errorf("%w: workflow target credential_mode %q is not supported", invocation.ErrInvalidInvocation, mode)
	}
	if strings.TrimSpace(callerPluginName) == "" {
		return "", fmt.Errorf("%w: workflow target credential_mode requires a caller plugin declaration", invocation.ErrAuthorizationDenied)
	}
	declared, ok, err := m.callerPluginInvokeCredentialMode(callerPluginName, pluginName, operation)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: workflow target credential_mode requires a declared invoke mode", invocation.ErrAuthorizationDenied)
	}
	if mode != declared {
		return "", fmt.Errorf("%w: workflow target credential_mode %q exceeds declared invoke mode %q", invocation.ErrAuthorizationDenied, mode, declared)
	}
	return mode, nil
}

func (m *Manager) requireOwnedSchedule(ctx context.Context, scheduleID string, p *principal.Principal) (*ManagedSchedule, error) {
	scheduleID = strings.TrimSpace(scheduleID)
	if scheduleID == "" {
		return nil, core.ErrNotFound
	}
	ref, err := m.findOwnedExecutionRef(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	if !m.allowTarget(ctx, p, ref.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
	if err != nil {
		return nil, err
	}
	deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: scheduleID})
	if err != nil {
		return nil, err
	}
	schedule := workflowScheduleFromDefinition(deployment, ref)
	if !scheduleMatchesExecutionRef(ref.ProviderName, schedule, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedSchedule{
		ProviderName: strings.TrimSpace(ref.ProviderName),
		Schedule:     schedule,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) requireOwnedEventTrigger(ctx context.Context, triggerID string, p *principal.Principal) (*ManagedEventTrigger, error) {
	triggerID = strings.TrimSpace(triggerID)
	if triggerID == "" {
		return nil, core.ErrNotFound
	}
	ref, err := m.findOwnedEventTriggerExecutionRef(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	if !m.allowTarget(ctx, p, ref.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(ref.ProviderName))
	if err != nil {
		return nil, err
	}
	deployment, err := provider.GetWorkflowDefinition(ctx, coreworkflow.GetDefinitionRequest{DefinitionID: triggerID})
	if err != nil {
		return nil, err
	}
	trigger := workflowEventTriggerFromDefinition(deployment, ref)
	if !eventTriggerMatchesExecutionRef(ref.ProviderName, trigger, ref) {
		return nil, core.ErrNotFound
	}
	return &ManagedEventTrigger{
		ProviderName: strings.TrimSpace(ref.ProviderName),
		Trigger:      trigger,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func (m *Manager) requireOwnedDefinitionExecutionRef(ctx context.Context, definitionID string, p *principal.Principal) (*coreworkflow.ExecutionReference, string, coreworkflow.Provider, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return nil, "", nil, core.ErrNotFound
	}
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, "", nil, err
	}
	prefix := definitionExecutionRefPrefix(definitionID)
	var match *coreworkflow.ExecutionReference
	var matchGeneration int64
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		generation := definitionGenerationFromExecutionRefID(ref.ID)
		if match != nil && generation == matchGeneration {
			return nil, "", nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, definitionID)
		}
		if match != nil && generation < matchGeneration {
			continue
		}
		match = ref
		matchGeneration = generation
	}
	if match == nil || !m.allowTarget(ctx, p, match.Target) {
		return nil, "", nil, core.ErrNotFound
	}
	providerName := strings.TrimSpace(match.ProviderName)
	provider, err := m.resolveProviderByName(providerName)
	if err != nil {
		return nil, "", nil, err
	}
	return match, providerName, provider, nil
}

func (m *Manager) listOwnedExecutionRefs(ctx context.Context, p *principal.Principal, activeOnly bool) ([]*coreworkflow.ExecutionReference, error) {
	if m == nil || m.workflow == nil {
		return nil, ErrExecutionRefsNotConfigured
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	out := []*coreworkflow.ExecutionReference{}
	for _, providerName := range m.workflow.ProviderNames() {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		store, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return nil, err
		}
		refs, err := store.ListExecutionReferences(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			ref = workflowExecutionRefForProvider(ref, providerName)
			if !executionRefOwnedBy(ref, p) || (activeOnly && !executionRefActive(ref)) {
				continue
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

func (m *Manager) findOwnedExecutionRef(ctx context.Context, scheduleID string, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	prefix := scheduleExecutionRefPrefix(scheduleID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, scheduleID)
		}
		match = ref
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	return match, nil
}

func (m *Manager) findOwnedEventTriggerExecutionRef(ctx context.Context, triggerID string, p *principal.Principal) (*coreworkflow.ExecutionReference, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	prefix := eventTriggerExecutionRefPrefix(triggerID)
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if !strings.HasPrefix(strings.TrimSpace(ref.ID), prefix) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, triggerID)
		}
		match = ref
	}
	if match == nil {
		return nil, core.ErrNotFound
	}
	return match, nil
}

func (m *Manager) compileWorkflowTargetPlan(ctx context.Context, provider coreworkflow.Provider, target coreworkflow.Target) (*workflowTargetPlan, error) {
	return m.compileWorkflowDefinitionPlan(ctx, provider, coreworkflow.DefinitionSpec{
		Target:                   target,
		WorkflowSemanticsVersion: workflowStepsSemanticsVersion,
	})
}

func (m *Manager) compileWorkflowDefinitionPlan(ctx context.Context, provider coreworkflow.Provider, spec coreworkflow.DefinitionSpec) (*workflowTargetPlan, error) {
	_ = ctx
	_ = provider
	target := spec.Target
	if len(target.Steps) > 0 && strings.TrimSpace(spec.WorkflowSemanticsVersion) == "" {
		spec.WorkflowSemanticsVersion = workflowStepsSemanticsVersion
	}
	targetDigest, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		return nil, fmt.Errorf("workflow target digest: %w", err)
	}
	if len(target.Steps) > 0 && strings.TrimSpace(spec.WorkflowSemanticsVersion) == "" {
		spec.WorkflowSemanticsVersion = workflowStepsSemanticsVersion
	}
	specDigest, err := coreworkflow.DefinitionSpecDigest(spec)
	if err != nil {
		return nil, fmt.Errorf("workflow definition spec digest: %w", err)
	}
	plan := &workflowTargetPlan{
		SpecDigest:   specDigest,
		TargetDigest: targetDigest,
	}
	if actionTableDigest, err := coreworkflow.TargetActionTableDigest(target); err != nil {
		return nil, fmt.Errorf("workflow action table digest: %w", err)
	} else {
		plan.ActionTableDigest = actionTableDigest
	}
	if len(target.Steps) > 0 {
		plan.SemanticsVersion = workflowStepsSemanticsVersion
	}
	return plan, nil
}

func workflowDefinitionSpecForApply(spec coreworkflow.DefinitionSpec, idempotencyScope string) coreworkflow.DefinitionSpec {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		spec.ID = newDefinitionID(idempotencyScope)
	}
	if spec.Generation == 0 {
		spec.Generation = 1
	}
	if strings.TrimSpace(spec.WorkflowSemanticsVersion) == "" {
		spec.WorkflowSemanticsVersion = workflowStepsSemanticsVersion
	}
	for i := range spec.Activations {
		if strings.TrimSpace(spec.Activations[i].ID) == "" {
			spec.Activations[i].ID = fmt.Sprintf("activation-%d", i+1)
		}
		if spec.Activations[i].Mode == "" {
			if spec.Activations[i].Event != nil {
				spec.Activations[i].Mode = coreworkflow.ActivationModeSignalOrStart
			} else {
				spec.Activations[i].Mode = coreworkflow.ActivationModeStart
			}
		}
	}
	return spec
}

func workflowDefinitionBinding(ref *coreworkflow.ExecutionReference, plan *workflowTargetPlan, idempotencyKey string, prepareOnly bool) *coreworkflow.DefinitionBinding {
	_ = prepareOnly
	if ref == nil {
		return nil
	}
	if plan == nil {
		plan = &workflowTargetPlan{}
	}
	return &coreworkflow.DefinitionBinding{
		ID:                       newWorkflowDefinitionBindingID(ref.ID, idempotencyKey),
		ExecutionRef:             strings.TrimSpace(ref.ID),
		ExecutionRefGeneration:   ref.Generation,
		SpecDigest:               strings.TrimSpace(plan.SpecDigest),
		TargetDigest:             strings.TrimSpace(plan.TargetDigest),
		ActionTableDigest:        strings.TrimSpace(plan.ActionTableDigest),
		PermissionsDigest:        strings.TrimSpace(ref.PermissionsDigest),
		WorkflowSemanticsVersion: strings.TrimSpace(plan.SemanticsVersion),
		RequestID:                strings.TrimSpace(idempotencyKey),
	}
}

func workflowDefinitionBindingForDefinition(ref *coreworkflow.ExecutionReference, plan *workflowTargetPlan, idempotencyKey, definitionID string, definitionGeneration int64) *coreworkflow.DefinitionBinding {
	binding := workflowDefinitionBinding(ref, plan, idempotencyKey, false)
	if binding == nil {
		return nil
	}
	binding.DefinitionID = strings.TrimSpace(definitionID)
	binding.DefinitionGeneration = definitionGeneration
	binding.RequestID = strings.TrimSpace(idempotencyKey)
	return binding
}

func workflowTargetPlanFromDefinition(deployment *coreworkflow.Definition, ref *coreworkflow.ExecutionReference) *workflowTargetPlan {
	plan := &workflowTargetPlan{}
	if deployment != nil {
		plan.SpecDigest = strings.TrimSpace(deployment.SpecDigest)
		plan.TargetDigest = strings.TrimSpace(deployment.TargetDigest)
		plan.ActionTableDigest = strings.TrimSpace(deployment.ActionTableDigest)
	}
	if ref != nil {
		if plan.TargetDigest == "" {
			plan.TargetDigest = strings.TrimSpace(ref.TargetDigest)
		}
		if plan.ActionTableDigest == "" {
			plan.ActionTableDigest = strings.TrimSpace(ref.ActionTableDigest)
		}
		plan.PermissionsDigest = strings.TrimSpace(ref.PermissionsDigest)
		plan.SemanticsVersion = strings.TrimSpace(ref.SemanticsVersion)
	}
	return plan
}

func (m *Manager) buildExecutionRefWithPermissionsAndPlan(executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string, permissions []core.AccessPermission, plan *workflowTargetPlan) (*coreworkflow.ExecutionReference, error) {
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	actor := workflowActorFromPrincipal(p)
	refPermissions := m.executionRefPermissionsWithOverride(p, target, callerPluginName, permissions)
	ref := &coreworkflow.ExecutionReference{
		ID:                  executionRefID,
		ProviderName:        strings.TrimSpace(providerName),
		Target:              target,
		CallerPluginName:    strings.TrimSpace(callerPluginName),
		SourceDefinitionID:  strings.TrimSpace(sourceDefinitionID),
		SubjectID:           subjectID,
		SubjectKind:         actor.SubjectKind,
		DisplayName:         actor.DisplayName,
		AuthSource:          actor.AuthSource,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		Permissions:         refPermissions,
	}
	applyWorkflowExecutionRefPlan(ref, plan, refPermissions)
	return ref, nil
}

func applyWorkflowExecutionRefPlan(ref *coreworkflow.ExecutionReference, plan *workflowTargetPlan, permissions []core.AccessPermission) {
	if ref == nil {
		return
	}
	if plan != nil {
		ref.TargetDigest = strings.TrimSpace(plan.TargetDigest)
		ref.ActionTableDigest = strings.TrimSpace(plan.ActionTableDigest)
		ref.SemanticsVersion = strings.TrimSpace(plan.SemanticsVersion)
	}
	if strings.TrimSpace(ref.TargetDigest) == "" {
		if digest, err := coreworkflow.TargetFingerprint(ref.Target); err == nil {
			ref.TargetDigest = digest
		}
	}
	ref.PermissionsDigest = workflowManagerSHA256(executionRefPermissionsScope(permissions))
	if strings.TrimSpace(ref.ActionTableDigest) == "" {
		if digest, err := coreworkflow.TargetActionTableDigest(ref.Target); err == nil {
			ref.ActionTableDigest = digest
		}
	}
	if strings.TrimSpace(ref.SemanticsVersion) == "" && len(ref.Target.Steps) > 0 {
		ref.SemanticsVersion = workflowStepsSemanticsVersion
	}
	if ref.Generation == 0 {
		ref.Generation = 1
	}
}

func (m *Manager) executionRefPermissionsWithOverride(p *principal.Principal, target coreworkflow.Target, callerPluginName string, override []core.AccessPermission) []core.AccessPermission {
	if override == nil {
		return m.executionRefPermissions(p, target, callerPluginName)
	}
	out := canonicalWorkflowAccessPermissions(override)
	if len(target.Steps) > 0 {
		out = appendWorkflowStepActionPermissions(out, target)
		out = canonicalWorkflowAccessPermissions(out)
	}
	if len(out) == 0 {
		return []core.AccessPermission{{Plugin: workflowNoProviderPermissionsPlugin}}
	}
	return out
}

func (m *Manager) executionRefPermissions(p *principal.Principal, target coreworkflow.Target, callerPluginName string) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil || p.TokenPermissions == nil {
		return principal.PermissionsToAccessPermissions(nil)
	}
	permissions := principal.ClonePermissionSet(p.TokenPermissions)
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil && m.callerPluginDeclaresInvoke(callerPluginName, step.Plugin.Name, step.Plugin.Operation) {
			addWorkflowPermission(permissions, step.Plugin.Name, step.Plugin.Operation)
		}
		if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil && m.callerPluginDeclaresInvoke(callerPluginName, step.OutputDelivery.Plugin.Name, step.OutputDelivery.Plugin.Operation) {
			addWorkflowPermission(permissions, step.OutputDelivery.Plugin.Name, step.OutputDelivery.Plugin.Operation)
		}
		if step.Agent == nil {
			continue
		}
		for j := range step.Agent.ToolRefs {
			tool := step.Agent.ToolRefs[j]
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" || pluginName == "*" || operation == "" {
				continue
			}
			if m.callerPluginDeclaresInvoke(callerPluginName, pluginName, operation) {
				addWorkflowPermission(permissions, pluginName, operation)
			}
		}
	}
	if len(target.Steps) > 0 {
		for i := range target.Steps {
			step := target.Steps[i]
			if step.Plugin != nil {
				addWorkflowPermission(permissions, step.Plugin.Name, step.Plugin.Operation)
			}
			if step.Agent != nil {
				addWorkflowProviderPermission(permissions, step.Agent.ProviderName)
				for j := range step.Agent.ToolRefs {
					tool := step.Agent.ToolRefs[j]
					pluginName := strings.TrimSpace(tool.Plugin)
					operation := strings.TrimSpace(tool.Operation)
					if pluginName == "" || pluginName == "*" || operation == "" {
						continue
					}
					if m.callerPluginDeclaresInvoke(callerPluginName, pluginName, operation) {
						addWorkflowPermission(permissions, pluginName, operation)
					}
				}
			}
			if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil {
				addWorkflowPermission(permissions, step.OutputDelivery.Plugin.Name, step.OutputDelivery.Plugin.Operation)
			}
		}
	}
	out := principal.PermissionsToAccessPermissions(permissions)
	if len(target.Steps) > 0 {
		out = appendWorkflowStepActionPermissions(out, target)
	}
	if len(out) == 0 {
		return []core.AccessPermission{{Plugin: workflowNoProviderPermissionsPlugin}}
	}
	return out
}

func executionRefPrincipal(p *principal.Principal, permissions []core.AccessPermission) *principal.Principal {
	p = principal.Canonicalized(p)
	if p == nil {
		return nil
	}
	compiled := principal.CompilePermissions(permissions)
	if permissions != nil && compiled == nil {
		compiled = principal.PermissionSet{}
	}
	next := *p
	next.TokenPermissions = compiled
	next.ActionPermissions = principal.CompileActionPermissions(permissions)
	next.Scopes = principal.PermissionPlugins(compiled)
	return principal.Canonicalize(&next)
}

func (m *Manager) callerPluginDeclaresInvoke(callerPluginName, pluginName, operation string) bool {
	callerPluginName = strings.TrimSpace(callerPluginName)
	pluginName = strings.TrimSpace(pluginName)
	operation = strings.TrimSpace(operation)
	if callerPluginName == "" || pluginName == "" || operation == "" || m == nil {
		return false
	}
	for _, invoke := range m.pluginInvokes[callerPluginName] {
		if strings.TrimSpace(invoke.Surface) != "" {
			continue
		}
		if strings.TrimSpace(invoke.Plugin) == pluginName && strings.TrimSpace(invoke.Operation) == operation {
			return true
		}
	}
	return false
}

func addWorkflowPermission(permissions principal.PermissionSet, pluginName, operation string) {
	pluginName = strings.TrimSpace(pluginName)
	operation = strings.TrimSpace(operation)
	if permissions == nil || pluginName == "" || operation == "" {
		return
	}
	if operations, ok := permissions[pluginName]; ok && operations == nil {
		return
	}
	operations := permissions[pluginName]
	if operations == nil {
		operations = map[string]struct{}{}
		permissions[pluginName] = operations
	}
	operations[operation] = struct{}{}
}

func addWorkflowProviderPermission(permissions principal.PermissionSet, pluginName string) {
	pluginName = strings.TrimSpace(pluginName)
	if permissions == nil || pluginName == "" {
		return
	}
	permissions[pluginName] = nil
}

func appendWorkflowStepActionPermissions(permissions []core.AccessPermission, target coreworkflow.Target) []core.AccessPermission {
	if len(target.Steps) == 0 {
		return permissions
	}
	actions := make([]string, 0, len(target.Steps)*2)
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil {
			if actionID, ok := coreworkflow.StepPluginActionID(step.ID); ok {
				actions = append(actions, actionID)
			}
		}
		if step.Agent != nil {
			if actionID, ok := coreworkflow.StepAgentActionID(step.ID); ok {
				actions = append(actions, actionID)
			}
		}
		if step.OutputDelivery != nil {
			if actionID, ok := coreworkflow.StepDeliveryActionID(step.ID); ok {
				actions = append(actions, actionID)
			}
		}
	}
	sort.Strings(actions)
	if len(actions) == 0 {
		return permissions
	}
	return append(permissions, core.AccessPermission{
		Plugin:  coreworkflow.StepActionPermissionPlugin,
		Actions: actions,
	})
}

func canonicalWorkflowAccessPermissions(values []core.AccessPermission) []core.AccessPermission {
	if len(values) == 0 {
		return nil
	}
	type permissionParts struct {
		provider   bool
		operations map[string]struct{}
		actions    map[string]struct{}
	}
	byPlugin := map[string]*permissionParts{}
	for i := range values {
		plugin := strings.TrimSpace(values[i].Plugin)
		if plugin == "" {
			continue
		}
		parts := byPlugin[plugin]
		if parts == nil {
			parts = &permissionParts{}
			byPlugin[plugin] = parts
		}
		if len(values[i].Operations) == 0 && len(values[i].Actions) == 0 {
			parts.provider = true
		}
		for _, operation := range values[i].Operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			if parts.operations == nil {
				parts.operations = map[string]struct{}{}
			}
			parts.operations[operation] = struct{}{}
		}
		for _, action := range values[i].Actions {
			action = strings.TrimSpace(action)
			if action == "" {
				continue
			}
			if parts.actions == nil {
				parts.actions = map[string]struct{}{}
			}
			parts.actions[action] = struct{}{}
		}
	}
	plugins := make([]string, 0, len(byPlugin))
	for plugin := range byPlugin {
		plugins = append(plugins, plugin)
	}
	sort.Strings(plugins)
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, plugin := range plugins {
		parts := byPlugin[plugin]
		operations := sortedWorkflowPermissionNames(parts.operations)
		actions := sortedWorkflowPermissionNames(parts.actions)
		if parts.provider {
			out = append(out, core.AccessPermission{Plugin: plugin})
			if len(actions) > 0 {
				out = append(out, core.AccessPermission{Plugin: plugin, Actions: actions})
			}
			continue
		}
		out = append(out, core.AccessPermission{Plugin: plugin, Operations: operations, Actions: actions})
	}
	return out
}

func sortedWorkflowPermissionNames(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func workflowExecutionReferenceStore(providerName string, provider coreworkflow.Provider) (coreworkflow.ExecutionReferenceStore, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: workflow provider %q is not configured", ErrExecutionRefsNotConfigured, strings.TrimSpace(providerName))
	}
	store, ok := provider.(coreworkflow.ExecutionReferenceStore)
	if !ok {
		return nil, fmt.Errorf("%w: workflow provider %q does not support execution references", ErrExecutionRefsNotConfigured, strings.TrimSpace(providerName))
	}
	return store, nil
}

func workflowExecutionRefForProvider(ref *coreworkflow.ExecutionReference, providerName string) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	providerName = strings.TrimSpace(providerName)
	refProviderName := strings.TrimSpace(ref.ProviderName)
	if providerName == "" || refProviderName == providerName {
		return ref
	}
	if refProviderName != "" {
		return nil
	}
	cloned := *ref
	cloned.ProviderName = providerName
	return &cloned
}

func isWorkflowProviderNotFound(err error) bool {
	return errors.Is(err, core.ErrNotFound) || status.Code(err) == codes.NotFound
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

const (
	targetAuthorizationComponentTarget               = "target"
	targetAuthorizationComponentAgentProvider        = "agent_provider"
	targetAuthorizationComponentAgentToolRef         = "agent_tool_ref"
	targetAuthorizationComponentOutputDelivery       = "output_delivery"
	targetAuthorizationComponentSessionReadyDelivery = "session_ready_delivery"
	targetAuthorizationComponentPluginTarget         = "plugin_target"

	targetAuthorizationReasonMissingAgentProvider               = "missing_agent_provider"
	targetAuthorizationReasonAuthorizerProviderDenied           = "authorizer_provider_denied"
	targetAuthorizationReasonPrincipalProviderPermissionDenied  = "principal_provider_permission_denied"
	targetAuthorizationReasonMissingToolProvider                = "missing_tool_provider"
	targetAuthorizationReasonInvalidSystemToolRef               = "invalid_system_tool_ref"
	targetAuthorizationReasonNonExactToolRefWithSystemTools     = "non_exact_tool_ref_with_system_tools"
	targetAuthorizationReasonAuthorizerOperationDenied          = "authorizer_operation_denied"
	targetAuthorizationReasonPrincipalOperationPermissionDenied = "principal_operation_permission_denied"
	targetAuthorizationReasonMissingPluginTarget                = "missing_plugin_target"
	targetAuthorizationReasonMissingPluginProvider              = "missing_plugin_provider"
	targetAuthorizationReasonMissingPluginOperation             = "missing_plugin_operation"
)

type targetAuthorizationDecision struct {
	allowed bool
	failure targetAuthorizationFailure
}

type targetAuthorizationFailure struct {
	component    string
	reason       string
	provider     string
	operation    string
	toolRefIndex int
}

func (m *Manager) allowTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	return m.checkTargetAuthorization(ctx, p, target).allowed
}

func (m *Manager) checkTargetAuthorization(ctx context.Context, p *principal.Principal, target coreworkflow.Target) targetAuthorizationDecision {
	if len(target.Steps) > 0 {
		for stepIndex := range target.Steps {
			step := target.Steps[stepIndex]
			if step.Plugin != nil {
				if decision := m.checkWorkflowPluginCallAuthorization(ctx, p, *step.Plugin, targetAuthorizationComponentPluginTarget, stepIndex); !decision.allowed {
					return decision
				}
			}
			if step.Agent != nil {
				agentProviderName := strings.TrimSpace(step.Agent.ProviderName)
				if agentProviderName == "" {
					return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonMissingAgentProvider, "", "", stepIndex)
				}
				if !m.allowProvider(ctx, p, agentProviderName) {
					return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonAuthorizerProviderDenied, agentProviderName, "", stepIndex)
				}
				if !principal.AllowsProviderPermission(p, agentProviderName) {
					return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonPrincipalProviderPermissionDenied, agentProviderName, "", stepIndex)
				}
				for i := range step.Agent.ToolRefs {
					tool := step.Agent.ToolRefs[i]
					if systemName := strings.TrimSpace(tool.System); systemName != "" {
						if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(tool.Operation) == "" {
							return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", i)
						}
						if strings.TrimSpace(tool.Plugin) != "" || strings.TrimSpace(tool.Connection) != "" || strings.TrimSpace(tool.Instance) != "" || tool.CredentialMode != "" {
							return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", i)
						}
						continue
					}
					pluginName := strings.TrimSpace(tool.Plugin)
					operation := strings.TrimSpace(tool.Operation)
					if pluginName == "" {
						return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonMissingToolProvider, "", operation, i)
					}
					if !m.allowProvider(ctx, p, pluginName) {
						return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonAuthorizerProviderDenied, pluginName, operation, i)
					}
					if !m.allowOperation(ctx, p, pluginName, operation) {
						return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonAuthorizerOperationDenied, pluginName, operation, i)
					}
					if !principal.AllowsOperationPermission(p, pluginName, operation) {
						return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonPrincipalOperationPermissionDenied, pluginName, operation, i)
					}
				}
			}
			if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil {
				if decision := m.checkWorkflowPluginCallAuthorization(ctx, p, *step.OutputDelivery.Plugin, targetAuthorizationComponentOutputDelivery, stepIndex); !decision.allowed {
					return decision
				}
			}
		}
		return targetAuthorizationAllowed()
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkWorkflowPluginCallAuthorization(ctx context.Context, p *principal.Principal, call coreworkflow.PluginCall, component string, index int) targetAuthorizationDecision {
	pluginName := strings.TrimSpace(call.Name)
	operation := strings.TrimSpace(call.Operation)
	if pluginName == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingPluginProvider, "", operation, index)
	}
	if operation == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingPluginOperation, pluginName, "", index)
	}
	if !m.allowProvider(ctx, p, pluginName) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerProviderDenied, pluginName, operation, index)
	}
	if !m.allowOperation(ctx, p, pluginName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerOperationDenied, pluginName, operation, index)
	}
	if !principal.AllowsOperationPermission(p, pluginName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonPrincipalOperationPermissionDenied, pluginName, operation, index)
	}
	return targetAuthorizationAllowed()
}

func targetAuthorizationAllowed() targetAuthorizationDecision {
	return targetAuthorizationDecision{allowed: true}
}

func targetAuthorizationDenied(component, reason, provider, operation string, toolRefIndex int) targetAuthorizationDecision {
	return targetAuthorizationDecision{
		failure: targetAuthorizationFailure{
			component:    component,
			reason:       reason,
			provider:     provider,
			operation:    operation,
			toolRefIndex: toolRefIndex,
		},
	}
}

func workflowAgentToolRefsContainSystem(refs []coreagent.ToolRef) bool {
	for i := range refs {
		if strings.TrimSpace(refs[i].System) != "" {
			return true
		}
	}
	return false
}

func validateWorkflowAgentToolRefs(refs []coreagent.ToolRef) error {
	hasSystemTools := workflowAgentToolRefsContainSystem(refs)
	for i := range refs {
		ref := refs[i]
		systemName := strings.TrimSpace(ref.System)
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		connection := strings.TrimSpace(ref.Connection)
		instance := strings.TrimSpace(ref.Instance)
		if systemName != "" {
			if pluginName != "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d] must set exactly one of plugin or system", invocation.ErrInvalidInvocation, i)
			}
			if systemName != coreagent.SystemToolWorkflow {
				return fmt.Errorf("%w: workflow agent tool_refs[%d].system %q is not supported", invocation.ErrInvalidInvocation, i, systemName)
			}
			if operation == "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d].operation is required for system tool refs", invocation.ErrOperationNotFound, i)
			}
			if connection != "" || instance != "" || ref.CredentialMode != "" {
				return fmt.Errorf("%w: workflow agent tool_refs[%d] system refs cannot include connection, instance, or credential mode", invocation.ErrInvalidInvocation, i)
			}
			continue
		}
		if !hasSystemTools {
			continue
		}
		if pluginName == "" || pluginName == "*" || operation == "" {
			return fmt.Errorf("%w: workflow agent tool_refs[%d] must be an exact plugin operation when workflow system tools are delegated", invocation.ErrInvalidInvocation, i)
		}
	}
	return nil
}

func cloneWorkflowValueMap(values map[string]coreworkflow.Value) map[string]coreworkflow.Value {
	if values == nil {
		return nil
	}
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = coreworkflow.CloneValue(values[key])
	}
	return out
}

func cloneWorkflowStepWhen(when *coreworkflow.StepWhen) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	out := *when
	out.Value = coreworkflow.CloneValue(when.Value)
	return &out
}

func workflowValueKindCount(value coreworkflow.Value) int {
	count := 0
	if value.LiteralSet {
		count++
	}
	if value.Object != nil {
		count++
	}
	if value.Array != nil {
		count++
	}
	if value.Template != nil {
		count++
	}
	if strings.TrimSpace(value.RunInput) != "" {
		count++
	}
	if strings.TrimSpace(value.SignalPayload) != "" {
		count++
	}
	if value.StepOutput != nil {
		count++
	}
	return count
}

func workflowValueIsObjectOrUnset(value coreworkflow.Value) bool {
	count := workflowValueKindCount(value)
	return count == 0 || (count == 1 && value.Object != nil)
}

func validateWorkflowStepValueRefs(fieldName string, values map[string]coreworkflow.Value, seen map[string]struct{}) error {
	for key := range values {
		if err := validateWorkflowStepValueRef(fieldName+"."+key, values[key], seen); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowStepValueRef(fieldName string, value coreworkflow.Value, seen map[string]struct{}) error {
	if value.StepOutput != nil {
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		if stepID == "" {
			return fmt.Errorf("workflow %s.step_output.step_id is required", fieldName)
		}
		if _, ok := seen[stepID]; !ok {
			return fmt.Errorf("workflow %s.step_output.step_id %q must reference an earlier step", fieldName, stepID)
		}
		if strings.TrimSpace(value.StepOutput.Path) == "" {
			return fmt.Errorf("workflow %s.step_output.path is required", fieldName)
		}
	}
	for key := range value.Object {
		if err := validateWorkflowStepValueRef(fieldName+"."+key, value.Object[key], seen); err != nil {
			return err
		}
	}
	for i := range value.Array {
		if err := validateWorkflowStepValueRef(fmt.Sprintf("%s[%d]", fieldName, i), value.Array[i], seen); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) callerPluginInvokeCredentialMode(callerPluginName, pluginName, operation string) (core.ConnectionMode, bool, error) {
	callerPluginName = strings.TrimSpace(callerPluginName)
	pluginName = strings.TrimSpace(pluginName)
	operation = strings.TrimSpace(operation)
	if callerPluginName == "" || pluginName == "" || operation == "" || m == nil {
		return "", false, nil
	}
	for _, invoke := range m.pluginInvokes[callerPluginName] {
		if strings.TrimSpace(invoke.Surface) != "" {
			continue
		}
		if strings.TrimSpace(invoke.Plugin) != pluginName || strings.TrimSpace(invoke.Operation) != operation {
			continue
		}
		mode := core.NormalizeOptionalConnectionMode(invoke.CredentialMode)
		switch mode {
		case "":
			return "", false, nil
		case core.ConnectionModeNone, core.ConnectionModeUser:
			return mode, true, nil
		default:
			return "", false, fmt.Errorf("%w: caller plugin invoke credentialMode %q is not supported", invocation.ErrInvalidInvocation, invoke.CredentialMode)
		}
	}
	return "", false, nil
}

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func executionRefOwnedBy(ref *coreworkflow.ExecutionReference, p *principal.Principal) bool {
	if ref == nil || p == nil {
		return false
	}
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ref.SubjectID) == subjectID
}

func executionRefActive(ref *coreworkflow.ExecutionReference) bool {
	return ref != nil && (ref.RevokedAt == nil || ref.RevokedAt.IsZero())
}

func executionRefsByProvider(refs []*coreworkflow.ExecutionReference) map[string][]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string][]*coreworkflow.ExecutionReference)
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

func executionRefsByID(refs []*coreworkflow.ExecutionReference) map[string]*coreworkflow.ExecutionReference {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]*coreworkflow.ExecutionReference, len(refs))
	for _, ref := range refs {
		if ref == nil || strings.TrimSpace(ref.ID) == "" {
			continue
		}
		out[strings.TrimSpace(ref.ID)] = ref
	}
	return out
}

func scheduleMatchesExecutionRef(providerName string, schedule *coreworkflow.Schedule, ref *coreworkflow.ExecutionReference) bool {
	if schedule == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(schedule.Target, ref)
}

func eventTriggerMatchesExecutionRef(providerName string, trigger *coreworkflow.EventTrigger, ref *coreworkflow.ExecutionReference) bool {
	if trigger == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(trigger.Target, ref)
}

func definitionMatchesExecutionRef(providerName string, deployment *coreworkflow.Definition, ref *coreworkflow.ExecutionReference) bool {
	if deployment == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	if definitionID := strings.TrimSpace(deployment.Spec.ID); definitionID != "" && definitionIDFromExecutionRefID(ref.ID) != definitionID {
		return false
	}
	return targetMatchesExecutionRef(deployment.Spec.Target, ref)
}

func runMatchesExecutionRef(providerName string, run *coreworkflow.Run, ref *coreworkflow.ExecutionReference) bool {
	if run == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	if executionRef := strings.TrimSpace(run.ExecutionRef); executionRef != "" && executionRef != strings.TrimSpace(ref.ID) {
		return false
	}
	if workflowTargetIsSet(run.Target) {
		return targetMatchesExecutionRef(run.Target, ref)
	}
	if targetDigest := strings.TrimSpace(run.TargetDigest); targetDigest != "" {
		refTargetDigest := strings.TrimSpace(ref.TargetDigest)
		if refTargetDigest == "" {
			digest, err := coreworkflow.TargetFingerprint(ref.Target)
			if err != nil {
				return false
			}
			refTargetDigest = digest
		}
		return targetDigest == refTargetDigest
	}
	return false
}

func targetMatchesExecutionRef(target coreworkflow.Target, ref *coreworkflow.ExecutionReference) bool {
	if ref == nil {
		return false
	}
	return coreworkflow.TargetsEqual(target, ref.Target)
}

func signalOrStartExecutionRefMatches(ref *coreworkflow.ExecutionReference, executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal, callerPluginName string, permissions []core.AccessPermission) bool {
	if ref == nil {
		return false
	}
	if strings.TrimSpace(ref.ID) != strings.TrimSpace(executionRefID) {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	if strings.TrimSpace(ref.CallerPluginName) != strings.TrimSpace(callerPluginName) {
		return false
	}
	if strings.TrimSpace(ref.CredentialSubjectID) != strings.TrimSpace(principal.EffectiveCredentialSubjectID(principal.Canonicalized(p))) {
		return false
	}
	if executionRefPermissionsScope(ref.Permissions) != executionRefPermissionsScope(permissions) {
		return false
	}
	return executionRefOwnedBy(ref, p) && targetMatchesExecutionRef(target, ref)
}

func runExecutionRefMatches(ref *coreworkflow.ExecutionReference, executionRefID, providerName string, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string, permissions []core.AccessPermission) bool {
	if !signalOrStartExecutionRefMatches(ref, executionRefID, providerName, target, p, callerPluginName, permissions) {
		return false
	}
	return strings.TrimSpace(ref.SourceDefinitionID) == strings.TrimSpace(sourceDefinitionID)
}

func normalizeEventMatch(match coreworkflow.EventMatch) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    strings.TrimSpace(match.Type),
		Source:  strings.TrimSpace(match.Source),
		Subject: strings.TrimSpace(match.Subject),
	}
}

func workflowActorFromPrincipal(p *principal.Principal) coreworkflow.Actor {
	p = principal.Canonicalized(p)
	if p == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   strings.TrimSpace(p.SubjectID),
		SubjectKind: string(p.Kind),
		DisplayName: workflowActorDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func workflowActorDisplayName(p *principal.Principal) string {
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

func scheduleExecutionRefID(scheduleID string) string {
	return scheduleExecutionRefPrefix(scheduleID) + uuid.NewString()
}

func newScheduleID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.schedule:"+idempotencyScope)).String()
}

func workflowCreateIdempotencyScope(p *principal.Principal, callerPluginName, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{strings.TrimSpace(principalSubjectID(p)), strings.TrimSpace(callerPluginName), idempotencyKey}, "\x00")
}

func newScheduleExecutionRefID(scheduleID, idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return scheduleExecutionRefID(scheduleID)
	}
	return scheduleExecutionRefPrefix(scheduleID) + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.schedule.ref:"+strings.TrimSpace(scheduleID)+":"+idempotencyScope)).String()
}

func scheduleExecutionRefPrefix(scheduleID string) string {
	return workflowScheduleExecutionRefBasePrefix + strings.TrimSpace(scheduleID) + ":"
}

func scheduleIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowScheduleExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowScheduleExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func eventTriggerExecutionRefID(triggerID string) string {
	return eventTriggerExecutionRefPrefix(triggerID) + uuid.NewString()
}

func newEventTriggerID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.event-trigger:"+idempotencyScope)).String()
}

func newEventTriggerExecutionRefID(triggerID, idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return eventTriggerExecutionRefID(triggerID)
	}
	return eventTriggerExecutionRefPrefix(triggerID) + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.event-trigger.ref:"+strings.TrimSpace(triggerID)+":"+idempotencyScope)).String()
}

func eventTriggerExecutionRefPrefix(triggerID string) string {
	return workflowEventTriggerExecutionRefBasePrefix + strings.TrimSpace(triggerID) + ":"
}

func eventTriggerIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowEventTriggerExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowEventTriggerExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func newDefinitionID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.definition:"+idempotencyScope)).String()
}

func newDefinitionExecutionRefID(definitionID string, generation int64) string {
	if generation <= 0 {
		generation = 1
	}
	return definitionExecutionRefPrefix(definitionID) + strconv.FormatInt(generation, 10)
}

func definitionExecutionRefPrefix(definitionID string) string {
	return workflowDefinitionExecutionRefBasePrefix + strings.TrimSpace(definitionID) + ":"
}

func definitionIDFromExecutionRefID(executionRefID string) string {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowDefinitionExecutionRefBasePrefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, workflowDefinitionExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 {
		return ""
	}
	return rest[:lastColon]
}

func definitionGenerationFromExecutionRefID(executionRefID string) int64 {
	trimmed := strings.TrimSpace(executionRefID)
	if !strings.HasPrefix(trimmed, workflowDefinitionExecutionRefBasePrefix) {
		return 0
	}
	rest := strings.TrimPrefix(trimmed, workflowDefinitionExecutionRefBasePrefix)
	lastColon := strings.LastIndex(rest, ":")
	if lastColon <= 0 || lastColon == len(rest)-1 {
		return 0
	}
	generation, err := strconv.ParseInt(rest[lastColon+1:], 10, 64)
	if err != nil || generation < 0 {
		return 0
	}
	return generation
}

func runExecutionRefID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = uuid.NewString()
	}
	return workflowRunExecutionRefBasePrefix + value
}

func newRunExecutionRefID(idempotencyScope, workflowKey string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return runExecutionRefID("")
	}
	scope := strings.Join([]string{"gestalt.workflow.run", idempotencyScope, strings.TrimSpace(workflowKey)}, "\x00")
	return runExecutionRefID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String())
}

func newWorkflowDefinitionBindingID(executionRefID, requestID string) string {
	scope := strings.Join([]string{
		"gestalt.workflow.definition_binding.v1",
		strings.TrimSpace(executionRefID),
		strings.TrimSpace(requestID),
	}, "\x00")
	return "wdb_" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String()
}

func signalOrStartExecutionRefID(providerName, workflowKey string, target coreworkflow.Target, p *principal.Principal, callerPluginName string, permissions []core.AccessPermission) (string, error) {
	targetFingerprint, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		return "", fmt.Errorf("workflow target fingerprint: %w", err)
	}
	scope := strings.Join([]string{
		"gestalt.workflow.run.signal_or_start.ref.v2",
		strings.TrimSpace(providerName),
		strings.TrimSpace(workflowKey),
		strings.TrimSpace(principalSubjectID(p)),
		strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		strings.TrimSpace(callerPluginName),
		targetFingerprint,
		executionRefPermissionsScope(permissions),
	}, "\x00")
	return runExecutionRefID(uuid.NewSHA1(uuid.NameSpaceURL, []byte(scope)).String()), nil
}

func executionRefPermissionsScope(permissions []core.AccessPermission) string {
	if permissions == nil {
		return "nil"
	}
	if len(permissions) == 0 {
		return "empty"
	}
	var b strings.Builder
	b.WriteString("set\x1f")
	wrote := false
	for _, permission := range permissions {
		plugin := strings.TrimSpace(permission.Plugin)
		if plugin == "" {
			continue
		}
		wrote = true
		b.WriteString(plugin)
		b.WriteByte('\x1e')
		operations := append([]string(nil), permission.Operations...)
		sort.Strings(operations)
		for _, operation := range operations {
			operation = strings.TrimSpace(operation)
			if operation == "" {
				continue
			}
			b.WriteString(operation)
			b.WriteByte('\x1d')
		}
		b.WriteByte('\x1e')
		actions := append([]string(nil), permission.Actions...)
		sort.Strings(actions)
		for _, action := range actions {
			action = strings.TrimSpace(action)
			if action == "" {
				continue
			}
			b.WriteString(action)
			b.WriteByte('\x1d')
		}
		b.WriteByte('\x1f')
	}
	if !wrote {
		return "empty"
	}
	return b.String()
}

func (m *Manager) managedSignalResponse(ctx context.Context, p *principal.Principal, providerName string, provider coreworkflow.Provider, resp *coreworkflow.SignalRunResponse, candidateRef *coreworkflow.ExecutionReference, targetPrincipalSource signalTargetPrincipalSource) (*ManagedRunSignal, error) {
	if resp == nil || resp.Run == nil {
		return nil, core.ErrNotFound
	}
	providerName = strings.TrimSpace(providerName)
	ref := candidateRef
	if !runMatchesExecutionRef(providerName, resp.Run, ref) || strings.TrimSpace(ref.ID) != strings.TrimSpace(resp.Run.ExecutionRef) {
		ref = nil
	}
	if ref == nil {
		store, err := workflowExecutionReferenceStore(providerName, provider)
		if err != nil {
			return nil, err
		}
		ref, err = store.GetExecutionReference(ctx, strings.TrimSpace(resp.Run.ExecutionRef))
		if err != nil {
			return nil, err
		}
		ref = workflowExecutionRefForProvider(ref, providerName)
	}
	targetPrincipal := p
	if targetPrincipalSource == signalTargetPrincipalExecutionRef {
		targetPrincipal = workflowprincipal.RuntimePrincipalFromExecutionReference(ref)
	}
	if !executionRefOwnedBy(ref, p) || !executionRefActive(ref) || !m.allowTarget(ctx, targetPrincipal, ref.Target) || !runMatchesExecutionRef(providerName, resp.Run, ref) {
		return nil, core.ErrNotFound
	}
	workflowKey := strings.TrimSpace(resp.WorkflowKey)
	if workflowKey == "" {
		workflowKey = strings.TrimSpace(resp.Run.WorkflowKey)
	}
	return &ManagedRunSignal{
		ProviderName: providerName,
		Run:          resp.Run,
		Signal:       resp.Signal,
		StartedRun:   resp.StartedRun,
		WorkflowKey:  workflowKey,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
}

func existingProvider(value *ManagedSchedule) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func existingRunProvider(value *ManagedRun) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func existingEventTriggerProvider(value *ManagedEventTrigger) coreworkflow.Provider {
	if value == nil {
		return nil
	}
	return value.provider
}

func workflowScheduleDefinitionSpec(scheduleID string, target coreworkflow.Target, req ScheduleUpsert) coreworkflow.DefinitionSpec {
	return coreworkflow.DefinitionSpec{
		ID:                       strings.TrimSpace(scheduleID),
		Generation:               1,
		Target:                   target,
		Paused:                   req.Paused,
		Permissions:              append([]core.AccessPermission(nil), req.Permissions...),
		WorkflowSemanticsVersion: workflowStepsSemanticsVersion,
		Activations: []coreworkflow.Activation{{
			ID:     "schedule",
			Paused: req.Paused,
			Mode:   coreworkflow.ActivationModeStart,
			Schedule: &coreworkflow.ScheduleActivation{
				Cron:     strings.TrimSpace(req.Cron),
				Timezone: strings.TrimSpace(req.Timezone),
			},
		}},
	}
}

func workflowEventTriggerDefinitionSpec(triggerID string, target coreworkflow.Target, match coreworkflow.EventMatch, req EventTriggerUpsert) coreworkflow.DefinitionSpec {
	return coreworkflow.DefinitionSpec{
		ID:                       strings.TrimSpace(triggerID),
		Generation:               1,
		Target:                   target,
		Paused:                   req.Paused,
		Permissions:              append([]core.AccessPermission(nil), req.Permissions...),
		WorkflowSemanticsVersion: workflowStepsSemanticsVersion,
		Activations: []coreworkflow.Activation{{
			ID:     "event",
			Paused: req.Paused,
			Mode:   coreworkflow.ActivationModeSignalOrStart,
			Event: &coreworkflow.EventActivation{
				Match: match,
			},
		}},
	}
}

func workflowScheduleFromDefinition(deployment *coreworkflow.Definition, ref *coreworkflow.ExecutionReference) *coreworkflow.Schedule {
	if deployment == nil {
		return nil
	}
	spec := deployment.Spec
	var activation coreworkflow.Activation
	for i := range spec.Activations {
		if spec.Activations[i].Schedule != nil {
			activation = spec.Activations[i]
			break
		}
	}
	if activation.Schedule == nil {
		return nil
	}
	return &coreworkflow.Schedule{
		ID:           strings.TrimSpace(spec.ID),
		Cron:         strings.TrimSpace(activation.Schedule.Cron),
		Timezone:     strings.TrimSpace(activation.Schedule.Timezone),
		Target:       spec.Target,
		Paused:       spec.Paused || activation.Paused || deployment.Status == coreworkflow.DefinitionStatusPaused,
		CreatedAt:    deployment.CreatedAt,
		UpdatedAt:    deployment.UpdatedAt,
		CreatedBy:    workflowActorFromExecutionReference(ref),
		ExecutionRef: workflowExecutionRefID(ref, deployment),
	}
}

func workflowEventTriggerFromDefinition(deployment *coreworkflow.Definition, ref *coreworkflow.ExecutionReference) *coreworkflow.EventTrigger {
	if deployment == nil {
		return nil
	}
	spec := deployment.Spec
	var activation coreworkflow.Activation
	for i := range spec.Activations {
		if spec.Activations[i].Event != nil {
			activation = spec.Activations[i]
			break
		}
	}
	if activation.Event == nil {
		return nil
	}
	return &coreworkflow.EventTrigger{
		ID:           strings.TrimSpace(spec.ID),
		Match:        activation.Event.Match,
		Target:       spec.Target,
		Paused:       spec.Paused || activation.Paused || deployment.Status == coreworkflow.DefinitionStatusPaused,
		CreatedAt:    deployment.CreatedAt,
		UpdatedAt:    deployment.UpdatedAt,
		CreatedBy:    workflowActorFromExecutionReference(ref),
		ExecutionRef: workflowExecutionRefID(ref, deployment),
	}
}

func workflowExecutionRefID(ref *coreworkflow.ExecutionReference, deployment *coreworkflow.Definition) string {
	if ref != nil && strings.TrimSpace(ref.ID) != "" {
		return strings.TrimSpace(ref.ID)
	}
	if deployment != nil && deployment.Binding != nil {
		return strings.TrimSpace(deployment.Binding.ExecutionRef)
	}
	return ""
}

func workflowActorFromExecutionReference(ref *coreworkflow.ExecutionReference) coreworkflow.Actor {
	if ref == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   strings.TrimSpace(ref.SubjectID),
		SubjectKind: strings.TrimSpace(ref.SubjectKind),
		DisplayName: strings.TrimSpace(ref.DisplayName),
		AuthSource:  strings.TrimSpace(ref.AuthSource),
	}
}

func (m *Manager) normalizeSignal(signal coreworkflow.Signal, p *principal.Principal) (coreworkflow.Signal, error) {
	signal.ID = strings.TrimSpace(signal.ID)
	signal.Name = strings.TrimSpace(signal.Name)
	signal.IdempotencyKey = strings.TrimSpace(signal.IdempotencyKey)
	signal.Payload = maps.Clone(signal.Payload)
	signal.Metadata = maps.Clone(signal.Metadata)
	if signal.Name == "" {
		return coreworkflow.Signal{}, ErrWorkflowSignalNameRequired
	}
	if signal.CreatedBy == (coreworkflow.Actor{}) {
		signal.CreatedBy = workflowActorFromPrincipal(p)
	}
	if signal.CreatedAt == nil || signal.CreatedAt.IsZero() {
		value := m.now().UTC()
		signal.CreatedAt = &value
	} else {
		value := signal.CreatedAt.UTC()
		signal.CreatedAt = &value
	}
	return signal, nil
}

func normalizePublishedEvent(event coreworkflow.Event, now time.Time) coreworkflow.Event {
	event.ID = strings.TrimSpace(event.ID)
	event.Source = strings.TrimSpace(event.Source)
	event.SpecVersion = strings.TrimSpace(event.SpecVersion)
	event.Type = strings.TrimSpace(event.Type)
	event.Subject = strings.TrimSpace(event.Subject)
	event.DataContentType = strings.TrimSpace(event.DataContentType)
	event.Data = maps.Clone(event.Data)
	event.Extensions = maps.Clone(event.Extensions)
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.SpecVersion == "" {
		event.SpecVersion = defaultWorkflowEventSpecVersion
	}
	if event.Time == nil || event.Time.IsZero() {
		value := now.UTC()
		event.Time = &value
	} else {
		value := event.Time.UTC()
		event.Time = &value
	}
	return event
}
