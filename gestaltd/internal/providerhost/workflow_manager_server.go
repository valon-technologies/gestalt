package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type WorkflowManagerServer struct {
	proto.UnimplementedWorkflowManagerHostServer

	pluginName string
	manager    workflowmanager.Service
	tokens     *InvocationTokenManager
}

func NewWorkflowManagerServer(pluginName string, manager workflowmanager.Service, tokens *InvocationTokenManager) *WorkflowManagerServer {
	return &WorkflowManagerServer{
		pluginName: pluginName,
		manager:    manager,
		tokens:     tokens,
	}
}

func (s *WorkflowManagerServer) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	upsert, err := workflowManagerScheduleUpsert(
		req.GetProviderName(),
		req.GetCron(),
		req.GetTimezone(),
		req.GetTarget(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.IdempotencyKey = strings.TrimSpace(req.GetIdempotencyKey())
	managed, err := s.manager.CreateSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), upsert)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) StartRun(ctx context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	target, err := workflowManagerTarget(req.GetTarget())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.StartRun(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.RunStart{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Target:           target,
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		WorkflowKey:      strings.TrimSpace(req.GetWorkflowKey()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow run: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) SignalRun(ctx context.Context, req *proto.WorkflowManagerSignalRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.SignalRun(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.RunSignal{
		RunID:  runID,
		Signal: workflowSignalFromProto(req.GetSignal()),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowRunSignalToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow run signal: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	target, err := workflowManagerTarget(req.GetTarget())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.SignalOrStartRun(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.RunSignalOrStart{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		WorkflowKey:      strings.TrimSpace(req.GetWorkflowKey()),
		Target:           target,
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		Signal:           workflowSignalFromProto(req.GetSignal()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowRunSignalToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow run signal: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.GetSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), scheduleID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) UpdateSchedule(ctx context.Context, req *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	upsert, err := workflowManagerScheduleUpsert(
		req.GetProviderName(),
		req.GetCron(),
		req.GetTimezone(),
		req.GetTarget(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	managed, err := s.manager.UpdateSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), scheduleID, upsert)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) DeleteSchedule(ctx context.Context, req *proto.WorkflowManagerDeleteScheduleRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	if err := s.manager.DeleteSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), scheduleID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowManagerServer) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.PauseSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), scheduleID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) ResumeSchedule(ctx context.Context, req *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.ResumeSchedule(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), scheduleID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) CreateEventTrigger(ctx context.Context, req *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	upsert, err := workflowManagerEventTriggerUpsert(
		req.GetProviderName(),
		req.GetMatch(),
		req.GetTarget(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.IdempotencyKey = strings.TrimSpace(req.GetIdempotencyKey())
	managed, err := s.manager.CreateEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), upsert)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowEventTriggerToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow trigger: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) GetEventTrigger(ctx context.Context, req *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	triggerID := strings.TrimSpace(req.GetTriggerId())
	if triggerID == "" {
		return nil, status.Error(codes.InvalidArgument, "trigger_id is required")
	}
	managed, err := s.manager.GetEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), triggerID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowEventTriggerToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow trigger: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) UpdateEventTrigger(ctx context.Context, req *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	triggerID := strings.TrimSpace(req.GetTriggerId())
	if triggerID == "" {
		return nil, status.Error(codes.InvalidArgument, "trigger_id is required")
	}
	upsert, err := workflowManagerEventTriggerUpsert(
		req.GetProviderName(),
		req.GetMatch(),
		req.GetTarget(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	managed, err := s.manager.UpdateEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), triggerID, upsert)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowEventTriggerToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow trigger: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) DeleteEventTrigger(ctx context.Context, req *proto.WorkflowManagerDeleteEventTriggerRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	triggerID := strings.TrimSpace(req.GetTriggerId())
	if triggerID == "" {
		return nil, status.Error(codes.InvalidArgument, "trigger_id is required")
	}
	if err := s.manager.DeleteEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), triggerID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowManagerServer) PauseEventTrigger(ctx context.Context, req *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	triggerID := strings.TrimSpace(req.GetTriggerId())
	if triggerID == "" {
		return nil, status.Error(codes.InvalidArgument, "trigger_id is required")
	}
	managed, err := s.manager.PauseEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), triggerID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowEventTriggerToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow trigger: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) ResumeEventTrigger(ctx context.Context, req *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	triggerID := strings.TrimSpace(req.GetTriggerId())
	if triggerID == "" {
		return nil, status.Error(codes.InvalidArgument, "trigger_id is required")
	}
	managed, err := s.manager.ResumeEventTrigger(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), triggerID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowEventTriggerToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow trigger: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) PublishEvent(ctx context.Context, req *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	event, err := workflowEventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "event: %v", err)
	}
	published, err := s.manager.PublishEvent(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), event)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := workflowEventToProto(published)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow event: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) tokenContext(token string) (plugininvokerservice.TokenContext, error) {
	tokenCtx, err := s.tokens.ResolveToken(token, s.pluginName)
	if err != nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	return tokenCtx, nil
}

