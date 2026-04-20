package providerhost

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recordingWorkflowProvider struct {
	startRunReq           coreworkflow.StartRunRequest
	startRunCalled        bool
	listRunsReq           coreworkflow.ListRunsRequest
	upsertScheduleReq     coreworkflow.UpsertScheduleRequest
	upsertScheduleCalled  bool
	listSchedulesReq      coreworkflow.ListSchedulesRequest
	upsertEventTriggerReq coreworkflow.UpsertEventTriggerRequest
	listEventTriggersReq  coreworkflow.ListEventTriggersRequest
	publishEventReq       coreworkflow.PublishEventRequest
}

type workflowProviderFunc struct {
	startRun           func(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error)
	getRun             func(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error)
	upsertSchedule     func(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error)
	getSchedule        func(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error)
	deleteSchedule     func(context.Context, coreworkflow.DeleteScheduleRequest) error
	pauseSchedule      func(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error)
	resumeSchedule     func(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error)
	upsertEventTrigger func(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error)
	getEventTrigger    func(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error)
	deleteEventTrigger func(context.Context, coreworkflow.DeleteEventTriggerRequest) error
	pauseEventTrigger  func(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error)
	resumeEventTrigger func(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error)
}

func (p workflowProviderFunc) StartRun(ctx context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	if p.startRun != nil {
		return p.startRun(ctx, req)
	}
	return &coreworkflow.Run{}, nil
}

func (p workflowProviderFunc) GetRun(ctx context.Context, req coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	if p.getRun != nil {
		return p.getRun(ctx, req)
	}
	return &coreworkflow.Run{}, nil
}

func (p workflowProviderFunc) ListRuns(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	return nil, nil
}

func (p workflowProviderFunc) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p workflowProviderFunc) UpsertSchedule(ctx context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.upsertSchedule != nil {
		return p.upsertSchedule(ctx, req)
	}
	return &coreworkflow.Schedule{}, nil
}

func (p workflowProviderFunc) GetSchedule(ctx context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.getSchedule != nil {
		return p.getSchedule(ctx, req)
	}
	return &coreworkflow.Schedule{}, nil
}

func (p workflowProviderFunc) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}

func (p workflowProviderFunc) DeleteSchedule(ctx context.Context, req coreworkflow.DeleteScheduleRequest) error {
	if p.deleteSchedule != nil {
		return p.deleteSchedule(ctx, req)
	}
	return nil
}

func (p workflowProviderFunc) PauseSchedule(ctx context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.pauseSchedule != nil {
		return p.pauseSchedule(ctx, req)
	}
	return &coreworkflow.Schedule{}, nil
}

func (p workflowProviderFunc) ResumeSchedule(ctx context.Context, req coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	if p.resumeSchedule != nil {
		return p.resumeSchedule(ctx, req)
	}
	return &coreworkflow.Schedule{}, nil
}

func (p workflowProviderFunc) UpsertEventTrigger(ctx context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.upsertEventTrigger != nil {
		return p.upsertEventTrigger(ctx, req)
	}
	return &coreworkflow.EventTrigger{}, nil
}

func (p workflowProviderFunc) GetEventTrigger(ctx context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.getEventTrigger != nil {
		return p.getEventTrigger(ctx, req)
	}
	return &coreworkflow.EventTrigger{}, nil
}

func (p workflowProviderFunc) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}

func (p workflowProviderFunc) DeleteEventTrigger(ctx context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
	if p.deleteEventTrigger != nil {
		return p.deleteEventTrigger(ctx, req)
	}
	return nil
}

func (p workflowProviderFunc) PauseEventTrigger(ctx context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.pauseEventTrigger != nil {
		return p.pauseEventTrigger(ctx, req)
	}
	return &coreworkflow.EventTrigger{}, nil
}

func (p workflowProviderFunc) ResumeEventTrigger(ctx context.Context, req coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	if p.resumeEventTrigger != nil {
		return p.resumeEventTrigger(ctx, req)
	}
	return &coreworkflow.EventTrigger{}, nil
}

func (p workflowProviderFunc) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}

func (p workflowProviderFunc) Ping(context.Context) error { return nil }
func (p workflowProviderFunc) Close() error               { return nil }

func (p *recordingWorkflowProvider) StartRun(_ context.Context, req coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	p.startRunCalled = true
	p.startRunReq = req
	return &coreworkflow.Run{
		ID:           "run-1",
		Status:       coreworkflow.RunStatusPending,
		Target:       req.Target,
		CreatedBy:    req.CreatedBy,
		ExecutionRef: req.ExecutionRef,
		Trigger: coreworkflow.RunTrigger{
			Manual: true,
		},
	}, nil
}

func (p *recordingWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{
		ID:     "run-1",
		Status: coreworkflow.RunStatusRunning,
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "refresh",
		},
	}, nil
}

func (p *recordingWorkflowProvider) ListRuns(_ context.Context, req coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	p.listRunsReq = req
	return []*coreworkflow.Run{
		{
			ID:     "run-1",
			Status: coreworkflow.RunStatusRunning,
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "refresh",
			},
		},
		{
			ID:     "run-foreign",
			Status: coreworkflow.RunStatusRunning,
			Target: coreworkflow.Target{
				PluginName: "analytics",
				Operation:  "refresh",
			},
		},
	}, nil
}

