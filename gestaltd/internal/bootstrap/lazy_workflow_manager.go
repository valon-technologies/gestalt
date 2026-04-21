package bootstrap

import (
	"context"
	"fmt"
	"sync"

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
