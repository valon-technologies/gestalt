package gestalt_test

import (
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestNewBoundWorkflowRunUsesNativeTimes(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 12, 0, 0, 123_000_000, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	run := gestalt.NewBoundWorkflowRun(gestalt.BoundWorkflowRunInput{
		ID:        "run-1",
		Status:    gestalt.WorkflowRunStatusValueRunning,
		CreatedAt: createdAt,
		StartedAt: &startedAt,
	})

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
	run := gestalt.NewBoundWorkflowRun(gestalt.BoundWorkflowRunInput{})
	if run.GetCreatedAt() != nil {
		t.Fatalf("created_at = %#v, want nil", run.GetCreatedAt())
	}

	schedule := gestalt.NewBoundWorkflowSchedule(gestalt.BoundWorkflowScheduleInput{})
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

func TestNewAuthorizationModelRefUsesNativeTime(t *testing.T) {
	createdAt := time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC)
	ref := gestalt.NewAuthorizationModelRef("model-1", "v1", createdAt)

	if ref.GetId() != "model-1" || ref.GetVersion() != "v1" {
		t.Fatalf("ref = %q/%q, want model-1/v1", ref.GetId(), ref.GetVersion())
	}
	if got := ref.GetCreatedAt().AsTime(); !got.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", got, createdAt)
	}
}