func (p *recordingWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p *recordingWorkflowProvider) UpsertSchedule(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	p.upsertScheduleCalled = true
	p.upsertScheduleReq = req
	return &coreworkflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		Paused:       req.Paused,
		ExecutionRef: req.ExecutionRef,
		CreatedBy:    req.RequestedBy,
	}, nil
}

func (p *recordingWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{
		ID:       "sched-1",
		Cron:     "*/5 * * * *",
		Timezone: "UTC",
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "refresh",
		},
	}, nil
}

func (p *recordingWorkflowProvider) ListSchedules(_ context.Context, req coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	p.listSchedulesReq = req
	return []*coreworkflow.Schedule{
		{
			ID:       "sched-1",
			Cron:     "*/5 * * * *",
			Timezone: "UTC",
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "refresh",
			},
		},
		{
			ID:       "sched-foreign",
			Cron:     "*/5 * * * *",
			Timezone: "UTC",
			Target: coreworkflow.Target{
				PluginName: "analytics",
				Operation:  "refresh",
			},
		},
	}, nil
}

func (p *recordingWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return nil
}

func (p *recordingWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p *recordingWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p *recordingWorkflowProvider) UpsertEventTrigger(_ context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	p.upsertEventTriggerReq = req
	return &coreworkflow.EventTrigger{
		ID:        req.TriggerID,
		Match:     req.Match,
		Target:    req.Target,
		Paused:    req.Paused,
		CreatedBy: req.RequestedBy,
	}, nil
}

func (p *recordingWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{
		ID: "trigger-1",
		Match: coreworkflow.EventMatch{
			Type: "roadmap.task.updated",
		},
		Target: coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "refresh",
		},
	}, nil
}

func (p *recordingWorkflowProvider) ListEventTriggers(_ context.Context, req coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	p.listEventTriggersReq = req
	return []*coreworkflow.EventTrigger{
		{
			ID: "trigger-1",
			Match: coreworkflow.EventMatch{
				Type: "roadmap.task.updated",
			},
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "refresh",
			},
		},
		{
			ID: "trigger-foreign",
			Match: coreworkflow.EventMatch{
				Type: "roadmap.task.updated",
			},
			Target: coreworkflow.Target{
				PluginName: "analytics",
				Operation:  "refresh",
			},
		},
	}, nil
}

func (p *recordingWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}

func (p *recordingWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p *recordingWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p *recordingWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) error {
	p.publishEventReq = req
	return nil
}
func (p *recordingWorkflowProvider) Ping(context.Context) error { return nil }
func (p *recordingWorkflowProvider) Close() error               { return nil }

func mustStruct(t *testing.T, value map[string]any) *structpb.Struct {
	t.Helper()
	got, err := structpb.NewStruct(value)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return got
}

func staticWorkflowResolver(provider coreworkflow.Provider, allowed, managedSchedules, managedEventTriggers map[string]struct{}, err error) workflowProviderResolver {
	return func() (coreworkflow.Provider, map[string]struct{}, WorkflowManagedIDs, error) {
		return provider, allowed, WorkflowManagedIDs{
			Schedules:     managedSchedules,
			EventTriggers: managedEventTriggers,
		}, err
	}
}

