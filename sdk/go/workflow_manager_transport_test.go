package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
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

	created, err := client.CreateSchedule(context.Background(), &proto.WorkflowManagerCreateScheduleRequest{
		ProviderName:   "managed",
		Cron:           "*/5 * * * *",
		IdempotencyKey: "workflow-schedule-key-go",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want %q", created.GetProviderName(), "managed")
	}
	if created.GetSchedule().GetId() != "sched-1" {
		t.Fatalf("schedule id = %q, want %q", created.GetSchedule().GetId(), "sched-1")
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

	signalPayload, err := gestalt.StructFromAny(map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("StructFromAny: %v", err)
	}
	signalExtensions, err := gestalt.ValuesFromMap(map[string]any{"attempt": 1})
	if err != nil {
		t.Fatalf("ValuesFromMap: %v", err)
	}
	createdAtValue := time.Date(1969, 12, 31, 23, 59, 59, 999_000_000, time.UTC)
	createdAt := gestalt.TimestampFromTimePtr(&createdAtValue)
	resp, err := client.SignalOrStartRun(context.Background(), &proto.WorkflowManagerSignalOrStartRunRequest{
		ProviderName: "local",
		WorkflowKey:  "slack:T123:C123:1700000000.000001",
		Target: &proto.BoundWorkflowTarget{
			Kind: &proto.BoundWorkflowTarget_Agent{
				Agent: &proto.BoundWorkflowAgentTarget{
					ProviderName: "simple",
					Model:        "gpt-5.5",
					Prompt:       "Respond in thread.",
				},
			},
		},
		IdempotencyKey: "slack-event-123",
		Signal: &proto.WorkflowSignal{
			Name:           "slack.message",
			IdempotencyKey: "slack-event-123",
			Payload:        signalPayload,
			CreatedAt:      createdAt,
		},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if resp.GetProviderName() != "local" || resp.GetRun().GetId() != "run-1" || !resp.GetStartedRun() {
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
	if payload := gestalt.MapFromStruct(got.GetSignal().GetPayload()); payload["ok"] != true {
		t.Fatalf("signal payload = %#v", payload)
	}
	event := &proto.WorkflowEvent{Extensions: signalExtensions}
	if extensions := gestalt.MapFromValues(event.GetExtensions()); extensions["attempt"] != float64(1) {
		t.Fatalf("signal extensions = %#v", extensions)
	}
	roundTripCreatedAt, err := gestalt.TimePtrFromTimestamp(got.GetSignal().GetCreatedAt())
	if err != nil {
		t.Fatalf("TimePtrFromTimestamp: %v", err)
	}
	if roundTripCreatedAt == nil || !roundTripCreatedAt.Equal(createdAtValue) {
		t.Fatalf("created_at = %v, want %v", roundTripCreatedAt, createdAtValue)
	}
	if gestalt.CloneStruct(got.GetSignal().GetPayload()) == got.GetSignal().GetPayload() {
		t.Fatal("CloneStruct returned the original pointer")
	}
	if gestalt.CloneTimestamp(got.GetSignal().GetCreatedAt()) == got.GetSignal().GetCreatedAt() {
		t.Fatal("CloneTimestamp returned the original pointer")
	}
	if value, err := gestalt.StructFromAny(nil); err != nil || value != nil {
		t.Fatalf("StructFromAny(nil) = %#v, %v; want nil, nil", value, err)
	}
	if value, err := gestalt.StructFromAny(map[string]any{}); err != nil || value == nil || len(value.GetFields()) != 0 {
		t.Fatalf("StructFromAny(empty map) = %#v, %v; want empty struct, nil", value, err)
	}
	if value, err := gestalt.StructFromAny([]string{"bad"}); err == nil || value != nil {
		t.Fatalf("StructFromAny(non-map) = %#v, %v; want nil error", value, err)
	}
	if value, err := gestalt.ValuesFromMap(nil); err != nil || value != nil {
		t.Fatalf("ValuesFromMap(nil) = %#v, %v; want nil, nil", value, err)
	}
	if value := gestalt.MapFromValues(nil); value != nil {
		t.Fatalf("MapFromValues(nil) = %#v, want nil", value)
	}
	if value := gestalt.TimestampFromTimePtr(nil); value != nil {
		t.Fatalf("TimestampFromTimePtr(nil) = %#v, want nil", value)
	}
	var setTimeValue *timestamppb.Timestamp
	setTimeWant := time.Unix(1700, 25).UTC()
	gestalt.SetTime(&setTimeValue, setTimeWant)
	if setTimeValue == nil || !setTimeValue.AsTime().Equal(setTimeWant) {
		t.Fatalf("SetTime = %#v, want %v", setTimeValue, setTimeWant)
	}
	gestalt.SetTime(&setTimeValue, time.Time{})
	if setTimeValue != nil {
		t.Fatalf("SetTime(zero) = %#v, want nil", setTimeValue)
	}
	setOptionalWant := time.Unix(1800, 0).UTC()
	gestalt.SetOptionalTime(&setTimeValue, &setOptionalWant)
	if setTimeValue == nil || !setTimeValue.AsTime().Equal(setOptionalWant) {
		t.Fatalf("SetOptionalTime = %#v, want %v", setTimeValue, setOptionalWant)
	}
	gestalt.SetOptionalTime(&setTimeValue, nil)
	if setTimeValue != nil {
		t.Fatalf("SetOptionalTime(nil) = %#v, want nil", setTimeValue)
	}
	if value, err := gestalt.TimePtrFromTimestamp(nil); err != nil || value != nil {
		t.Fatalf("TimePtrFromTimestamp(nil) = %#v, %v; want nil, nil", value, err)
	}
	if value, err := gestalt.TimePtrFromTimestamp(&timestamppb.Timestamp{Nanos: -1}); err == nil || value != nil {
		t.Fatalf("TimePtrFromTimestamp(invalid) = %#v, %v; want nil error", value, err)
	}
}
