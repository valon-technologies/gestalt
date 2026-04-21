package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type WorkflowManagerServer struct {
	proto.UnimplementedWorkflowManagerHostServer

	manager   workflowmanager.Service
	snapshots *RequestSnapshotStore
}

func NewWorkflowManagerServer(manager workflowmanager.Service, snapshots *RequestSnapshotStore) *WorkflowManagerServer {
	return &WorkflowManagerServer{
		manager:   manager,
		snapshots: snapshots,
	}
}

func (s *WorkflowManagerServer) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	snapshot, err := s.snapshot(req.GetRequestHandle())
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
	managed, err := s.manager.CreateSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, upsert)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	snapshot, err := s.snapshot(req.GetRequestHandle())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.GetSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, scheduleID)
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
	snapshot, err := s.snapshot(req.GetRequestHandle())
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
	managed, err := s.manager.UpdateSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, scheduleID, upsert)
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
	snapshot, err := s.snapshot(req.GetRequestHandle())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	if err := s.manager.DeleteSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, scheduleID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *WorkflowManagerServer) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	snapshot, err := s.snapshot(req.GetRequestHandle())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.PauseSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, scheduleID)
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
	snapshot, err := s.snapshot(req.GetRequestHandle())
	if err != nil {
		return nil, err
	}
	scheduleID := strings.TrimSpace(req.GetScheduleId())
	if scheduleID == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule_id is required")
	}
	managed, err := s.manager.ResumeSchedule(restoreRequestSnapshotContext(ctx, snapshot, ""), snapshot.principal, scheduleID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowScheduleToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow schedule: %v", err)
	}
	return resp, nil
}

func (s *WorkflowManagerServer) snapshot(handle string) (requestSnapshot, error) {
	snapshot, err := s.snapshots.snapshot(handle)
	if err != nil {
		return requestSnapshot{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	return snapshot, nil
}

func workflowManagerScheduleUpsert(
	providerName string,
	cron string,
	timezone string,
	targetProto *proto.BoundWorkflowTarget,
	paused bool,
) (workflowmanager.ScheduleUpsert, error) {
	target := workflowTargetFromProto(targetProto)
	if strings.TrimSpace(target.PluginName) == "" {
		return workflowmanager.ScheduleUpsert{}, status.Error(codes.InvalidArgument, "target.plugin_name is required")
	}
	if strings.TrimSpace(target.Operation) == "" {
		return workflowmanager.ScheduleUpsert{}, status.Error(codes.InvalidArgument, "target.operation is required")
	}
	return workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(providerName),
		Cron:         strings.TrimSpace(cron),
		Timezone:     strings.TrimSpace(timezone),
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
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured), errors.Is(err, workflowmanager.ErrExecutionRefsNotConfigured), errors.Is(err, invocation.ErrNoToken), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
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
