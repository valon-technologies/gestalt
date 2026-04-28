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
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/internal/workflowprincipal"
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
	CallerPluginName string
}

type EventTriggerUpsert struct {
	ProviderName     string
	Match            coreworkflow.EventMatch
	Target           coreworkflow.Target
	Paused           bool
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
	target, err := m.resolveTarget(ctx, p, req.Target)
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
	target, err := m.resolveTarget(ctx, p, req.Target)
	if err != nil {
		return nil, err
	}
	signal, err := m.normalizeSignal(req.Signal, p)
	if err != nil {
		return nil, err
	}

	executionRefID := runExecutionRefID(uuid.NewString())
	ref, err := m.putExecutionRef(ctx, executionRefID, providerName, provider, target, p, req.CallerPluginName)
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
		m.revokeExecutionRef(ctx, ref)
		return nil, err
	}
	if resp == nil || resp.Run == nil || strings.TrimSpace(resp.Run.ExecutionRef) != executionRefID {
		m.revokeExecutionRef(ctx, ref)
	}
	managed, err := m.managedSignalResponse(ctx, p, providerName, provider, resp, ref, signalTargetPrincipalExecutionRef)
	if err != nil && resp != nil && resp.Run != nil && strings.TrimSpace(resp.Run.ExecutionRef) == executionRefID {
		m.revokeExecutionRef(ctx, ref)
	}
	return managed, err
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
	target, err := m.resolveTarget(ctx, p, req.Target)
	if err != nil {
		return nil, err
	}

	scheduleID := uuid.NewString()
	executionRefID := scheduleExecutionRefID(scheduleID)
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
	target, err := m.resolveTarget(ctx, p, req.Target)
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
	target, err := m.resolveTarget(ctx, p, req.Target)
	if err != nil {
		return nil, err
	}
	match := normalizeEventMatch(req.Match)
	if strings.TrimSpace(match.Type) == "" {
		return nil, ErrWorkflowEventMatchRequired
	}

	triggerID := uuid.NewString()
	executionRefID := eventTriggerExecutionRefID(triggerID)
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
	target, err := m.resolveTarget(ctx, p, req.Target)
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

func (m *Manager) resolveTarget(ctx context.Context, p *principal.Principal, target coreworkflow.Target) (coreworkflow.Target, error) {
	hasPlugin := target.Plugin != nil && coreworkflow.PluginTargetSet(*target.Plugin)
	hasAgent := target.Agent != nil
	if hasAgent && hasPlugin {
		return coreworkflow.Target{}, fmt.Errorf("workflow target must set exactly one of plugin or agent")
	}
	if hasAgent {
		return m.resolveAgentTarget(ctx, p, *target.Agent)
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

func (m *Manager) resolveAgentTarget(ctx context.Context, p *principal.Principal, target coreworkflow.AgentTarget) (coreworkflow.Target, error) {
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
	if target.ToolSource == coreagent.ToolSourceModeUnspecified {
		target.ToolSource = coreagent.ToolSourceModeNativeSearch
	}
	if target.ToolSource != coreagent.ToolSourceModeNativeSearch {
		return coreworkflow.Target{}, fmt.Errorf("unsupported workflow agent tool source %q", target.ToolSource)
	}
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
	targetFingerprint, err := coreworkflow.TargetFingerprint(target)
	if err != nil {
		return nil, fmt.Errorf("workflow target fingerprint: %w", err)
	}
	actor := workflowActorFromPrincipal(p)
	return store.PutExecutionReference(ctx, &coreworkflow.ExecutionReference{
		ID:                  executionRefID,
		ProviderName:        strings.TrimSpace(providerName),
		Target:              target,
		TargetFingerprint:   targetFingerprint,
		CallerPluginName:    strings.TrimSpace(callerPluginName),
		SubjectID:           subjectID,
		SubjectKind:         actor.SubjectKind,
		DisplayName:         actor.DisplayName,
		AuthSource:          actor.AuthSource,
		CredentialSubjectID: strings.TrimSpace(principal.EffectiveCredentialSubjectID(p)),
		Permissions:         m.executionRefPermissions(p, target, callerPluginName),
	})
}

func (m *Manager) executionRefPermissions(p *principal.Principal, target coreworkflow.Target, callerPluginName string) []core.AccessPermission {
	p = principal.Canonicalized(p)
	if p == nil || p.TokenPermissions == nil {
		return principal.PermissionsToAccessPermissions(nil)
	}
	permissions := cloneWorkflowPermissionSet(p.TokenPermissions)
	if target.Agent != nil {
		for _, tool := range target.Agent.ToolRefs {
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
	return principal.PermissionsToAccessPermissions(permissions)
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
		for _, tool := range target.Agent.ToolRefs {
			pluginName := strings.TrimSpace(tool.Plugin)
			operation := strings.TrimSpace(tool.Operation)
			if pluginName == "" {
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
	if strings.TrimSpace(ref.TargetFingerprint) != "" {
		fingerprint, err := coreworkflow.TargetFingerprint(target)
		return err == nil && fingerprint == strings.TrimSpace(ref.TargetFingerprint)
	}
	if ref.Target.Agent != nil || target.Agent != nil {
		return false
	}
	if target.Plugin == nil || ref.Target.Plugin == nil {
		return false
	}
	left := *target.Plugin
	right := *ref.Target.Plugin
	return strings.TrimSpace(left.PluginName) == strings.TrimSpace(right.PluginName) &&
		strings.TrimSpace(left.Operation) == strings.TrimSpace(right.Operation) &&
		strings.TrimSpace(left.Connection) == strings.TrimSpace(right.Connection) &&
		strings.TrimSpace(left.Instance) == strings.TrimSpace(right.Instance)
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
