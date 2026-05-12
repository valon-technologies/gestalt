package gestalt_test

import (
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestNewBoundWorkflowRunUsesNativeTimes(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 123_000_000, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	run, err := gestalt.NewBoundWorkflowRun(gestalt.BoundWorkflowRunInput{
		ID:        "run-1",
		Status:    gestalt.WorkflowRunStatusValueRunning,
		CreatedAt: createdAt,
		StartedAt: &startedAt,
	})
	if err != nil {
		t.Fatalf("NewBoundWorkflowRun: %v", err)
	}

	if run.GetId() != "run-1" {
		t.Fatalf("id = %q, want run-1", run.GetId())
	}
	if got := run.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
	if got := run.GetStartedAt().AsTime(); !got.Equal(startedAt) {
		t.Fatalf("started_at = %v, want %v", got, startedAt)
	}
	if run.GetCompletedAt() != nil {
		t.Fatalf("completed_at = %#v, want nil", run.GetCompletedAt())
	}
}

func TestWorkflowNativeBuildersOmitZeroTimes(t *testing.T) {
	run, err := gestalt.NewBoundWorkflowRun(gestalt.BoundWorkflowRunInput{})
	if err != nil {
		t.Fatalf("NewBoundWorkflowRun: %v", err)
	}
	if run.GetCreatedAt() != nil {
		t.Fatalf("created_at = %#v, want nil", run.GetCreatedAt())
	}

	schedule, err := gestalt.NewBoundWorkflowSchedule(gestalt.BoundWorkflowScheduleInput{})
	if err != nil {
		t.Fatalf("NewBoundWorkflowSchedule: %v", err)
	}
	if schedule.GetCreatedAt() != nil || schedule.GetUpdatedAt() != nil || schedule.GetNextRunAt() != nil {
		t.Fatalf("schedule timestamps = %#v/%#v/%#v, want nil", schedule.GetCreatedAt(), schedule.GetUpdatedAt(), schedule.GetNextRunAt())
	}
}

func TestNewWorkflowSignalUsesNativePayloadsAndTime(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	signal, err := gestalt.NewWorkflowSignal(gestalt.WorkflowSignalInput{
		ID:      "signal-1",
		Name:    "changed",
		Payload: map[string]any{"count": 2},
		Metadata: struct {
			Source string `json:"source"`
		}{Source: "test"},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewWorkflowSignal: %v", err)
	}
	if got := signal.GetPayload().AsMap()["count"]; got != float64(2) {
		t.Fatalf("payload.count = %#v, want 2", got)
	}
	if got := signal.GetMetadata().AsMap()["source"]; got != "test" {
		t.Fatalf("metadata.source = %#v, want test", got)
	}
	if got := signal.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
}

func TestNewWorkflowEventUsesNativeDataExtensionsAndTime(t *testing.T) {
	eventTime := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	event, err := gestalt.NewWorkflowEvent(gestalt.WorkflowEventInput{
		ID:              "event-1",
		Source:          "provider",
		SpecVersion:     "1.0",
		Type:            "thing.changed",
		Subject:         "thing:1",
		Time:            eventTime,
		DataContentType: "application/json",
		Data:            map[string]any{"ok": true},
		Extensions:      map[string]any{"attempt": 1},
	})
	if err != nil {
		t.Fatalf("NewWorkflowEvent: %v", err)
	}
	if got := event.GetTime().AsTime(); !got.Equal(eventTime) {
		t.Fatalf("time = %v, want %v", got, eventTime)
	}
	if got := event.GetData().AsMap()["ok"]; got != true {
		t.Fatalf("data.ok = %#v, want true", got)
	}
	if got := event.GetExtensions()["attempt"].AsInterface(); got != float64(1) {
		t.Fatalf("extensions.attempt = %#v, want 1", got)
	}
}

func TestNewBoundWorkflowPluginTargetUsesNativeInput(t *testing.T) {
	target, err := gestalt.NewBoundWorkflowTarget(gestalt.BoundWorkflowTargetInput{
		Plugin: &gestalt.BoundWorkflowPluginTargetInput{
			PluginName: "slack",
			Operation:  "chat.postMessage",
			Input: struct {
				Channel string `json:"channel"`
			}{Channel: "C123"},
			Connection:     "workspace",
			Instance:       "T123",
			CredentialMode: "user",
		},
	})
	if err != nil {
		t.Fatalf("NewBoundWorkflowTarget: %v", err)
	}
	plugin := target.GetPlugin()
	if plugin.GetPluginName() != "slack" || plugin.GetOperation() != "chat.postMessage" {
		t.Fatalf("plugin target = %q/%q, want slack/chat.postMessage", plugin.GetPluginName(), plugin.GetOperation())
	}
	if got := plugin.GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("plugin input channel = %#v, want C123", got)
	}
}

func TestNewBoundWorkflowAgentTargetCopiesNativeFields(t *testing.T) {
	target, err := gestalt.NewBoundWorkflowTarget(gestalt.BoundWorkflowTargetInput{
		Agent: &gestalt.BoundWorkflowAgentTargetInput{
			ProviderName: "agent",
			Model:        "gpt-5.5",
			Prompt:       "Summarize",
			Messages: []*gestalt.AgentMessage{
				{Role: "user", Parts: []gestalt.AgentMessagePart{{Type: gestalt.AgentMessagePartTypeText, Text: "hello"}}},
			},
			ToolRefs:       []*gestalt.AgentToolRef{{Plugin: "search", Operation: "query"}},
			ResponseSchema: map[string]any{"type": "object"},
			Metadata:       map[string]any{"source": "test"},
			TimeoutSeconds: 30,
			OutputDelivery: &gestalt.WorkflowOutputDeliveryInput{
				Target: &gestalt.BoundWorkflowPluginTargetInput{
					PluginName: "slack",
					Operation:  "chat.postMessage",
					Input:      map[string]any{"channel": "C123"},
				},
				InputBindings: []gestalt.WorkflowOutputBindingInput{
					{
						InputField: "text",
						Value:      &gestalt.WorkflowOutputValueSourceInput{AgentOutput: "text"},
					},
				},
			},
			SessionReadyDelivery: &gestalt.WorkflowOutputDeliveryInput{
				Target: &gestalt.BoundWorkflowPluginTargetInput{
					PluginName: "slack",
					Operation:  "chat.postMessage",
					Input:      map[string]any{"channel": "C123"},
				},
				InputBindings: []gestalt.WorkflowOutputBindingInput{
					{
						InputField: "session_id",
						Value:      &gestalt.WorkflowOutputValueSourceInput{AgentSession: "id"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBoundWorkflowTarget: %v", err)
	}
	agent := target.GetAgent()
	if agent.GetProviderName() != "agent" || agent.GetModel() != "gpt-5.5" {
		t.Fatalf("agent target = %q/%q, want agent/gpt-5.5", agent.GetProviderName(), agent.GetModel())
	}
	if got := agent.GetResponseSchema().AsMap()["type"]; got != "object" {
		t.Fatalf("response schema type = %#v, want object", got)
	}
	if got := agent.GetOutputDelivery().GetInputBindings()[0].GetValue().GetAgentOutput(); got != "text" {
		t.Fatalf("output binding = %#v, want text", got)
	}
	if got := agent.GetSessionReadyDelivery().GetInputBindings()[0].GetValue().GetAgentSession(); got != "id" {
		t.Fatalf("session ready binding = %#v, want id", got)
	}
}

func TestBoundWorkflowTargetInputFromTargetDoesNotAliasJSONFields(t *testing.T) {
	original, err := gestalt.NewBoundWorkflowTarget(gestalt.BoundWorkflowTargetInput{
		Plugin: &gestalt.BoundWorkflowPluginTargetInput{
			PluginName: "slack",
			Operation:  "chat.postMessage",
			Input:      map[string]any{"channel": "C123"},
		},
	})
	if err != nil {
		t.Fatalf("NewBoundWorkflowTarget(original): %v", err)
	}
	roundTrip, err := gestalt.NewBoundWorkflowTarget(gestalt.BoundWorkflowTargetInputFromTarget(original))
	if err != nil {
		t.Fatalf("NewBoundWorkflowTarget(roundTrip): %v", err)
	}

	original.GetPlugin().GetInput().Fields["channel"], err = gestalt.ValueFromAny("C999")
	if err != nil {
		t.Fatalf("ValueFromAny: %v", err)
	}
	if got := roundTrip.GetPlugin().GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("round-trip input channel = %#v, want C123", got)
	}
}

func TestWorkflowRunFromRunDoesNotAliasNestedFields(t *testing.T) {
	triggeredAt := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	run, err := gestalt.NewBoundWorkflowRun(gestalt.BoundWorkflowRunInput{
		ID: "run-1",
		Target: &gestalt.BoundWorkflowTargetInput{
			Plugin: &gestalt.BoundWorkflowPluginTargetInput{
				PluginName: "slack",
				Operation:  "chat.postMessage",
				Input:      map[string]any{"channel": "C123"},
			},
		},
		Trigger: &gestalt.WorkflowRunTriggerInput{
			Event: &gestalt.WorkflowEventTriggerInvocationInput{
				TriggerID: "trigger-1",
				Event: &gestalt.WorkflowEventInput{
					ID:          "event-1",
					Source:      "github",
					SpecVersion: "1.0",
					Type:        "issue.created",
					Time:        triggeredAt,
					Data:        map[string]any{"ok": true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBoundWorkflowRun: %v", err)
	}

	copied, err := gestalt.NewBoundWorkflowRunFromRun(run)
	if err != nil {
		t.Fatalf("NewBoundWorkflowRunFromRun: %v", err)
	}
	run.GetTarget().GetPlugin().GetInput().Fields["channel"], err = gestalt.ValueFromAny("C999")
	if err != nil {
		t.Fatalf("ValueFromAny(channel): %v", err)
	}
	run.GetTrigger().GetEvent().GetEvent().GetData().Fields["ok"], err = gestalt.ValueFromAny(false)
	if err != nil {
		t.Fatalf("ValueFromAny(ok): %v", err)
	}

	if got := copied.GetTarget().GetPlugin().GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("copied target channel = %#v, want C123", got)
	}
	if got := copied.GetTrigger().GetEvent().GetEvent().GetData().AsMap()["ok"]; got != true {
		t.Fatalf("copied trigger event ok = %#v, want true", got)
	}
}

func TestWorkflowExecutionReferenceFromReferenceDoesNotAliasNestedFields(t *testing.T) {
	ref, err := gestalt.NewWorkflowExecutionReference(gestalt.WorkflowExecutionReferenceInput{
		ID: "ref-1",
		Target: &gestalt.BoundWorkflowTargetInput{
			Plugin: &gestalt.BoundWorkflowPluginTargetInput{
				PluginName: "slack",
				Operation:  "chat.postMessage",
				Input:      map[string]any{"channel": "C123"},
			},
		},
		Permissions: []gestalt.WorkflowAccessPermissionInput{{Plugin: "slack", Operations: []string{"chat.postMessage"}}},
		RunAs:       &gestalt.WorkflowRunAsSubjectInput{SubjectID: "service_account:slack"},
	})
	if err != nil {
		t.Fatalf("NewWorkflowExecutionReference: %v", err)
	}

	copied, err := gestalt.NewWorkflowExecutionReferenceFromReference(ref)
	if err != nil {
		t.Fatalf("NewWorkflowExecutionReferenceFromReference: %v", err)
	}
	ref.GetTarget().GetPlugin().GetInput().Fields["channel"], err = gestalt.ValueFromAny("C999")
	if err != nil {
		t.Fatalf("ValueFromAny(channel): %v", err)
	}
	ref.GetPermissions()[0].Operations[0] = "changed"
	ref.GetRunAs().SubjectId = "changed"

	if got := copied.GetTarget().GetPlugin().GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("copied target channel = %#v, want C123", got)
	}
	if got := copied.GetPermissions()[0].GetOperations()[0]; got != "chat.postMessage" {
		t.Fatalf("copied permission operation = %#v, want chat.postMessage", got)
	}
	if got := copied.GetRunAs().GetSubjectId(); got != "service_account:slack" {
		t.Fatalf("copied run_as subject = %#v, want service_account:slack", got)
	}
}

func TestNewAuthorizationModelRefUsesNativeTime(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
	ref := gestalt.NewAuthorizationModelRef("model-1", "v1", createdAt)

	if ref.GetId() != "model-1" || ref.GetVersion() != "v1" {
		t.Fatalf("ref = %q/%q, want model-1/v1", ref.GetId(), ref.GetVersion())
	}
	if got := ref.GetCreatedAt(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
}
