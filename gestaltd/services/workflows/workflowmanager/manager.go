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
const workflowNoProviderPermissionsPlugin = "__gestalt.workflow.no_provider_permissions__"
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
	CreateDefinition(ctx context.Context, p *principal.Principal, req DefinitionUpsert) (*ManagedDefinition, error)
	GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error)
	UpdateDefinition(ctx context.Context, p *principal.Principal, definitionID string, req DefinitionUpsert) (*ManagedDefinition, error)
	DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) error
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

type DefinitionUpsert struct {
	ProviderName     string
	Target           coreworkflow.Target
	IdempotencyKey   string
	CallerPluginName string
	Permissions      []core.AccessPermission
}

type EventTriggerUpsert struct {
	ProviderName     string
	Match            coreworkflow.EventMatch
	Target           coreworkflow.Target
	DefinitionID     string
	Paused           bool
	IdempotencyKey   string
	CallerPluginName string
}

type RunStart struct {
	ProviderName     string
	Target           coreworkflow.Target
	DefinitionID     string
	IdempotencyKey   string
	WorkflowKey      string
	CallerPluginName string
	Permissions      []core.AccessPermission
}

type RunSignal struct {
	RunID  string
	Signal coreworkflow.Signal
}

type RunSignalOrStart struct {
	ProviderName     string
	WorkflowKey      string
	Target           coreworkflow.Target
	DefinitionID     string
	IdempotencyKey   string
	Signal           coreworkflow.Signal
	CallerPluginName string
}

type EventPublish struct {
	ProviderName string
	// PluginName is trusted owner context. Callers must derive or authorize it before
	// entering the workflow manager; the manager only normalizes and forwards it.
	PluginName string
	Event      coreworkflow.Event
}

type ManagedDefinition struct {
	ProviderName string
	Definition   *coreworkflow.ExecutionReference
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

func (m *Manager) CreateDefinition(ctx context.Context, p *principal.Principal, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionCreate)
	audit.setCallerPlugin(req.CallerPluginName)
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, "")
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	definitionID := newDefinitionID("")
	if idempotencyKey != "" {
		definitionID = newDefinitionID(workflowCreateIdempotencyScope(p, req.CallerPluginName, idempotencyKey))
		existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
		if err == nil {
			if !managedDefinitionMatchesUpsert(existing, providerName, target) {
				return nil, fmt.Errorf("%w: workflow definition idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			audit.setObjectTarget(workflowAuditTargetDefinition, existing.Definition.ID, "")
			return existing, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	ref, err := m.putExecutionRefWithPermissions(ctx, definitionID, providerName, provider, target, p, req.CallerPluginName, "", req.Permissions)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: providerName,
		Definition:   ref,
		provider:     provider,
	}, nil
}

func (m *Manager) GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*ManagedDefinition, error) {
	return m.requireOwnedDefinition(ctx, definitionID, p)
}

func (m *Manager) UpdateDefinition(ctx context.Context, p *principal.Principal, definitionID string, req DefinitionUpsert) (out *ManagedDefinition, err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionUpdate)
	audit.setCallerPlugin(req.CallerPluginName)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	defer func() {
		if out != nil && out.Definition != nil {
			audit.setProvider(out.ProviderName)
			audit.setObjectTarget(workflowAuditTargetDefinition, out.Definition.ID, "")
			audit.setWorkflowTarget(out.Definition.Target)
		}
		audit.finish(ctx, err)
	}()
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
	if err != nil {
		return nil, err
	}
	audit.setProvider(existing.ProviderName)
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	if strings.TrimSpace(existing.ProviderName) != providerName {
		if _, err := m.revokeExecutionRefWithError(ctx, existing.Definition); err != nil {
			return nil, err
		}
		ref, err := m.putExecutionRefWithPermissions(ctx, strings.TrimSpace(definitionID), providerName, provider, target, p, req.CallerPluginName, "", req.Permissions)
		if err != nil {
			m.restoreExecutionRef(ctx, existing.Definition)
			return nil, err
		}
		return &ManagedDefinition{
			ProviderName: providerName,
			Definition:   ref,
			provider:     provider,
		}, nil
	}
	ref, err := m.putExecutionRefWithPermissions(ctx, strings.TrimSpace(definitionID), providerName, provider, target, p, req.CallerPluginName, "", req.Permissions)
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: providerName,
		Definition:   ref,
		provider:     provider,
	}, nil
}

