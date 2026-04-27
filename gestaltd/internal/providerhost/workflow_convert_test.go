package providerhost

import (
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestWorkflowTargetToProtoKeepsFlatPluginFieldsForProviderCompatibility(t *testing.T) {
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
	if got := target.GetPluginName(); got != "demo" {
		t.Fatalf("flat plugin_name = %q, want %q", got, "demo")
	}
	if got := target.GetOperation(); got != "refresh" {
		t.Fatalf("flat operation = %q, want %q", got, "refresh")
	}
	if got := target.GetConnection(); got != "workspace" {
		t.Fatalf("flat connection = %q, want %q", got, "workspace")
	}
	if got := target.GetInstance(); got != "primary" {
		t.Fatalf("flat instance = %q, want %q", got, "primary")
	}
	input := mapFromStruct(target.GetInput())
	if got := input["customer_id"]; got != "cust_123" {
		t.Fatalf("flat input customer_id = %#v, want %q", got, "cust_123")
	}
}

func TestWorkflowTargetFromProtoAcceptsFlatPluginFieldsForProviderCompatibility(t *testing.T) {
	t.Parallel()

	input, err := structpb.NewStruct(map[string]any{
		"customer_id": "cust_123",
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	target := workflowTargetFromProto(&proto.BoundWorkflowTarget{
		PluginName: " demo ",
		Operation:  " refresh ",
		Connection: " workspace ",
		Instance:   " primary ",
		Input:      input,
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

func TestWorkflowTargetFromProtoRejectsFlatPluginAndAgent(t *testing.T) {
	t.Parallel()

	_, err := workflowTargetFromProtoStrict(&proto.BoundWorkflowTarget{
		PluginName: "demo",
		Agent: &proto.BoundWorkflowAgentTarget{
			ProviderName: "openai",
		},
	})
	if err == nil {
		t.Fatal("workflowTargetFromProtoStrict succeeded, want error")
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
