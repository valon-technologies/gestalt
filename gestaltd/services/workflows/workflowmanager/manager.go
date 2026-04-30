package workflowmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/workflowprincipal"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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
const workflowNoProviderPermissionsPlugin = "__gestalt.workflow.no_provider_permissions__"
const defaultWorkflowEventSpecVersion = "1.0"

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
	ListRuns(ctx context.Context, p *principal.Principal) ([]*ManagedRun, error)
	StartRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error)
	GetRun(ctx context.Context, p *principal.Principal, runID string) (*ManagedRun, error)
	CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*ManagedRun, error)
	SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (*ManagedRunSignal, error)
	SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (*ManagedRunSignal, error)
	PublishEvent(ctx context.Context, p *principal.Principal, event coreworkflow.Event) (coreworkflow.Event, error)
}

type Config struct {
	Providers         *registry.ProviderMap[core.Provider]
	Workflow          WorkflowControl
	Agent             AgentControl
	AgentManager      agentmanager.Service
	Invoker           invocation.Invoker
	Authorizer        authorization.RuntimeAuthorizer
	DefaultConnection map[string]string
	CatalogConnection map[string]string
	PluginInvokes     map[string][]config.PluginInvocationDependency
	Now               func() time.Time
}

type Manager struct {
	providers         *registry.ProviderMap[core.Provider]
	workflow          WorkflowControl
	agent             AgentControl
	agentManager      agentmanager.Service
	invoker           invocation.Invoker
	authorizer        authorization.RuntimeAuthorizer
	defaultConnection map[string]string
	catalogConnection map[string]string
	pluginInvokes     map[string][]config.PluginInvocationDependency
	now               func() time.Time
}

type ScheduleUpsert struct {
	ProviderName     string
	Cron             string
	Timezone         string
	Target           coreworkflow.Target
	Paused           bool
	IdempotencyKey   string
	CallerPluginName string
}

type EventTriggerUpsert struct {
	ProviderName     string
	Match            coreworkflow.EventMatch
	Target           coreworkflow.Target
	Paused           bool
	IdempotencyKey   string
	CallerPluginName string
}

type RunStart struct {
	ProviderName     string
	Target           coreworkflow.Target
	IdempotencyKey   string
	WorkflowKey      string
	CallerPluginName string
}

type RunSignal struct {
	RunID  string
	Signal coreworkflow.Signal
}

type RunSignalOrStart struct {
	ProviderName     string
	WorkflowKey      string
	Target           coreworkflow.Target
	IdempotencyKey   string
	Signal           coreworkflow.Signal
	CallerPluginName string
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
		workflow:          cfg.Workflow,
		agent:             cfg.Agent,
		agentManager:      cfg.AgentManager,
		invoker:           cfg.Invoker,
		authorizer:        cfg.Authorizer,
		defaultConnection: maps.Clone(cfg.DefaultConnection),
		catalogConnection: maps.Clone(cfg.CatalogConnection),
		pluginInvokes:     pluginInvokes,
		now:               now,
	}
}

