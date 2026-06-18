package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowTransportHarness struct {
	proto.UnimplementedWorkflowServer

	mu          sync.Mutex
	definitions []*proto.ApplyWorkflowProviderDefinitionRequest
	signals     []*proto.SignalOrStartWorkflowProviderRunRequest
	tokens      []string
}

func (h *workflowTransportHarness) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.definitions = append(h.definitions, gproto.Clone(req).(*proto.ApplyWorkflowProviderDefinitionRequest))
	h.mu.Unlock()

	spec := req.GetSpec()
	return &proto.WorkflowDefinition{
		ProviderName: req.GetProviderName(),
		Id:           spec.GetId(),
		Generation:   3,
		Target:       spec.GetTarget(),
		Activations:  spec.GetActivations(),
	}, nil
}

func (h *workflowTransportHarness) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.signals = append(h.signals, gproto.Clone(req).(*proto.SignalOrStartWorkflowProviderRunRequest))
	h.mu.Unlock()

	return &proto.SignalWorkflowRunResponse{
		Run: &proto.WorkflowRun{
			ProviderName:         req.GetProviderName(),
			Id:                   "run-1",
			WorkflowKey:          req.GetWorkflowKey(),
			DefinitionId:         req.GetDefinitionId(),
			DefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		},
		Signal:      req.GetSignal(),
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func workflowTransportRequestContext() *client.RequestContext {
	return &client.RequestContext{
		Subject: &client.SubjectContext{
			Id:                  "user:transport",
						Email:               "transport@example.test",
		},
	}
}

func TestTransport_WorkflowApplyDefinitionTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	workflow, err := client.ConnectWorkflow(context.Background(), "", client.WithRequestContext(workflowTransportRequestContext()))
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}

	applied, err := workflow.ApplyDefinition(context.Background(), "managed", "workflow-definition-key-go", &client.WorkflowDefinitionSpec{
		Id: "definition-1",
		Target: &client.BoundWorkflowTarget{Steps: []*client.WorkflowStep{{
			Id: "review",
			Action: &client.WorkflowStepActionApp{Value: &client.WorkflowStepAppCall{
				Name:      "github",
				Operation: "pullRequests.review",
			}},
		}}},
		Activations: []*client.WorkflowActivation{{
			Id: "github_pr",
			Trigger: &client.WorkflowActivationTriggerEvent{Value: &client.WorkflowEventActivation{Match: &client.WorkflowEventMatch{
				Type:   "github.pull_request",
				Source: "github",
			}}},
		}},
	})
	if err != nil {
		t.Fatalf("ApplyDefinition: %v", err)
	}
	if applied.ProviderName != "managed" || applied.Id != "definition-1" || applied.Generation != 3 {
		t.Fatalf("definition = %#v", applied)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.definitions) != 1 {
		t.Fatalf("definition requests len = %d, want 1", len(harness.definitions))
	}
	if got := harness.definitions[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("subject = %q, want user:transport", got)
	}
	if harness.definitions[0].GetProviderName() != "managed" || harness.definitions[0].GetSpec().GetId() != "definition-1" {
		t.Fatalf("apply definition request = %+v", harness.definitions[0])
	}
	if harness.definitions[0].GetIdempotencyKey() != "workflow-definition-key-go" {
		t.Fatalf("idempotency key = %q, want workflow-definition-key-go", harness.definitions[0].GetIdempotencyKey())
	}
}

func TestTransport_WorkflowSignalOrStartRunPropagatesRequestContext(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	workflow, err := client.ConnectWorkflow(context.Background(), "", client.WithRequestContext(workflowTransportRequestContext()))
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}

	createdAtValue := time.Date(1969, 12, 31, 23, 59, 59, 999_000_000, time.UTC)
	resp, err := workflow.SignalOrStartRunRaw(context.Background(), &client.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:                 "local",
		WorkflowKey:                  "slack:T123:C123:1700000000.000001",
		DefinitionId:                 "definition-1",
		ExpectedDefinitionGeneration: 9,
		Input:                        map[string]any{"thread_ts": "1700000000.000001"},
		IdempotencyKey:               "slack-event-123",
		Signal: &client.WorkflowSignal{
			Name:           "slack.message",
			IdempotencyKey: "slack-event-123",
			Payload:        map[string]any{"ok": true},
			CreatedAt:      &createdAtValue,
		},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if resp.Run == nil || resp.Run.ProviderName != "local" || resp.Run.Id != "run-1" || !resp.StartedRun {
		t.Fatalf("response = %#v", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.signals) != 1 {
		t.Fatalf("signal requests len = %d, want 1", len(harness.signals))
	}
	got := harness.signals[0]
	if got.GetContext().GetSubject().GetId() != "user:transport" {
		t.Fatalf("subject = %q, want user:transport", got.GetContext().GetSubject().GetId())
	}
	if got.GetDefinitionId() != "definition-1" || got.GetExpectedDefinitionGeneration() != 9 {
		t.Fatalf("definition pin = %q/%d", got.GetDefinitionId(), got.GetExpectedDefinitionGeneration())
	}
	if got.GetWorkflowKey() != "slack:T123:C123:1700000000.000001" || got.GetSignal().GetName() != "slack.message" {
		t.Fatalf("signal request = %+v", got)
	}
	if payload := got.GetSignal().GetPayload().AsMap(); payload["ok"] != true {
		t.Fatalf("signal payload = %#v", payload)
	}
	if input := got.GetInput().AsMap(); input["thread_ts"] != "1700000000.000001" {
		t.Fatalf("run input = %#v", input)
	}
	roundTripCreatedAt := got.GetSignal().GetCreatedAt()
	if roundTripCreatedAt == nil {
		t.Fatalf("created_at is nil, want %v", createdAtValue)
	}
	if err := roundTripCreatedAt.CheckValid(); err != nil {
		t.Fatalf("created_at invalid: %v", err)
	}
	if !roundTripCreatedAt.AsTime().Equal(createdAtValue) {
		t.Fatalf("created_at = %v, want %v", roundTripCreatedAt.AsTime(), createdAtValue)
	}
	if err := (&timestamppb.Timestamp{Nanos: -1}).CheckValid(); err == nil {
		t.Fatal("invalid timestamp CheckValid() = nil, want error")
	}
}
