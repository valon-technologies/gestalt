package workflows

import (
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestWorkflowTargetToProtoUsesNestedPluginTarget(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{
		Plugin: &coreworkflow.PluginTarget{
			PluginName:     "demo",
			Operation:      "refresh",
			Connection:     "workspace",
			Instance:       "primary",
			CredentialMode: core.ConnectionModeNone,
			Input: map[string]any{
				"customer_id": "cust_123",
			},
		},
	})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if target.GetPlugin() == nil {
		t.Fatal("nested plugin target is nil")
	}
	if got := target.GetPlugin().GetPluginName(); got != "demo" {
		t.Fatalf("nested plugin_name = %q, want %q", got, "demo")
	}
	if got := target.GetPlugin().GetCredentialMode(); got != string(core.ConnectionModeNone) {
		t.Fatalf("nested credential_mode = %q, want %q", got, core.ConnectionModeNone)
	}
	input := mapFromStruct(target.GetPlugin().GetInput())
	if got := input["customer_id"]; got != "cust_123" {
		t.Fatalf("nested input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetFromProtoAcceptsNestedPluginFields(t *testing.T) {
	t.Parallel()

	input, err := structpb.NewStruct(map[string]any{
		"customer_id": "cust_123",
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	target := workflowTargetFromProto(&proto.BoundWorkflowTarget{
		Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{
				PluginName:     " demo ",
				Operation:      " refresh ",
				Connection:     " workspace ",
				Instance:       " primary ",
				CredentialMode: " none ",
				Input:          input,
			},
		},
	})
	if target.Plugin == nil {
		t.Fatal("plugin target is nil")
	}
	if got := target.Plugin.PluginName; got != "demo" {
		t.Fatalf("plugin name = %q, want %q", got, "demo")
	}
	if got := target.Plugin.Operation; got != "refresh" {
		t.Fatalf("operation = %q, want %q", got, "refresh")
	}
	if got := target.Plugin.Connection; got != "workspace" {
		t.Fatalf("connection = %q, want %q", got, "workspace")
	}
	if got := target.Plugin.Instance; got != "primary" {
		t.Fatalf("instance = %q, want %q", got, "primary")
	}
	if got := target.Plugin.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := target.Plugin.Input["customer_id"]; got != "cust_123" {
		t.Fatalf("input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowAgentTargetProtoRoundTrips(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName: "managed",
		Prompt:       "Sync roadmap",
		OutputDelivery: &coreworkflow.OutputDelivery{
			Target: coreworkflow.PluginTarget{
				PluginName:     "notification",
				Operation:      "reply",
				CredentialMode: core.ConnectionModeUser,
				Input:          map[string]any{"format": "plain"},
			},
			CredentialMode: core.ConnectionModeNone,
			InputBindings: []coreworkflow.OutputBinding{
				{InputField: "text", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
				{InputField: "ref", Value: coreworkflow.OutputValueSource{SignalPayload: "reply_ref"}},
				{InputField: "source", Value: coreworkflow.OutputValueSource{Literal: "workflow"}},
			},
		},
		SessionReadyDelivery: &coreworkflow.OutputDelivery{
			Target: coreworkflow.PluginTarget{
				PluginName: "notification",
				Operation:  "started",
				Input:      map[string]any{"format": "plain"},
			},
			CredentialMode: core.ConnectionModeNone,
			InputBindings: []coreworkflow.OutputBinding{
				{InputField: "session_id", Value: coreworkflow.OutputValueSource{AgentSession: "id"}},
				{InputField: "reply_ref", Value: coreworkflow.OutputValueSource{SignalPayload: "reply_ref"}},
			},
		},
	}})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if target.GetAgent() == nil {
		t.Fatal("nested agent target is nil")
	}
	if target.GetAgent().GetOutputDelivery().GetTarget().GetPluginName() != "notification" {
		t.Fatalf("output delivery = %#v", target.GetAgent().GetOutputDelivery())
	}
	if target.GetAgent().GetOutputDelivery().GetCredentialMode() != string(core.ConnectionModeNone) {
		t.Fatalf("output delivery credential mode = %q", target.GetAgent().GetOutputDelivery().GetCredentialMode())
	}
	if target.GetAgent().GetSessionReadyDelivery().GetTarget().GetPluginName() != "notification" {
		t.Fatalf("session ready delivery = %#v", target.GetAgent().GetSessionReadyDelivery())
	}
	if target.GetAgent().GetSessionReadyDelivery().GetInputBindings()[0].GetValue().GetAgentSession() != "id" {
		t.Fatalf("session ready delivery agent session source = %#v", target.GetAgent().GetSessionReadyDelivery().GetInputBindings())
	}
	if got := target.GetAgent().GetOutputDelivery().GetTarget().GetCredentialMode(); got != "" {
		t.Fatalf("output delivery nested target credential mode = %q, want empty", got)
	}
	roundTrip := workflowTargetFromProto(target)
	if roundTrip.Agent == nil || roundTrip.Agent.ProviderName != "managed" {
		t.Fatalf("round trip agent target = %#v", roundTrip.Agent)
	}
	if roundTrip.Agent.OutputDelivery == nil {
		t.Fatalf("round trip output delivery is nil")
	}
	if got := roundTrip.Agent.OutputDelivery.Target.Input["format"]; got != "plain" {
		t.Fatalf("round trip output delivery input = %#v", roundTrip.Agent.OutputDelivery.Target.Input)
	}
	if len(roundTrip.Agent.OutputDelivery.InputBindings) != 3 {
		t.Fatalf("round trip output delivery bindings = %#v", roundTrip.Agent.OutputDelivery.InputBindings)
	}
	if got := roundTrip.Agent.OutputDelivery.CredentialMode; got != core.ConnectionModeNone {
		t.Fatalf("round trip output delivery credential mode = %q, want %q", got, core.ConnectionModeNone)
	}
	if got := roundTrip.Agent.OutputDelivery.InputBindings[2].Value.Literal; got != "workflow" {
		t.Fatalf("round trip literal = %#v", got)
	}
	if roundTrip.Agent.SessionReadyDelivery == nil {
		t.Fatalf("round trip session ready delivery is nil")
	}
	if got := roundTrip.Agent.SessionReadyDelivery.InputBindings[0].Value.AgentSession; got != "id" {
		t.Fatalf("round trip agent session source = %q, want id", got)
	}
}

func TestWorkflowAgentTargetStepsProtoRoundTrip(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName: "managed",
		Model:        "deep",
		Metadata:     map[string]any{"route": "datadog"},
		Steps: []coreworkflow.AgentStep{
			{
				ID:       "diagnosis",
				Prompt:   "Diagnose the alert.",
				Messages: []coreagent.Message{{Role: "system", Text: "Use concise replies."}},
				ToolRefs: []coreagent.ToolRef{{Plugin: "datadog", Operation: "queryLogs"}},
				ResponseSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"actionable_for_pr": map[string]any{"type": "boolean"}},
				},
				ModelOptions:   map[string]any{"temperature": 0},
				Metadata:       map[string]any{"kind": "diagnosis"},
				TimeoutSeconds: 45,
				OutputDelivery: &coreworkflow.OutputDelivery{
					Target: coreworkflow.PluginTarget{PluginName: "slack", Operation: "reply"},
					InputBindings: []coreworkflow.OutputBinding{
						{InputField: "text", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
					},
				},
			},
			{
				ID:       "pr_fix",
				Prompt:   "Open the PR.",
				ToolRefs: []coreagent.ToolRef{{Plugin: "github", Operation: "createPullRequest"}},
				When: &coreworkflow.AgentStepWhen{
					StepID:     "diagnosis",
					OutputPath: "structured_output.actionable_for_pr",
					Equals:     true,
					EqualsSet:  true,
				},
			},
		},
	}})
	if err != nil {
		t.Fatalf("workflowTargetToProto: %v", err)
	}
	if got := target.GetAgent().GetSteps()[0].GetResponseSchema().AsMap()["type"]; got != "object" {
		t.Fatalf("step response schema = %#v, want object", got)
	}
	if got := target.GetAgent().GetSteps()[1].GetWhen().GetEquals().GetBoolValue(); got != true {
		t.Fatalf("step when equals = %v, want true", got)
	}

	roundTrip := workflowTargetFromProto(target)
	if roundTrip.Agent == nil || len(roundTrip.Agent.Steps) != 2 {
		t.Fatalf("round trip agent steps = %#v", roundTrip.Agent)
	}
	diagnosis := roundTrip.Agent.Steps[0]
	if diagnosis.ID != "diagnosis" || diagnosis.Prompt != "Diagnose the alert." || diagnosis.TimeoutSeconds != 45 {
		t.Fatalf("round trip diagnosis step = %#v", diagnosis)
	}
	if len(diagnosis.Messages) != 1 || diagnosis.Messages[0].Role != "system" {
		t.Fatalf("round trip diagnosis messages = %#v", diagnosis.Messages)
	}
	if len(diagnosis.ToolRefs) != 1 || diagnosis.ToolRefs[0].Plugin != "datadog" || diagnosis.ToolRefs[0].Operation != "queryLogs" {
		t.Fatalf("round trip diagnosis tool refs = %#v", diagnosis.ToolRefs)
	}
	if diagnosis.OutputDelivery == nil || diagnosis.OutputDelivery.Target.PluginName != "slack" {
		t.Fatalf("round trip diagnosis output delivery = %#v", diagnosis.OutputDelivery)
	}
	prFix := roundTrip.Agent.Steps[1]
	if prFix.When == nil || prFix.When.StepID != "diagnosis" || prFix.When.OutputPath != "structured_output.actionable_for_pr" || prFix.When.Equals != true {
		t.Fatalf("round trip pr_fix when = %#v", prFix.When)
	}
}