func workflowManagerScheduleUpsert(
	providerName string,
	cron string,
	timezone string,
	targetProto *proto.BoundWorkflowTarget,
	paused bool,
) (workflowmanager.ScheduleUpsert, error) {
	target, err := workflowManagerTarget(targetProto)
	if err != nil {
		return workflowmanager.ScheduleUpsert{}, err
	}
	return workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(providerName),
		Cron:         strings.TrimSpace(cron),
		Timezone:     strings.TrimSpace(timezone),
		Target:       target,
		Paused:       paused,
	}, nil
}

func workflowManagerTarget(targetProto *proto.BoundWorkflowTarget) (coreworkflow.Target, error) {
	target := workflowTargetFromProto(targetProto)
	if target.Agent == nil {
		if target.Plugin == nil {
			return coreworkflow.Target{}, status.Error(codes.InvalidArgument, "target.plugin.plugin_name is required")
		}
		pluginTarget := *target.Plugin
		if strings.TrimSpace(pluginTarget.PluginName) == "" {
			return coreworkflow.Target{}, status.Error(codes.InvalidArgument, "target.plugin.plugin_name is required")
		}
		if strings.TrimSpace(pluginTarget.Operation) == "" {
			return coreworkflow.Target{}, status.Error(codes.InvalidArgument, "target.plugin.operation is required")
		}
	} else if strings.TrimSpace(target.Agent.ProviderName) == "" {
		return coreworkflow.Target{}, status.Error(codes.InvalidArgument, "target.agent.provider_name is required")
	}
	return target, nil
}

func workflowManagerEventTriggerUpsert(
	providerName string,
	matchProto *proto.WorkflowEventMatch,
	targetProto *proto.BoundWorkflowTarget,
	paused bool,
) (workflowmanager.EventTriggerUpsert, error) {
	target, err := workflowManagerTarget(targetProto)
	if err != nil {
		return workflowmanager.EventTriggerUpsert{}, err
	}
	match := workflowEventMatchFromProto(matchProto)
	if strings.TrimSpace(match.Type) == "" {
		return workflowmanager.EventTriggerUpsert{}, status.Error(codes.InvalidArgument, "match.type is required")
	}
	return workflowmanager.EventTriggerUpsert{
		ProviderName: strings.TrimSpace(providerName),
		Match:        match,
		Target:       target,
		Paused:       paused,
	}, nil
}

func workflowManagerStatusError(err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := status.FromError(err); ok {
		return existing.Err()
	}
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured), errors.Is(err, workflowmanager.ErrExecutionRefsNotConfigured), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowEventMatchRequired), errors.Is(err, workflowmanager.ErrWorkflowEventTypeRequired), errors.Is(err, workflowmanager.ErrWorkflowKeyRequired), errors.Is(err, workflowmanager.ErrWorkflowSignalNameRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowScheduleSubject), errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, workflowmanager.ErrDuplicateExecutionRefs), errors.Is(err, invocation.ErrInternal):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound), errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Unknown, err.Error())
	}
}

var _ proto.WorkflowManagerHostServer = (*WorkflowManagerServer)(nil)
