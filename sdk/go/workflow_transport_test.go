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
	"google.golang.org/protobuf/types/known/structpb"
)

type workflowProviderHarness struct {
	gestalt.UnimplementedWorkflowProvider
	closeTracker
}

func (p *workflowProviderHarness) Configure(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

func (p *workflowProviderHarness) PlanWorkflow(_ context.Context, req *gestalt.PlanWorkflowRequest) (*gestalt.PlanWorkflowResponse, error) {
	return &gestalt.PlanWorkflowResponse{
		AcceptedSpecDigest:        req.SpecDigest,
		ProviderPlanID:            "plan-1",
		ProviderPlanDigest:        "plan-digest-1",
		SupportedFeatureFlags:     []string{"provider_interpreted_steps_v1"},
		ProviderPlanFormatVersion: "workflow-steps-v1",
	}, nil
}

func (p *workflowProviderHarness) ApplyWorkflowDeployment(_ context.Context, req *gestalt.ApplyWorkflowDeploymentRequest) (*gestalt.WorkflowDeployment, error) {
	return &gestalt.WorkflowDeployment{
		Spec:               req.Spec,
		Status:             gestalt.WorkflowDeploymentStatusValueActive,
		AppliedGeneration:  req.Spec.Generation,
		SpecDigest:         req.Binding.SpecDigest,
		TargetDigest:       req.Binding.TargetDigest,
		ActionTableDigest:  req.Binding.ActionTableDigest,
		ProviderPlanID:     req.Plan.ProviderPlanID,
		ProviderPlanDigest: req.Plan.ProviderPlanDigest,
		Binding:            req.Binding,
	}, nil
}

func (p *workflowProviderHarness) StartWorkflowRun(_ context.Context, req *gestalt.StartWorkflowRunRequest) (*gestalt.WorkflowRun, error) {
	return &gestalt.WorkflowRun{
		ID:                   "run-1",
		DeploymentID:         req.DeploymentID,
		DeploymentGeneration: req.DeploymentGeneration,
		WorkflowKey:          req.WorkflowKey,
		Status:               gestalt.WorkflowRunStatusValueRunning,
		Input:                req.Input,
	}, nil
}

func TestWorkflowProviderDeploymentTransport(t *testing.T) {
	socket := newSocketPath(t, "workflow.sock")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(proto.EnvProviderName, "workflow-test")

	ctx, cancel := context.WithCancel(context.Background())
	provider := &workflowProviderHarness{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeWorkflowProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("workflow provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	client := proto.NewWorkflowProviderClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	target := &proto.BoundWorkflowTarget{Steps: []*proto.WorkflowStep{{
		Id: "diagnose",
		Action: &proto.WorkflowStep_Plugin{Plugin: &proto.WorkflowStepPluginCall{
			Name:      "datadog",
			Operation: "query",
			Input: &proto.WorkflowValue{Kind: &proto.WorkflowValue_Object{Object: &proto.WorkflowObject{
				Fields: map[string]*proto.WorkflowValue{},
			}}},
		}},
	}}}
	spec := &proto.WorkflowDeploymentSpec{
		Id:         "deploy-1",
		Generation: 1,
		Target:     target,
	}

	plan, err := client.PlanWorkflow(rpcCtx, &proto.PlanWorkflowRequest{Spec: spec, SpecDigest: "spec-digest-1"})
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if plan.GetProviderPlanDigest() != "plan-digest-1" {
		t.Fatalf("provider plan digest = %q", plan.GetProviderPlanDigest())
	}

	deployment, err := client.ApplyWorkflowDeployment(rpcCtx, &proto.ApplyWorkflowDeploymentRequest{
		Spec: spec,
		Plan: plan,
		Binding: &proto.WorkflowDeploymentBinding{
			DeploymentId:         "deploy-1",
			DeploymentGeneration: 1,
			SpecDigest:           "spec-digest-1",
			TargetDigest:         "target-digest-1",
			ActionTableDigest:    "action-table-digest-1",
		},
	})
	if err != nil {
		t.Fatalf("ApplyWorkflowDeployment: %v", err)
	}
	if deployment.GetStatus() != proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_ACTIVE {
		t.Fatalf("deployment status = %v", deployment.GetStatus())
	}

	run, err := client.StartWorkflowRun(rpcCtx, &proto.StartWorkflowRunRequest{
		DeploymentId:         "deploy-1",
		DeploymentGeneration: 1,
		WorkflowKey:          "wf-key",
		Input:                &structpb.Struct{Fields: map[string]*structpb.Value{"severity": structpb.NewStringValue("high")}},
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if run.GetId() != "run-1" || run.GetWorkflowKey() != "wf-key" {
		t.Fatalf("run = %#v", run)
	}
}

type workflowManagerHarness struct {
	proto.UnimplementedWorkflowManagerHostServer

	mu      sync.Mutex
	applies []*proto.WorkflowManagerApplyDeploymentRequest
	signals []*proto.WorkflowManagerSignalOrStartRunRequest
	tokens  []string
}

func (h *workflowManagerHarness) ApplyDeployment(ctx context.Context, req *proto.WorkflowManagerApplyDeploymentRequest) (*proto.ManagedWorkflowDeployment, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.applies = append(h.applies, gproto.Clone(req).(*proto.WorkflowManagerApplyDeploymentRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowDeployment{
		ProviderName: req.GetProviderName(),
		Deployment: &proto.WorkflowDeployment{
			Spec:              req.GetSpec(),
			Status:            proto.WorkflowDeploymentStatus_WORKFLOW_DEPLOYMENT_STATUS_ACTIVE,
			AppliedGeneration: req.GetSpec().GetGeneration(),
		},
	}, nil
}

func (h *workflowManagerHarness) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.signals = append(h.signals, gproto.Clone(req).(*proto.WorkflowManagerSignalOrStartRunRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowRunSignal{
		ProviderName: req.GetProviderName(),
		Run: &proto.WorkflowRun{
			Id:           "run-1",
			DeploymentId: req.GetDeploymentId(),
			WorkflowKey:  req.GetWorkflowKey(),
			Input:        req.GetInput(),
			Status:       proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING,
		},
		Signal:      req.GetSignal(),
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func TestWorkflowManagerDeploymentTransport(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerHarness{}
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

	applied, err := client.ApplyDeployment(context.Background(), gestalt.WorkflowManagerApplyDeployment{
		ProviderName:   "managed",
		IdempotencyKey: "apply-key",
		Spec: &gestalt.WorkflowDeploymentSpec{
			ID:         "deploy-1",
			Generation: 2,
			Target: &gestalt.BoundWorkflowTarget{Steps: []gestalt.WorkflowStep{{
				ID: "diagnose",
				Plugin: &gestalt.WorkflowStepPluginCall{
					Name:      "datadog",
					Operation: "query",
				},
			}}},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDeployment: %v", err)
	}
	if applied.ProviderName != "managed" || applied.Deployment == nil || applied.Deployment.Status != gestalt.WorkflowDeploymentStatusValueActive {
		t.Fatalf("applied deployment = %#v", applied)
	}

	signaled, err := client.SignalOrStartRun(context.Background(), gestalt.WorkflowManagerSignalOrStartRun{
		ProviderName:         "managed",
		DeploymentID:         "deploy-1",
		DeploymentGeneration: 2,
		WorkflowKey:          "wf-key",
		Input:                map[string]any{"severity": "high"},
		IdempotencyKey:       "signal-key",
		Signal: &gestalt.WorkflowSignal{
			Name:    "incident.update",
			Payload: map[string]any{"ok": true},
		},
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}
	if signaled.Run == nil || signaled.Run.ID != "run-1" || !signaled.StartedRun {
		t.Fatalf("signaled run = %#v", signaled)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want two relay-token-go entries", harness.tokens)
	}
	if len(harness.applies) != 1 {
		t.Fatalf("apply requests len = %d, want 1", len(harness.applies))
	}
	if got := harness.applies[0].GetInvocationToken(); got != "parent-token" {
		t.Fatalf("apply invocation token = %q, want parent-token", got)
	}
	if got := harness.applies[0].GetIdempotencyKey(); got != "apply-key" {
		t.Fatalf("apply idempotency key = %q, want apply-key", got)
	}
	if got := harness.applies[0].GetSpec().GetId(); got != "deploy-1" {
		t.Fatalf("apply spec id = %q, want deploy-1", got)
	}
	if len(harness.signals) != 1 {
		t.Fatalf("signal requests len = %d, want 1", len(harness.signals))
	}
	got := harness.signals[0]
	if got.GetInvocationToken() != "parent-token" || got.GetIdempotencyKey() != "signal-key" {
		t.Fatalf("signal tokens = %q/%q", got.GetInvocationToken(), got.GetIdempotencyKey())
	}
	if got.GetInput().AsMap()["severity"] != "high" || got.GetSignal().GetPayload().AsMap()["ok"] != true {
		t.Fatalf("signal request payloads = %#v/%#v", got.GetInput().AsMap(), got.GetSignal().GetPayload().AsMap())
	}
}

type workflowHostHarness struct {
	proto.UnimplementedWorkflowHostServer

	mu       sync.Mutex
	requests []*proto.InvokeWorkflowActionRequest
	tokens   []string
}

func (h *workflowHostHarness) InvokeWorkflowAction(ctx context.Context, req *proto.InvokeWorkflowActionRequest) (*proto.WorkflowActionResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.InvokeWorkflowActionRequest))
	h.mu.Unlock()

	return &proto.WorkflowActionResult{
		ActionEventId: "action-event-1",
		Status:        202,
		Body:          req.GetSelector().GetActionId(),
	}, nil
}

func TestWorkflowHostActionTransport(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowHostHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowHostSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowHostSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowHost()
	if err != nil {
		t.Fatalf("WorkflowHost: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.InvokeWorkflowAction(context.Background(), gestalt.InvokeWorkflowActionInput{
		Selector: &gestalt.WorkflowHostActionSelector{
			RunID:         "run-1",
			StepID:        "diagnose",
			ActionID:      "step/diagnose/plugin",
			AttemptNumber: 1,
		},
		Plugin: &gestalt.WorkflowPluginActionPayload{Input: map[string]any{"query": "status"}},
	})
	if err != nil {
		t.Fatalf("InvokeWorkflowAction: %v", err)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != "step/diagnose/plugin" {
		t.Fatalf("response = %#v", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("invoke requests len = %d, want 1", len(harness.requests))
	}
}
