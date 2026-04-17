package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type WorkflowManagedIDs struct {
	Schedules     map[string]struct{}
	EventTriggers map[string]struct{}
}

type workflowProviderResolver func() (coreworkflow.Provider, map[string]struct{}, WorkflowManagedIDs, error)

type WorkflowServer struct {
	proto.UnimplementedWorkflowServer
	resolver   workflowProviderResolver
	pluginName string
}

func NewWorkflowServer(pluginName string, resolver workflowProviderResolver) *WorkflowServer {
	return &WorkflowServer{
		resolver:   resolver,
		pluginName: pluginName,
	}
}

func (s *WorkflowServer) StartRun(ctx context.Context, req *proto.StartWorkflowRunRequest) (*proto.WorkflowRun, error) {
	provider, allowed, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	target := pluginWorkflowTargetFromProto(s.pluginName, req.GetTarget())
	if err := validateWorkflowOperationAllowed(target.Operation, allowed); err != nil {
		return nil, err
	}
	run, err := provider.StartRun(ctx, coreworkflow.StartRunRequest{
		Target:         target,
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorFromContext(ctx),
	})
	if err != nil {
		return nil, workflowServerError("workflow start run", err)
	}
	return workflowRunToScopedPluginProto(s.pluginName, run)
}

func (s *WorkflowServer) GetRun(ctx context.Context, req *proto.GetWorkflowRunRequest) (*proto.WorkflowRun, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{
		PluginName: s.pluginName,
		RunID:      req.GetRunId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow get run", err)
	}
	return workflowRunToScopedPluginProto(s.pluginName, run)
}

func (s *WorkflowServer) ListRuns(ctx context.Context, _ *proto.ListWorkflowRunsRequest) (*proto.ListWorkflowRunsResponse, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	runs, err := provider.ListRuns(ctx, coreworkflow.ListRunsRequest{PluginName: s.pluginName})
	if err != nil {
		return nil, workflowServerError("workflow list runs", err)
	}
	resp := &proto.ListWorkflowRunsResponse{Runs: make([]*proto.WorkflowRun, 0, len(runs))}
	for _, run := range runs {
		value, err := workflowRunToScopedPluginProto(s.pluginName, run)
		if err != nil {
			return nil, workflowServerError("workflow list runs", err)
		}
		resp.Runs = append(resp.Runs, value)
	}
	return resp, nil
}

func (s *WorkflowServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowRunRequest) (*proto.WorkflowRun, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	run, err := provider.CancelRun(ctx, coreworkflow.CancelRunRequest{
		PluginName: s.pluginName,
		RunID:      req.GetRunId(),
		Reason:     req.GetReason(),
	})
	if err != nil {
		return nil, workflowServerError("workflow cancel run", err)
	}
	return workflowRunToScopedPluginProto(s.pluginName, run)
}