func TestWorkflowServerStartRunScopesTargetToPlugin(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:   principal.UserSubjectID("user-123"),
		Kind:        principal.KindUser,
		DisplayName: "Ada",
		Source:      principal.SourceAPIToken,
	})

	resp, err := srv.StartRun(ctx, &proto.StartWorkflowRunRequest{
		Target: &proto.WorkflowTarget{
			Operation:  "refresh",
			Input:      mustStruct(t, map[string]any{"taskId": "task-123", "full": true}),
			Connection: "analytics",
			Instance:   "tenant-a",
		},
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if !provider.startRunCalled {
		t.Fatal("expected provider StartRun to be called")
	}
	if provider.startRunReq.Target.PluginName != "roadmap" {
		t.Fatalf("target plugin = %q, want %q", provider.startRunReq.Target.PluginName, "roadmap")
	}
	if provider.startRunReq.Target.Operation != "refresh" {
		t.Fatalf("target operation = %q, want %q", provider.startRunReq.Target.Operation, "refresh")
	}
	if provider.startRunReq.Target.Connection != "analytics" || provider.startRunReq.Target.Instance != "tenant-a" {
		t.Fatalf("target selectors = %#v", provider.startRunReq.Target)
	}
	if !reflect.DeepEqual(provider.startRunReq.Target.Input, map[string]any{"taskId": "task-123", "full": true}) {
		t.Fatalf("target input = %#v", provider.startRunReq.Target.Input)
	}
	if provider.startRunReq.IdempotencyKey != "idem-1" {
		t.Fatalf("idempotency key = %q, want %q", provider.startRunReq.IdempotencyKey, "idem-1")
	}
	if provider.startRunReq.CreatedBy.SubjectID != principal.UserSubjectID("user-123") || provider.startRunReq.CreatedBy.AuthSource != principal.SourceAPIToken.String() {
		t.Fatalf("createdBy = %#v", provider.startRunReq.CreatedBy)
	}
	if resp.GetTarget().GetOperation() != "refresh" {
		t.Fatalf("response operation = %q, want %q", resp.GetTarget().GetOperation(), "refresh")
	}
	if got := resp.GetTarget().GetInput().AsMap(); !reflect.DeepEqual(got, map[string]any{"taskId": "task-123", "full": true}) {
		t.Fatalf("response input = %#v", got)
	}
	if resp.GetTarget().GetConnection() != "analytics" || resp.GetTarget().GetInstance() != "tenant-a" {
		t.Fatalf("response target selectors = %#v", resp.GetTarget())
	}
	if resp.GetCreatedBy().GetSubjectId() != principal.UserSubjectID("user-123") || resp.GetCreatedBy().GetDisplayName() != "Ada" {
		t.Fatalf("response createdBy = %#v", resp.GetCreatedBy())
	}
}

func TestWorkflowServerNamespacesScheduleIDsPerPlugin(t *testing.T) {
	t.Parallel()

	schedules := map[string]*coreworkflow.Schedule{}
	provider := workflowProviderFunc{
		upsertSchedule: func(_ context.Context, req coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
			value := &coreworkflow.Schedule{
				ID:       req.ScheduleID,
				Cron:     req.Cron,
				Timezone: req.Timezone,
				Target:   req.Target,
				Paused:   req.Paused,
			}
			schedules[req.ScheduleID] = value
			return value, nil
		},
		getSchedule: func(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
			value, ok := schedules[req.ScheduleID]
			if !ok {
				return nil, status.Error(codes.NotFound, "missing schedule")
			}
			return value, nil
		},
		deleteSchedule: func(_ context.Context, req coreworkflow.DeleteScheduleRequest) error {
			delete(schedules, req.ScheduleID)
			return nil
		},
		pauseSchedule: func(_ context.Context, req coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
			value, ok := schedules[req.ScheduleID]
			if !ok {
				return nil, status.Error(codes.NotFound, "missing schedule")
			}
			cloned := *value
			cloned.Paused = true
			schedules[req.ScheduleID] = &cloned
			return &cloned, nil
		},
	}

	roadmap := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))
	analytics := NewWorkflowServer("analytics", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	for _, tc := range []struct {
		server *WorkflowServer
		plugin string
	}{
		{server: roadmap, plugin: "roadmap"},
		{server: analytics, plugin: "analytics"},
	} {
		_, err := tc.server.UpsertSchedule(context.Background(), &proto.UpsertWorkflowScheduleRequest{
			ScheduleId: "nightly",
			Cron:       "*/5 * * * *",
			Timezone:   "UTC",
			Target:     &proto.WorkflowTarget{Operation: "refresh"},
		})
		if err != nil {
			t.Fatalf("UpsertSchedule(%s): %v", tc.plugin, err)
		}
	}

	roadmapID := scopedWorkflowObjectID("roadmap", "nightly")
	analyticsID := scopedWorkflowObjectID("analytics", "nightly")
	if len(schedules) != 2 {
		t.Fatalf("stored schedules = %#v", schedules)
	}
	if _, ok := schedules[roadmapID]; !ok {
		t.Fatalf("missing roadmap scoped schedule %q", roadmapID)
	}
	if _, ok := schedules[analyticsID]; !ok {
		t.Fatalf("missing analytics scoped schedule %q", analyticsID)
	}

	got, err := roadmap.GetSchedule(context.Background(), &proto.GetWorkflowScheduleRequest{ScheduleId: "nightly"})
	if err != nil {
		t.Fatalf("GetSchedule(roadmap): %v", err)
	}
	if got.GetId() != "nightly" {
		t.Fatalf("GetSchedule id = %q, want nightly", got.GetId())
	}

	paused, err := roadmap.PauseSchedule(context.Background(), &proto.PauseWorkflowScheduleRequest{ScheduleId: "nightly"})
	if err != nil {
		t.Fatalf("PauseSchedule(roadmap): %v", err)
	}
	if !paused.GetPaused() {
		t.Fatalf("PauseSchedule response = %#v", paused)
	}
	if schedules[analyticsID].Paused {
		t.Fatalf("analytics schedule was mutated by roadmap pause: %#v", schedules[analyticsID])
	}

	if _, err := roadmap.DeleteSchedule(context.Background(), &proto.DeleteWorkflowScheduleRequest{ScheduleId: "nightly"}); err != nil {
		t.Fatalf("DeleteSchedule(roadmap): %v", err)
	}
	if _, ok := schedules[roadmapID]; ok {
		t.Fatalf("roadmap schedule still present after delete: %#v", schedules)
	}
	if _, ok := schedules[analyticsID]; !ok {
		t.Fatalf("analytics schedule missing after roadmap delete: %#v", schedules)
	}
}

