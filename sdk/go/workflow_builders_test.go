package gestalt

import (
	"testing"
	"time"
)

func TestNewBoundWorkflowRunUsesNativeTimes(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 123_000_000, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	run, err := boundWorkflowRunToProto(BoundWorkflowRun{
		ID:        "run-1",
		Status:    WorkflowRunStatusValueRunning,
		CreatedAt: createdAt,
		StartedAt: &startedAt,
	})
	if err != nil {
		t.Fatalf("boundWorkflowRunToProto: %v", err)
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
	run, err := boundWorkflowRunToProto(BoundWorkflowRun{})
	if err != nil {
		t.Fatalf("boundWorkflowRunToProto: %v", err)
	}
	if run.GetCreatedAt() != nil {
		t.Fatalf("created_at = %#v, want nil", run.GetCreatedAt())
	}

	schedule, err := boundWorkflowScheduleToProto(BoundWorkflowSchedule{})
	if err != nil {
		t.Fatalf("boundWorkflowScheduleToProto: %v", err)
	}
	if schedule.GetCreatedAt() != nil || schedule.GetUpdatedAt() != nil || schedule.GetNextRunAt() != nil {
		t.Fatalf("schedule timestamps = %#v/%#v/%#v, want nil", schedule.GetCreatedAt(), schedule.GetUpdatedAt(), schedule.GetNextRunAt())
	}
}

func TestNewWorkflowSignalUsesNativePayloadsAndTime(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	signal, err := workflowSignalToProto(WorkflowSignal{
		ID:      "signal-1",
		Name:    "changed",
		Payload: map[string]any{"count": 2},
		Metadata: struct {
			Source string `json:"source"`
		}{Source: "test"},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("workflowSignalToProto: %v", err)
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
	event, err := workflowEventToProto(WorkflowEvent{
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
		t.Fatalf("workflowEventToProto: %v", err)
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

func TestNewBoundWorkflowStepsTargetUsesNativeValues(t *testing.T) {
	target, err := boundWorkflowTargetToProto(BoundWorkflowTarget{
		Steps: []WorkflowStep{{
			ID: "post",
			App: &WorkflowStepAppCall{
				Name:      "slack",
				Operation: "chat.postMessage",
				Input: WorkflowValue{Object: map[string]WorkflowValue{
					"channel": {Literal: "C123", LiteralSet: true},
				}},
				Connection:     "workspace",
				Instance:       "T123",
				CredentialMode: "subject",
			},
		}},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto: %v", err)
	}
	if len(target.GetSteps()) != 1 {
		t.Fatalf("steps = %d, want 1", len(target.GetSteps()))
	}
	app := target.GetSteps()[0].GetApp()
	if app.GetName() != "slack" || app.GetOperation() != "chat.postMessage" {
		t.Fatalf("app step = %q/%q, want slack/chat.postMessage", app.GetName(), app.GetOperation())
	}
	if got := app.GetInput().GetObject().GetFields()["channel"].GetLiteral().AsInterface(); got != "C123" {
		t.Fatalf("app input channel = %#v, want C123", got)
	}
}

func TestAgentToolRefCarriesRunAs(t *testing.T) {
	input := AgentToolRef{
		App:       "notion",
		Operation: "search",
		RunAs: &Subject{
			ID:                  "service_account:gestalt-support-notion",
			CredentialSubjectID: "service_account:notion-credential",
			Email:               "support@example.com",
		},
	}

	encoded := agentToolRefToProto(input)
	if got := encoded.GetRunAs().GetId(); got != "service_account:gestalt-support-notion" {
		t.Fatalf("encoded runAs subject = %q", got)
	}
	roundTrip := agentToolRefFromProto(encoded)
	if roundTrip.RunAs == nil || roundTrip.RunAs.Email != "support@example.com" {
		t.Fatalf("round-trip runAs = %#v", roundTrip.RunAs)
	}
}

func TestBoundWorkflowTargetAgentStepCopiesNativeFields(t *testing.T) {
	target, err := boundWorkflowTargetToProto(BoundWorkflowTarget{
		Steps: []WorkflowStep{
			{
				ID: "diagnosis",
				Inputs: map[string]WorkflowValue{
					"thread_ts": {SignalPayload: "event.ts"},
				},
				Agent: &WorkflowStepAgentTurn{
					Provider: "agent",
					Model:    "gpt-5.5",
					Prompt:   WorkflowText{Template: "Diagnose ${inputs.thread_ts}"},
					Messages: []WorkflowAgentMessage{{
						Role: "system",
						Text: WorkflowText{Template: "brief"},
					}},
					Tools: []AgentToolRef{{App: "datadog", Operation: "queryLogs"}},
					Output: &AgentOutput{
						Structured: &AgentStructuredOutput{Schema: map[string]any{"type": "object"}},
					},
					ModelOptions: map[string]any{"temperature": 0},
				},
				TimeoutSeconds: 45,
				Metadata:       map[string]any{"kind": "diagnosis"},
			},
			{
				ID: "pr_fix",
				Agent: &WorkflowStepAgentTurn{
					Provider: "agent",
					Model:    "gpt-5.5",
					Prompt:   WorkflowText{Template: "Open a PR"},
					Tools:    []AgentToolRef{{App: "github", Operation: "createPullRequest"}},
				},
				When: &WorkflowStepWhen{
					Value: WorkflowValue{
						StepOutput: &WorkflowStepOutputSource{
							StepID: "diagnosis",
							Path:   "agent.structuredOutput.actionableForPr",
						},
					},
					Equals: true,
				},
				TimeoutSeconds: 45,
			},
		},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto: %v", err)
	}
	if len(target.GetSteps()) != 2 {
		t.Fatalf("steps = %d, want two", len(target.GetSteps()))
	}
	diagnosis := target.GetSteps()[0]
	agent := diagnosis.GetAgent()
	if agent.GetProvider() != "agent" || agent.GetModel() != "gpt-5.5" {
		t.Fatalf("agent step = %q/%q, want agent/gpt-5.5", agent.GetProvider(), agent.GetModel())
	}
	if got := agent.GetOutput().GetStructured().GetSchema().AsMap()["type"]; got != "object" {
		t.Fatalf("output schema type = %#v, want object", got)
	}
	if got := agent.GetTools()[0].GetApp(); got != "datadog" {
		t.Fatalf("step tool ref app = %q, want datadog", got)
	}
	if got := target.GetSteps()[1].GetWhen().GetEquals().GetBoolValue(); got != true {
		t.Fatalf("step when equals = %v, want true", got)
	}
	roundTrip := boundWorkflowTargetFromProto(target)
	if len(roundTrip.Steps) != 2 {
		t.Fatalf("round trip steps = %#v, want two", roundTrip.Steps)
	}
	if roundTrip.Steps[0].ID != "diagnosis" || roundTrip.Steps[0].TimeoutSeconds != 45 {
		t.Fatalf("round trip diagnosis step = %#v", roundTrip.Steps[0])
	}
	if roundTrip.Steps[1].When == nil || roundTrip.Steps[1].When.Equals != true {
		t.Fatalf("round trip pr_fix when = %#v", roundTrip.Steps[1].When)
	}
}

func TestBoundWorkflowTargetFromTargetDoesNotAliasJSONFields(t *testing.T) {
	original, err := boundWorkflowTargetToProto(BoundWorkflowTarget{
		Steps: []WorkflowStep{{
			ID: "post",
			App: &WorkflowStepAppCall{
				Name:      "slack",
				Operation: "chat.postMessage",
				Input: WorkflowValue{Object: map[string]WorkflowValue{
					"channel": {Literal: "C123", LiteralSet: true},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto(original): %v", err)
	}
	roundTrip, err := boundWorkflowTargetToProto(boundWorkflowTargetFromProto(original))
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto(roundTrip): %v", err)
	}

	original.GetSteps()[0].GetApp().GetInput().GetObject().Fields["channel"], err = workflowValueToProto(WorkflowValue{Literal: "C999", LiteralSet: true})
	if err != nil {
		t.Fatalf("workflowValueToProto: %v", err)
	}
	if got := roundTrip.GetSteps()[0].GetApp().GetInput().GetObject().GetFields()["channel"].GetLiteral().AsInterface(); got != "C123" {
		t.Fatalf("round-trip input channel = %#v, want C123", got)
	}
}

func TestWorkflowRunFromRunDoesNotAliasNestedFields(t *testing.T) {
	triggeredAt := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	run, err := boundWorkflowRunToProto(BoundWorkflowRun{
		ID: "run-1",
		Target: &BoundWorkflowTarget{
			Steps: []WorkflowStep{{
				ID: "post",
				App: &WorkflowStepAppCall{
					Name:      "slack",
					Operation: "chat.postMessage",
					Input: WorkflowValue{Object: map[string]WorkflowValue{
						"channel": {Literal: "C123", LiteralSet: true},
					}},
				},
			}},
		},
		Trigger: &WorkflowRunTrigger{
			Event: &WorkflowEventTriggerInvocation{
				TriggerID: "trigger-1",
				Event: &WorkflowEvent{
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
		t.Fatalf("boundWorkflowRunToProto: %v", err)
	}

	copied, err := cloneBoundWorkflowRunProto(run)
	if err != nil {
		t.Fatalf("cloneBoundWorkflowRunProto: %v", err)
	}
	run.GetTarget().GetSteps()[0].GetApp().GetInput().GetObject().Fields["channel"], err = workflowValueToProto(WorkflowValue{Literal: "C999", LiteralSet: true})
	if err != nil {
		t.Fatalf("workflowValueToProto(channel): %v", err)
	}
	run.GetTrigger().GetEvent().GetEvent().GetData().Fields["ok"], err = valueFromAny(false)
	if err != nil {
		t.Fatalf("valueFromAny(ok): %v", err)
	}

	if got := copied.GetTarget().GetSteps()[0].GetApp().GetInput().GetObject().GetFields()["channel"].GetLiteral().AsInterface(); got != "C123" {
		t.Fatalf("copied target channel = %#v, want C123", got)
	}
	if got := copied.GetTrigger().GetEvent().GetEvent().GetData().AsMap()["ok"]; got != true {
		t.Fatalf("copied trigger event ok = %#v, want true", got)
	}
}

func TestNewAuthorizationModelRefUsesNativeTime(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
	ref := NewAuthorizationModelRef("model-1", "v1", createdAt)

	if ref.GetId() != "model-1" || ref.GetVersion() != "v1" {
		t.Fatalf("ref = %q/%q, want model-1/v1", ref.GetId(), ref.GetVersion())
	}
	if got := ref.GetCreatedAt(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
}
