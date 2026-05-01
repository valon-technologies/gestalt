package workflows

import (
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestWorkflowTargetToProtoUsesNestedPluginTarget(t *testing.T) {
	t.Parallel()

	target, err := workflowTargetToProto(coreworkflow.Target{
		Plugin: &coreworkflow.PluginTarget{
			PluginName: "demo",
			Operation:  "refresh",
			Connection: "workspace",
			Instance:   "primary",
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
				PluginName: " demo ",
				Operation:  " refresh ",
				Connection: " workspace ",
				Instance:   " primary ",
				Input:      input,
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
				PluginName: "notification",
				Operation:  "reply",
				Input:      map[string]any{"format": "plain"},
			},
			CredentialMode: core.ConnectionModeNone,
			InputBindings: []coreworkflow.OutputBinding{
				{InputField: "text", Value: coreworkflow.OutputValueSource{AgentOutput: "text"}},
				{InputField: "ref", Value: coreworkflow.OutputValueSource{SignalPayload: "reply_ref"}},
				{InputField: "source", Value: coreworkflow.OutputValueSource{Literal: "workflow"}},
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
