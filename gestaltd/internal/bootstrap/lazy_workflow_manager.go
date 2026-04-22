package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
)

type lazyWorkflowManager struct {
	mu     sync.RWMutex
	target workflowmanager.Service
}

func newLazyWorkflowManager() *lazyWorkflowManager {
	return &lazyWorkflowManager{}
}

func (l *lazyWorkflowManager) SetTarget(target workflowmanager.Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

func (l *lazyWorkflowManager) ListSchedules(ctx context.Context, p *principal.Principal) ([]*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListSchedules(ctx, p)
}

func (l *lazyWorkflowManager) CreateSchedule(ctx context.Context, p *principal.Principal, req workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateSchedule(ctx, p, req)
}

func (l *lazyWorkflowManager) GetSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetSchedule(ctx, p, scheduleID)
}

func (l *lazyWorkflowManager) UpdateSchedule(ctx context.Context, p *principal.Principal, scheduleID string, req workflowmanager.ScheduleUpsert) (*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.UpdateSchedule(ctx, p, scheduleID, req)
}

func (l *lazyWorkflowManager) DeleteSchedule(ctx context.Context, p *principal.Principal, scheduleID string) error {
	target, err := l.current()
	if err != nil {
		return err
	}
	return target.DeleteSchedule(ctx, p, scheduleID)
}

func (l *lazyWorkflowManager) PauseSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.PauseSchedule(ctx, p, scheduleID)
}

func (l *lazyWorkflowManager) ResumeSchedule(ctx context.Context, p *principal.Principal, scheduleID string) (*workflowmanager.ManagedSchedule, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResumeSchedule(ctx, p, scheduleID)
}

func (l *lazyWorkflowManager) ListEventTriggers(ctx context.Context, p *principal.Principal) ([]*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListEventTriggers(ctx, p)
}

func (l *lazyWorkflowManager) CreateEventTrigger(ctx context.Context, p *principal.Principal, req workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateEventTrigger(ctx, p, req)
}

func (l *lazyWorkflowManager) GetEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetEventTrigger(ctx, p, triggerID)
}

func (l *lazyWorkflowManager) UpdateEventTrigger(ctx context.Context, p *principal.Principal, triggerID string, req workflowmanager.EventTriggerUpsert) (*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.UpdateEventTrigger(ctx, p, triggerID, req)
}

func (l *lazyWorkflowManager) DeleteEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) error {
	target, err := l.current()
	if err != nil {
		return err
	}
	return target.DeleteEventTrigger(ctx, p, triggerID)
}

func (l *lazyWorkflowManager) PauseEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.PauseEventTrigger(ctx, p, triggerID)
}

func (l *lazyWorkflowManager) ResumeEventTrigger(ctx context.Context, p *principal.Principal, triggerID string) (*workflowmanager.ManagedEventTrigger, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResumeEventTrigger(ctx, p, triggerID)
}

func (l *lazyWorkflowManager) ListRuns(ctx context.Context, p *principal.Principal) ([]*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRuns(ctx, p)
}

func (l *lazyWorkflowManager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRun(ctx, p, runID)
}

func (l *lazyWorkflowManager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelRun(ctx, p, runID, reason)
}

func (l *lazyWorkflowManager) PublishEvent(ctx context.Context, p *principal.Principal, event coreworkflow.Event) (coreworkflow.Event, error) {
	target, err := l.current()
	if err != nil {
		return coreworkflow.Event{}, err
	}
	return target.PublishEvent(ctx, p, event)
}

func (l *lazyWorkflowManager) current() (workflowmanager.Service, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("workflow manager is not available")
	}
	return target, nil
}

var _ workflowmanager.Service = (*lazyWorkflowManager)(nil)