func (m *Manager) DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) (err error) {
	p = principal.Canonicalized(p)
	ctx, audit := m.beginWorkflowAudit(ctx, p, workflowAuditOperationDefinitionDelete)
	audit.setObjectTarget(workflowAuditTargetDefinition, definitionID, "")
	defer func() {
		audit.finish(ctx, err)
	}()
	existing, err := m.requireOwnedDefinition(ctx, definitionID, p)
	if err != nil {
		return err
	}
	audit.setProvider(existing.ProviderName)
	if existing.Definition != nil {
		audit.setWorkflowTarget(existing.Definition.Target)
	}
	m.revokeExecutionRef(ctx, existing.Definition)
	return nil
}

func managedDefinitionMatchesUpsert(existing *ManagedDefinition, providerName string, target coreworkflow.Target) bool {
	if existing == nil || existing.Definition == nil {
		return false
	}
	if strings.TrimSpace(existing.ProviderName) != strings.TrimSpace(providerName) {
		return false
	}
	return coreworkflow.TargetsEqual(existing.Definition.Target, target)
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
	providerName, provider, target, err := m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)

	executionRefID := newRunExecutionRefID(workflowCreateIdempotencyScope(p, req.CallerPluginName, req.IdempotencyKey), req.WorkflowKey)
	ref, createdRef, err := m.putRunExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName, req.DefinitionID, req.Permissions)
	if err != nil {
		return nil, err
	}
	run, err := provider.StartRun(ctx, coreworkflow.StartRunRequest{
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		WorkflowKey:    strings.TrimSpace(req.WorkflowKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		ExecutionRef:   executionRefID,
	})
	if err != nil {
		if createdRef {
			m.revokeExecutionRef(ctx, ref)
		}
		return nil, err
	}
	if !runMatchesExecutionRef(providerName, run, ref) || strings.TrimSpace(ref.ID) != strings.TrimSpace(run.ExecutionRef) {
		if createdRef {
			m.revokeExecutionRef(ctx, ref)
		}
		return nil, core.ErrNotFound
	}
	return &ManagedRun{
		ProviderName: providerName,
		Run:          run,
		ExecutionRef: ref,
		provider:     provider,
	}, nil
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
	providerName := ""
	var target coreworkflow.Target
	executionRefID := ""
	var targetAuthFailure *targetAuthorizationFailure
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
		if targetAuthFailure != nil {
			audit.setWorkflowTargetAuthorizationFailure(target, *targetAuthFailure)
		}
		audit.finish(ctx, err)
		if err != nil {
			logWorkflowSignalOrStartFailure(ctx, req, phase, targetAuthFailure, err)
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
	phase = "resolve_provider_target"
	var provider coreworkflow.Provider
	providerName, provider, target, err = m.resolveRequestProviderTarget(ctx, p, req.ProviderName, req.Target, req.DefinitionID, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	audit.setProvider(providerName)
	audit.setWorkflowTarget(target)
	phase = "normalize_signal"
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}

	phase = "authorize_target"
	executionRefPermissions := m.executionRefPermissions(p, target, req.CallerPluginName)
	targetAuth := m.checkTargetAuthorization(ctx, executionRefPrincipal(p, executionRefPermissions), target)
	if !targetAuth.allowed {
		targetAuthFailure = &targetAuth.failure
		return nil, core.ErrNotFound
	}
	phase = "derive_execution_ref"
	executionRefID, err = signalOrStartExecutionRefID(providerName, workflowKey, target, p, req.CallerPluginName, executionRefPermissions)
	if err != nil {
		return nil, err
	}
	phase = "put_execution_ref"
	ref, err := m.putSignalOrStartExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName, req.DefinitionID, executionRefPermissions)
	if err != nil {
		return nil, err
	}
	phase = "provider_signal_or_start"
	resp, err := provider.SignalOrStartRun(ctx, coreworkflow.SignalOrStartRunRequest{
		WorkflowKey:    workflowKey,
		Target:         target,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		CreatedBy:      workflowActorFromPrincipal(p),
		ExecutionRef:   executionRefID,
		Signal:         signal,
	})
	if err != nil {
		return nil, err
	}
	phase = "bind_response"
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
		return "workflow_execution_refs_not_configured"
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

func (m *Manager) PublishEvent(ctx context.Context, p *principal.Principal, req EventPublish) (out coreworkflow.Event, err error) {
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
		return coreworkflow.Event{}, ErrWorkflowSubjectRequired
	}
	if m == nil || m.workflow == nil {
		return coreworkflow.Event{}, ErrWorkflowNotConfigured
	}

	providerSelection := strings.TrimSpace(req.ProviderName)
	pluginName := strings.TrimSpace(req.PluginName)
	event := req.Event
	event = normalizePublishedEvent(event, m.now())
	if strings.TrimSpace(event.Type) == "" {
		return coreworkflow.Event{}, ErrWorkflowEventTypeRequired
	}
	publishedBy := workflowActorFromPrincipal(p)

	if providerSelection != "" {
		providerName, provider, err := m.resolveProviderSelection(providerSelection)
		if err != nil {
			return coreworkflow.Event{}, err
		}
		audit.setProvider(providerName)
		if err := provider.PublishEvent(ctx, coreworkflow.PublishEventRequest{
			PluginName:  pluginName,
			Event:       event,
			PublishedBy: publishedBy,
		}); err != nil {
			return coreworkflow.Event{}, err
		}
		return event, nil
	}

	providerNames := m.workflow.ProviderNames()
	if len(providerNames) > 0 {
		finishAudit = false
	}
	for _, providerName := range providerNames {
		providerAudit := audit.clone()
		providerAudit.setProvider(providerName)
		providerAudit.setObjectTarget(workflowAuditTargetEvent, "", event.Type)
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			providerAudit.finish(ctx, err)
			return coreworkflow.Event{}, err
		}
		if err := provider.PublishEvent(ctx, coreworkflow.PublishEventRequest{
			PluginName:  pluginName,
			Event:       event,
			PublishedBy: publishedBy,
		}); err != nil {
			providerAudit.finish(ctx, err)
			return coreworkflow.Event{}, err
		}
		providerAudit.finish(ctx, nil)
	}
	return event, nil
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
		schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
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
				if !managedScheduleMatchesDefinitionUpsert(existing, req) {
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
	executionRefID := newScheduleExecutionRefID(scheduleID, idempotencyScope)
	ref, err := m.putExecutionRefWithPermissions(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName, scheduleUpsertSourceDefinitionID(req), req.Permissions)
	if err != nil {
		return nil, err
	}
	schedule, err := provider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   scheduleID,
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	return &ManagedSchedule{
		ProviderName: providerName,
		Schedule:     schedule,
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

func managedScheduleMatchesDefinitionUpsert(existing *ManagedSchedule, req ScheduleUpsert) bool {
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

	executionRefID := scheduleExecutionRefID(strings.TrimSpace(existing.Schedule.ID))
	nextRef, err := m.putExecutionRefWithPermissions(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerPluginName, scheduleUpsertSourceDefinitionID(req), req.Permissions)
	if err != nil {
		return nil, err
	}
	schedule, err := nextProvider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:   strings.TrimSpace(existing.Schedule.ID),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, nextRef)
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingProvider(existing).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
			ScheduleID: strings.TrimSpace(existing.Schedule.ID),
		}); err != nil {
			_ = nextProvider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
				ScheduleID: strings.TrimSpace(existing.Schedule.ID),
			})
			m.revokeExecutionRef(ctx, nextRef)
			return nil, err
		}
	}
	if existing.ExecutionRef != nil && existing.ExecutionRef.ID != "" && existing.ExecutionRef.ID != executionRefID {
		m.revokeExecutionRef(ctx, existing.ExecutionRef)
	}
	return &ManagedSchedule{
		ProviderName: nextProviderName,
		Schedule:     schedule,
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
	if err := existingProvider(value).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
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
	schedule, err := existingProvider(value).PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
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
	schedule, err := existingProvider(value).ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Schedule = schedule
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
		trigger, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: triggerID})
		if err != nil {
			if isWorkflowProviderNotFound(err) {
				continue
			}
			return nil, err
		}
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
				if !managedEventTriggerMatchesDefinitionUpsert(existing, match, req) {
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
	executionRefID := newEventTriggerExecutionRefID(triggerID, idempotencyScope)
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName, req.DefinitionID)
	if err != nil {
		return nil, err
	}
	trigger, err := provider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
		TriggerID:    triggerID,
		Match:        match,
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	return &ManagedEventTrigger{
		ProviderName: providerName,
		Trigger:      trigger,
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

func managedEventTriggerMatchesDefinitionUpsert(existing *ManagedEventTrigger, match coreworkflow.EventMatch, req EventTriggerUpsert) bool {
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

	executionRefID := eventTriggerExecutionRefID(strings.TrimSpace(existing.Trigger.ID))
	nextRef, err := m.putExecutionRef(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerPluginName, req.DefinitionID)
	if err != nil {
		return nil, err
	}
	trigger, err := nextProvider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
		TriggerID:    strings.TrimSpace(existing.Trigger.ID),
		Match:        match,
		Target:       target,
		Paused:       req.Paused,
		RequestedBy:  workflowActorFromPrincipal(p),
		ExecutionRef: executionRefID,
	})
	if err != nil {
		m.revokeExecutionRef(ctx, nextRef)
		return nil, err
	}
	if strings.TrimSpace(existing.ProviderName) != nextProviderName {
		if err := existingEventTriggerProvider(existing).DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
			TriggerID: strings.TrimSpace(existing.Trigger.ID),
		}); err != nil {
			_ = nextProvider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
				TriggerID: strings.TrimSpace(existing.Trigger.ID),
			})
			m.revokeExecutionRef(ctx, nextRef)
			return nil, err
		}
	}
	if existing.ExecutionRef != nil && existing.ExecutionRef.ID != "" && existing.ExecutionRef.ID != executionRefID {
		m.revokeExecutionRef(ctx, existing.ExecutionRef)
	}
	return &ManagedEventTrigger{
		ProviderName: nextProviderName,
		Trigger:      trigger,
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
	if err := existingEventTriggerProvider(value).DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
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
	trigger, err := existingEventTriggerProvider(value).PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = trigger
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
	trigger, err := existingEventTriggerProvider(value).ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	})
	if err != nil {
		return nil, err
	}
	value.Trigger = trigger
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
	definition, err := m.requireOwnedDefinition(ctx, definitionID, p)
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
	resolvedTarget, err := m.resolveTarget(ctx, p, definition.Definition.Target, callerPluginName)
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
	out := coreworkflow.Target{Steps: make([]coreworkflow.Step, 0, len(target.Steps))}
	seen := map[string]struct{}{}
	for i := range target.Steps {
		step := target.Steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if step.ID == "" {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id is required", i)
		}
		if _, exists := seen[step.ID]; exists {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].id duplicates %q", i, step.ID)
		}
		if step.TimeoutSeconds < 0 {
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].timeout_seconds must not be negative", i)
		}
		if err := validateWorkflowStepValueMapRefs(fmt.Sprintf("workflow target.steps[%d].inputs", i), step.Inputs, seen); err != nil {
			return coreworkflow.Target{}, err
		}
		switch {
		case step.Plugin != nil && step.Agent != nil:
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d] must set exactly one of plugin or agent", i)
		case step.Plugin != nil:
			plugin, err := m.resolveWorkflowStepPlugin(ctx, p, *step.Plugin, callerPluginName)
			if err != nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].plugin: %w", i, err)
			}
			if err := validateWorkflowStepValueRefs(fmt.Sprintf("workflow target.steps[%d].plugin.input", i), plugin.Input, seen); err != nil {
				return coreworkflow.Target{}, err
			}
			step.Plugin = &plugin
		case step.Agent != nil:
			agent, err := m.resolveWorkflowStepAgent(ctx, p, *step.Agent)
			if err != nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].agent: %w", i, err)
			}
			step.Agent = &agent
		default:
			return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d] must set plugin or agent", i)
		}
		if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil {
			plugin, err := m.resolveWorkflowStepPlugin(ctx, p, *step.OutputDelivery.Plugin, callerPluginName)
			if err != nil {
				return coreworkflow.Target{}, fmt.Errorf("workflow target.steps[%d].output_delivery.plugin: %w", i, err)
			}
			if err := validateWorkflowStepValueRefs(fmt.Sprintf("workflow target.steps[%d].output_delivery.plugin.input", i), plugin.Input, workflowStepSeenWithCurrent(seen, step.ID)); err != nil {
				return coreworkflow.Target{}, err
			}
			step.OutputDelivery.Plugin = &plugin
		}
		if step.When != nil {
			if err := validateWorkflowStepWhen(i, step.When, seen); err != nil {
				return coreworkflow.Target{}, err
			}
		}
		seen[step.ID] = struct{}{}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

