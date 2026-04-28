package providerhost

import (
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
		Plugin: &proto.BoundWorkflowPluginTarget{
			PluginName: " demo ",
			Operation:  " refresh ",
			Connection: " workspace ",
			Instance:   " primary ",
			Input:      input,
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

func TestWorkflowTargetFromProtoRejectsNestedPluginAndAgent(t *testing.T) {
	t.Parallel()

	_, err := workflowTargetFromProtoStrict(&proto.BoundWorkflowTarget{
		Plugin: &proto.BoundWorkflowPluginTarget{
			PluginName: "demo",
		},
		Agent: &proto.BoundWorkflowAgentTarget{
			ProviderName: "openai",
		},
	})
	if err == nil {
		t.Fatal("workflowTargetFromProtoStrict succeeded, want error")
	}
}

func TestWorkflowPublishEventRequestCarriesPrivateInputAndPublisherActor(t *testing.T) {
	t.Parallel()

	privateInput, err := structpb.NewStruct(map[string]any{
		"reply_ref": "signed-ref",
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	req, err := workflowEventFromProto(&proto.WorkflowEvent{
		Id:     "evt-1",
		Source: "slack",
		Type:   "com.valon.slack.event",
	})
	if err != nil {
		t.Fatalf("workflowEventFromProto: %v", err)
	}
	publishedBy := workflowActorFromProto(&proto.WorkflowActor{
		SubjectId:           "user:ada",
		CredentialSubjectId: "user:ada-credential",
		SubjectKind:         "user",
		DisplayName:         "Ada",
		AuthSource:          "http_binding",
	})
	coreReq := coreworkflow.PublishEventRequest{
		PluginName:   "slack",
		Event:        req,
		PrivateInput: mapFromStruct(privateInput),
		PublishedBy:  publishedBy,
	}
	pbEvent, err := workflowEventToProto(coreReq.Event)
	if err != nil {
		t.Fatalf("workflowEventToProto: %v", err)
	}
	roundTrip := &proto.PublishWorkflowProviderEventRequest{
		PluginName:   coreReq.PluginName,
		Event:        pbEvent,
		PrivateInput: privateInput,
		PublishedBy:  workflowActorToProto(coreReq.PublishedBy),
	}
	if roundTrip.GetPluginName() != "slack" {
		t.Fatalf("plugin name = %q, want slack", roundTrip.GetPluginName())
	}
	if got := mapFromStruct(roundTrip.GetPrivateInput())["reply_ref"]; got != "signed-ref" {
		t.Fatalf("private input reply_ref = %#v, want signed-ref", got)
	}
	if got := workflowActorFromProto(roundTrip.GetPublishedBy()).CredentialSubjectID; got != "user:ada-credential" {
		t.Fatalf("published_by credential subject = %q, want user:ada-credential", got)
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
