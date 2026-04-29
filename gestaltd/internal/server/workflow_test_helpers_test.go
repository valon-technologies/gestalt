package server_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

type stubWorkflowControl struct {
	defaultProviderName string
	provider            coreworkflow.Provider
	providers           map[string]coreworkflow.Provider
	selectionErr        error
	providerErr         error
}

func (s *stubWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	if s.selectionErr != nil {
		return "", nil, s.selectionErr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = s.defaultProviderName
	}
	if name == "" {
		return "", nil, errors.New("provider not found")
	}
	provider, err := s.ResolveProvider(name)
	if err != nil {
		return "", nil, err
	}
	return name, provider, nil
}

func (s *stubWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	if s.providerErr != nil {
		return nil, s.providerErr
	}
	if s.providers != nil {
		provider, ok := s.providers[name]
		if !ok {
			return nil, errors.New("provider not found")
		}
		return provider, nil
	}
	return s.provider, nil
}

func (s *stubWorkflowControl) ProviderNames() []string {
	if s.providers != nil {
		names := make([]string, 0, len(s.providers))
		for name := range s.providers {
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	if strings.TrimSpace(s.defaultProviderName) != "" {
		return []string{strings.TrimSpace(s.defaultProviderName)}
	}
	if s.provider != nil {
		return []string{"default"}
	}
	return nil
}

type memoryWorkflowProvider struct {
	runs             map[string]*coreworkflow.Run
	executionRefs    map[string]*coreworkflow.ExecutionReference
	publishEventReqs []coreworkflow.PublishEventRequest
	cancelReqs       []coreworkflow.CancelRunRequest
	getRunErr        error
	listRunsErr      error
	cancelRunErr     error
	publishEventErr  error
}

func newMemoryWorkflowProvider() *memoryWorkflowProvider {
	return &memoryWorkflowProvider{
		runs:          map[string]*coreworkflow.Run{},
		executionRefs: map[string]*coreworkflow.ExecutionReference{},
	}
}

func (p *memoryWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetRun(_ context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	if p.getRunErr != nil {
		return nil, p.getRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowRun(run), nil
}

func (p *memoryWorkflowProvider) ListRuns(_ context.Context, _ coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	if p.listRunsErr != nil {
		return nil, p.listRunsErr
	}
	out := make([]*coreworkflow.Run, 0, len(p.runs))
	for _, run := range p.runs {
		if run != nil {
			out = append(out, cloneWorkflowRun(run))
		}
	}
	return out, nil
}

func (p *memoryWorkflowProvider) CancelRun(_ context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	if p.cancelRunErr != nil {
		return nil, p.cancelRunErr
	}
	run, ok := p.runs[req.RunID]
	if !ok || run == nil {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	run.Status = coreworkflow.RunStatusCanceled
	run.CompletedAt = &now
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		run.StatusMessage = reason
	}
	p.cancelReqs = append(p.cancelReqs, req)
	return cloneWorkflowRun(run), nil
}

func (p *memoryWorkflowProvider) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}

func (p *memoryWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return core.ErrNotFound
}

func (p *memoryWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, errors.New("not implemented")
}

func (p *memoryWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}

func (p *memoryWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return core.ErrNotFound
}

func (p *memoryWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}

func (p *memoryWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	if p.publishEventErr != nil {
		return p.publishEventErr
	}
	req.Event = cloneWorkflowEvent(req.Event)
	p.publishEventReqs = append(p.publishEventReqs, req)
	return nil
}

func (p *memoryWorkflowProvider) PutExecutionReference(_ context.Context, ref *coreworkflow.ExecutionReference) (*coreworkflow.ExecutionReference, error) {
	if p.executionRefs == nil {
		p.executionRefs = map[string]*coreworkflow.ExecutionReference{}
	}
	stored := cloneWorkflowExecutionReference(ref)
	if strings.TrimSpace(stored.TargetFingerprint) == "" {
		return nil, errors.New("workflow execution reference target fingerprint is required")
	}
	p.executionRefs[stored.ID] = stored
	return cloneWorkflowExecutionReference(stored), nil
}

func (p *memoryWorkflowProvider) GetExecutionReference(_ context.Context, id string) (*coreworkflow.ExecutionReference, error) {
	ref := p.executionRefs[strings.TrimSpace(id)]
	if ref == nil {
		return nil, core.ErrNotFound
	}
	return cloneWorkflowExecutionReference(ref), nil
}

func (p *memoryWorkflowProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*coreworkflow.ExecutionReference, error) {
	subjectID = strings.TrimSpace(subjectID)
	out := make([]*coreworkflow.ExecutionReference, 0, len(p.executionRefs))
	for _, ref := range p.executionRefs {
		if ref == nil {
			continue
		}
		if subjectID != "" && strings.TrimSpace(ref.SubjectID) != subjectID {
			continue
		}
		out = append(out, cloneWorkflowExecutionReference(ref))
	}
	return out, nil
}

func (p *memoryWorkflowProvider) Ping(context.Context) error { return nil }
func (p *memoryWorkflowProvider) Close() error               { return nil }

func cloneWorkflowExecutionReference(ref *coreworkflow.ExecutionReference) *coreworkflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	cloned := *ref
	cloned.Target = cloneWorkflowTarget(ref.Target)
	cloned.Permissions = slices.Clone(ref.Permissions)
	for i := range cloned.Permissions {
		cloned.Permissions[i].Operations = slices.Clone(cloned.Permissions[i].Operations)
		cloned.Permissions[i].Actions = slices.Clone(cloned.Permissions[i].Actions)
	}
	cloned.CreatedAt = cloneWorkflowTime(ref.CreatedAt)
	cloned.RevokedAt = cloneWorkflowTime(ref.RevokedAt)
	return &cloned
}

func cloneWorkflowRun(run *coreworkflow.Run) *coreworkflow.Run {
	if run == nil {
		return nil
	}
	cloned := *run
	cloned.Target = cloneWorkflowTarget(run.Target)
	cloned.Trigger = cloneWorkflowRunTrigger(run.Trigger)
	cloned.CreatedAt = cloneWorkflowTime(run.CreatedAt)
	cloned.StartedAt = cloneWorkflowTime(run.StartedAt)
	cloned.CompletedAt = cloneWorkflowTime(run.CompletedAt)
	return &cloned
}

func cloneWorkflowRunTrigger(trigger coreworkflow.RunTrigger) coreworkflow.RunTrigger {
	cloned := trigger
	if trigger.Schedule != nil {
		value := *trigger.Schedule
		value.ScheduledFor = cloneWorkflowTime(trigger.Schedule.ScheduledFor)
		cloned.Schedule = &value
	}
	if trigger.Event != nil {
		value := *trigger.Event
		value.Event = cloneWorkflowEvent(trigger.Event.Event)
		cloned.Event = &value
	}
	return cloned
}

func cloneWorkflowTarget(target coreworkflow.Target) coreworkflow.Target {
	cloned := coreworkflow.Target{}
	if target.Plugin != nil {
		plugin := *target.Plugin
		plugin.Input = maps.Clone(target.Plugin.Input)
		cloned.Plugin = &plugin
	}
	if target.Agent != nil {
		agent := *target.Agent
		agent.Messages = slices.Clone(target.Agent.Messages)
		for i := range agent.Messages {
			agent.Messages[i].Metadata = maps.Clone(agent.Messages[i].Metadata)
		}
		agent.ToolRefs = slices.Clone(target.Agent.ToolRefs)
		agent.ResponseSchema = maps.Clone(target.Agent.ResponseSchema)
		agent.ProviderOptions = maps.Clone(target.Agent.ProviderOptions)
		agent.Metadata = maps.Clone(target.Agent.Metadata)
		cloned.Agent = &agent
	}
	return cloned
}

func cloneWorkflowEvent(event coreworkflow.Event) coreworkflow.Event {
	cloned := event
	cloned.Time = cloneWorkflowTime(event.Time)
	cloned.Data = maps.Clone(event.Data)
	cloned.Extensions = maps.Clone(event.Extensions)
	return cloned
}

func cloneWorkflowTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
