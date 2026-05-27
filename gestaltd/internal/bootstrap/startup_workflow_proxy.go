package bootstrap

import (
	"context"
	"fmt"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type startupWorkflowProviderProxy struct {
	providerName string
	tracker      *startupWaitTracker
	gate         startupGate[coreworkflow.Provider]
}

func newStartupWorkflowProviderProxy(providerName string, tracker *startupWaitTracker) *startupWorkflowProviderProxy {
	return &startupWorkflowProviderProxy{
		providerName: providerName,
		tracker:      tracker,
		gate:         newStartupGate[coreworkflow.Provider](),
	}
}

func (p *startupWorkflowProviderProxy) publish(provider coreworkflow.Provider) {
	p.finish(provider, nil)
}

func (p *startupWorkflowProviderProxy) fail(err error) {
	p.finish(nil, err)
}

func (p *startupWorkflowProviderProxy) finish(provider coreworkflow.Provider, err error) {
	if err == nil && provider == nil {
		err = fmt.Errorf("workflow provider is not available")
	}
	p.gate.finish(provider, err)
}

func (p *startupWorkflowProviderProxy) await(ctx context.Context) (coreworkflow.Provider, error) {
	provider, err := p.gate.await(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("workflow provider is not available")
	}
	return provider, nil
}

func (p *startupWorkflowProviderProxy) CreateDefinition(ctx context.Context, req coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.CreateDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetDefinition(ctx context.Context, req coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpdateDefinition(ctx context.Context, req coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpdateDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteDefinition(ctx context.Context, req coreworkflow.DeleteDefinitionRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteDefinition(ctx, req)
}

func (p *startupWorkflowProviderProxy) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.StartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListRuns(ctx, req)
}

func (p *startupWorkflowProviderProxy) CancelRun(ctx context.Context, req coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalRun(ctx context.Context, req coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.SignalRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) SignalOrStartRun(ctx context.Context, req coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.SignalOrStartRun(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListSchedules(ctx context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListSchedules(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeSchedule(ctx, req)
}

func (p *startupWorkflowProviderProxy) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForApp(ctx, startupWorkflowTargetAppName(req.Target))
	if err != nil {
		return nil, err
	}
	return provider.UpsertEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ListEventTriggers(ctx context.Context, req coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListEventTriggers(ctx, req)
}

func (p *startupWorkflowProviderProxy) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return err
	}
	return provider.DeleteEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PauseEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	provider, err := p.awaitForContextApp(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResumeEventTrigger(ctx, req)
}

func (p *startupWorkflowProviderProxy) PublishEvent(ctx context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	provider, err := p.awaitForApp(ctx, req.AppName)
	if err != nil {
		return nil, err
	}
	return provider.PublishEvent(ctx, req)
}

func (p *startupWorkflowProviderProxy) Ping(ctx context.Context) error {
	provider, err := p.awaitForApp(ctx, "")
	if err != nil {
		return err
	}
	return provider.Ping(ctx)
}

func (p *startupWorkflowProviderProxy) Close() error {
	provider, ready, _ := p.gate.resolved()
	if !ready || provider == nil {
		return nil
	}
	return provider.Close()
}

func (p *startupWorkflowProviderProxy) awaitForApp(ctx context.Context, appName string) (coreworkflow.Provider, error) {
	done, err := p.beginCallerWait(ctx, appName)
	if err != nil {
		return nil, err
	}
	defer done()
	return p.await(ctx)
}

func (p *startupWorkflowProviderProxy) awaitForContextApp(ctx context.Context) (coreworkflow.Provider, error) {
	appName := strings.TrimSpace(invocation.WorkflowContextString(invocation.WorkflowContextFromContext(ctx), "app"))
	return p.awaitForApp(ctx, appName)
}

func startupWorkflowTargetAppName(target coreworkflow.Target) string {
	for i := range target.Steps {
		if target.Steps[i].App != nil {
			return strings.TrimSpace(target.Steps[i].App.Name)
		}
	}
	return ""
}

func (p *startupWorkflowProviderProxy) beginCallerWait(ctx context.Context, appName string) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	target := newStartupProviderNode(invocation.ProviderKindWorkflow, p.providerName)
	if done, ok, err := p.tracker.beginCallerProviderWait(ctx, target); ok || err != nil {
		return done, err
	}
	return p.tracker.beginWait(
		newStartupProviderNode(invocation.ProviderKindApp, appName),
		target,
	)
}
