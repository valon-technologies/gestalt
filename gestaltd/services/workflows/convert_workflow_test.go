package workflows

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestWorkflowTargetProtoRoundTripsStepsOnly(t *testing.T) {
	t.Parallel()

	target := coreworkflow.Target{Steps: []coreworkflow.Step{
		{
			ID: "diagnose",
			Inputs: map[string]coreworkflow.Value{
				"text":  {SignalPayload: "event.text"},
				"event": {RunInput: "data.alert"},
			},
			Agent: &coreworkflow.AgentTurn{
				ProviderName: "openai",
				Model:        "gpt-5.5",
				SessionKey:   "incident",
				Prompt:       coreworkflow.Text{Template: "Diagnose ${inputs.text}"},
				ToolRefs:     []coreagent.ToolRef{{Plugin: "datadog", Operation: "query_monitor"}},
			},
		},
		{
			ID: "notify",
			Plugin: &coreworkflow.PluginCall{
				Name:      "slack",
				Operation: "reply",
				Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnose", Path: "agent.text"}},
				}},
			},
			When: &coreworkflow.StepWhen{
				Value:     coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnose", Path: "agent.structuredOutput.actionable"}},
				Equals:    true,
				EqualsSet: true,
			},
			OutputDelivery: &coreworkflow.StepDelivery{
				Plugin: &coreworkflow.PluginCall{Name: "audit", Operation: "record", Input: coreworkflow.Value{RunInput: "activation_id"}},
			},
		},
	}}

	pb, err := workflowTargetToProto(target)
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if got := len(pb.GetSteps()); got != 2 {
		t.Fatalf("steps len = %d, want 2", got)
	}
	if got := pb.GetSteps()[0].GetAgent().GetSessionKey().GetTemplate(); got != "incident" {
		t.Fatalf("session key template = %q, want incident", got)
	}

	roundTrip := workflowTargetFromProto(pb)
	if !coreworkflow.TargetsEqual(target, roundTrip) {
		t.Fatalf("round trip target mismatch:\n got: %#v\nwant: %#v", roundTrip, target)
	}
}

func TestWorkflowDeploymentProtoRoundTrips(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.May, 18, 10, 0, 0, 0, time.UTC)
	deployment := &coreworkflow.Deployment{
		Spec: coreworkflow.DeploymentSpec{
			ID:         "deploy-1",
			Generation: 3,
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID: "notify",
				Plugin: &coreworkflow.PluginCall{
					Name:      "slack",
					Operation: "reply",
					Input:     coreworkflow.Value{RunInput: "message"},
				},
			}}},
			Activations: []coreworkflow.Activation{{
				ID:             "manual",
				Manual:         true,
				Mode:           coreworkflow.ActivationModeStart,
				Input:          coreworkflow.Value{Literal: map[string]any{"source": "test"}, LiteralSet: true},
				RunKey:         coreworkflow.Value{Template: &coreworkflow.Text{Template: "run-${input.id}"}},
				IdempotencyKey: coreworkflow.Value{RunInput: "request_id"},
			}},
			RunAs:       &core.RunAsSubject{SubjectID: "service_account:workflow"},
			Permissions: []core.AccessPermission{{Plugin: "slack", Operations: []string{"reply"}}},
			Labels:      map[string]string{"team": "ops"},
		},
		Status:             coreworkflow.DeploymentStatusActive,
		CreatedAt:          &createdAt,
		UpdatedAt:          &createdAt,
		AppliedGeneration:  3,
		SpecDigest:         "sha256:spec",
		TargetDigest:       "sha256:target",
		ActionTableDigest:  "sha256:actions",
		ProviderPlanID:     "plan-1",
		ProviderPlanDigest: "sha256:plan",
		Binding: &coreworkflow.DeploymentBinding{
			ID:                   "binding-1",
			ExecutionRef:         "exec-1",
			DeploymentID:         "deploy-1",
			DeploymentGeneration: 3,
			ActionTableDigest:    "sha256:actions",
			RequestID:            "request-1",
		},
	}

	pb, err := workflowDeploymentToProto(deployment)
	if err != nil {
		t.Fatalf("workflowDeploymentToProto: %v", err)
	}
	if got := pb.GetSpec().GetActivations()[0].GetManual(); got == nil {
		t.Fatal("manual activation is nil")
	}
	if got := pb.GetBinding().GetActionTableDigest(); got != "sha256:actions" {
		t.Fatalf("binding action table digest = %q", got)
	}

	roundTrip, err := workflowDeploymentFromProto(pb)
	if err != nil {
		t.Fatalf("workflowDeploymentFromProto: %v", err)
	}
	if roundTrip.Spec.ID != "deploy-1" || roundTrip.Status != coreworkflow.DeploymentStatusActive {
		t.Fatalf("round trip deployment = %#v", roundTrip)
	}
	if len(roundTrip.Spec.Activations) != 1 || !roundTrip.Spec.Activations[0].Manual {
		t.Fatalf("round trip activations = %#v", roundTrip.Spec.Activations)
	}
	if roundTrip.Binding == nil || roundTrip.Binding.RequestID != "request-1" {
		t.Fatalf("round trip binding = %#v", roundTrip.Binding)
	}
}

