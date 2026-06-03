package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
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

func (l *lazyWorkflowManager) ApplyDefinition(ctx context.Context, p *principal.Principal, req workflowmanager.DefinitionApply) (*workflowmanager.ManagedDefinition, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ApplyDefinition(ctx, p, req)
}

func (l *lazyWorkflowManager) GetDefinition(ctx context.Context, p *principal.Principal, definitionID string) (*workflowmanager.ManagedDefinition, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetDefinition(ctx, p, definitionID)
}

func (l *lazyWorkflowManager) ListDefinitions(ctx context.Context, p *principal.Principal) (*workflowmanager.ListDefinitionsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListDefinitions(ctx, p)
}

func (l *lazyWorkflowManager) SetDefinitionPaused(ctx context.Context, p *principal.Principal, definitionID string, paused bool) (*workflowmanager.ManagedDefinition, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.SetDefinitionPaused(ctx, p, definitionID, paused)
}

func (l *lazyWorkflowManager) SetActivationPaused(ctx context.Context, p *principal.Principal, definitionID, activationID string, paused bool) (*workflowmanager.ManagedDefinition, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.SetActivationPaused(ctx, p, definitionID, activationID, paused)
}

func (l *lazyWorkflowManager) DeleteDefinition(ctx context.Context, p *principal.Principal, definitionID string) error {
	target, err := l.current()
	if err != nil {
		return err
	}
	return target.DeleteDefinition(ctx, p, definitionID)
}

func (l *lazyWorkflowManager) ListRuns(ctx context.Context, p *principal.Principal, req coreworkflow.ListRunsRequest) (*workflowmanager.ListRunsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRuns(ctx, p, req)
}

func (l *lazyWorkflowManager) StartRun(ctx context.Context, p *principal.Principal, req workflowmanager.RunStart) (*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.StartRun(ctx, p, req)
}

func (l *lazyWorkflowManager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRun(ctx, p, runID)
}

func (l *lazyWorkflowManager) GetRunEvents(ctx context.Context, p *principal.Principal, runID string) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRunEvents(ctx, p, runID)
}

func (l *lazyWorkflowManager) GetRunOutput(ctx context.Context, p *principal.Principal, runID string) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRunOutput(ctx, p, runID)
}

func (l *lazyWorkflowManager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*workflowmanager.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelRun(ctx, p, runID, reason)
}

func (l *lazyWorkflowManager) SignalRun(ctx context.Context, p *principal.Principal, req workflowmanager.RunSignal) (*workflowmanager.ManagedRunSignal, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.SignalRun(ctx, p, req)
}

func (l *lazyWorkflowManager) SignalOrStartRun(ctx context.Context, p *principal.Principal, req workflowmanager.RunSignalOrStart) (*workflowmanager.ManagedRunSignal, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.SignalOrStartRun(ctx, p, req)
}

func (l *lazyWorkflowManager) DeliverEvent(ctx context.Context, p *principal.Principal, req workflowmanager.EventDeliver) (coreworkflow.Event, error) {
	target, err := l.current()
	if err != nil {
		return coreworkflow.Event{}, err
	}
	return target.DeliverEvent(ctx, p, req)
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
