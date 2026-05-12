package workflows

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type InvocationTokenManager = plugininvokerservice.InvocationTokenManager
type ManagerService = workflowmanager.Service

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return plugininvokerservice.NewInvocationTokenManager(secret)
}

type ManagerServer struct {
	proto.UnimplementedWorkflowManagerHostServer

	pluginName string
	manager    ManagerService
	tokens     *InvocationTokenManager
}

func NewManagerServer(pluginName string, manager ManagerService, tokens *InvocationTokenManager) *ManagerServer {
	return &ManagerServer{
		pluginName: pluginName,
		manager:    manager,
		tokens:     tokens,
	}
}

func (s *ManagerServer) CreateDefinition(ctx context.Context, req *proto.WorkflowManagerCreateDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsCreate); err != nil {
		return nil, err
	}
	target, err := workflowManagerTarget(req.GetTarget())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.CreateDefinition(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.DefinitionUpsert{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Target:           target,
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ManagerServer) GetDefinition(ctx context.Context, req *proto.WorkflowManagerGetDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsGet); err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	managed, err := s.manager.GetDefinition(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), definitionID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ManagerServer) UpdateDefinition(ctx context.Context, req *proto.WorkflowManagerUpdateDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsUpdate); err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	target, err := workflowManagerTarget(req.GetTarget())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.UpdateDefinition(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), definitionID, workflowmanager.DefinitionUpsert{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Target:           target,
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ManagerServer) DeleteDefinition(ctx context.Context, req *proto.WorkflowManagerDeleteDefinitionRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsDelete); err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	if err := s.manager.DeleteDefinition(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), definitionID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ManagerServer) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesCreate); err != nil {
		return nil, err
	}
	upsert, err := workflowManagerScheduleUpsert(
		req.GetProviderName(),
		req.GetCron(),
		req.GetTimezone(),
		req.GetTarget(),
		req.GetDefinitionId(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.IdempotencyKey = strings.TrimSpace(req.GetIdempotencyKey())
	upsert.DefinitionID = strings.TrimSpace(req.GetDefinitionId())
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

func (s *ManagerServer) StartRun(ctx context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsStart); err != nil {
		return nil, err
	}
	target, err := workflowManagerTargetOrDefinition(req.GetTarget(), req.GetDefinitionId())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.StartRun(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.RunStart{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Target:           target,
		DefinitionID:     strings.TrimSpace(req.GetDefinitionId()),
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

func (s *ManagerServer) SignalRun(ctx context.Context, req *proto.WorkflowManagerSignalRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsSignal); err != nil {
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

func (s *ManagerServer) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsSignalOrStart); err != nil {
		return nil, err
	}
	target, err := workflowManagerTargetOrDefinition(req.GetTarget(), req.GetDefinitionId())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.SignalOrStartRun(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), workflowmanager.RunSignalOrStart{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		WorkflowKey:      strings.TrimSpace(req.GetWorkflowKey()),
		Target:           target,
		DefinitionID:     strings.TrimSpace(req.GetDefinitionId()),
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

func (s *ManagerServer) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesGet); err != nil {
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

func (s *ManagerServer) UpdateSchedule(ctx context.Context, req *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesUpdate); err != nil {
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
		req.GetDefinitionId(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.DefinitionID = strings.TrimSpace(req.GetDefinitionId())
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

func (s *ManagerServer) DeleteSchedule(ctx context.Context, req *proto.WorkflowManagerDeleteScheduleRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesDelete); err != nil {
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

func (s *ManagerServer) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesPause); err != nil {
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

func (s *ManagerServer) ResumeSchedule(ctx context.Context, req *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationSchedulesResume); err != nil {
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

func (s *ManagerServer) CreateEventTrigger(ctx context.Context, req *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersCreate); err != nil {
		return nil, err
	}
	upsert, err := workflowManagerEventTriggerUpsert(
		req.GetProviderName(),
		req.GetMatch(),
		req.GetTarget(),
		req.GetDefinitionId(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.IdempotencyKey = strings.TrimSpace(req.GetIdempotencyKey())
	upsert.DefinitionID = strings.TrimSpace(req.GetDefinitionId())
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

func (s *ManagerServer) GetEventTrigger(ctx context.Context, req *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersGet); err != nil {
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

func (s *ManagerServer) UpdateEventTrigger(ctx context.Context, req *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersUpdate); err != nil {
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
		req.GetDefinitionId(),
		req.GetPaused(),
	)
	if err != nil {
		return nil, err
	}
	upsert.CallerPluginName = strings.TrimSpace(s.pluginName)
	upsert.DefinitionID = strings.TrimSpace(req.GetDefinitionId())
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

func (s *ManagerServer) DeleteEventTrigger(ctx context.Context, req *proto.WorkflowManagerDeleteEventTriggerRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersDelete); err != nil {
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

func (s *ManagerServer) PauseEventTrigger(ctx context.Context, req *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersPause); err != nil {
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

func (s *ManagerServer) ResumeEventTrigger(ctx context.Context, req *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventTriggersResume); err != nil {
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

func (s *ManagerServer) PublishEvent(ctx context.Context, req *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventsPublish); err != nil {
		return nil, err
	}
	event, err := workflowEventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "event: %v", err)
	}
	published, err := s.manager.PublishEvent(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), req.GetProviderName(), event)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := workflowEventToProto(published)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow event: %v", err)
	}
	return resp, nil
}

func (s *ManagerServer) tokenContext(token string) (plugininvokerservice.TokenContext, error) {
	tokenCtx, err := s.tokens.ResolveToken(token, s.pluginName)
	if err != nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	return tokenCtx, nil
}

func (s *ManagerServer) requireWorkflowGrant(tokenCtx plugininvokerservice.TokenContext, operation string) error {
	if tokenCtx.AllowsWorkflowManagerOperation(operation) {
		return nil
	}
	return status.Errorf(codes.PermissionDenied, "workflow manager operation %q is not allowed for plugin %q", operation, strings.TrimSpace(s.pluginName))
}

func workflowManagerScheduleUpsert(
	providerName string,
	cron string,
	timezone string,
	targetProto *proto.BoundWorkflowTarget,
	definitionID string,
	paused bool,
) (workflowmanager.ScheduleUpsert, error) {
	target, err := workflowManagerTargetOrDefinition(targetProto, definitionID)
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

func workflowManagerTargetOrDefinition(targetProto *proto.BoundWorkflowTarget, definitionID string) (coreworkflow.Target, error) {
	if strings.TrimSpace(definitionID) != "" && !workflowManagerTargetProtoIsSet(targetProto) {
		return coreworkflow.Target{}, nil
	}
	return workflowManagerTarget(targetProto)
}

func workflowManagerTargetProtoIsSet(targetProto *proto.BoundWorkflowTarget) bool {
	return targetProto != nil && (targetProto.GetPlugin() != nil || targetProto.GetAgent() != nil)
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
	definitionID string,
	paused bool,
) (workflowmanager.EventTriggerUpsert, error) {
	target, err := workflowManagerTargetOrDefinition(targetProto, definitionID)
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
	case errors.Is(err, workflowmanager.ErrWorkflowEventMatchRequired), errors.Is(err, workflowmanager.ErrWorkflowEventTypeRequired), errors.Is(err, workflowmanager.ErrWorkflowKeyRequired), errors.Is(err, workflowmanager.ErrWorkflowSignalNameRequired), errors.Is(err, invocation.ErrInvalidInvocation):
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

var _ proto.WorkflowManagerHostServer = (*ManagerServer)(nil)