func validateWorkflowStepWhen(index int, when *coreworkflow.StepWhen, previousSteps map[string]struct{}) error {
	if when == nil {
		return nil
	}
	if !workflowValueIsSet(when.Value) {
		return fmt.Errorf("workflow target.steps[%d].when.value is required", index)
	}
	if err := validateWorkflowStepWhenValue(index, when.Value, previousSteps); err != nil {
		return err
	}
	if !when.EqualsSet {
		return fmt.Errorf("workflow target.steps[%d].when.equals is required", index)
	}
	if !jsonvalue.IsScalar(when.Equals) {
		return fmt.Errorf("workflow target.steps[%d].when.equals must be a scalar JSON value", index)
	}
	return nil
}

func workflowValueIsSet(value coreworkflow.Value) bool {
	switch {
	case value.LiteralSet:
		return true
	case value.Object != nil:
		return true
	case value.Array != nil:
		return true
	case value.Template != nil:
		return true
	case strings.TrimSpace(value.RunInput) != "":
		return true
	case strings.TrimSpace(value.SignalPayload) != "":
		return true
	case value.StepOutput != nil:
		return true
	default:
		return false
	}
}

func validateWorkflowStepValueMapRefs(path string, values map[string]coreworkflow.Value, previousSteps map[string]struct{}) error {
	for key := range values {
		if err := validateWorkflowStepValueRefs(path+"."+key, values[key], previousSteps); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowStepValueRefs(path string, value coreworkflow.Value, previousSteps map[string]struct{}) error {
	if value.StepOutput != nil {
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		if stepID == "" {
			return fmt.Errorf("%s.step_output.step_id is required", path)
		}
		if _, ok := previousSteps[stepID]; !ok {
			return fmt.Errorf("%s.step_output.step_id %q must reference an earlier step", path, stepID)
		}
		if strings.TrimSpace(value.StepOutput.Path) == "" {
			return fmt.Errorf("%s.step_output.path is required", path)
		}
	}
	for key := range value.Object {
		if err := validateWorkflowStepValueRefs(path+"."+key, value.Object[key], previousSteps); err != nil {
			return err
		}
	}
	for i := range value.Array {
		if err := validateWorkflowStepValueRefs(fmt.Sprintf("%s[%d]", path, i), value.Array[i], previousSteps); err != nil {
			return err
		}
	}
	return nil
}

func workflowStepSeenWithCurrent(seen map[string]struct{}, stepID string) map[string]struct{} {
	out := make(map[string]struct{}, len(seen)+1)
	for key := range seen {
		out[key] = struct{}{}
	}
	out[strings.TrimSpace(stepID)] = struct{}{}
	return out
}

func validateWorkflowStepWhenValue(index int, value coreworkflow.Value, previousSteps map[string]struct{}) error {
	return validateWorkflowStepValueRefs(fmt.Sprintf("workflow target.steps[%d].when.value", index), value, previousSteps)
}

func (m *Manager) resolveWorkflowStepPlugin(ctx context.Context, p *principal.Principal, target coreworkflow.PluginCall, callerPluginName string) (coreworkflow.PluginCall, error) {
	pluginName := strings.TrimSpace(target.Name)
	if pluginName == "" {
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: workflow target plugin is required", invocation.ErrInvalidInvocation)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrInvalidInvocation)
	}
	credentialMode, err := m.normalizeWorkflowPluginTargetCredentialMode(target.CredentialMode, callerPluginName, pluginName, operation)
	if err != nil {
		return coreworkflow.PluginCall{}, err
	}
	if m == nil || m.providers == nil {
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: workflow providers are not configured", invocation.ErrInternal)
	}
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreworkflow.PluginCall{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return coreworkflow.PluginCall{}, invocation.ErrAuthorizationDenied
	}
	if credentialMode != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, credentialMode)
	}

	connection := strings.TrimSpace(target.Connection)
	if connection != "" && !core.SafeConnectionValue(connection) {
		return coreworkflow.PluginCall{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = core.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(target.Instance)
	if instance != "" && !core.SafeInstanceValue(instance) {
		return coreworkflow.PluginCall{}, fmt.Errorf("instance name contains invalid characters")
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
		return coreworkflow.PluginCall{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) && !m.callerPluginDeclaresInvoke(callerPluginName, pluginName, opMeta.ID) {
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return coreworkflow.PluginCall{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, pluginName, connection, sessionInstance)
		if err != nil {
			return coreworkflow.PluginCall{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}
	return coreworkflow.PluginCall{
		Name:           pluginName,
		Operation:      opMeta.ID,
		Connection:     connection,
		Instance:       sessionInstance,
		CredentialMode: credentialMode,
		Input:          target.Input,
	}, nil
}

func (m *Manager) resolveWorkflowStepAgent(ctx context.Context, p *principal.Principal, target coreworkflow.AgentTurn) (coreworkflow.AgentTurn, error) {
	if m == nil || m.agent == nil || m.agentManager == nil {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: agent workflows are not configured", invocation.ErrInternal)
	}
	providerName, _, err := m.agent.ResolveProviderSelection(target.ProviderName)
	if err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	target.ProviderName = strings.TrimSpace(providerName)
	target.Prompt.Template = strings.TrimSpace(target.Prompt.Template)
	for i := range target.Messages {
		target.Messages[i].Role = strings.TrimSpace(target.Messages[i].Role)
		target.Messages[i].Text.Template = strings.TrimSpace(target.Messages[i].Text.Template)
	}
	if target.Prompt.Template == "" && len(target.Messages) == 0 {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: workflow target agent prompt or messages is required", invocation.ErrInvalidInvocation)
	}
	if !m.allowProvider(ctx, p, target.ProviderName) || !principal.AllowsProviderPermission(p, target.ProviderName) {
		return coreworkflow.AgentTurn{}, fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, target.ProviderName)
	}
	target.Model = strings.TrimSpace(target.Model)
	target.SessionKey = strings.TrimSpace(target.SessionKey)
	target.ToolRefs = append([]coreagent.ToolRef(nil), target.ToolRefs...)
	if err := validateWorkflowAgentToolRefs(target.ToolRefs); err != nil {
		return coreworkflow.AgentTurn{}, err
	}
	target.ResponseSchema = maps.Clone(target.ResponseSchema)
	target.ModelOptions = maps.Clone(target.ModelOptions)
	return target, nil
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

func (m *Manager) requireOwnedDefinition(ctx context.Context, definitionID string, p *principal.Principal) (*ManagedDefinition, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" || !strings.HasPrefix(definitionID, workflowDefinitionExecutionRefBasePrefix) {
		return nil, core.ErrNotFound
	}
	refs, err := m.listOwnedExecutionRefs(ctx, p, true)
	if err != nil {
		return nil, err
	}
	var match *coreworkflow.ExecutionReference
	for _, ref := range refs {
		if ref == nil || strings.TrimSpace(ref.ID) != definitionID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, definitionID)
		}
		match = ref
	}
	if match == nil || !m.allowTarget(ctx, p, match.Target) {
		return nil, core.ErrNotFound
	}
	provider, err := m.resolveProviderByName(strings.TrimSpace(match.ProviderName))
	if err != nil {
		return nil, err
	}
	return &ManagedDefinition{
		ProviderName: strings.TrimSpace(match.ProviderName),
		Definition:   match,
		provider:     provider,
	}, nil
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
	schedule, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: scheduleID})
	if err != nil {
		return nil, err
	}
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
	trigger, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: triggerID})
	if err != nil {
		return nil, err
	}
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