func TestWorkflowServerNamespacesEventTriggerIDsPerPlugin(t *testing.T) {
	t.Parallel()

	triggers := map[string]*coreworkflow.EventTrigger{}
	provider := workflowProviderFunc{
		upsertEventTrigger: func(_ context.Context, req coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
			value := &coreworkflow.EventTrigger{
				ID:     req.TriggerID,
				Match:  req.Match,
				Target: req.Target,
				Paused: req.Paused,
			}
			triggers[req.TriggerID] = value
			return value, nil
		},
		getEventTrigger: func(_ context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
			value, ok := triggers[req.TriggerID]
			if !ok {
				return nil, status.Error(codes.NotFound, "missing event trigger")
			}
			return value, nil
		},
		deleteEventTrigger: func(_ context.Context, req coreworkflow.DeleteEventTriggerRequest) error {
			delete(triggers, req.TriggerID)
			return nil
		},
		pauseEventTrigger: func(_ context.Context, req coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
			value, ok := triggers[req.TriggerID]
			if !ok {
				return nil, status.Error(codes.NotFound, "missing event trigger")
			}
			cloned := *value
			cloned.Paused = true
			triggers[req.TriggerID] = &cloned
			return &cloned, nil
		},
	}

	roadmap := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))
	analytics := NewWorkflowServer("analytics", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	for _, tc := range []struct {
		server *WorkflowServer
		plugin string
	}{
		{server: roadmap, plugin: "roadmap"},
		{server: analytics, plugin: "analytics"},
	} {
		_, err := tc.server.UpsertEventTrigger(context.Background(), &proto.UpsertWorkflowEventTriggerRequest{
			TriggerId: "trigger-1",
			Match:     &proto.WorkflowEventMatch{Type: "entity.updated"},
			Target:    &proto.WorkflowTarget{Operation: "refresh"},
		})
		if err != nil {
			t.Fatalf("UpsertEventTrigger(%s): %v", tc.plugin, err)
		}
	}

	roadmapID := scopedWorkflowObjectID("roadmap", "trigger-1")
	analyticsID := scopedWorkflowObjectID("analytics", "trigger-1")
	if len(triggers) != 2 {
		t.Fatalf("stored triggers = %#v", triggers)
	}
	if _, ok := triggers[roadmapID]; !ok {
		t.Fatalf("missing roadmap scoped trigger %q", roadmapID)
	}
	if _, ok := triggers[analyticsID]; !ok {
		t.Fatalf("missing analytics scoped trigger %q", analyticsID)
	}

	got, err := roadmap.GetEventTrigger(context.Background(), &proto.GetWorkflowEventTriggerRequest{TriggerId: "trigger-1"})
	if err != nil {
		t.Fatalf("GetEventTrigger(roadmap): %v", err)
	}
	if got.GetId() != "trigger-1" {
		t.Fatalf("GetEventTrigger id = %q, want trigger-1", got.GetId())
	}

	paused, err := roadmap.PauseEventTrigger(context.Background(), &proto.PauseWorkflowEventTriggerRequest{TriggerId: "trigger-1"})
	if err != nil {
		t.Fatalf("PauseEventTrigger(roadmap): %v", err)
	}
	if !paused.GetPaused() {
		t.Fatalf("PauseEventTrigger response = %#v", paused)
	}
	if triggers[analyticsID].Paused {
		t.Fatalf("analytics trigger was mutated by roadmap pause: %#v", triggers[analyticsID])
	}

	if _, err := roadmap.DeleteEventTrigger(context.Background(), &proto.DeleteWorkflowEventTriggerRequest{TriggerId: "trigger-1"}); err != nil {
		t.Fatalf("DeleteEventTrigger(roadmap): %v", err)
	}
	if _, ok := triggers[roadmapID]; ok {
		t.Fatalf("roadmap trigger still present after delete: %#v", triggers)
	}
	if _, ok := triggers[analyticsID]; !ok {
		t.Fatalf("analytics trigger missing after roadmap delete: %#v", triggers)
	}
}

func TestWorkflowServerFallsBackToBareScheduleIDsForScopedReads(t *testing.T) {
	t.Parallel()

	provider := workflowProviderFunc{
		getSchedule: func(_ context.Context, req coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
			if req.ScheduleID != "legacy-nightly" {
				return nil, status.Error(codes.NotFound, "missing schedule")
			}
			return &coreworkflow.Schedule{
				ID:       "legacy-nightly",
				Cron:     "*/5 * * * *",
				Timezone: "UTC",
				Target: coreworkflow.Target{
					PluginName: "roadmap",
					Operation:  "refresh",
				},
			}, nil
		},
	}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	got, err := srv.GetSchedule(context.Background(), &proto.GetWorkflowScheduleRequest{ScheduleId: "legacy-nightly"})
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.GetId() != "legacy-nightly" {
		t.Fatalf("GetSchedule id = %q, want legacy-nightly", got.GetId())
	}
}

func TestWorkflowServerFallsBackToBareEventTriggerIDsForScopedReads(t *testing.T) {
	t.Parallel()

	provider := workflowProviderFunc{
		getEventTrigger: func(_ context.Context, req coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
			if req.TriggerID != "legacy-trigger" {
				return nil, status.Error(codes.NotFound, "missing trigger")
			}
			return &coreworkflow.EventTrigger{
				ID: "legacy-trigger",
				Match: coreworkflow.EventMatch{
					Type: "entity.updated",
				},
				Target: coreworkflow.Target{
					PluginName: "roadmap",
					Operation:  "refresh",
				},
			}, nil
		},
	}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	got, err := srv.GetEventTrigger(context.Background(), &proto.GetWorkflowEventTriggerRequest{TriggerId: "legacy-trigger"})
	if err != nil {
		t.Fatalf("GetEventTrigger: %v", err)
	}
	if got.GetId() != "legacy-trigger" {
		t.Fatalf("GetEventTrigger id = %q, want legacy-trigger", got.GetId())
	}
}