func TestWorkflowRunProtoRoundTripsDeploymentFields(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.May, 18, 15, 0, 0, 0, time.UTC)
	run := &coreworkflow.Run{
		ID:                     "run-1",
		DeploymentID:           "deploy-1",
		DeploymentGeneration:   4,
		Status:                 coreworkflow.RunStatusRunning,
		WorkflowKey:            "datadog-root",
		ExecutionRef:           "workflow_run:ref",
		ExecutionRefGeneration: 9,
		TargetDigest:           "sha256:target",
		SpecDigest:             "sha256:spec",
		ActionTableDigest:      "sha256:actions",
		PlanDigest:             "sha256:plan",
		Input:                  map[string]any{"message": "hello"},
		Trigger: coreworkflow.RunTrigger{
			DeploymentID:         "deploy-1",
			DeploymentGeneration: 4,
			ActivationID:         "manual",
			Manual:               true,
		},
		Steps: []coreworkflow.StepState{{
			StepID:        "diagnose",
			StepIndex:     0,
			Status:        coreworkflow.StepStatusSucceeded,
			AttemptNumber: 1,
			OutputSummary: &coreworkflow.OutputSummary{
				EnvelopeVersion: "workflow.output.v1",
				Kind:            "agent",
				SizeBytes:       128,
				SHA256:          "sha256:output",
				Truncated:       true,
				Redacted:        true,
				MediaType:       "application/json",
			},
			OutputRef: "out-1",
			UpdatedAt: &updatedAt,
		}},
	}

	pb, err := workflowRunToProto(run)
	if err != nil {
		t.Fatalf("workflowRunToProto: %v", err)
	}
	if pb.GetDeploymentId() != "deploy-1" || pb.GetExecutionRefGeneration() != 9 {
		t.Fatalf("run deployment/execution fields = %#v", pb)
	}
	if got := pb.GetInput().AsMap()["message"]; got != "hello" {
		t.Fatalf("input message = %#v", got)
	}

	roundTrip, err := workflowRunFromProto(pb)
	if err != nil {
		t.Fatalf("workflowRunFromProto: %v", err)
	}
	if roundTrip.DeploymentID != "deploy-1" || roundTrip.ActionTableDigest != "sha256:actions" {
		t.Fatalf("round trip digests = %#v", roundTrip)
	}
	if len(roundTrip.Steps) != 1 || roundTrip.Steps[0].OutputSummary == nil || roundTrip.Steps[0].OutputSummary.SHA256 != "sha256:output" {
		t.Fatalf("round trip steps = %#v", roundTrip.Steps)
	}
}

func TestWorkflowActionRequestFromProtoIncludesDeploymentSelector(t *testing.T) {
	t.Parallel()

	input, err := structpb.NewStruct(map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	req, err := workflowActionRequestFromProto(&proto.InvokeWorkflowActionRequest{
		Selector: &proto.WorkflowHostActionSelector{
			ExecutionRef:      "exec-1",
			ActionTableDigest: "sha256:actions",
		},
		Action: &proto.InvokeWorkflowActionRequest_Plugin{
			Plugin: &proto.WorkflowPluginActionPayload{Input: input},
		},
	})
	if err != nil {
		t.Fatalf("workflowActionRequestFromProto: %v", err)
	}
	if req.Selector.ExecutionRef != "exec-1" || req.Selector.ActionTableDigest != "sha256:actions" {
		t.Fatalf("selector = %#v", req.Selector)
	}
	if req.Plugin == nil || req.Plugin.Input["text"] != "hello" {
		t.Fatalf("plugin payload = %#v", req.Plugin)
	}
}