func (m *Manager) putExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string) (*coreworkflow.ExecutionReference, error) {
	return m.putExecutionRefWithPermissions(ctx, executionRefID, providerName, provider, target, p, callerPluginName, sourceDefinitionID, nil)
}

func (m *Manager) putExecutionRefWithPermissions(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	actor := workflowActorFromPrincipal(p)
	return store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
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
		Permissions:         m.executionRefPermissionsWithOverride(p, target, callerPluginName, permissions),
	})
}

func (m *Manager) putRunExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, bool, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, false, err
	}
	expectedPermissions := m.executionRefPermissionsWithOverride(p, target, callerPluginName, permissions)
	existing, err := store.GetExecutionReference(ctx, executionRefID)
	if err == nil {
		existing = workflowExecutionRefForProvider(existing, providerName)
		if !runExecutionRefMatches(existing, executionRefID, providerName, target, p, callerPluginName, sourceDefinitionID, expectedPermissions) {
			return nil, false, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, executionRefID)
		}
		if executionRefActive(existing) {
			return existing, false, nil
		}
	} else if !isWorkflowProviderNotFound(err) {
		return nil, false, err
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(principalSubjectID(p))
	if subjectID == "" {
		return nil, false, ErrWorkflowSubjectRequired
	}
	actor := workflowActorFromPrincipal(p)
	ref, err := store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
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
		Permissions:         expectedPermissions,
	})
	if err != nil {
		return nil, false, err
	}
	return ref, true, nil
}

