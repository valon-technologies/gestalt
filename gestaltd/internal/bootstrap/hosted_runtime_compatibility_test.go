package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
)

func TestHostedRuntimeSessionCompatibilityReasonDetectsImageMismatch(t *testing.T) {
	t.Parallel()

	session := &pluginruntime.Session{Metadata: map[string]string{
		hostedRuntimeMetadataTemplate:     "agent-runtime",
		hostedRuntimeMetadataCurrentImage: "runtime@sha256:current",
		hostedRuntimeMetadataActualImage:  "runtime@sha256:old",
		hostedRuntimeMetadataImageMatch:   "false",
	}}

	reason := hostedRuntimeSessionCompatibilityReason(session)
	if !strings.Contains(reason, "agent-runtime image mismatch") {
		t.Fatalf("reason = %q, want image mismatch", reason)
	}
}

func TestHostedRuntimeSessionCompatibilityReasonTreatsMissingMetadataAsCompatible(t *testing.T) {
	t.Parallel()

	if reason := hostedRuntimeSessionCompatibilityReason(&pluginruntime.Session{}); reason != "" {
		t.Fatalf("reason = %q, want compatible", reason)
	}
}

func TestHostedAgentPoolDoesNotRouteNewWorkToKnownStaleBackend(t *testing.T) {
	t.Parallel()

	pool := &hostedAgentProviderPool{}
	backend := &hostedAgentPoolBackend{
		provider:        &pingAgentProvider{},
		runtimeSession:  staleRuntimeSessionForTest(),
		runtimeDrainAt:  nil,
		forceCloseAt:    nil,
		liveTurns:       map[string]struct{}{},
		runtimeProvider: nil,
	}
	pool.backends = []*hostedAgentPoolBackend{backend}
	pool.mu.Lock()
	accepts := pool.backendAcceptsNewWorkLocked(backend, time.Now().UTC())
	pool.mu.Unlock()
	if accepts {
		t.Fatalf("backendAcceptsNewWorkLocked = true, want false for stale runtime session")
	}
}

func TestHostedWorkflowWorkerKnownStaleSessionIsNotReady(t *testing.T) {
	t.Parallel()

	pool := &hostedWorkflowProviderPool{}
	worker := &hostedWorkflowWorker{
		provider:       &noopWorkflowProvider{},
		runtimeSession: staleRuntimeSessionForTest(),
	}
	pool.workers = []*hostedWorkflowWorker{worker}
	pool.mu.Lock()
	available := pool.workerAvailableLocked(worker, time.Now().UTC())
	reason := pool.runtimeSessionRetirementReason(worker.runtimeSession, nil, time.Now().UTC())
	pool.mu.Unlock()
	if available {
		t.Fatalf("workerAvailableLocked = true, want false for stale runtime session")
	}
	if !strings.Contains(reason, "image mismatch") {
		t.Fatalf("retirement reason = %q, want image mismatch", reason)
	}
}

func staleRuntimeSessionForTest() *pluginruntime.Session {
	return &pluginruntime.Session{Metadata: map[string]string{
		hostedRuntimeMetadataTemplate:     "agent-runtime",
		hostedRuntimeMetadataCurrentImage: "runtime@sha256:current",
		hostedRuntimeMetadataActualImage:  "runtime@sha256:old",
		hostedRuntimeMetadataImageMatch:   "false",
	}}
}

type noopWorkflowProvider struct{}

func (p *noopWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return nil
}
func (p *noopWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}
func (p *noopWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}
func (p *noopWorkflowProvider) Ping(context.Context) error { return nil }
func (p *noopWorkflowProvider) Close() error               { return nil }