func TestWorkflowServerRejectsDisabledScheduleOperations(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	_, err := srv.UpsertSchedule(context.Background(), &proto.UpsertWorkflowScheduleRequest{
		ScheduleId: "sched-1",
		Cron:       "*/5 * * * *",
		Timezone:   "UTC",
		Target: &proto.WorkflowTarget{
			Operation: "blocked",
		},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want %v", status.Code(err), codes.PermissionDenied)
	}
	if provider.upsertScheduleCalled {
		t.Fatal("provider UpsertSchedule should not be called for disabled operations")
	}
}

func TestWorkflowServerRejectsConfigManagedScheduleMutations(t *testing.T) {
	t.Parallel()

	managedID := coreworkflow.ConfigManagedSchedulePrefix + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, srv *WorkflowServer) error
	}{
		{
			name: "upsert",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.UpsertSchedule(context.Background(), &proto.UpsertWorkflowScheduleRequest{
					ScheduleId: managedID,
					Cron:       "*/5 * * * *",
					Timezone:   "UTC",
					Target:     &proto.WorkflowTarget{Operation: "refresh"},
				})
				return err
			},
		},
		{
			name: "delete",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.DeleteSchedule(context.Background(), &proto.DeleteWorkflowScheduleRequest{
					ScheduleId: managedID,
				})
				return err
			},
		},
		{
			name: "pause",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.PauseSchedule(context.Background(), &proto.PauseWorkflowScheduleRequest{
					ScheduleId: managedID,
				})
				return err
			},
		},
		{
			name: "resume",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.ResumeSchedule(context.Background(), &proto.ResumeWorkflowScheduleRequest{
					ScheduleId: managedID,
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
				upsertSchedule: func(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
					t.Fatal("provider UpsertSchedule should not be called for config-managed ids")
					return nil, nil
				},
				deleteSchedule: func(context.Context, coreworkflow.DeleteScheduleRequest) error {
					t.Fatal("provider DeleteSchedule should not be called for config-managed ids")
					return nil
				},
				pauseSchedule: func(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
					t.Fatal("provider PauseSchedule should not be called for config-managed ids")
					return nil, nil
				},
				resumeSchedule: func(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
					t.Fatal("provider ResumeSchedule should not be called for config-managed ids")
					return nil, nil
				},
			}, map[string]struct{}{"refresh": {}}, map[string]struct{}{managedID: {}}, nil, nil))

			err := tc.run(t, srv)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("status code = %v, want %v", status.Code(err), codes.PermissionDenied)
			}
		})
	}
}

func TestWorkflowServerAllowsUserScheduleIDsThatOnlyShareCfgPrefix(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	_, err := srv.UpsertSchedule(context.Background(), &proto.UpsertWorkflowScheduleRequest{
		ScheduleId: "cfg_backup",
		Cron:       "*/5 * * * *",
		Timezone:   "UTC",
		Target: &proto.WorkflowTarget{
			Operation:  "refresh",
			Connection: "analytics",
			Instance:   "tenant-a",
		},
	})
	if err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}
	if !provider.upsertScheduleCalled {
		t.Fatal("provider UpsertSchedule was not called")
	}
	if provider.upsertScheduleReq.ScheduleID != scopedWorkflowObjectID("roadmap", "cfg_backup") {
		t.Fatalf("schedule id = %q, want %q", provider.upsertScheduleReq.ScheduleID, scopedWorkflowObjectID("roadmap", "cfg_backup"))
	}
	if provider.upsertScheduleReq.Target.Connection != "analytics" || provider.upsertScheduleReq.Target.Instance != "tenant-a" {
		t.Fatalf("schedule target selectors = %#v", provider.upsertScheduleReq.Target)
	}
}

func TestWorkflowServerRejectsConfigManagedEventTriggerMutations(t *testing.T) {
	t.Parallel()

	managedID := coreworkflow.ConfigManagedSchedulePrefix + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, srv *WorkflowServer) error
	}{
		{
			name: "upsert",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.UpsertEventTrigger(context.Background(), &proto.UpsertWorkflowEventTriggerRequest{
					TriggerId: managedID,
					Match: &proto.WorkflowEventMatch{
						Type: "roadmap.task.updated",
					},
					Target: &proto.WorkflowTarget{Operation: "refresh"},
				})
				return err
			},
		},
		{
			name: "delete",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.DeleteEventTrigger(context.Background(), &proto.DeleteWorkflowEventTriggerRequest{
					TriggerId: managedID,
				})
				return err
			},
		},
		{
			name: "pause",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.PauseEventTrigger(context.Background(), &proto.PauseWorkflowEventTriggerRequest{
					TriggerId: managedID,
				})
				return err
			},
		},
		{
			name: "resume",
			run: func(t *testing.T, srv *WorkflowServer) error {
				_, err := srv.ResumeEventTrigger(context.Background(), &proto.ResumeWorkflowEventTriggerRequest{
					TriggerId: managedID,
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
				upsertEventTrigger: func(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
					t.Fatal("provider UpsertEventTrigger should not be called for config-managed ids")
					return nil, nil
				},
				deleteEventTrigger: func(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
					t.Fatal("provider DeleteEventTrigger should not be called for config-managed ids")
					return nil
				},
				pauseEventTrigger: func(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
					t.Fatal("provider PauseEventTrigger should not be called for config-managed ids")
					return nil, nil
				},
				resumeEventTrigger: func(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
					t.Fatal("provider ResumeEventTrigger should not be called for config-managed ids")
					return nil, nil
				},
			}, map[string]struct{}{"refresh": {}}, nil, map[string]struct{}{managedID: {}}, nil))

			err := tc.run(t, srv)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("status code = %v, want %v", status.Code(err), codes.PermissionDenied)
			}
		})
	}
}