func (m *Manager) executionRefPermissionsWithOverride(p *principal.Principal, target coreworkflow.Target, callerPluginName string, override []core.AccessPermission) []core.AccessPermission {
	if override == nil {
		return m.executionRefPermissions(p, target, callerPluginName)
	}
	out := principal.PermissionsToAccessPermissions(principal.CompilePermissions(override))
	if len(out) == 0 {
		return []core.AccessPermission{{Plugin: workflowNoProviderPermissionsPlugin}}
	}
	return out
}

func (m *Manager) putSignalOrStartExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName, sourceDefinitionID string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	existing, err := store.GetExecutionReference(ctx, executionRefID)
	if err == nil {
		existing = workflowExecutionRefForProvider(existing, providerName)
		if !signalOrStartExecutionRefMatches(existing, executionRefID, providerName, target, p, callerPluginName, permissions) {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateExecutionRefs, executionRefID)
		}
		if executionRefActive(existing) {
			return existing, nil
		}
	} else if !isWorkflowProviderNotFound(err) {
		return nil, err
	}

	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, callerPluginName, sourceDefinitionID)
	if err != nil {
		return nil, err
	}
	return ref, nil
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
	out := principal.PermissionsToAccessPermissions(permissions)
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
	next.ActionPermissions = nil
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

