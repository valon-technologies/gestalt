package workflowmanager

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func TestPublishEventScopesPrivateInputToSelectedProvider(t *testing.T) {
	t.Parallel()

	primary := &publishRecordingWorkflowProvider{}
	secondary := &publishRecordingWorkflowProvider{}
	control := publishWorkflowControl{
		defaultName: "primary",
		names:       []string{"primary", "secondary"},
		providers: map[string]*publishRecordingWorkflowProvider{
			"primary":   primary,
			"secondary": secondary,
		},
	}
	manager := New(Config{
		Workflow: control,
		Now: func() time.Time {
			return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
		},
	})
	actor := principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")})

	_, err := manager.PublishEvent(context.Background(), actor, coreworkflow.PublishEventRequest{
		Event: coreworkflow.Event{
			Type:   "com.valon.slack.event",
			Source: "slack",
		},
		PrivateInput: map[string]any{"reply_ref": "signed-ref"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(primary.publishEventRequests) != 1 {
		t.Fatalf("primary publish requests = %d, want 1", len(primary.publishEventRequests))
	}
	if len(secondary.publishEventRequests) != 0 {
		t.Fatalf("secondary publish requests = %d, want 0", len(secondary.publishEventRequests))
	}
	got := primary.publishEventRequests[0]
	if got.ProviderName != "primary" {
		t.Fatalf("provider name = %q, want primary", got.ProviderName)
	}
	if got.PrivateInput["reply_ref"] != "signed-ref" {
		t.Fatalf("private input = %#v, want reply_ref", got.PrivateInput)
	}
}

func TestPublishEventFansOutPublicEvents(t *testing.T) {
	t.Parallel()

	primary := &publishRecordingWorkflowProvider{}
	secondary := &publishRecordingWorkflowProvider{}
	control := publishWorkflowControl{
		defaultName: "primary",
		names:       []string{"primary", "secondary"},
		providers: map[string]*publishRecordingWorkflowProvider{
			"primary":   primary,
			"secondary": secondary,
		},
	}
	manager := New(Config{Workflow: control})
	actor := principal.Canonicalize(&principal.Principal{SubjectID: principal.UserSubjectID("ada")})

	_, err := manager.PublishEvent(context.Background(), actor, coreworkflow.PublishEventRequest{
		Event: coreworkflow.Event{Type: "com.example.public"},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(primary.publishEventRequests) != 1 || len(secondary.publishEventRequests) != 1 {
		t.Fatalf("publish requests primary=%d secondary=%d, want 1 each", len(primary.publishEventRequests), len(secondary.publishEventRequests))
	}
}

type publishWorkflowControl struct {
	defaultName string
	names       []string
	providers   map[string]*publishRecordingWorkflowProvider
}

func (c publishWorkflowControl) ResolveProvider(name string) (coreworkflow.Provider, error) {
	provider, ok := c.providers[name]
	if !ok {
		return nil, core.ErrNotFound
	}
	return provider, nil
}

func (c publishWorkflowControl) ResolveProviderSelection(name string) (string, coreworkflow.Provider, error) {
	if name == "" {
		name = c.defaultName
	}
	provider, ok := c.providers[name]
	if !ok {
		return "", nil, core.ErrNotFound
	}
	return name, provider, nil
}

func (c publishWorkflowControl) ProviderNames() []string {
	return append([]string(nil), c.names...)
}

type publishRecordingWorkflowProvider struct {
	publishEventRequests []coreworkflow.PublishEventRequest
}

func (p *publishRecordingWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}
func (p *publishRecordingWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}
func (p *publishRecordingWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return nil
}
func (p *publishRecordingWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}
func (p *publishRecordingWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}
func (p *publishRecordingWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return nil, core.ErrNotFound
}
func (p *publishRecordingWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	p.publishEventRequests = append(p.publishEventRequests, req)
	return nil
}
func (p *publishRecordingWorkflowProvider) Ping(context.Context) error { return nil }
func (p *publishRecordingWorkflowProvider) Close() error               { return nil }