func TestWorkflowServerAllowsUserEventTriggerIDsThatOnlyShareCfgPrefix(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	_, err := srv.UpsertEventTrigger(context.Background(), &proto.UpsertWorkflowEventTriggerRequest{
		TriggerId: "cfg_backup",
		Match: &proto.WorkflowEventMatch{
			Type: "roadmap.task.updated",
		},
		Target: &proto.WorkflowTarget{Operation: "refresh"},
	})
	if err != nil {
		t.Fatalf("UpsertEventTrigger: %v", err)
	}
	if provider.upsertEventTriggerReq.TriggerID != scopedWorkflowObjectID("roadmap", "cfg_backup") {
		t.Fatalf("trigger id = %q, want %q", provider.upsertEventTriggerReq.TriggerID, scopedWorkflowObjectID("roadmap", "cfg_backup"))
	}
}

func TestWorkflowServerPrefersWorkflowCreatorForWorkflowMutations(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID:   principal.WorkloadSubjectID("planner"),
		Kind:        principal.KindWorkload,
		DisplayName: "Planner",
		Source:      principal.SourceWorkloadToken,
	})
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{
		"createdBy": map[string]any{
			"subjectId":   principal.UserSubjectID("user-123"),
			"subjectKind": string(principal.KindUser),
			"displayName": "Ada",
			"authSource":  principal.SourceAPIToken.String(),
		},
	})

	run, err := srv.StartRun(ctx, &proto.StartWorkflowRunRequest{
		Target: &proto.WorkflowTarget{
			Operation: "refresh",
		},
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	schedule, err := srv.UpsertSchedule(ctx, &proto.UpsertWorkflowScheduleRequest{
		ScheduleId: "sched-1",
		Cron:       "*/5 * * * *",
		Timezone:   "UTC",
		Target: &proto.WorkflowTarget{
			Operation: "refresh",
		},
	})
	if err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}
	trigger, err := srv.UpsertEventTrigger(ctx, &proto.UpsertWorkflowEventTriggerRequest{
		TriggerId: "trigger-1",
		Match: &proto.WorkflowEventMatch{
			Type: "roadmap.task.updated",
		},
		Target: &proto.WorkflowTarget{
			Operation: "refresh",
		},
	})
	if err != nil {
		t.Fatalf("UpsertEventTrigger: %v", err)
	}

	for name, actor := range map[string]coreworkflow.Actor{
		"run":      provider.startRunReq.CreatedBy,
		"schedule": provider.upsertScheduleReq.RequestedBy,
		"trigger":  provider.upsertEventTriggerReq.RequestedBy,
	} {
		if actor.SubjectID != principal.UserSubjectID("user-123") || actor.SubjectKind != string(principal.KindUser) || actor.AuthSource != principal.SourceAPIToken.String() {
			t.Fatalf("%s createdBy = %#v", name, actor)
		}
	}
	if run.GetCreatedBy().GetSubjectId() != principal.UserSubjectID("user-123") {
		t.Fatalf("run createdBy = %#v", run.GetCreatedBy())
	}
	if schedule.GetCreatedBy().GetSubjectId() != principal.UserSubjectID("user-123") {
		t.Fatalf("schedule createdBy = %#v", schedule.GetCreatedBy())
	}
	if trigger.GetCreatedBy().GetSubjectId() != principal.UserSubjectID("user-123") {
		t.Fatalf("trigger createdBy = %#v", trigger.GetCreatedBy())
	}
}

func TestWorkflowServerRejectsCrossPluginProviderResponses(t *testing.T) {
	t.Parallel()

	t.Run("run", func(t *testing.T) {
		t.Parallel()

		srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
			startRun: func(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
				return &coreworkflow.Run{
					ID:     "run-1",
					Status: coreworkflow.RunStatusPending,
					Target: coreworkflow.Target{
						PluginName: "analytics",
						Operation:  "refresh",
					},
				}, nil
			},
		}, map[string]struct{}{"refresh": {}}, nil, nil, nil))

		_, err := srv.StartRun(context.Background(), &proto.StartWorkflowRunRequest{
			Target: &proto.WorkflowTarget{Operation: "refresh"},
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("status code = %v, want %v", status.Code(err), codes.Internal)
		}
	})

	t.Run("schedule", func(t *testing.T) {
		t.Parallel()

		srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
			upsertSchedule: func(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
				return &coreworkflow.Schedule{
					ID:       "sched-1",
					Cron:     "*/5 * * * *",
					Timezone: "UTC",
					Target: coreworkflow.Target{
						PluginName: "analytics",
						Operation:  "refresh",
					},
				}, nil
			},
		}, map[string]struct{}{"refresh": {}}, nil, nil, nil))

		_, err := srv.UpsertSchedule(context.Background(), &proto.UpsertWorkflowScheduleRequest{
			ScheduleId: "sched-1",
			Cron:       "*/5 * * * *",
			Timezone:   "UTC",
			Target: &proto.WorkflowTarget{
				Operation: "refresh",
			},
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("status code = %v, want %v", status.Code(err), codes.Internal)
		}
	})

	t.Run("event trigger", func(t *testing.T) {
		t.Parallel()

		srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
			upsertEventTrigger: func(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
				return &coreworkflow.EventTrigger{
					ID: "trigger-1",
					Match: coreworkflow.EventMatch{
						Type: "roadmap.task.updated",
					},
					Target: coreworkflow.Target{
						PluginName: "analytics",
						Operation:  "refresh",
					},
				}, nil
			},
		}, map[string]struct{}{"refresh": {}}, nil, nil, nil))

		_, err := srv.UpsertEventTrigger(context.Background(), &proto.UpsertWorkflowEventTriggerRequest{
			TriggerId: "trigger-1",
			Match: &proto.WorkflowEventMatch{
				Type: "roadmap.task.updated",
			},
			Target: &proto.WorkflowTarget{
				Operation: "refresh",
			},
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("status code = %v, want %v", status.Code(err), codes.Internal)
		}
	})
}