func (m *Manager) revokeExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	_, _ = m.revokeExecutionRefWithError(ctx, ref)
}

func (m *Manager) revokeExecutionRefWithError(ctx context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if m == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return nil, nil
	}
	providerName := strings.TrimSpace(ref.ProviderName)
	provider, err := m.resolveProviderByName(providerName)
	if err != nil {
		return nil, err
	}
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return nil, err
	}
	cloned := *ref
	now := m.now().UTC().Truncate(time.Second)
	cloned.RevokedAt = &now
	return store.PutExecutionReference(ctx, &cloned)
}

func (m *Manager) restoreExecutionRef(ctx context.Context, ref *coreworkflow.ExecutionReference) {
	if m == nil || ref == nil || strings.TrimSpace(ref.ID) == "" {
		return
	}
	providerName := strings.TrimSpace(ref.ProviderName)
	provider, err := m.resolveProviderByName(providerName)
	if err != nil {
		return
	}
	store, err := workflowExecutionReferenceStore(providerName, provider)
	if err != nil {
		return
	}
	cloned := *ref
	_, _ = store.PutExecutionReference(ctx, &cloned)
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
	if len(target.Steps) == 0 {
		return targetAuthorizationDenied(targetAuthorizationComponentTarget, targetAuthorizationReasonMissingPluginTarget, "", "", -1)
	}
	for stepIndex := range target.Steps {
		step := target.Steps[stepIndex]
		if step.Plugin != nil {
			if denied := m.checkWorkflowStepPluginAuthorization(ctx, p, step.Plugin, targetAuthorizationComponentPluginTarget); !denied.allowed {
				return denied
			}
		}
		if step.Agent != nil {
			agentProviderName := strings.TrimSpace(step.Agent.ProviderName)
			if agentProviderName == "" {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonMissingAgentProvider, "", "", -1)
			}
			if !m.allowProvider(ctx, p, agentProviderName) {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonAuthorizerProviderDenied, agentProviderName, "", -1)
			}
			if !principal.AllowsProviderPermission(p, agentProviderName) {
				return targetAuthorizationDenied(targetAuthorizationComponentAgentProvider, targetAuthorizationReasonPrincipalProviderPermissionDenied, agentProviderName, "", -1)
			}
			hasSystemTools := workflowAgentToolRefsContainSystem(step.Agent.ToolRefs)
			for i := range step.Agent.ToolRefs {
				if denied := m.checkWorkflowAgentToolAuthorization(ctx, p, step.Agent.ToolRefs[i], hasSystemTools, i); !denied.allowed {
					return denied
				}
			}
		}
		if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil {
			if denied := m.checkWorkflowStepPluginAuthorization(ctx, p, step.OutputDelivery.Plugin, targetAuthorizationComponentOutputDelivery); !denied.allowed {
				return denied
			}
		}
		_ = stepIndex
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkWorkflowStepPluginAuthorization(ctx context.Context, p *principal.Principal, plugin *coreworkflow.PluginCall, component string) targetAuthorizationDecision {
	if plugin == nil {
		return targetAuthorizationAllowed()
	}
	pluginName := strings.TrimSpace(plugin.Name)
	operation := strings.TrimSpace(plugin.Operation)
	if pluginName == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingPluginProvider, "", operation, -1)
	}
	if operation == "" {
		return targetAuthorizationDenied(component, targetAuthorizationReasonMissingPluginOperation, pluginName, "", -1)
	}
	if !m.allowProvider(ctx, p, pluginName) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerProviderDenied, pluginName, operation, -1)
	}
	if !m.allowOperation(ctx, p, pluginName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonAuthorizerOperationDenied, pluginName, operation, -1)
	}
	if !principal.AllowsOperationPermission(p, pluginName, operation) {
		return targetAuthorizationDenied(component, targetAuthorizationReasonPrincipalOperationPermissionDenied, pluginName, operation, -1)
	}
	return targetAuthorizationAllowed()
}

