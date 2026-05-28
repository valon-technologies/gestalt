package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowTransportHarness struct {
	proto.UnimplementedWorkflowProviderServer

	mu       sync.Mutex
	requests []*proto.UpsertWorkflowProviderScheduleRequest
	signals  []*proto.SignalOrStartWorkflowProviderRunRequest
	tokens   []string
}

func (h *workflowTransportHarness) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.UpsertWorkflowProviderScheduleRequest))
	h.mu.Unlock()

	return &proto.BoundWorkflowSchedule{
		ProviderName: req.GetProviderName(),
		Id:           "sched-1",
		Cron:         req.GetCron(),
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
		Run: &proto.BoundWorkflowRun{
			ProviderName: req.GetProviderName(),
			Id:           "run-1",
			WorkflowKey:  req.GetWorkflowKey(),
		},
		Signal:      req.GetSignal(),
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func TestTransport_WorkflowTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewWorkflow("parent-token")
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	defer func() { _ = client.Close() }()

	created, err := client.CreateSchedule(context.Background(), gestalt.WorkflowCreateSchedule{
		ProviderName:   "managed",
		Cron:           "*/5 * * * *",
		IdempotencyKey: "workflow-schedule-key-go",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.ProviderName != "managed" {
		t.Fatalf("provider_name = %q, want %q", created.ProviderName, "managed")
	}
	if created.Schedule == nil || created.Schedule.ID != "sched-1" {
		t.Fatalf("schedule = %#v, want id sched-1", created.Schedule)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("create schedule requests len = %d, want 1", len(harness.requests))
	}
	if harness.requests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", harness.requests[0].GetInvocationToken(), "parent-token")
	}
	if harness.requests[0].GetProviderName() != "managed" || harness.requests[0].GetCron() != "*/5 * * * *" {
		t.Fatalf("create schedule request = %+v, want provider_name=managed cron=*/5 * * * *", harness.requests[0])
	}
	if harness.requests[0].GetIdempotencyKey() != "workflow-schedule-key-go" {
		t.Fatalf("idempotency key = %q, want workflow-schedule-key-go", harness.requests[0].GetIdempotencyKey())
	}
}

func TestTransport_WorkflowSignalOrStartRunInjectsInvocationToken(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewWorkflow("parent-token")
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	defer func() { _ = client.Close() }()

	createdAtValue := time.Date(1969, 12, 31, 23, 59, 59, 999_000_000, time.UTC)
	resp, err := client.SignalOrStartRun(context.Background(), gestalt.WorkflowSignalOrStartRun{
		ProviderName: "local",
		WorkflowKey:  "slack:T123:C123:1700000000.000001",
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID: "respond",
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider: "simple",
				Model:    "gpt-5.5",
				Prompt:   gestalt.WorkflowText{Template: "Respond in thread."},
				Output:   gestalt.AgentOutput{Text: &gestalt.AgentTextOutput{}},
			},
		}}},
		IdempotencyKey: "slack-event-123",
		Signal: &gestalt.WorkflowSignal{
			Name:           "slack.message",
			IdempotencyKey: "slack-event-123",
			Payload:        map[string]any{"ok": true},
			CreatedAt:      createdAtValue,
		},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if resp.ProviderName != "local" || resp.Run == nil || resp.Run.ID != "run-1" || !resp.StartedRun {
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
	if got.GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", got.GetInvocationToken(), "parent-token")
	}
	if got.GetWorkflowKey() != "slack:T123:C123:1700000000.000001" || got.GetSignal().GetName() != "slack.message" {
		t.Fatalf("signal request = %+v", got)
	}
	if payload := got.GetSignal().GetPayload().AsMap(); payload["ok"] != true {
		t.Fatalf("signal payload = %#v", payload)
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

func TestTransport_WorkflowSignalOrStartRunNativeValues(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewWorkflow("parent-token")
	if err != nil {
		t.Fatalf("Workflow: %v", err)
	}
	defer func() { _ = client.Close() }()

	createdAt := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	resp, err := client.SignalOrStartRun(context.Background(), gestalt.WorkflowSignalOrStartRun{
		ProviderName: "local",
		WorkflowKey:  "slack:T123:C123:1700000000.000001",
		Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
			ID: "respond",
			Agent: &gestalt.WorkflowStepAgentTurn{
				Provider: "simple",
				Model:    "gpt-5.5",
				Messages: []gestalt.WorkflowAgentMessage{{
					Role: "user",
					Text: gestalt.WorkflowText{Template: "Respond in thread."},
				}},
				Output: gestalt.AgentOutput{Text: &gestalt.AgentTextOutput{}},
			},
		}}},
		IdempotencyKey: "slack-event-123",
		Signal: &gestalt.WorkflowSignal{
			Name:           "slack.message",
			IdempotencyKey: "slack-event-123",
			Payload:        map[string]any{"ok": true},
			CreatedAt:      createdAt,
		},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if resp.ProviderName != "local" || resp.Run == nil || resp.Run.ID != "run-1" || !resp.StartedRun {
		t.Fatalf("response = %#v", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.signals) != 1 {
		t.Fatalf("signal requests len = %d, want 1", len(harness.signals))
	}
	got := harness.signals[0]
	if got.GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want parent-token", got.GetInvocationToken())
	}
	if got.GetTarget().GetSteps()[0].GetAgent().GetMessages()[0].GetText().GetTemplate() != "Respond in thread." {
		t.Fatalf("agent messages = %#v", got.GetTarget().GetSteps()[0].GetAgent().GetMessages())
	}
	if payload := got.GetSignal().GetPayload().AsMap(); payload["ok"] != true {
		t.Fatalf("signal payload = %#v", payload)
	}
	if !got.GetSignal().GetCreatedAt().AsTime().Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got.GetSignal().GetCreatedAt().AsTime(), createdAt)
	}
}