func TestWorkflowAgentTargetSessionReadyDeliveryProtoErrorsUseFieldName(t *testing.T) {
	t.Parallel()

	_, err := workflowTargetToProto(coreworkflow.Target{Agent: &coreworkflow.AgentTarget{
		ProviderName: "managed",
		Prompt:       "reply",
		SessionReadyDelivery: &coreworkflow.OutputDelivery{
			Target: coreworkflow.PluginTarget{
				PluginName: "notification",
				Operation:  "started",
			},
			InputBindings: []coreworkflow.OutputBinding{
				{InputField: "session_id", Value: coreworkflow.OutputValueSource{}},
			},
		},
	}})
	if err == nil {
		t.Fatal("workflowTargetToProto error is nil")
	}
	if !strings.Contains(err.Error(), "workflow agent session_ready_delivery.input_bindings[0].value") {
		t.Fatalf("workflowTargetToProto error = %v, want session_ready_delivery field name", err)
	}
}

func TestWorkflowTargetFromProtoPreservesEmptyPluginKind(t *testing.T) {
	t.Parallel()

	target := workflowTargetFromProto(&proto.BoundWorkflowTarget{
		Kind: &proto.BoundWorkflowTarget_Plugin{Plugin: &proto.BoundWorkflowPluginTarget{}},
	})
	if target.Plugin == nil {
		t.Fatal("plugin target is nil")
	}
}

func TestWorkflowExecutionReferenceProtoRoundTripsRunAsSubject(t *testing.T) {
	t.Parallel()

	ref := &coreworkflow.ExecutionReference{
		ID:                  "workflow_schedule:cfg_123:ref",
		ProviderName:        "temporal",
		Target:              coreworkflow.Target{Plugin: &coreworkflow.PluginTarget{PluginName: "brain", Operation: "sources.sync"}},
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