func (s *WorkflowServer) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	provider, allowed, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	target := pluginWorkflowTargetFromProto(s.pluginName, req.GetTarget())
	if err := validateWorkflowOperationAllowed(target.Operation, allowed); err != nil {
		return nil, err
	}
	value, err := provider.UpsertSchedule(ctx, coreworkflow.UpsertScheduleRequest{
		ScheduleID:  req.GetScheduleId(),
		Cron:        req.GetCron(),
		Timezone:    req.GetTimezone(),
		Target:      target,
		Paused:      req.GetPaused(),
		RequestedBy: workflowActorFromContext(ctx),
	})
	if err != nil {
		return nil, workflowServerError("workflow upsert schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) GetSchedule(ctx context.Context, req *proto.GetWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	value, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{
		PluginName: s.pluginName,
		ScheduleID: req.GetScheduleId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow get schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ListSchedules(ctx context.Context, _ *proto.ListWorkflowSchedulesRequest) (*proto.ListWorkflowSchedulesResponse, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	values, err := provider.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{PluginName: s.pluginName})
	if err != nil {
		return nil, workflowServerError("workflow list schedules", err)
	}
	resp := &proto.ListWorkflowSchedulesResponse{Schedules: make([]*proto.WorkflowSchedule, 0, len(values))}
	for _, value := range values {
		pbValue, err := workflowScheduleToScopedPluginProto(s.pluginName, value)
		if err != nil {
			return nil, workflowServerError("workflow list schedules", err)
		}
		resp.Schedules = append(resp.Schedules, pbValue)
	}
	return resp, nil
}

func (s *WorkflowServer) DeleteSchedule(ctx context.Context, req *proto.DeleteWorkflowScheduleRequest) (*emptypb.Empty, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	if err := provider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		PluginName: s.pluginName,
		ScheduleID: req.GetScheduleId(),
	}); err != nil {
		return nil, workflowServerError("workflow delete schedule", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowServer) PauseSchedule(ctx context.Context, req *proto.PauseWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	value, err := provider.PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{
		PluginName: s.pluginName,
		ScheduleID: req.GetScheduleId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow pause schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ResumeSchedule(ctx context.Context, req *proto.ResumeWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	value, err := provider.ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{
		PluginName: s.pluginName,
		ScheduleID: req.GetScheduleId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow resume schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	provider, allowed, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	target := pluginWorkflowTargetFromProto(s.pluginName, req.GetTarget())
	if err := validateWorkflowOperationAllowed(target.Operation, allowed); err != nil {
		return nil, err
	}
	value, err := provider.UpsertEventTrigger(ctx, coreworkflow.UpsertEventTriggerRequest{
		TriggerID:   req.GetTriggerId(),
		Match:       workflowEventMatchFromProto(req.GetMatch()),
		Target:      target,
		Paused:      req.GetPaused(),
		RequestedBy: workflowActorFromContext(ctx),
	})
	if err != nil {
		return nil, workflowServerError("workflow upsert event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) GetEventTrigger(ctx context.Context, req *proto.GetWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	value, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{
		PluginName: s.pluginName,
		TriggerID:  req.GetTriggerId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow get event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ListEventTriggers(ctx context.Context, _ *proto.ListWorkflowEventTriggersRequest) (*proto.ListWorkflowEventTriggersResponse, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	values, err := provider.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{PluginName: s.pluginName})
	if err != nil {
		return nil, workflowServerError("workflow list event triggers", err)
	}
	resp := &proto.ListWorkflowEventTriggersResponse{Triggers: make([]*proto.WorkflowEventTrigger, 0, len(values))}
	for _, value := range values {
		pbValue, err := workflowEventTriggerToScopedPluginProto(s.pluginName, value)
		if err != nil {
			return nil, workflowServerError("workflow list event triggers", err)
		}
		resp.Triggers = append(resp.Triggers, pbValue)
	}
	return resp, nil
}

func (s *WorkflowServer) DeleteEventTrigger(ctx context.Context, req *proto.DeleteWorkflowEventTriggerRequest) (*emptypb.Empty, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	if err := provider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
		PluginName: s.pluginName,
		TriggerID:  req.GetTriggerId(),
	}); err != nil {
		return nil, workflowServerError("workflow delete event trigger", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowServer) PauseEventTrigger(ctx context.Context, req *proto.PauseWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	value, err := provider.PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{
		PluginName: s.pluginName,
		TriggerID:  req.GetTriggerId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow pause event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ResumeEventTrigger(ctx context.Context, req *proto.ResumeWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	value, err := provider.ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{
		PluginName: s.pluginName,
		TriggerID:  req.GetTriggerId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow resume event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) PublishEvent(ctx context.Context, req *proto.PublishWorkflowEventRequest) (*emptypb.Empty, error) {
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	event, err := workflowEventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "workflow publish event: %v", err)
	}
	if err := provider.PublishEvent(ctx, coreworkflow.PublishEventRequest{
		PluginName: s.pluginName,
		Event:      event,
	}); err != nil {
		return nil, workflowServerError("workflow publish event", err)
	}
	return &emptypb.Empty{}, nil
}

func workflowServerError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := status.FromError(err); ok {
		return status.Errorf(existing.Code(), "%s: %s", operation, existing.Message())
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}

func workflowActorFromContext(ctx context.Context) coreworkflow.Actor {
	if actor, ok := workflowActorFromWorkflowContext(ctx); ok {
		return actor
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return coreworkflow.Actor{}
	}
	return coreworkflow.Actor{
		SubjectID:   subjectIDForPrincipal(p),
		SubjectKind: subjectKindForPrincipal(p),
		DisplayName: subjectDisplayName(p),
		AuthSource:  p.AuthSource(),
	}
}

func workflowActorFromWorkflowContext(ctx context.Context) (coreworkflow.Actor, bool) {
	workflow := invocation.WorkflowContextFromContext(ctx)
	if workflow == nil {
		return coreworkflow.Actor{}, false
	}
	value, ok := workflow["createdBy"].(map[string]any)
	if !ok {
		return coreworkflow.Actor{}, false
	}
	actor := coreworkflow.Actor{
		SubjectID:   invocation.WorkflowContextString(value, "subjectId"),
		SubjectKind: invocation.WorkflowContextString(value, "subjectKind"),
		DisplayName: invocation.WorkflowContextString(value, "displayName"),
		AuthSource:  invocation.WorkflowContextString(value, "authSource"),
	}
	return actor, !isEmptyWorkflowActor(actor)
}

func isEmptyWorkflowActor(actor coreworkflow.Actor) bool {
	return actor.SubjectID == "" &&
		actor.SubjectKind == "" &&
		actor.DisplayName == "" &&
		actor.AuthSource == ""
}

func workflowRunToScopedPluginProto(pluginName string, run *coreworkflow.Run) (*proto.WorkflowRun, error) {
	if run == nil {
		return nil, nil
	}
	if err := validateWorkflowTargetScope(pluginName, run.Target, "run", run.ID); err != nil {
		return nil, err
	}
	return workflowRunToPluginProto(run)
}

func workflowScheduleToScopedPluginProto(pluginName string, schedule *coreworkflow.Schedule) (*proto.WorkflowSchedule, error) {
	if schedule == nil {
		return nil, nil
	}
	if err := validateWorkflowTargetScope(pluginName, schedule.Target, "schedule", schedule.ID); err != nil {
		return nil, err
	}
	return workflowScheduleToPluginProto(schedule)
}

func workflowEventTriggerToScopedPluginProto(pluginName string, trigger *coreworkflow.EventTrigger) (*proto.WorkflowEventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	if err := validateWorkflowTargetScope(pluginName, trigger.Target, "event trigger", trigger.ID); err != nil {
		return nil, err
	}
	return workflowEventTriggerToPluginProto(trigger)
}

func validateWorkflowTargetScope(pluginName string, target coreworkflow.Target, objectKind, objectID string) error {
	if pluginName == "" {
		return nil
	}
	if target.PluginName == pluginName {
		return nil
	}
	if objectID == "" {
		return status.Errorf(codes.Internal, "workflow %s target plugin %q does not match scoped plugin %q", objectKind, target.PluginName, pluginName)
	}
	return status.Errorf(codes.Internal, "workflow %s %q target plugin %q does not match scoped plugin %q", objectKind, objectID, target.PluginName, pluginName)
}

func (s *WorkflowServer) resolve() (coreworkflow.Provider, map[string]struct{}, WorkflowManagedIDs, error) {
	if s == nil || s.resolver == nil {
		return nil, nil, WorkflowManagedIDs{}, status.Error(codes.FailedPrecondition, "workflow provider is not configured")
	}
	provider, allowed, managedIDs, err := s.resolver()
	if err != nil {
		return nil, nil, WorkflowManagedIDs{}, status.Errorf(codes.FailedPrecondition, "workflow provider: %v", err)
	}
	if provider == nil {
		return nil, nil, WorkflowManagedIDs{}, status.Error(codes.FailedPrecondition, "workflow provider is not available")
	}
	return provider, allowed, managedIDs, nil
}

func validateWorkflowOperationAllowed(operation string, allowed map[string]struct{}) error {
	if operation == "" {
		return status.Error(codes.InvalidArgument, "workflow target operation is required")
	}
	if len(allowed) == 0 {
		return status.Error(codes.FailedPrecondition, "workflow target operations are not configured")
	}
	if _, ok := allowed[operation]; !ok {
		return status.Errorf(codes.PermissionDenied, "workflow target operation %q is not enabled", operation)
	}
	return nil
}

func rejectManagedWorkflowObjectID(kind, objectID, plural string, managedIDs map[string]struct{}) error {
	if _, ok := managedIDs[objectID]; ok {
		return status.Errorf(codes.PermissionDenied, "workflow %s id %q is reserved for config-managed %s", kind, objectID, plural)
	}
	return nil
}

var _ proto.WorkflowServer = (*WorkflowServer)(nil)
