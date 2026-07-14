package gestalt

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/client"
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
		ID:    "definition-1",
		RunAs: "service_account:workflow-runner",
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

func TestWorkflowBuilderInlineExtractSummarizeChain(t *testing.T) {
	builder := DefineWorkflow("summarize-analysis", "service_account:workflow-runner").
		On(Event("analysis.extract.requested", func() map[string]WorkflowValue {
			return map[string]WorkflowValue{
				"analysis_id": WorkflowRefSignal("data.analysis_id"),
			}
		})).
		Step("extract", WorkflowStepConfig{
			App: &WorkflowStepAppConfig{
				Name:      "dealHub",
				Operation: "analyses.extractRowWorkflow",
				Input: func() map[string]WorkflowValue {
					return map[string]WorkflowValue{
						"analysis_id": WorkflowRefInput("analysis_id"),
					}
				},
			},
		}).
		Step("summarize", WorkflowStepConfig{
			Agent: &WorkflowStepAgentConfig{
				Provider: "openai",
				Model:    "gpt-5",
				Prompt: Text(
					"Summarize the extracted analysis: ",
					WorkflowRefStepOutput("extract", "app.body.json"),
				),
			},
		})

	spec, err := builder.ToSpec()
	if err != nil {
		t.Fatalf("ToSpec: %v", err)
	}
	if spec.ID != "summarize-analysis" || spec.RunAs != "service_account:workflow-runner" {
		t.Fatalf("identity = %#v", spec)
	}
	if len(spec.Activations) != 1 || len(spec.Target.Steps) != 2 {
		t.Fatalf("definition shape = %#v", spec)
	}
	if got := spec.Activations[0].Input.Object["analysis_id"].Signal; got != "data.analysis_id" {
		t.Fatalf("activation input = %q", got)
	}
	if got := spec.Target.Steps[0].App.Input.Object["analysis_id"].Input; got != "analysis_id" {
		t.Fatalf("extract input = %q", got)
	}
	if got := spec.Target.Steps[1].Agent.Prompt.Template; got != "Summarize the extracted analysis: ${{ steps.extract.outputs.app.body.json }}" {
		t.Fatalf("summarize prompt = %q", got)
	}
}

func TestWorkflowBuilderRejectsBothAppAndAgent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when configuring both app and agent on one step")
		}
	}()
	DefineWorkflow("invalid-step", "service_account:workflow-runner").Step("broken", WorkflowStepConfig{
		App: &WorkflowStepAppConfig{
			Name:      "dealHub",
			Operation: "extractRow",
		},
		Agent: &WorkflowStepAgentConfig{
			Provider: "openai",
			Prompt:   "summarize",
		},
	})
}

func TestWorkflowBuilderValidationAndApplySource(t *testing.T) {
	if _, err := DefineWorkflow("", "service_account:workflow-runner").ToSpec(); err == nil {
		t.Fatal("expected ID validation error")
	}
	if _, err := DefineWorkflow("workflow", "").ToSpec(); err == nil {
		t.Fatal("expected RunAs validation error")
	}

	applyStub := func(source client.WorkflowDefinitionSpecSource) (*client.WorkflowDefinitionSpec, error) {
		return source.ToWorkflowDefinitionSpec()
	}
	spec, err := applyStub(DefineWorkflow("workflow", "service_account:workflow-runner"))
	if err != nil {
		t.Fatalf("apply source: %v", err)
	}
	if spec.Id != "workflow" || spec.RunAs != "service_account:workflow-runner" {
		t.Fatalf("client spec = %#v", spec)
	}
}

func TestWorkflowTextLowersPluralStepPaths(t *testing.T) {
	got := Text(
		WorkflowRefStepOutput("extract", "body"),
		" / ",
		WorkflowRefStepInput("extract", "analysis_id"),
	)
	want := "${{ steps.extract.outputs.body }} / ${{ steps.extract.inputs.analysis_id }}"
	if got != want {
		t.Fatalf("Text = %q, want %q", got, want)
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
