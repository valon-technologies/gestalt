package workflowwire

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func protoValueFromAnyMust(value any) *structpb.Value {
	out, err := protoutil.ValueFromAny(value)
	if err != nil {
		panic(err)
	}
	return out
}

func TestWorkflowTargetToProtoUsesAppStep(t *testing.T) {
	t.Parallel()

	target, err := TargetToProto(coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID: "refresh",
			App: &coreworkflow.AppCall{
				Name:           "demo",
				Operation:      "refresh",
				Connection:     "workspace",
				Instance:       "primary",
				CredentialMode: core.ConnectionModeNone,
				Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
					"customer_id": {Literal: "cust_123", LiteralSet: true},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if len(target.GetSteps()) != 1 || target.GetSteps()[0].GetApp() == nil {
		t.Fatalf("steps target = %#v, want one app step", target)
	}
	app := target.GetSteps()[0].GetApp()
	if got := app.GetName(); got != "demo" {
		t.Fatalf("app name = %q, want %q", got, "demo")
	}
	if got := app.GetCredentialMode(); got != string(core.ConnectionModeNone) {
		t.Fatalf("credential_mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := app.GetInput().GetObject().GetFields()["customer_id"].GetLiteral().AsInterface(); got != "cust_123" {
		t.Fatalf("input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetFromProtoAcceptsStepAppFields(t *testing.T) {
	t.Parallel()

	target := TargetFromProto(&proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{
			Id: " refresh ",
			Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
				Name:           " demo ",
				Operation:      " refresh ",
				Connection:     " workspace ",
				Instance:       " primary ",
				CredentialMode: " none ",
				Input: &proto.WorkflowValue{Kind: &proto.WorkflowValue_Object{Object: &proto.WorkflowObject{Fields: map[string]*proto.WorkflowValue{
					"customer_id": {Kind: &proto.WorkflowValue_Literal{Literal: protoValueFromAnyMust("cust_123")}},
				}}}},
			}},
		}},
	})
	if len(target.Steps) != 1 || target.Steps[0].App == nil {
		t.Fatalf("target steps = %#v, want one app step", target.Steps)
	}
	app := target.Steps[0].App
	if got := app.Name; got != "demo" {
		t.Fatalf("app name = %q, want %q", got, "demo")
	}
	if got := app.Operation; got != "refresh" {
		t.Fatalf("operation = %q, want %q", got, "refresh")
	}
	if got := app.Connection; got != "workspace" {
		t.Fatalf("connection = %q, want %q", got, "workspace")
	}
	if got := app.Instance; got != "primary" {
		t.Fatalf("instance = %q, want %q", got, "primary")
	}
	if got := app.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := app.Input.Object["customer_id"].Literal; got != "cust_123" {
		t.Fatalf("input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetStepsProtoRoundTrip(t *testing.T) {
	t.Parallel()

	target, err := TargetToProto(coreworkflow.Target{
		Steps: []coreworkflow.Step{
			{
				ID: "diagnosis",
				Inputs: map[string]coreworkflow.Value{
					"thread_ts": {SignalPayload: "event.ts"},
				},
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "managed",
					Model:        "deep",
					Prompt:       coreworkflow.Text{Template: "Diagnose the alert."},
					Messages:     []coreworkflow.AgentMessage{{Role: "system", Text: coreworkflow.Text{Template: "Use concise replies."}}},
					ToolRefs:     []coreagent.ToolRef{{App: "datadog", Operation: "queryLogs"}},
					Output: coreagent.Output{
						Structured: &coreagent.StructuredOutput{Schema: map[string]any{
							"type":       "object",
							"properties": map[string]any{"actionableForPr": map[string]any{"type": "boolean"}},
						}},
					},
					ModelOptions: map[string]any{"temperature": 0},
				},
				Metadata:       map[string]any{"kind": "diagnosis"},
				TimeoutSeconds: 45,
			},
			{
				ID: "reply",
				App: &coreworkflow.AppCall{
					Name:      "slack",
					Operation: "reply",
					Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.output.structured.text"}},
					}},
				},
			},
			{
				ID: "pr_fix",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "managed",
					Model:        "deep",
					Prompt:       coreworkflow.Text{Template: "Open the PR."},
					ToolRefs:     []coreagent.ToolRef{{App: "github", Operation: "createPullRequest"}},
					Output:       coreagent.Output{Text: &coreagent.TextOutput{}},
				},
				When: &coreworkflow.StepWhen{
					Value:     coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.output.structured.value.actionableForPr"}},
					Equals:    true,
					EqualsSet: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if got := target.GetSteps()[0].GetAgent().GetOutput().GetStructured().GetSchema().AsMap()["type"]; got != "object" {
		t.Fatalf("step response schema = %#v, want object", got)
	}
	if got := target.GetSteps()[2].GetWhen().GetEquals().GetBoolValue(); got != true {
		t.Fatalf("step when equals = %v, want true", got)
	}

	roundTrip := TargetFromProto(target)
	if len(roundTrip.Steps) != 3 {
		t.Fatalf("round trip steps = %#v", roundTrip.Steps)
	}
	diagnosis := roundTrip.Steps[0]
	if diagnosis.ID != "diagnosis" || diagnosis.Agent == nil || diagnosis.Agent.Prompt.Template != "Diagnose the alert." || diagnosis.TimeoutSeconds != 45 {
		t.Fatalf("round trip diagnosis step = %#v", diagnosis)
	}
	if len(diagnosis.Agent.Messages) != 1 || diagnosis.Agent.Messages[0].Role != "system" {
		t.Fatalf("round trip diagnosis messages = %#v", diagnosis.Agent.Messages)
	}
	if len(diagnosis.Agent.ToolRefs) != 1 || diagnosis.Agent.ToolRefs[0].App != "datadog" || diagnosis.Agent.ToolRefs[0].Operation != "queryLogs" {
		t.Fatalf("round trip diagnosis tool refs = %#v", diagnosis.Agent.ToolRefs)
	}
	reply := roundTrip.Steps[1]
	if reply.App == nil || reply.App.Name != "slack" || reply.App.Operation != "reply" {
		t.Fatalf("round trip reply step = %#v", reply)
	}
	prFix := roundTrip.Steps[2]
	if prFix.When == nil || prFix.When.Value.StepOutput.StepID != "diagnosis" || prFix.When.Value.StepOutput.Path != "agent.output.structured.value.actionableForPr" || prFix.When.Equals != true {
		t.Fatalf("round trip pr_fix when = %#v", prFix.When)
	}
}

func TestWorkflowTargetFromProtoPreservesEmptyStepApp(t *testing.T) {
	t.Parallel()

	target := TargetFromProto(&proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{}}}},
	})
	if len(target.Steps) != 1 || target.Steps[0].App == nil {
		t.Fatalf("steps = %#v, want empty app step", target.Steps)
	}
}

