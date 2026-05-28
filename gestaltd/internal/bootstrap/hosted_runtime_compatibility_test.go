package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestHostedRuntimeSessionCompatibilityReasonDetectsImageMismatch(t *testing.T) {
	t.Parallel()

	session := &proto.RuntimeSession{Metadata: map[string]string{
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

	if reason := hostedRuntimeSessionCompatibilityReason(&proto.RuntimeSession{}); reason != "" {
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

	pool := &hostedWorkflowWorkerPool{}
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

func staleRuntimeSessionForTest() *proto.RuntimeSession {
	return &proto.RuntimeSession{Metadata: map[string]string{
		hostedRuntimeMetadataTemplate:     "agent-runtime",
		hostedRuntimeMetadataCurrentImage: "runtime@sha256:current",
		hostedRuntimeMetadataActualImage:  "runtime@sha256:old",
		hostedRuntimeMetadataImageMatch:   "false",
	}}
}

type noopWorkflowProvider struct{}

func (p *noopWorkflowProvider) CreateDefinition(context.Context, *proto.CreateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) UpdateDefinition(context.Context, *proto.UpdateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest) error {
	return nil
}
func (p *noopWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return &proto.ListWorkflowProviderRunsResponse{}, nil
}
func (p *noopWorkflowProvider) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) UpsertSchedule(context.Context, *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetSchedule(context.Context, *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListSchedules(context.Context, *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	return &proto.ListWorkflowProviderSchedulesResponse{}, nil
}
func (p *noopWorkflowProvider) DeleteSchedule(context.Context, *proto.DeleteWorkflowProviderScheduleRequest) error {
	return nil
}
func (p *noopWorkflowProvider) PauseSchedule(context.Context, *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ResumeSchedule(context.Context, *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) UpsertEventTrigger(context.Context, *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetEventTrigger(context.Context, *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListEventTriggers(context.Context, *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	return &proto.ListWorkflowProviderEventTriggersResponse{}, nil
}
func (p *noopWorkflowProvider) DeleteEventTrigger(context.Context, *proto.DeleteWorkflowProviderEventTriggerRequest) error {
	return nil
}
func (p *noopWorkflowProvider) PauseEventTrigger(context.Context, *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ResumeEventTrigger(context.Context, *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) PublishEvent(_ context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return req.GetEvent(), nil
}
func (p *noopWorkflowProvider) Ping(context.Context) error { return nil }
func (p *noopWorkflowProvider) Close() error               { return nil }
