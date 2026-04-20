package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
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
	ctx = workflowScopeContext(ctx, s.pluginName)
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
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	run, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{
		RunID: req.GetRunId(),
	})
	if err != nil {
		return nil, workflowServerError("workflow get run", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, run.Target, "run", req.GetRunId()); err != nil {
		return nil, workflowServerError("workflow get run", err)
	}
	return workflowRunToScopedPluginProto(s.pluginName, run)
}

func (s *WorkflowServer) ListRuns(ctx context.Context, _ *proto.ListWorkflowRunsRequest) (*proto.ListWorkflowRunsResponse, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	runs, err := provider.ListRuns(ctx, coreworkflow.ListRunsRequest{})
	if err != nil {
		return nil, workflowServerError("workflow list runs", err)
	}
	resp := &proto.ListWorkflowRunsResponse{Runs: make([]*proto.WorkflowRun, 0, len(runs))}
	for _, run := range runs {
		if !workflowTargetInScope(s.pluginName, run.Target) {
			continue
		}
		value, err := workflowRunToScopedPluginProto(s.pluginName, run)
		if err != nil {
			return nil, workflowServerError("workflow list runs", err)
		}
		resp.Runs = append(resp.Runs, value)
	}
	return resp, nil
}

func (s *WorkflowServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowRunRequest) (*proto.WorkflowRun, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	existing, err := provider.GetRun(ctx, coreworkflow.GetRunRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, workflowServerError("workflow cancel run", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "run", req.GetRunId()); err != nil {
		return nil, workflowServerError("workflow cancel run", err)
	}
	run, err := provider.CancelRun(ctx, coreworkflow.CancelRunRequest{
		RunID:  req.GetRunId(),
		Reason: req.GetReason(),
	})
	if err != nil {
		return nil, workflowServerError("workflow cancel run", err)
	}
	return workflowRunToScopedPluginProto(s.pluginName, run)
}

func (s *WorkflowServer) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
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
		ScheduleID:  scopedWorkflowObjectID(s.pluginName, req.GetScheduleId()),
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
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	value, _, err := getScopedWorkflowSchedule(ctx, provider, s.pluginName, req.GetScheduleId())
	if err != nil {
		return nil, workflowServerError("workflow get schedule", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, value.Target, "schedule", req.GetScheduleId()); err != nil {
		return nil, workflowServerError("workflow get schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ListSchedules(ctx context.Context, _ *proto.ListWorkflowSchedulesRequest) (*proto.ListWorkflowSchedulesResponse, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	values, err := provider.ListSchedules(ctx, coreworkflow.ListSchedulesRequest{})
	if err != nil {
		return nil, workflowServerError("workflow list schedules", err)
	}
	resp := &proto.ListWorkflowSchedulesResponse{Schedules: make([]*proto.WorkflowSchedule, 0, len(values))}
	for _, value := range values {
		if !workflowTargetInScope(s.pluginName, value.Target) {
			continue
		}
		pbValue, err := workflowScheduleToScopedPluginProto(s.pluginName, value)
		if err != nil {
			return nil, workflowServerError("workflow list schedules", err)
		}
		resp.Schedules = append(resp.Schedules, pbValue)
	}
	return resp, nil
}

func (s *WorkflowServer) DeleteSchedule(ctx context.Context, req *proto.DeleteWorkflowScheduleRequest) (*emptypb.Empty, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	existing, scheduleID, err := getScopedWorkflowSchedule(ctx, provider, s.pluginName, req.GetScheduleId())
	if err != nil {
		return nil, workflowServerError("workflow delete schedule", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "schedule", req.GetScheduleId()); err != nil {
		return nil, workflowServerError("workflow delete schedule", err)
	}
	if err := provider.DeleteSchedule(ctx, coreworkflow.DeleteScheduleRequest{
		ScheduleID: scheduleID,
	}); err != nil {
		return nil, workflowServerError("workflow delete schedule", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowServer) PauseSchedule(ctx context.Context, req *proto.PauseWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	existing, scheduleID, err := getScopedWorkflowSchedule(ctx, provider, s.pluginName, req.GetScheduleId())
	if err != nil {
		return nil, workflowServerError("workflow pause schedule", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "schedule", req.GetScheduleId()); err != nil {
		return nil, workflowServerError("workflow pause schedule", err)
	}
	value, err := provider.PauseSchedule(ctx, coreworkflow.PauseScheduleRequest{
		ScheduleID: scheduleID,
	})
	if err != nil {
		return nil, workflowServerError("workflow pause schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ResumeSchedule(ctx context.Context, req *proto.ResumeWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("schedule", req.GetScheduleId(), "schedules", managedIDs.Schedules); err != nil {
		return nil, err
	}
	existing, scheduleID, err := getScopedWorkflowSchedule(ctx, provider, s.pluginName, req.GetScheduleId())
	if err != nil {
		return nil, workflowServerError("workflow resume schedule", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "schedule", req.GetScheduleId()); err != nil {
		return nil, workflowServerError("workflow resume schedule", err)
	}
	value, err := provider.ResumeSchedule(ctx, coreworkflow.ResumeScheduleRequest{
		ScheduleID: scheduleID,
	})
	if err != nil {
		return nil, workflowServerError("workflow resume schedule", err)
	}
	return workflowScheduleToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
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
		TriggerID:   scopedWorkflowObjectID(s.pluginName, req.GetTriggerId()),
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
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	value, _, err := getScopedWorkflowEventTrigger(ctx, provider, s.pluginName, req.GetTriggerId())
	if err != nil {
		return nil, workflowServerError("workflow get event trigger", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, value.Target, "event trigger", req.GetTriggerId()); err != nil {
		return nil, workflowServerError("workflow get event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ListEventTriggers(ctx context.Context, _ *proto.ListWorkflowEventTriggersRequest) (*proto.ListWorkflowEventTriggersResponse, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, _, err := s.resolve()
	if err != nil {
		return nil, err
	}
	values, err := provider.ListEventTriggers(ctx, coreworkflow.ListEventTriggersRequest{})
	if err != nil {
		return nil, workflowServerError("workflow list event triggers", err)
	}
	resp := &proto.ListWorkflowEventTriggersResponse{Triggers: make([]*proto.WorkflowEventTrigger, 0, len(values))}
	for _, value := range values {
		if !workflowTargetInScope(s.pluginName, value.Target) {
			continue
		}
		pbValue, err := workflowEventTriggerToScopedPluginProto(s.pluginName, value)
		if err != nil {
			return nil, workflowServerError("workflow list event triggers", err)
		}
		resp.Triggers = append(resp.Triggers, pbValue)
	}
	return resp, nil
}

func (s *WorkflowServer) DeleteEventTrigger(ctx context.Context, req *proto.DeleteWorkflowEventTriggerRequest) (*emptypb.Empty, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	existing, triggerID, err := getScopedWorkflowEventTrigger(ctx, provider, s.pluginName, req.GetTriggerId())
	if err != nil {
		return nil, workflowServerError("workflow delete event trigger", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "event trigger", req.GetTriggerId()); err != nil {
		return nil, workflowServerError("workflow delete event trigger", err)
	}
	if err := provider.DeleteEventTrigger(ctx, coreworkflow.DeleteEventTriggerRequest{
		TriggerID: triggerID,
	}); err != nil {
		return nil, workflowServerError("workflow delete event trigger", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowServer) PauseEventTrigger(ctx context.Context, req *proto.PauseWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	existing, triggerID, err := getScopedWorkflowEventTrigger(ctx, provider, s.pluginName, req.GetTriggerId())
	if err != nil {
		return nil, workflowServerError("workflow pause event trigger", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "event trigger", req.GetTriggerId()); err != nil {
		return nil, workflowServerError("workflow pause event trigger", err)
	}
	value, err := provider.PauseEventTrigger(ctx, coreworkflow.PauseEventTriggerRequest{
		TriggerID: triggerID,
	})
	if err != nil {
		return nil, workflowServerError("workflow pause event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) ResumeEventTrigger(ctx context.Context, req *proto.ResumeWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
	provider, _, managedIDs, err := s.resolve()
	if err != nil {
		return nil, err
	}
	if err := rejectManagedWorkflowObjectID("event trigger", req.GetTriggerId(), "event triggers", managedIDs.EventTriggers); err != nil {
		return nil, err
	}
	existing, triggerID, err := getScopedWorkflowEventTrigger(ctx, provider, s.pluginName, req.GetTriggerId())
	if err != nil {
		return nil, workflowServerError("workflow resume event trigger", err)
	}
	if err := requireWorkflowTargetScope(s.pluginName, existing.Target, "event trigger", req.GetTriggerId()); err != nil {
		return nil, workflowServerError("workflow resume event trigger", err)
	}
	value, err := provider.ResumeEventTrigger(ctx, coreworkflow.ResumeEventTriggerRequest{
		TriggerID: triggerID,
	})
	if err != nil {
		return nil, workflowServerError("workflow resume event trigger", err)
	}
	return workflowEventTriggerToScopedPluginProto(s.pluginName, value)
}

func (s *WorkflowServer) PublishEvent(ctx context.Context, req *proto.PublishWorkflowEventRequest) (*emptypb.Empty, error) {
	ctx = workflowScopeContext(ctx, s.pluginName)
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

func workflowScopeContext(ctx context.Context, pluginName string) context.Context {
	return invocation.WithWorkflowContextString(ctx, "plugin", pluginName)
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
	cloned := *schedule
	cloned.ID = unscopedWorkflowObjectID(pluginName, schedule.ID)
	return workflowScheduleToPluginProto(&cloned)
}

func workflowEventTriggerToScopedPluginProto(pluginName string, trigger *coreworkflow.EventTrigger) (*proto.WorkflowEventTrigger, error) {
	if trigger == nil {
		return nil, nil
	}
	if err := validateWorkflowTargetScope(pluginName, trigger.Target, "event trigger", trigger.ID); err != nil {
		return nil, err
	}
	cloned := *trigger
	cloned.ID = unscopedWorkflowObjectID(pluginName, trigger.ID)
	return workflowEventTriggerToPluginProto(&cloned)
}

func validateWorkflowTargetScope(pluginName string, target coreworkflow.Target, objectKind, objectID string) error {
	if workflowTargetInScope(pluginName, target) {
		return nil
	}
	if objectID == "" {
		return status.Errorf(codes.Internal, "workflow %s target plugin %q does not match scoped plugin %q", objectKind, target.PluginName, pluginName)
	}
	return status.Errorf(codes.Internal, "workflow %s %q target plugin %q does not match scoped plugin %q", objectKind, objectID, target.PluginName, pluginName)
}

func requireWorkflowTargetScope(pluginName string, target coreworkflow.Target, objectKind, objectID string) error {
	if workflowTargetInScope(pluginName, target) {
		return nil
	}
	if objectID == "" {
		return status.Errorf(codes.NotFound, "workflow %s not found", objectKind)
	}
	return status.Errorf(codes.NotFound, "workflow %s %q not found", objectKind, objectID)
}

func workflowTargetInScope(pluginName string, target coreworkflow.Target) bool {
	if pluginName == "" {
		return true
	}
	return target.PluginName == pluginName
}

func getScopedWorkflowSchedule(ctx context.Context, provider coreworkflow.Provider, pluginName, scheduleID string) (*coreworkflow.Schedule, string, error) {
	for _, candidate := range workflowScopedObjectIDs(pluginName, scheduleID) {
		value, err := provider.GetSchedule(ctx, coreworkflow.GetScheduleRequest{ScheduleID: candidate})
		if err == nil {
			return value, candidate, nil
		}
		if !errors.Is(err, core.ErrNotFound) && status.Code(err) != codes.NotFound {
			return nil, "", err
		}
	}
	return nil, "", status.Errorf(codes.NotFound, "workflow schedule %q not found", scheduleID)
}

func getScopedWorkflowEventTrigger(ctx context.Context, provider coreworkflow.Provider, pluginName, triggerID string) (*coreworkflow.EventTrigger, string, error) {
	for _, candidate := range workflowScopedObjectIDs(pluginName, triggerID) {
		value, err := provider.GetEventTrigger(ctx, coreworkflow.GetEventTriggerRequest{TriggerID: candidate})
		if err == nil {
			return value, candidate, nil
		}
		if !errors.Is(err, core.ErrNotFound) && status.Code(err) != codes.NotFound {
			return nil, "", err
		}
	}
	return nil, "", status.Errorf(codes.NotFound, "workflow event trigger %q not found", triggerID)
}

func workflowScopedObjectIDs(pluginName, objectID string) []string {
	scoped := scopedWorkflowObjectID(pluginName, objectID)
	if scoped == objectID {
		return []string{objectID}
	}
	return []string{scoped, strings.TrimSpace(objectID)}
}

func scopedWorkflowObjectID(pluginName, objectID string) string {
	objectID = strings.TrimSpace(objectID)
	if pluginName == "" || objectID == "" {
		return objectID
	}
	return "plugin$" + pluginName + "$" + objectID
}

func unscopedWorkflowObjectID(pluginName, objectID string) string {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return objectID
	}
	prefix := "plugin$" + pluginName + "$"
	return strings.TrimPrefix(objectID, prefix)
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
