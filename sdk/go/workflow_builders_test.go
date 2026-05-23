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

func TestNewBoundWorkflowAppTargetUsesNativeValues(t *testing.T) {
	target, err := boundWorkflowTargetToProto(BoundWorkflowTarget{
		App: &BoundWorkflowAppTarget{
			AppName: "slack",
			Operation:  "chat.postMessage",
			Input: struct {
				Channel string `json:"channel"`
			}{Channel: "C123"},
			Connection:     "workspace",
			Instance:       "T123",
			CredentialMode: "subject",
		},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto: %v", err)
	}
	app := target.GetApp()
	if app.GetAppName() != "slack" || app.GetOperation() != "chat.postMessage" {
		t.Fatalf("app target = %q/%q, want slack/chat.postMessage", app.GetAppName(), app.GetOperation())
	}
	if got := app.GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("app input channel = %#v, want C123", got)
	}
}

func TestAgentToolRefCarriesRunAs(t *testing.T) {
	input := AgentToolRef{
		App: "notion",
		Operation: "search",
		RunAs: &Subject{
			ID:                  "service_account:gestalt-support-notion",
			Kind:                "service_account",
			CredentialSubjectID: "service_account:notion-credential",
			DisplayName:         "Gestalt Support Notion",
			AuthSource:          "notion_service_account",
		},
		RunAsExternalIdentity: &ExternalIdentity{
			Type: "notion_workspace",
			ID:   "valon-support",
		},
	}

	copied := NewAgentToolRef(input)
	input.RunAs.ID = "changed"
	input.RunAsExternalIdentity.ID = "changed"
	if copied.RunAs == nil || copied.RunAs.ID != "service_account:gestalt-support-notion" {
		t.Fatalf("copied runAs = %#v, want independent copy", copied.RunAs)
	}
	if copied.RunAsExternalIdentity == nil || copied.RunAsExternalIdentity.ID != "valon-support" {
		t.Fatalf("copied external identity = %#v, want independent copy", copied.RunAsExternalIdentity)
	}

	encoded := agentToolRefToProto(*copied)
	if got := encoded.GetRunAs().GetId(); got != "service_account:gestalt-support-notion" {
		t.Fatalf("encoded runAs subject = %q", got)
	}
	if got := encoded.GetRunAsExternalIdentity().GetId(); got != "valon-support" {
		t.Fatalf("encoded external identity = %q", got)
	}
	roundTrip := agentToolRefFromProto(encoded)
	if roundTrip.RunAs == nil || roundTrip.RunAs.DisplayName != "Gestalt Support Notion" {
		t.Fatalf("round-trip runAs = %#v", roundTrip.RunAs)
	}
	if roundTrip.RunAsExternalIdentity == nil || roundTrip.RunAsExternalIdentity.Type != "notion_workspace" {
		t.Fatalf("round-trip external identity = %#v", roundTrip.RunAsExternalIdentity)
	}
}

