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

func (p *noopWorkflowProvider) ApplyDefinition(context.Context, *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	return &proto.ListWorkflowProviderDefinitionsResponse{}, nil
}
func (p *noopWorkflowProvider) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest) error {
	return nil
}
func (p *noopWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return &proto.ListWorkflowProviderRunsResponse{}, nil
}
func (p *noopWorkflowProvider) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return &proto.GetWorkflowProviderRunEventsResponse{}, nil
}
func (p *noopWorkflowProvider) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return &proto.GetWorkflowProviderRunOutputResponse{}, nil
}
func (p *noopWorkflowProvider) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, nil
}
func (p *noopWorkflowProvider) DeliverEvent(_ context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return req.GetEvent(), nil
}
func (p *noopWorkflowProvider) Ping(context.Context) error { return nil }
func (p *noopWorkflowProvider) Close() error               { return nil }