func (m *Manager) checkWorkflowAgentToolAuthorization(ctx context.Context, p *principal.Principal, tool coreagent.ToolRef, hasSystemTools bool, index int) targetAuthorizationDecision {
	if systemName := strings.TrimSpace(tool.System); systemName != "" {
		if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(tool.Operation) == "" {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", index)
		}
		if strings.TrimSpace(tool.Plugin) != "" || strings.TrimSpace(tool.Connection) != "" || strings.TrimSpace(tool.Instance) != "" || tool.CredentialMode != "" {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonInvalidSystemToolRef, "", "", index)
		}
		return targetAuthorizationAllowed()
	}
	pluginName := strings.TrimSpace(tool.Plugin)
	operation := strings.TrimSpace(tool.Operation)
	if pluginName == "" {
		return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonMissingToolProvider, "", operation, index)
	}
	if hasSystemTools && (pluginName == "*" || operation == "") {
		return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonNonExactToolRefWithSystemTools, pluginName, operation, index)
	}
	if operation == "" {
		if !m.allowProvider(ctx, p, pluginName) {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonAuthorizerProviderDenied, pluginName, "", index)
		}
		if !principal.AllowsProviderPermission(p, pluginName) {
			return targetAuthorizationDenied(targetAuthorizationComponentAgentToolRef, targetAuthorizationReasonPrincipalProviderPermissionDenied, pluginName, "", index)
		}
		return targetAuthorizationAllowed()
	}
	return m.checkWorkflowStepPluginAuthorization(ctx, p, &coreworkflow.PluginCall{Name: pluginName, Operation: operation}, targetAuthorizationComponentAgentToolRef)
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

func runMatchesExecutionRef(providerName string, run *coreworkflow.Run, ref *coreworkflow.ExecutionReference) bool {
	if run == nil || ref == nil {
		return false
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && strings.TrimSpace(ref.ProviderName) != providerName {
		return false
	}
	return targetMatchesExecutionRef(run.Target, ref)
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

func newDefinitionID(idempotencyScope string) string {
	idempotencyScope = strings.TrimSpace(idempotencyScope)
	if idempotencyScope == "" {
		return workflowDefinitionExecutionRefBasePrefix + uuid.NewString()
	}
	return workflowDefinitionExecutionRefBasePrefix + uuid.NewSHA1(uuid.NameSpaceURL, []byte("gestalt.workflow.definition:"+idempotencyScope)).String()
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
