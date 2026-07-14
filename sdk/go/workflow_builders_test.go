package gestalt

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkflowRunUsesNativeTimesInputAndSteps(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 123_000_000, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	completedAt := startedAt.Add(time.Minute)
	run, err := workflowRunToProto(WorkflowRun{
		ID:                   "run-1",
		Status:               WorkflowRunStatusValueRunning,
		CreatedAt:            createdAt,
		StartedAt:            &startedAt,
		CompletedAt:          &completedAt,
		DefinitionID:         "definition-1",
		DefinitionGeneration: 4,
		CurrentStepID:        "review",
		Input:                map[string]any{"customer": map[string]any{"id": "cust_1"}},
		Steps: []WorkflowStepExecution{{
			StepID: "review",
			Status: WorkflowStepStatusValueRunning,
			Input:  map[string]any{"thread": "123.456"},
			Attempts: []WorkflowStepAttempt{{
				ID:             "attempt-1",
				Status:         WorkflowStepStatusValueRunning,
				IdempotencyKey: "run-1:review:4",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("workflowRunToProto: %v", err)
	}

	if run.GetId() != "run-1" || run.GetDefinitionId() != "definition-1" || run.GetDefinitionGeneration() != 4 {
		t.Fatalf("run identity = %#v", run)
	}
	if got := run.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
	if got := run.GetStartedAt().AsTime(); !got.Equal(startedAt) {
		t.Fatalf("started_at = %v, want %v", got, startedAt)
	}
	if got := run.GetCompletedAt().AsTime(); !got.Equal(completedAt) {
		t.Fatalf("completed_at = %v, want %v", got, completedAt)
	}
	if got := run.GetInput().AsMap()["customer"].(map[string]any)["id"]; got != "cust_1" {
		t.Fatalf("run input customer.id = %#v, want cust_1", got)
	}
	if len(run.GetSteps()) != 1 || run.GetSteps()[0].GetStepId() != "review" {
		t.Fatalf("steps = %#v", run.GetSteps())
	}
}

func TestWorkflowNativeBuildersOmitZeroTimes(t *testing.T) {
	run, err := workflowRunToProto(WorkflowRun{})
	if err != nil {
		t.Fatalf("workflowRunToProto: %v", err)
	}
	if run.GetCreatedAt() != nil || run.GetStartedAt() != nil || run.GetCompletedAt() != nil {
		t.Fatalf("run timestamps = %#v/%#v/%#v, want nil", run.GetCreatedAt(), run.GetStartedAt(), run.GetCompletedAt())
	}

	definition, err := workflowDefinitionToProto(WorkflowDefinition{})
	if err != nil {
		t.Fatalf("workflowDefinitionToProto: %v", err)
	}
	if definition.GetCreatedAt() != nil || definition.GetUpdatedAt() != nil {
		t.Fatalf("definition timestamps = %#v/%#v, want nil", definition.GetCreatedAt(), definition.GetUpdatedAt())
	}
}

func TestWorkflowSignalAndEventUseNativePayloads(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	signal, err := workflowSignalToProto(WorkflowSignal{
		ID:        "signal-1",
		Name:      "changed",
		Payload:   map[string]any{"count": 2},
		Metadata:  map[string]any{"source": "test"},
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

func TestWorkflowDefinitionSpecStepsAndActivationsUseNewValueRoots(t *testing.T) {
	spec, err := workflowDefinitionSpecToProto(WorkflowDefinitionSpec{
		ID: "definition-1",
		Target: &BoundWorkflowTarget{Steps: []WorkflowStep{
			{
				ID: "collect",
				Inputs: map[string]WorkflowValue{
					"thread_ts": {Signal: "data.thread_ts"},
				},
				App: &WorkflowStepAppCall{
					Name:      "slack",
					Operation: "threads.getContext",
					Input: WorkflowValue{Object: map[string]WorkflowValue{
						"thread_ts": {Input: "thread_ts"},
					}},
				},
			},
			{
				ID: "reply",
				App: &WorkflowStepAppCall{
					Name:      "slack",
					Operation: "chat.postMessage",
					Input: WorkflowValue{Object: map[string]WorkflowValue{
						"context": {StepOutput: &WorkflowStepOutputSource{StepID: "collect", Path: "app.body.json"}},
						"thread":  {StepInput: &WorkflowStepInputSource{StepID: "collect", Path: "thread_ts"}},
					}},
				},
				When: &WorkflowStepWhen{
					Value:  WorkflowValue{Input: "enabled"},
					Equals: true,
				},
			},
		}},
		Activations: []WorkflowActivation{{
			ID: "slack_message",
			Event: &WorkflowEventActivation{Match: &WorkflowEventMatch{
				Type:    "slack.message",
				Source:  "slack",
				Subject: "thread",
			}},
			Input: WorkflowValue{Object: map[string]WorkflowValue{
				"thread_ts": {Signal: "data.thread_ts"},
				"enabled":   {Literal: true, LiteralSet: true},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("workflowDefinitionSpecToProto: %v", err)
	}

	if spec.GetId() != "definition-1" || len(spec.GetTarget().GetSteps()) != 2 || len(spec.GetActivations()) != 1 {
		t.Fatalf("spec = %#v", spec)
	}
	input := spec.GetTarget().GetSteps()[0].GetInputs()["thread_ts"]
	if got := input.GetSignal().GetPath(); got != "data.thread_ts" {
		t.Fatalf("signal input path = %q, want data.thread_ts", got)
	}
	appInput := spec.GetTarget().GetSteps()[1].GetApp().GetInput().GetObject().GetFields()
	if got := appInput["context"].GetStepOutput().GetStepId(); got != "collect" {
		t.Fatalf("step output step id = %q, want collect", got)
	}
	if got := appInput["thread"].GetStepInput().GetPath(); got != "thread_ts" {
		t.Fatalf("step input path = %q, want thread_ts", got)
	}
	activation := spec.GetActivations()[0]
	if activation.GetEvent().GetMatch().GetType() != "slack.message" {
		t.Fatalf("activation event = %#v", activation.GetEvent())
	}
}

func TestWorkflowRunTriggerRoundTripUsesActivationID(t *testing.T) {
	scheduledFor := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
	trigger, err := workflowRunTriggerToProto(WorkflowRunTrigger{
		Schedule: &WorkflowScheduleTrigger{ActivationID: "nightly", ScheduledFor: &scheduledFor},
	})
	if err != nil {
		t.Fatalf("workflowRunTriggerToProto: %v", err)
	}
	copied, err := cloneWorkflowRunTriggerProto(trigger)
	if err != nil {
		t.Fatalf("cloneWorkflowRunTriggerProto: %v", err)
	}
	if got := copied.GetSchedule().GetActivationId(); got != "nightly" {
		t.Fatalf("activation id = %q, want nightly", got)
	}
	if got := copied.GetSchedule().GetScheduledFor().AsTime(); !got.Equal(scheduledFor) {
		t.Fatalf("scheduled_for = %v, want %v", got, scheduledFor)
	}
}

func TestDefineWorkflowRequiresRunAs(t *testing.T) {
	if _, err := DefineWorkflow(DefineWorkflowOptions{ID: "demo"}); err == nil {
		t.Fatal("expected RunAs validation error")
	}
}

func TestTypedWorkflowBuilderMatchesExtractRowExample(t *testing.T) {
	builder, err := DefineWorkflow(DefineWorkflowOptions{
		ID:    "extractRow",
		RunAs: "service_account:deal-hub-extraction",
	})
	if err != nil {
		t.Fatalf("DefineWorkflow: %v", err)
	}
	spec := builder.
		On(Event("deal_hub.analyses.extract.requested", func(scope WorkflowEventScope) map[string]WorkflowValue {
			return map[string]WorkflowValue{
				"analysisId": scope.Data("analysisId"),
			}
		}, WorkflowEventActivationOptions{})).
		Step("extract", WorkflowStepConfig{
			App: &WorkflowStepAppConfig{
				Name:      "dealHub",
				Operation: "analyses.extractRowWorkflow",
				Input: func(scope WorkflowStepScope) map[string]WorkflowValue {
					return map[string]WorkflowValue{
						"analysisId": scope.Input("analysisId"),
					}
				},
			},
		}).
		ToSpec()

	contract, err := LoadWorkflowLoweringContract()
	if err != nil {
		t.Fatalf("LoadWorkflowLoweringContract: %v", err)
	}
	var expected map[string]any
	for _, caseData := range contract.Cases {
		if caseData.Name == "extract_row" {
			expected = caseData.ExpectedSpec
			break
		}
	}
	if expected == nil {
		t.Fatal("missing extract_row fixture")
	}
	got, err := CanonicalWorkflowDefinitionSpec(spec)
	if err != nil {
		t.Fatalf("CanonicalWorkflowDefinitionSpec: %v", err)
	}
	assertWorkflowDefineJSONEqual(t, expected, got)
}

func TestWorkflowDefineGoldenFixtures(t *testing.T) {
	contract, err := LoadWorkflowLoweringContract()
	if err != nil {
		t.Fatalf("LoadWorkflowLoweringContract: %v", err)
	}
	for _, caseData := range contract.Cases {
		t.Run(caseData.Name, func(t *testing.T) {
			builder, err := BuildWorkflowFromLoweringCase(caseData)
			if err != nil {
				t.Fatalf("BuildWorkflowFromLoweringCase: %v", err)
			}
			got, err := CanonicalWorkflowDefinitionSpec(builder.ToSpec())
			if err != nil {
				t.Fatalf("CanonicalWorkflowDefinitionSpec: %v", err)
			}
			assertWorkflowDefineJSONEqual(t, caseData.ExpectedSpec, got)
		})
	}
}

func TestResolveWorkflowDefinitionSpecAcceptsBuilder(t *testing.T) {
	builder, err := DefineWorkflow(DefineWorkflowOptions{
		ID:    "extractRow",
		RunAs: "service_account:deal-hub-extraction",
	})
	if err != nil {
		t.Fatalf("DefineWorkflow: %v", err)
	}
	builder.On(Schedule("0 2 * * *", func(scope WorkflowActivationScope) map[string]WorkflowValue {
		return map[string]WorkflowValue{"reason": scope.Input("reason")}
	}, WorkflowScheduleActivationOptions{}))

	fromBuilder, err := ResolveWorkflowDefinitionSpec(builder)
	if err != nil {
		t.Fatalf("ResolveWorkflowDefinitionSpec(builder): %v", err)
	}
	fromSpec, err := ResolveWorkflowDefinitionSpec(fromBuilder)
	if err != nil {
		t.Fatalf("ResolveWorkflowDefinitionSpec(spec): %v", err)
	}
	if len(fromBuilder.Activations) != 1 || fromBuilder.Activations[0].Schedule == nil {
		t.Fatalf("activations = %#v", fromBuilder.Activations)
	}
	if fromSpec.ID != "extractRow" {
		t.Fatalf("id = %q, want extractRow", fromSpec.ID)
	}
}

func assertWorkflowDefineJSONEqual(t *testing.T, expected, actual map[string]any) {
	t.Helper()
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected: %v", err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("marshal actual: %v", err)
	}
	if string(expectedJSON) != string(actualJSON) {
		t.Fatalf("spec mismatch\nexpected: %s\nactual:   %s", expectedJSON, actualJSON)
	}
}