func TestWorkflowValueProtoRoundTripPreservesEmptyCollections(t *testing.T) {
	t.Parallel()

	value := coreworkflow.Value{Object: map[string]coreworkflow.Value{
		"empty_object": {Object: map[string]coreworkflow.Value{}},
		"empty_array":  {Array: []coreworkflow.Value{}},
	}}

	pb, err := workflowValueToProto(value)
	if err != nil {
		t.Fatalf("workflowValueToProto: %v", err)
	}
	if pb.GetObject() == nil {
		t.Fatalf("proto value = %#v, want object", pb)
	}
	if pb.GetObject().GetFields()["empty_object"].GetObject() == nil {
		t.Fatalf("empty object proto = %#v, want object kind", pb.GetObject().GetFields()["empty_object"])
	}
	if pb.GetObject().GetFields()["empty_array"].GetArray() == nil {
		t.Fatalf("empty array proto = %#v, want array kind", pb.GetObject().GetFields()["empty_array"])
	}

	roundTrip := workflowValueFromProto(pb)
	if got := roundTrip.Object["empty_object"].Object; got == nil || len(got) != 0 {
		t.Fatalf("round trip empty object = %#v, want empty object", got)
	}
	if got := roundTrip.Object["empty_array"].Array; got == nil || len(got) != 0 {
		t.Fatalf("round trip empty array = %#v, want empty array", got)
	}
}

func TestWorkflowEventToProtoEncodesNilExtensionAsNullValue(t *testing.T) {
	t.Parallel()

	event, err := EventToProto(coreworkflow.Event{
		ID:          "event-1",
		Source:      "test",
		SpecVersion: "1.0",
		Type:        "demo.created",
		Extensions:  map[string]any{"missing": nil},
	})
	if err != nil {
		t.Fatalf("EventToProto: %v", err)
	}
	extension := event.GetExtensions()["missing"]
	if extension == nil || extension.GetNullValue() != structpb.NullValue_NULL_VALUE {
		t.Fatalf("nil extension = %#v, want protobuf null value", extension)
	}
	if _, err := protojson.Marshal(event); err != nil {
		t.Fatalf("marshal event with nil extension: %v", err)
	}
}

func TestWorkflowRunTriggerToProtoPrefersScheduleOverManual(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	trigger, err := RunTriggerToProto(coreworkflow.RunTrigger{
		Manual: true,
		Schedule: &coreworkflow.ScheduleTrigger{
			ScheduleID:   "sched-1",
			ScheduledFor: &scheduledFor,
		},
	})
	if err != nil {
		t.Fatalf("workflowRunTriggerToProto: %v", err)
	}
	if trigger == nil || trigger.GetSchedule() == nil {
		t.Fatalf("trigger = %#v, want schedule trigger", trigger)
	}
	if got := trigger.GetSchedule().GetScheduleId(); got != "sched-1" {
		t.Fatalf("schedule id = %q, want %q", got, "sched-1")
	}
	if got := trigger.GetManual(); got != nil {
		t.Fatalf("manual trigger = %#v, want nil", got)
	}
}
