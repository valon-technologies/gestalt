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

func protoValueFromAnyMust(value any) *structpb.Value {
	out, err := protoValueFromAny(value)
	if err != nil {
		panic(err)
	}
	return out
}

func TestWorkflowTargetToProtoUsesStepPluginTarget(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{
		Steps: []coreworkflow.Step{{
			ID: "refresh",
			Plugin: &coreworkflow.PluginCall{
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
	if len(target.GetSteps()) != 1 || target.GetSteps()[0].GetPlugin() == nil {
		t.Fatalf("steps target = %#v, want one plugin step", target)
	}
	plugin := target.GetSteps()[0].GetPlugin()
	if got := plugin.GetName(); got != "demo" {
		t.Fatalf("plugin name = %q, want %q", got, "demo")
	}
	if got := plugin.GetCredentialMode(); got != string(core.ConnectionModeNone) {
		t.Fatalf("credential_mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := plugin.GetInput().GetObject().GetFields()["customer_id"].GetLiteral().AsInterface(); got != "cust_123" {
		t.Fatalf("input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetFromProtoAcceptsStepPluginFields(t *testing.T) {
	t.Parallel()

	target := workflowTargetFromProto(&proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{
			Id: " refresh ",
			Action: &proto.WorkflowStep_Plugin{Plugin: &proto.WorkflowStepPluginCall{
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
	if len(target.Steps) != 1 || target.Steps[0].Plugin == nil {
		t.Fatalf("target steps = %#v, want one plugin step", target.Steps)
	}
	plugin := target.Steps[0].Plugin
	if got := plugin.Name; got != "demo" {
		t.Fatalf("plugin name = %q, want %q", got, "demo")
	}
	if got := plugin.Operation; got != "refresh" {
		t.Fatalf("operation = %q, want %q", got, "refresh")
	}
	if got := plugin.Connection; got != "workspace" {
		t.Fatalf("connection = %q, want %q", got, "workspace")
	}
	if got := plugin.Instance; got != "primary" {
		t.Fatalf("instance = %q, want %q", got, "primary")
	}
	if got := plugin.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := plugin.Input.Object["customer_id"].Literal; got != "cust_123" {
		t.Fatalf("input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetStepsProtoRoundTrip(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{
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
					ToolRefs:     []coreagent.ToolRef{{Plugin: "datadog", Operation: "queryLogs"}},
					ResponseSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"actionableForPr": map[string]any{"type": "boolean"}},
					},
					ModelOptions: map[string]any{"temperature": 0},
				},
				Metadata:       map[string]any{"kind": "diagnosis"},
				TimeoutSeconds: 45,
			},
			{
				ID: "reply",
				Plugin: &coreworkflow.PluginCall{
					Name:      "slack",
					Operation: "reply",
					Input: coreworkflow.Value{Object: map[string]coreworkflow.Value{
						"text": {StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.text"}},
					}},
				},
			},
			{
				ID: "pr_fix",
				Agent: &coreworkflow.AgentTurn{
					ProviderName: "managed",
					Model:        "deep",
					Prompt:       coreworkflow.Text{Template: "Open the PR."},
					ToolRefs:     []coreagent.ToolRef{{Plugin: "github", Operation: "createPullRequest"}},
				},
				When: &coreworkflow.StepWhen{
					Value:     coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{StepID: "diagnosis", Path: "agent.structuredOutput.actionableForPr"}},
					Equals:    true,
					EqualsSet: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if got := target.GetSteps()[0].GetAgent().GetResponseSchema().AsMap()["type"]; got != "object" {
		t.Fatalf("step response schema = %#v, want object", got)
	}
	if got := target.GetSteps()[2].GetWhen().GetEquals().GetBoolValue(); got != true {
		t.Fatalf("step when equals = %v, want true", got)
	}

	roundTrip := workflowTargetFromProto(target)
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
	if len(diagnosis.Agent.ToolRefs) != 1 || diagnosis.Agent.ToolRefs[0].Plugin != "datadog" || diagnosis.Agent.ToolRefs[0].Operation != "queryLogs" {
		t.Fatalf("round trip diagnosis tool refs = %#v", diagnosis.Agent.ToolRefs)
	}
	reply := roundTrip.Steps[1]
	if reply.Plugin == nil || reply.Plugin.Name != "slack" || reply.Plugin.Operation != "reply" {
		t.Fatalf("round trip reply step = %#v", reply)
	}
	prFix := roundTrip.Steps[2]
	if prFix.When == nil || prFix.When.Value.StepOutput.StepID != "diagnosis" || prFix.When.Value.StepOutput.Path != "agent.structuredOutput.actionableForPr" || prFix.When.Equals != true {
		t.Fatalf("round trip pr_fix when = %#v", prFix.When)
	}
}

func TestWorkflowTargetFromProtoPreservesEmptyStepPlugin(t *testing.T) {
	t.Parallel()

	target := workflowTargetFromProto(&proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{Action: &proto.WorkflowStep_Plugin{Plugin: &proto.WorkflowStepPluginCall{}}}},
	})
	if len(target.Steps) != 1 || target.Steps[0].Plugin == nil {
		t.Fatalf("steps = %#v, want empty plugin step", target.Steps)
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

func TestWorkflowExecutionReferenceProtoRoundTripsRunAsSubject(t *testing.T) {
	t.Parallel()

	ref := &coreworkflow.ExecutionReference{
		ID:                  "workflow_schedule:cfg_123:ref",
		ProviderName:        "temporal",
		Target:              coreworkflow.Target{Steps: []coreworkflow.Step{{ID: "sync", Plugin: &coreworkflow.PluginCall{Name: "brain", Operation: "sources.sync"}}}},
		SourceDefinitionID:  "workflow_definition:def-123",
		SubjectID:           "system:config",
		SubjectKind:         "system",
		DisplayName:         "Gestalt config",
		AuthSource:          "config",
		CredentialSubjectID: "system:config",
		RunAs: &core.RunAsSubject{
			SubjectID:   "service_account:brain-sync",
			DisplayName: "Brain sync",
			AuthSource:  "config",
		},
		Permissions: []core.AccessPermission{{
			Plugin:     "brain",
			Operations: []string{"sources.sync"},
		}},
	}
	pb, err := workflowExecutionReferenceToProto(ref)
	if err != nil {
		t.Fatalf("workflowExecutionReferenceToProto: %v", err)
	}
	if pb.GetSubjectId() != "system:config" || pb.GetCredentialSubjectId() != "system:config" {
		t.Fatalf("owner fields = (%q, %q), want config owner", pb.GetSubjectId(), pb.GetCredentialSubjectId())
	}
	if pb.GetRunAs().GetSubjectId() != "service_account:brain-sync" || pb.GetRunAs().GetSubjectKind() != "service_account" {
		t.Fatalf("runAs proto = %#v", pb.GetRunAs())
	}
	if pb.GetSourceDefinitionId() != "workflow_definition:def-123" {
		t.Fatalf("source definition id = %q, want stored definition id", pb.GetSourceDefinitionId())
	}
	roundTrip, err := workflowExecutionReferenceFromProto(pb)
	if err != nil {
		t.Fatalf("workflowExecutionReferenceFromProto: %v", err)
	}
	if roundTrip.SubjectID != "system:config" || roundTrip.CredentialSubjectID != "system:config" {
		t.Fatalf("round trip owner fields = (%q, %q)", roundTrip.SubjectID, roundTrip.CredentialSubjectID)
	}
	if roundTrip.RunAs == nil || roundTrip.RunAs.SubjectID != "service_account:brain-sync" || roundTrip.RunAs.CredentialSubjectID != "service_account:brain-sync" {
		t.Fatalf("round trip runAs = %#v", roundTrip.RunAs)
	}
	if roundTrip.SourceDefinitionID != "workflow_definition:def-123" {
		t.Fatalf("round trip source definition id = %q", roundTrip.SourceDefinitionID)
	}
}

func TestWorkflowRunTriggerToProtoPrefersScheduleOverManual(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 30, 0, 0, time.UTC)
	trigger, err := workflowRunTriggerToProto(coreworkflow.RunTrigger{
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