func (m *Manager) ListRuns(ctx context.Context, p *principal.Principal) ([]*ManagedRun, error) {
	refs, err := m.listOwnedExecutionRefs(ctx, p, false)
	if err != nil {
		return nil, err
	}
	refsByProvider := executionRefsByProvider(refs)
	out := make([]*ManagedRun, 0, len(refs))
	for providerName, providerRefs := range refsByProvider {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return nil, err
		}
		runs, err := provider.ListRuns(ctx, coreworkflow.ListRunsRequest{})
		if err != nil {
			return nil, err
		}
		refIndex := executionRefsByID(providerRefs)
		for _, run := range runs {
			if run == nil {
				continue
			}
			ref := refIndex[strings.TrimSpace(run.ExecutionRef)]
			if ref == nil || !m.allowTarget(ctx, p, ref.Target) || !runMatchesExecutionRef(providerName, run, ref) {
				continue
			}
			out = append(out, &ManagedRun{
				ProviderName: providerName,
				Run:          run,
				ExecutionRef: ref,
				provider:     provider,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.Run != nil && right.Run != nil && left.Run.CreatedAt != nil && right.Run.CreatedAt != nil && !left.Run.CreatedAt.Equal(*right.Run.CreatedAt) {
			return left.Run.CreatedAt.After(*right.Run.CreatedAt)
		}
		leftID := ""
		rightID := ""
		if left.Run != nil {
			leftID = left.Run.ID
		}
		if right.Run != nil {
			rightID = right.Run.ID
		}
		return leftID < rightID
	})
	return out, nil
}

func (m *Manager) StartRun(ctx context.Context, p *principal.Principal, req RunStart) (*ManagedRun, error) {
	p = principal.Canonicalized(p)
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

	executionRefID := runExecutionRefID(uuid.NewString())
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName)
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
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	if !runMatchesExecutionRef(providerName, run, ref) {
		m.revokeExecutionRef(ctx, ref)
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

func (m *Manager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*ManagedRun, error) {
	value, err := m.GetRun(ctx, p, runID)
	if err != nil {
		return nil, err
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

func (m *Manager) SignalRun(ctx context.Context, p *principal.Principal, req RunSignal) (*ManagedRunSignal, error) {
	value, err := m.GetRun(ctx, p, req.RunID)
	if err != nil {
		return nil, err
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

func (m *Manager) SignalOrStartRun(ctx context.Context, p *principal.Principal, req RunSignalOrStart) (*ManagedRunSignal, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	workflowKey := strings.TrimSpace(req.WorkflowKey)
	if workflowKey == "" {
		return nil, ErrWorkflowKeyRequired
	}
	providerName, provider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}

	executionRefPermissions := m.executionRefPermissions(p, target, req.CallerPluginName)
	if !m.allowTarget(ctx, executionRefPrincipal(p, executionRefPermissions), target) {
		return nil, core.ErrNotFound
	}
	executionRefID, err := signalOrStartExecutionRefID(providerName, workflowKey, target, p, req.CallerPluginName, executionRefPermissions)
	if err != nil {
		return nil, err
	}
	ref, err := m.putSignalOrStartExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName, executionRefPermissions)
	if err != nil {
		return nil, err
	}
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
	return m.managedSignalResponse(ctx, p, providerName, provider, resp, ref, signalTargetPrincipalExecutionRef)
}

func (m *Manager) PublishEvent(ctx context.Context, p *principal.Principal, event coreworkflow.Event) (coreworkflow.Event, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return coreworkflow.Event{}, ErrWorkflowSubjectRequired
	}
	if m == nil || m.workflow == nil {
		return coreworkflow.Event{}, ErrWorkflowNotConfigured
	}

	event = normalizePublishedEvent(event, m.now())
	if strings.TrimSpace(event.Type) == "" {
		return coreworkflow.Event{}, ErrWorkflowEventTypeRequired
	}
	publishedBy := workflowActorFromPrincipal(p)

	providerNames := m.workflow.ProviderNames()
	for _, providerName := range providerNames {
		provider, err := m.resolveProviderByName(providerName)
		if err != nil {
			return coreworkflow.Event{}, err
		}
		if err := provider.PublishEvent(ctx, coreworkflow.PublishEventRequest{
			Event:       event,
			PublishedBy: publishedBy,
		}); err != nil {
			return coreworkflow.Event{}, err
		}
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

func (m *Manager) CreateSchedule(ctx context.Context, p *principal.Principal, req ScheduleUpsert) (*ManagedSchedule, error) {
	p = principal.Canonicalized(p)
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

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerPluginName, idempotencyKey)
	scheduleID := newScheduleID(idempotencyScope)
	if idempotencyKey != "" {
		existing, err := m.requireOwnedSchedule(ctx, scheduleID, p)
		if err == nil {
			if !managedScheduleMatchesUpsert(existing, providerName, target, req) {
				return nil, fmt.Errorf("%w: workflow schedule idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			return existing, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	executionRefID := newScheduleExecutionRefID(scheduleID, idempotencyScope)
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName)
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

func (m *Manager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	return m.requireOwnedSchedule(ctx, scheduleID, p)
}

func (m *Manager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req ScheduleUpsert) (*ManagedSchedule, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
	}
	nextProviderName, nextProvider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}

	executionRefID := scheduleExecutionRefID(strings.TrimSpace(existing.Schedule.ID))
	nextRef, err := m.putExecutionRef(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerPluginName)
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

func (m *Manager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) error {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return err
	}
	if err := existingProvider(value).DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		ScheduleID: strings.TrimSpace(value.Schedule.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
	return nil
}

func (m *Manager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
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

func (m *Manager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*ManagedSchedule, error) {
	value, err := m.requireOwnedSchedule(ctx, scheduleID, p)
	if err != nil {
		return nil, err
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

func (m *Manager) CreateEventTrigger(ctx context.Context, p *principal.Principal, req EventTriggerUpsert) (*ManagedEventTrigger, error) {
	p = principal.Canonicalized(p)
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
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyScope := workflowCreateIdempotencyScope(p, req.CallerPluginName, idempotencyKey)
	triggerID := newEventTriggerID(idempotencyScope)
	if idempotencyKey != "" {
		existing, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
		if err == nil {
			if !managedEventTriggerMatchesUpsert(existing, providerName, target, match, req) {
				return nil, fmt.Errorf("%w: workflow trigger idempotency key reused with different request", invocation.ErrInvalidInvocation)
			}
			return existing, nil
		}
		if !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	executionRefID := newEventTriggerExecutionRefID(triggerID, idempotencyScope)
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName)
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

func (m *Manager) GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	return m.requireOwnedEventTrigger(ctx, triggerID, p)
}

func (m *Manager) UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req EventTriggerUpsert) (*ManagedEventTrigger, error) {
	p = principal.Canonicalized(p)
	if strings.TrimSpace(principalSubjectID(p)) == "" {
		return nil, ErrWorkflowSubjectRequired
	}
	existing, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
	}
	nextProviderName, nextProvider, err := m.resolveProviderSelection(strings.TrimSpace(req.ProviderName))
	if err != nil {
		return nil, err
	}
	target, err := m.resolveTarget(ctx, p, req.Target, req.CallerPluginName)
	if err != nil {
		return nil, err
	}
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	executionRefID := eventTriggerExecutionRefID(strings.TrimSpace(existing.Trigger.ID))
	nextRef, err := m.putExecutionRef(ctx, executionRefID, nextProviderName, nextProvider, target, p, req.CallerPluginName)
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

func (m *Manager) DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) error {
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return err
	}
	if err := existingEventTriggerProvider(value).DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
		TriggerID: strings.TrimSpace(value.Trigger.ID),
	}); err != nil {
		return err
	}
	m.revokeExecutionRef(ctx, value.ExecutionRef)
	return nil
}

func (m *Manager) PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
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

func (m *Manager) ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*ManagedEventTrigger, error) {
	value, err := m.requireOwnedEventTrigger(ctx, triggerID, p)
	if err != nil {
		return nil, err
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

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target, callerPluginName string) (coreworkflow.Target, error) {
	hasPlugin := target.Plugin != nil
	hasAgent := target.Agent != nil
	if hasAgent && hasPlugin {
		return coreworkflow.Target{}, fmt.Errorf("workflow target must set exactly one of plugin or agent")
	}
	if hasAgent {
		return m.resolveAgentTarget(ctx, p, *target.Agent, callerPluginName)
	}
	pluginTarget := coreworkflow.PluginTarget{}
	if target.Plugin != nil {
		pluginTarget = *target.Plugin
	}
	return m.resolvePluginTarget(ctx, p, pluginTarget)
}

func (m *Manager) resolvePluginTarget(ctx context.Context, p *principal.Principal, target coreworkflow.PluginTarget) (coreworkflow.Target, error) {
	if m == nil || m.providers == nil {
		return coreworkflow.Target{}, fmt.Errorf("%w: workflow providers are not configured", invocation.ErrInternal)
	}
	pluginName := strings.TrimSpace(target.PluginName)
	if pluginName == "" {
		return coreworkflow.Target{}, fmt.Errorf("%w: workflow target plugin is required", invocation.ErrProviderNotFound)
	}
	prov, err := m.providers.Get(pluginName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return coreworkflow.Target{}, fmt.Errorf("%w: %q", invocation.ErrProviderNotFound, pluginName)
		}
		return coreworkflow.Target{}, fmt.Errorf("%w: looking up provider: %v", invocation.ErrInternal, err)
	}
	operation := strings.TrimSpace(target.Operation)
	if operation == "" {
		return coreworkflow.Target{}, fmt.Errorf("%w: workflow target operation is required", invocation.ErrOperationNotFound)
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return coreworkflow.Target{}, invocation.ErrAuthorizationDenied
	}

	connection := strings.TrimSpace(target.Connection)
	if connection != "" && !config.SafeConnectionValue(connection) {
		return coreworkflow.Target{}, fmt.Errorf("connection name contains invalid characters")
	}
	connection = config.ResolveConnectionAlias(connection)
	instance := strings.TrimSpace(target.Instance)
	if instance != "" && !config.SafeInstanceValue(instance) {
		return coreworkflow.Target{}, fmt.Errorf("instance name contains invalid characters")
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
		return coreworkflow.Target{}, err
	}
	if !principal.AllowsOperationPermission(p, pluginName, opMeta.ID) {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if m.authorizer != nil && !m.authorizer.AllowCatalogOperation(ctx, p, pluginName, opMeta) {
		return coreworkflow.Target{}, fmt.Errorf("%w: %s.%s", invocation.ErrAuthorizationDenied, pluginName, opMeta.ID)
	}
	if connection == "" {
		connection = resolvedConnection
	}
	if resolver != nil && sessionInstance == "" {
		resolvedCtx, _, err := resolver.ResolveToken(ctx, p, pluginName, connection, sessionInstance)
		if err != nil {
			return coreworkflow.Target{}, err
		}
		cred := invocation.CredentialContextFromContext(resolvedCtx)
		if cred.Connection != "" {
			connection = cred.Connection
		}
		if cred.Instance != "" {
			sessionInstance = cred.Instance
		}
	}
	pluginTarget := coreworkflow.PluginTarget{
		PluginName: pluginName,
		Operation:  opMeta.ID,
		Connection: connection,
		Instance:   sessionInstance,
		Input:      maps.Clone(target.Input),
	}
	return coreworkflow.Target{
		Plugin: &pluginTarget,
	}, nil
}

func (m *Manager) resolveAgentTarget(ctx context.Context, p *principal.Principal, target coreworkflow.AgentTarget, callerPluginName string) (coreworkflow.Target, error) {
	if m == nil || m.agent == nil || m.agentManager == nil {
		return coreworkflow.Target{}, fmt.Errorf("%w: agent workflows are not configured", invocation.ErrInternal)
	}
	providerName, _, err := m.agent.ResolveProviderSelection(target.ProviderName)
	if err != nil {
		return coreworkflow.Target{}, err
	}
	target.ProviderName = strings.TrimSpace(providerName)
	target.Model = strings.TrimSpace(target.Model)
	target.Prompt = strings.TrimSpace(target.Prompt)
	if strings.TrimSpace(target.Prompt) == "" && len(target.Messages) == 0 {
		return coreworkflow.Target{}, fmt.Errorf("workflow agent target prompt or messages is required")
	}
	if target.TimeoutSeconds < 0 {
		return coreworkflow.Target{}, fmt.Errorf("workflow agent target timeout_seconds must not be negative")
	}
	target.ResponseSchema = maps.Clone(target.ResponseSchema)
	target.ProviderOptions = maps.Clone(target.ProviderOptions)
	target.Metadata = maps.Clone(target.Metadata)
	target.Messages = append([]coreagent.Message(nil), target.Messages...)
	target.ToolRefs = append([]coreagent.ToolRef(nil), target.ToolRefs...)
	target.OutputDelivery = cloneWorkflowOutputDelivery(target.OutputDelivery)
	if err := validateWorkflowAgentToolRefs(target.ToolRefs); err != nil {
		return coreworkflow.Target{}, err
	}
	if err := m.normalizeWorkflowOutputDelivery(target.OutputDelivery, callerPluginName); err != nil {
		return coreworkflow.Target{}, err
	}
	return coreworkflow.Target{Agent: &target}, nil
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

func (m *Manager) putExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName string) (*coreworkflow.ExecutionReference, error) {
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
		SubjectID:           subjectID,
		SubjectKind:         actor.SubjectKind,
		DisplayName:         actor.DisplayName,
		AuthSource:          actor.AuthSource,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		Permissions:         m.executionRefPermissions(p, target, callerPluginName),
	})
}

func (m *Manager) putSignalOrStartExecutionRef(ctx context.Context, executionRefID, providerName string, provider coreworkflow.Provider, target coreworkflow.Target, p *principal.Principal, callerPluginName string, permissions []core.AccessPermission) (*coreworkflow.ExecutionReference, error) {
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

	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, callerPluginName)
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
	permissions := cloneWorkflowPermissionSet(p.TokenPermissions)
	if target.Agent != nil {
		for i := range target.Agent.ToolRefs {
			tool := target.Agent.ToolRefs[i]
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" || pluginName == "*" || operation == "" {
				continue
			}
			if m.callerPluginDeclaresInvoke(callerPluginName, pluginName, operation) {
				addWorkflowPermission(permissions, pluginName, operation)
			}
		}
		if pluginName, operation, ok := workflowOutputDeliveryOperation(target.Agent.OutputDelivery); ok && m.callerPluginDeclaresInvoke(callerPluginName, pluginName, operation) {
			addWorkflowPermission(permissions, pluginName, operation)
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

func cloneWorkflowPermissionSet(src principal.PermissionSet) principal.PermissionSet {
	if src == nil {
		return nil
	}
	out := make(principal.PermissionSet, len(src))
	for pluginName, operations := range src {
		if operations == nil {
			out[pluginName] = nil
			continue
		}
		copied := make(map[string]struct{}, len(operations))
		for operation := range operations {
			copied[operation] = struct{}{}
		}
		out[pluginName] = copied
	}
	return out
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
	now := m.now().UTC().Truncate(time.Second)
	cloned.RevokedAt = &now
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

func (m *Manager) allowTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) bool {
	if target.Agent != nil {
		hasSystemTools := workflowAgentToolRefsContainSystem(target.Agent.ToolRefs)
		for i := range target.Agent.ToolRefs {
			tool := target.Agent.ToolRefs[i]
			if systemName := strings.TrimSpace(tool.System); systemName != "" {
				if systemName != coreagent.SystemToolWorkflow || strings.TrimSpace(tool.Operation) == "" {
					return false
				}
				if strings.TrimSpace(tool.Plugin) != "" || strings.TrimSpace(tool.Connection) != "" || strings.TrimSpace(tool.Instance) != "" || tool.CredentialMode != "" {
					return false
				}
				continue
			}
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" {
				return false
			}
			if hasSystemTools && (pluginName == "*" || operation == "") {
				return false
			}
			if operation == "" {
				if !m.allowProvider(ctx, p, pluginName) || !principal.AllowsProviderPermission(p, pluginName) {
					return false
				}
				continue
			}
			if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
				return false
			}
			if !principal.AllowsOperationPermission(p, pluginName, operation) {
				return false
			}
		}
		if pluginName, operation, ok := workflowOutputDeliveryOperation(target.Agent.OutputDelivery); ok {
			if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
				return false
			}
			if !principal.AllowsOperationPermission(p, pluginName, operation) {
				return false
			}
		}
		return true
	}
	if target.Plugin == nil {
		return false
	}
	pluginTarget := *target.Plugin
	pluginName := strings.TrimSpace(pluginTarget.PluginName)
	operation := strings.TrimSpace(pluginTarget.Operation)
	if pluginName == "" || operation == "" {
		return false
	}
	if !m.allowProvider(ctx, p, pluginName) || !m.allowOperation(ctx, p, pluginName, operation) {
		return false
	}
	return principal.AllowsOperationPermission(p, pluginName, operation)
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

func cloneWorkflowOutputDelivery(delivery *coreworkflow.OutputDelivery) *coreworkflow.OutputDelivery {
	if delivery == nil {
		return nil
	}
	out := *delivery
	out.Target.Input = maps.Clone(delivery.Target.Input)
	out.InputBindings = append([]coreworkflow.OutputBinding(nil), delivery.InputBindings...)
	return &out
}

func (m *Manager) normalizeWorkflowOutputDelivery(delivery *coreworkflow.OutputDelivery, callerPluginName string) error {
	if delivery == nil {
		return nil
	}
	delivery.Target.PluginName = strings.TrimSpace(delivery.Target.PluginName)
	delivery.Target.Operation = strings.TrimSpace(delivery.Target.Operation)
	delivery.Target.Connection = strings.TrimSpace(delivery.Target.Connection)
	delivery.Target.Instance = strings.TrimSpace(delivery.Target.Instance)
	delivery.CredentialMode = core.ConnectionMode(strings.ToLower(strings.TrimSpace(string(delivery.CredentialMode))))
	if delivery.Target.PluginName == "" {
		return fmt.Errorf("%w: workflow agent output_delivery.target.plugin_name is required", invocation.ErrProviderNotFound)
	}
	if delivery.Target.Operation == "" {
		return fmt.Errorf("%w: workflow agent output_delivery.target.operation is required", invocation.ErrOperationNotFound)
	}
	if delivery.CredentialMode != "" && callerPluginName == "" {
		return fmt.Errorf("%w: workflow agent output_delivery.credential_mode requires a caller plugin declaration", invocation.ErrAuthorizationDenied)
	}
	if delivery.CredentialMode != "" && delivery.CredentialMode != core.ConnectionModeNone && delivery.CredentialMode != core.ConnectionModeUser {
		return fmt.Errorf("%w: workflow agent output_delivery.credential_mode %q is not supported", invocation.ErrInvalidInvocation, delivery.CredentialMode)
	}
	mode, declared, err := m.callerPluginInvokeCredentialMode(callerPluginName, delivery.Target.PluginName, delivery.Target.Operation)
	if err != nil {
		return err
	}
	if delivery.CredentialMode != "" && !declared {
		return fmt.Errorf("%w: workflow agent output_delivery.credential_mode requires a declared invoke mode", invocation.ErrAuthorizationDenied)
	}
	if delivery.CredentialMode != "" && delivery.CredentialMode != mode {
		return fmt.Errorf("%w: workflow agent output_delivery.credential_mode %q exceeds declared invoke mode %q", invocation.ErrAuthorizationDenied, delivery.CredentialMode, mode)
	}
	if delivery.CredentialMode == "" && declared {
		delivery.CredentialMode = mode
	}
	for i := range delivery.InputBindings {
		binding := &delivery.InputBindings[i]
		binding.InputField = strings.TrimSpace(binding.InputField)
		binding.Value.AgentOutput = strings.TrimSpace(binding.Value.AgentOutput)
		binding.Value.SignalPayload = strings.TrimSpace(binding.Value.SignalPayload)
		binding.Value.SignalMetadata = strings.TrimSpace(binding.Value.SignalMetadata)
		if binding.InputField == "" {
			return fmt.Errorf("%w: workflow agent output_delivery.input_bindings[%d].input_field is required", invocation.ErrInvalidInvocation, i)
		}
		if !workflowOutputValueSourceIsSet(binding.Value) {
			return fmt.Errorf("%w: workflow agent output_delivery.input_bindings[%d].value must set exactly one source", invocation.ErrInvalidInvocation, i)
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
		mode := core.ConnectionMode(strings.ToLower(strings.TrimSpace(string(invoke.CredentialMode))))
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

func workflowOutputValueSourceIsSet(source coreworkflow.OutputValueSource) bool {
	set := 0
	if strings.TrimSpace(source.AgentOutput) != "" {
		set++
	}
	if strings.TrimSpace(source.SignalPayload) != "" {
		set++
	}
	if strings.TrimSpace(source.SignalMetadata) != "" {
		set++
	}
	if source.Literal != nil {
		set++
	}
	return set == 1
}

func workflowOutputDeliveryOperation(delivery *coreworkflow.OutputDelivery) (string, string, bool) {
	if delivery == nil {
		return "", "", false
	}
	pluginName := strings.TrimSpace(delivery.Target.PluginName)
	operation := strings.TrimSpace(delivery.Target.Operation)
	if pluginName == "" || operation == "" {
		return "", "", false
	}
	return pluginName, operation, true
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

func runExecutionRefID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = uuid.NewString()
	}
	return workflowRunExecutionRefBasePrefix + value
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
		targetPrincipal = workflowprincipal.FromExecutionReference(ref)
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