func TestWorkflowServerListCallsScopeToPlugin(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	runs, err := srv.ListRuns(context.Background(), &proto.ListWorkflowRunsRequest{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	schedules, err := srv.ListSchedules(context.Background(), &proto.ListWorkflowSchedulesRequest{})
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	triggers, err := srv.ListEventTriggers(context.Background(), &proto.ListWorkflowEventTriggersRequest{})
	if err != nil {
		t.Fatalf("ListEventTriggers: %v", err)
	}

	for name, got := range map[string]any{
		"runs":      provider.listRunsReq,
		"schedules": provider.listSchedulesReq,
		"triggers":  provider.listEventTriggersReq,
	} {
		if !reflect.DeepEqual(got, reflect.New(reflect.TypeOf(got)).Elem().Interface()) {
			t.Fatalf("%s list request = %#v, want zero-value global request", name, got)
		}
	}
	if len(runs.GetRuns()) != 1 || runs.GetRuns()[0].GetTarget().GetOperation() != "refresh" {
		t.Fatalf("runs response = %+v", runs.GetRuns())
	}
	if len(schedules.GetSchedules()) != 1 || schedules.GetSchedules()[0].GetTarget().GetOperation() != "refresh" {
		t.Fatalf("schedules response = %+v", schedules.GetSchedules())
	}
	if len(triggers.GetTriggers()) != 1 || triggers.GetTriggers()[0].GetTarget().GetOperation() != "refresh" {
		t.Fatalf("triggers response = %+v", triggers.GetTriggers())
	}
}

func TestWorkflowHostServerForwardsInvokeRequest(t *testing.T) {
	t.Parallel()

	scheduledFor := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	var got coreworkflow.InvokeOperationRequest
	srv := NewWorkflowHostServer(
		"temporal",
		func(_ context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
			got = req
			return &coreworkflow.InvokeOperationResponse{
				Status: 202,
				Body:   `{"ok":true}`,
			}, nil
		},
		func(providerName, pluginName, operation string) bool {
			return providerName == "temporal" && pluginName == "roadmap" && operation == "refresh"
		},
	)

	resp, err := srv.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		Target: &proto.BoundWorkflowTarget{
			PluginName: " roadmap ",
			Operation:  " refresh ",
			Input:      mustStruct(t, map[string]any{"taskId": "task-123"}),
			Connection: " analytics ",
			Instance:   " tenant-a ",
		},
		RunId:        "run-123",
		ExecutionRef: "exec-ref-123",
		Trigger: &proto.WorkflowRunTrigger{
			Kind: &proto.WorkflowRunTrigger_Schedule{
				Schedule: &proto.WorkflowScheduleTrigger{
					ScheduleId:   "sched-1",
					ScheduledFor: timestamppb.New(scheduledFor),
				},
			},
		},
		Input:     mustStruct(t, map[string]any{"eventTaskId": "task-456"}),
		Metadata:  mustStruct(t, map[string]any{"attempt": float64(2)}),
		CreatedBy: &proto.WorkflowActor{SubjectId: principal.UserSubjectID("user-123"), SubjectKind: string(principal.KindUser), DisplayName: "Ada", AuthSource: principal.SourceAPIToken.String()},
	})
	if err != nil {
		t.Fatalf("InvokeOperation: %v", err)
	}
	if got.ProviderName != "temporal" {
		t.Fatalf("provider name = %q, want %q", got.ProviderName, "temporal")
	}
	if got.RunID != "run-123" {
		t.Fatalf("run id = %q, want %q", got.RunID, "run-123")
	}
	if got.Target.PluginName != "roadmap" || got.Target.Operation != "refresh" {
		t.Fatalf("target = %#v", got.Target)
	}
	if got.Target.Connection != "analytics" || got.Target.Instance != "tenant-a" {
		t.Fatalf("target selectors = %#v", got.Target)
	}
	if !reflect.DeepEqual(got.Target.Input, map[string]any{"taskId": "task-123"}) {
		t.Fatalf("target input = %#v", got.Target.Input)
	}
	if !reflect.DeepEqual(got.Input, map[string]any{"eventTaskId": "task-456"}) {
		t.Fatalf("input = %#v", got.Input)
	}
	if !reflect.DeepEqual(got.Metadata, map[string]any{"attempt": float64(2)}) {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
	if got.ExecutionRef != "exec-ref-123" {
		t.Fatalf("execution ref = %q, want %q", got.ExecutionRef, "exec-ref-123")
	}
	if got.CreatedBy.SubjectID != principal.UserSubjectID("user-123") || got.CreatedBy.DisplayName != "Ada" {
		t.Fatalf("createdBy = %#v", got.CreatedBy)
	}
	if got.Trigger.Schedule == nil || got.Trigger.Schedule.ScheduleID != "sched-1" {
		t.Fatalf("schedule trigger = %#v", got.Trigger.Schedule)
	}
	if got.Trigger.Schedule.ScheduledFor == nil || !got.Trigger.Schedule.ScheduledFor.Equal(scheduledFor) {
		t.Fatalf("scheduled for = %#v, want %v", got.Trigger.Schedule.ScheduledFor, scheduledFor)
	}
	if resp.GetStatus() != 202 || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
}

func TestWorkflowHostServerAllowsCrossPluginTargetsWhenAuthorized(t *testing.T) {
	t.Parallel()

	called := false
	srv := NewWorkflowHostServer(
		"temporal",
		func(_ context.Context, req coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
			called = true
			if req.Target.PluginName != "analytics" || req.Target.Operation != "refresh" {
				t.Fatalf("target = %#v", req.Target)
			}
			return &coreworkflow.InvokeOperationResponse{Status: 202}, nil
		},
		func(providerName, pluginName, operation string) bool {
			return providerName == "temporal" && pluginName == "analytics" && operation == "refresh"
		},
	)

	resp, err := srv.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		Target: &proto.BoundWorkflowTarget{
			PluginName: "analytics",
			Operation:  "refresh",
		},
	})
	if err != nil {
		t.Fatalf("InvokeOperation: %v", err)
	}
	if !called {
		t.Fatal("invoke was not called")
	}
	if resp.GetStatus() != 202 {
		t.Fatalf("status = %d, want 202", resp.GetStatus())
	}
}