func TestNewBoundWorkflowAgentTargetCopiesNativeFields(t *testing.T) {
	target, err := boundWorkflowTargetToProto(BoundWorkflowTarget{
		Agent: &BoundWorkflowAgentTarget{
			ProviderName: "agent",
			Model:        "gpt-5.5",
			Prompt:       "Summarize",
			Messages: []AgentMessage{
				{Role: "user", Parts: []AgentMessagePart{{Type: AgentMessagePartTypeText, Text: "hello"}}},
			},
			ToolRefs:       []AgentToolRef{{App: "search", Operation: "query"}},
			ResponseSchema: map[string]any{"type": "object"},
			Metadata:       map[string]any{"source": "test"},
			TimeoutSeconds: 30,
			OutputDelivery: &WorkflowOutputDelivery{
				Target: &BoundWorkflowAppTarget{
					AppName: "slack",
					Operation:  "chat.postMessage",
					Input:      map[string]any{"channel": "C123"},
				},
				InputBindings: []WorkflowOutputBinding{
					{
						InputField: "text",
						Value:      &WorkflowOutputValueSource{AgentOutput: "text"},
					},
				},
			},
			SessionReadyDelivery: &WorkflowOutputDelivery{
				Target: &BoundWorkflowAppTarget{
					AppName: "slack",
					Operation:  "chat.postMessage",
					Input:      map[string]any{"channel": "C123"},
				},
				InputBindings: []WorkflowOutputBinding{
					{
						InputField: "session_id",
						Value:      &WorkflowOutputValueSource{AgentSession: "id"},
					},
				},
			},
			Steps: []WorkflowAgentStep{
				{
					ID:             "diagnosis",
					Prompt:         "Diagnose",
					Messages:       []AgentMessage{{Role: "system", Parts: []AgentMessagePart{{Type: AgentMessagePartTypeText, Text: "brief"}}}},
					ToolRefs:       []AgentToolRef{{App: "datadog", Operation: "queryLogs"}},
					ResponseSchema: map[string]any{"type": "object"},
					ModelOptions:   map[string]any{"temperature": 0},
					TimeoutSeconds: 45,
					Metadata:       map[string]any{"kind": "diagnosis"},
					OutputDelivery: &WorkflowOutputDelivery{
						Target: &BoundWorkflowAppTarget{
							AppName: "slack",
							Operation:  "chat.postMessage",
						},
						InputBindings: []WorkflowOutputBinding{
							{
								InputField: "text",
								Value:      &WorkflowOutputValueSource{AgentOutput: "text"},
							},
						},
					},
				},
				{
					ID:       "pr_fix",
					Prompt:   "Open a PR",
					ToolRefs: []AgentToolRef{{App: "github", Operation: "createPullRequest"}},
					When: &WorkflowAgentStepWhen{
						StepID:     "diagnosis",
						OutputPath: "structured_output.actionable_for_pr",
						Equals:     true,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto: %v", err)
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
	if got := agent.GetSteps()[0].GetToolRefs()[0].GetApp(); got != "datadog" {
		t.Fatalf("step tool ref app = %q, want datadog", got)
	}
	if got := agent.GetSteps()[1].GetWhen().GetEquals().GetBoolValue(); got != true {
		t.Fatalf("step when equals = %v, want true", got)
	}
	roundTrip := boundWorkflowAgentTargetFromProto(agent)
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
		App: &BoundWorkflowAppTarget{
			AppName: "slack",
			Operation:  "chat.postMessage",
			Input:      map[string]any{"channel": "C123"},
		},
	})
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto(original): %v", err)
	}
	roundTrip, err := boundWorkflowTargetToProto(boundWorkflowTargetFromProto(original))
	if err != nil {
		t.Fatalf("boundWorkflowTargetToProto(roundTrip): %v", err)
	}

	original.GetApp().GetInput().Fields["channel"], err = valueFromAny("C999")
	if err != nil {
		t.Fatalf("valueFromAny: %v", err)
	}
	if got := roundTrip.GetApp().GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("round-trip input channel = %#v, want C123", got)
	}
}

func TestWorkflowRunFromRunDoesNotAliasNestedFields(t *testing.T) {
	triggeredAt := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	run, err := boundWorkflowRunToProto(BoundWorkflowRun{
		ID: "run-1",
		Target: &BoundWorkflowTarget{
			App: &BoundWorkflowAppTarget{
				AppName: "slack",
				Operation:  "chat.postMessage",
				Input:      map[string]any{"channel": "C123"},
			},
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
	run.GetTarget().GetApp().GetInput().Fields["channel"], err = valueFromAny("C999")
	if err != nil {
		t.Fatalf("valueFromAny(channel): %v", err)
	}
	run.GetTrigger().GetEvent().GetEvent().GetData().Fields["ok"], err = valueFromAny(false)
	if err != nil {
		t.Fatalf("valueFromAny(ok): %v", err)
	}

	if got := copied.GetTarget().GetApp().GetInput().AsMap()["channel"]; got != "C123" {
		t.Fatalf("copied target channel = %#v, want C123", got)
	}
	if got := copied.GetTrigger().GetEvent().GetEvent().GetData().AsMap()["ok"]; got != true {
		t.Fatalf("copied trigger event ok = %#v, want true", got)
	}
}

func TestWorkflowExecutionReferenceFromReferenceDoesNotAliasNestedFields(t *testing.T) {
	ref, err := workflowExecutionReferenceToProto(WorkflowExecutionReference{
		ID: "ref-1",
		Target: &BoundWorkflowTarget{
			App: &BoundWorkflowAppTarget{
				AppName: "slack",
				Operation:  "chat.postMessage",
				Input:      map[string]any{"channel": "C123"},
			},
		},
		Permissions: []WorkflowAccessPermission{{App: "slack", Operations: []string{"chat.postMessage"}}},
		RunAs:       &WorkflowRunAsSubject{SubjectID: "service_account:slack"},
	})
	if err != nil {
		t.Fatalf("workflowExecutionReferenceToProto: %v", err)
	}

	copied, err := cloneWorkflowExecutionReferenceProto(ref)
	if err != nil {
		t.Fatalf("cloneWorkflowExecutionReferenceProto: %v", err)
	}
	ref.GetTarget().GetApp().GetInput().Fields["channel"], err = valueFromAny("C999")
	if err != nil {
		t.Fatalf("valueFromAny(channel): %v", err)
	}
	ref.GetPermissions()[0].Operations[0] = "changed"
	ref.GetRunAs().SubjectId = "changed"

	if got := copied.GetTarget().GetApp().GetInput().AsMap()["channel"]; got != "C123" {
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
	ref := NewAuthorizationModelRef("model-1", "v1", createdAt)

	if ref.GetId() != "model-1" || ref.GetVersion() != "v1" {
		t.Fatalf("ref = %q/%q, want model-1/v1", ref.GetId(), ref.GetVersion())
	}
	if got := ref.GetCreatedAt(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
}
