package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowManagerTransportHarness struct {
	proto.UnimplementedWorkflowManagerHostServer

	mu       sync.Mutex
	requests []*proto.WorkflowManagerCreateScheduleRequest
	signals  []*proto.WorkflowManagerSignalOrStartRunRequest
	tokens   []string
}

func (h *workflowManagerTransportHarness) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.WorkflowManagerCreateScheduleRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowSchedule{
		ProviderName: req.GetProviderName(),
		Schedule: &proto.BoundWorkflowSchedule{
			Id:   "sched-1",
			Cron: req.GetCron(),
		},
	}, nil
}

func (h *workflowManagerTransportHarness) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.signals = append(h.signals, gproto.Clone(req).(*proto.WorkflowManagerSignalOrStartRunRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowRunSignal{
		ProviderName: req.GetProviderName(),
		Run: &proto.BoundWorkflowRun{
			Id:          "run-1",
			WorkflowKey: req.GetWorkflowKey(),
		},
		Signal:      req.GetSignal(),
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func TestTransport_WorkflowManagerTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowManagerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowManagerSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowManager("parent-token")
	if err != nil {
		t.Fatalf("WorkflowManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	created, err := client.CreateSchedule(context.Background(), gestalt.WorkflowManagerCreateSchedule{
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

func TestTransport_WorkflowManagerSignalOrStartRunInjectsInvocationToken(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowManagerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowManagerSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowManager("parent-token")
	if err != nil {
		t.Fatalf("WorkflowManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	createdAtValue := time.Date(1969, 12, 31, 23, 59, 59, 999_000_000, time.UTC)
	resp, err := client.SignalOrStartRun(context.Background(), gestalt.WorkflowManagerSignalOrStartRun{
		ProviderName: "local",
		WorkflowKey:  "slack:T123:C123:1700000000.000001",
		Target: &gestalt.BoundWorkflowTarget{Agent: &gestalt.BoundWorkflowAgentTarget{
			ProviderName: "simple",
			Model:        "gpt-5.5",
			Prompt:       "Respond in thread.",
		}},
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

func TestTransport_WorkflowManagerSignalOrStartRunNativeValues(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowManagerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowManagerSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowManager("parent-token")
	if err != nil {
		t.Fatalf("WorkflowManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	createdAt := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	resp, err := client.SignalOrStartRun(context.Background(), gestalt.WorkflowManagerSignalOrStartRun{
		ProviderName: "local",
		WorkflowKey:  "slack:T123:C123:1700000000.000001",
		Target: &gestalt.BoundWorkflowTarget{Agent: &gestalt.BoundWorkflowAgentTarget{
			ProviderName: "simple",
			Model:        "gpt-5.5",
			Messages: []gestalt.AgentMessage{{
				Role: "user",
				Text: "Respond in thread.",
			}},
		}},
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
	if got.GetTarget().GetAgent().GetMessages()[0].GetText() != "Respond in thread." {
		t.Fatalf("agent messages = %#v", got.GetTarget().GetAgent().GetMessages())
	}
	if payload := got.GetSignal().GetPayload().AsMap(); payload["ok"] != true {
		t.Fatalf("signal payload = %#v", payload)
	}
	if !got.GetSignal().GetCreatedAt().AsTime().Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got.GetSignal().GetCreatedAt().AsTime(), createdAt)
	}
}