func TestWorkflowHostServerRejectsDisallowedOperations(t *testing.T) {
	t.Parallel()

	srv := NewWorkflowHostServer(
		"temporal",
		func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
			t.Fatal("invoke should not be called for disallowed targets")
			return nil, nil
		},
		func(string, string, string) bool { return false },
	)

	_, err := srv.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		Target: &proto.BoundWorkflowTarget{
			PluginName: "roadmap",
			Operation:  "blocked",
		},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want %v", status.Code(err), codes.PermissionDenied)
	}
}

func TestWorkflowHostServerMapsInvocationErrors(t *testing.T) {
	t.Parallel()

	srv := NewWorkflowHostServer(
		"temporal",
		func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
			return nil, invocation.ErrOperationNotFound
		},
		func(string, string, string) bool { return true },
	)

	_, err := srv.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		Target: &proto.BoundWorkflowTarget{
			PluginName: "roadmap",
			Operation:  "missing",
		},
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status code = %v, want %v", status.Code(err), codes.NotFound)
	}
}

func TestWorkflowHostServerMapsInternalInvocationErrors(t *testing.T) {
	t.Parallel()

	srv := NewWorkflowHostServer(
		"temporal",
		func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error) {
			return nil, invocation.ErrInternal
		},
		func(string, string, string) bool { return true },
	)

	_, err := srv.InvokeOperation(context.Background(), &proto.InvokeWorkflowOperationRequest{
		Target: &proto.BoundWorkflowTarget{
			PluginName: "roadmap",
			Operation:  "sync",
		},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("status code = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestWorkflowServerPublishEventScopesPlugin(t *testing.T) {
	t.Parallel()

	provider := &recordingWorkflowProvider{}
	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(provider, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	_, err := srv.PublishEvent(context.Background(), &proto.PublishWorkflowEventRequest{
		Event: &proto.WorkflowEvent{
			Type:   "roadmap.task.updated",
			Source: "user-supplied",
		},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if provider.publishEventReq.PluginName != "roadmap" {
		t.Fatalf("publish plugin = %q, want %q", provider.publishEventReq.PluginName, "roadmap")
	}
	if provider.publishEventReq.Event.Type != "roadmap.task.updated" || provider.publishEventReq.Event.Source != "user-supplied" {
		t.Fatalf("publish event = %#v", provider.publishEventReq.Event)
	}
}

func TestWorkflowServerPreservesProviderStatusCodes(t *testing.T) {
	t.Parallel()

	srv := NewWorkflowServer("roadmap", staticWorkflowResolver(workflowProviderFunc{
		getRun: func(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
			return nil, status.Error(codes.NotFound, "missing run")
		},
	}, map[string]struct{}{"refresh": {}}, nil, nil, nil))

	_, err := srv.GetRun(context.Background(), &proto.GetWorkflowRunRequest{RunId: "run-123"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status code = %v, want %v", status.Code(err), codes.NotFound)
	}
	if got := status.Convert(err).Message(); got != "workflow get run: missing run" {
		t.Fatalf("status message = %q, want %q", got, "workflow get run: missing run")
	}
}
