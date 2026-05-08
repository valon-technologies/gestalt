package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeWorkflowProvider starts a gRPC server for a [WorkflowProvider].
func ServeWorkflowProvider(ctx context.Context, provider WorkflowProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindWorkflow, provider))
		proto.RegisterWorkflowProviderServer(srv, workflowProviderServer{provider: provider})
	})
}

type workflowProviderServer struct {
	proto.UnimplementedWorkflowProviderServer
	provider WorkflowProvider
}

func (s workflowProviderServer) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run, err := s.provider.StartRun(ctx, req)
	return run, providerRPCError("workflow start run", err)
}

func (s workflowProviderServer) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run, err := s.provider.GetRun(ctx, req)
	return run, providerRPCError("workflow get run", err)
}

func (s workflowProviderServer) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	resp, err := s.provider.ListRuns(ctx, req)
	return resp, providerRPCError("workflow list runs", err)
}

func (s workflowProviderServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run, err := s.provider.CancelRun(ctx, req)
	return run, providerRPCError("workflow cancel run", err)
}

func (s workflowProviderServer) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	resp, err := s.provider.SignalRun(ctx, req)
	return resp, providerRPCError("workflow signal run", err)
}

func (s workflowProviderServer) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	resp, err := s.provider.SignalOrStartRun(ctx, req)
	return resp, providerRPCError("workflow signal or start run", err)
}

func (s workflowProviderServer) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.UpsertSchedule(ctx, req)
	return schedule, providerRPCError("workflow upsert schedule", err)
}

func (s workflowProviderServer) GetSchedule(ctx context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.GetSchedule(ctx, req)
	return schedule, providerRPCError("workflow get schedule", err)
}

func (s workflowProviderServer) ListSchedules(ctx context.Context, req *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	resp, err := s.provider.ListSchedules(ctx, req)
	return resp, providerRPCError("workflow list schedules", err)
}

func (s workflowProviderServer) DeleteSchedule(ctx context.Context, req *proto.DeleteWorkflowProviderScheduleRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow delete schedule", s.provider.DeleteSchedule(ctx, req))
}

func (s workflowProviderServer) PauseSchedule(ctx context.Context, req *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.PauseSchedule(ctx, req)
	return schedule, providerRPCError("workflow pause schedule", err)
}

func (s workflowProviderServer) ResumeSchedule(ctx context.Context, req *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.ResumeSchedule(ctx, req)
	return schedule, providerRPCError("workflow resume schedule", err)
}

func (s workflowProviderServer) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.UpsertEventTrigger(ctx, req)
	return trigger, providerRPCError("workflow upsert event trigger", err)
}

func (s workflowProviderServer) GetEventTrigger(ctx context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.GetEventTrigger(ctx, req)
	return trigger, providerRPCError("workflow get event trigger", err)
}

func (s workflowProviderServer) ListEventTriggers(ctx context.Context, req *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	resp, err := s.provider.ListEventTriggers(ctx, req)
	return resp, providerRPCError("workflow list event triggers", err)
}

func (s workflowProviderServer) DeleteEventTrigger(ctx context.Context, req *proto.DeleteWorkflowProviderEventTriggerRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow delete event trigger", s.provider.DeleteEventTrigger(ctx, req))
}

func (s workflowProviderServer) PauseEventTrigger(ctx context.Context, req *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.PauseEventTrigger(ctx, req)
	return trigger, providerRPCError("workflow pause event trigger", err)
}

func (s workflowProviderServer) ResumeEventTrigger(ctx context.Context, req *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.ResumeEventTrigger(ctx, req)
	return trigger, providerRPCError("workflow resume event trigger", err)
}

func (s workflowProviderServer) PutExecutionReference(ctx context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref, err := s.provider.PutExecutionReference(ctx, req)
	return ref, providerRPCError("workflow put execution reference", err)
}

func (s workflowProviderServer) GetExecutionReference(ctx context.Context, req *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref, err := s.provider.GetExecutionReference(ctx, req)
	return ref, providerRPCError("workflow get execution reference", err)
}

func (s workflowProviderServer) ListExecutionReferences(ctx context.Context, req *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	resp, err := s.provider.ListExecutionReferences(ctx, req)
	return resp, providerRPCError("workflow list execution references", err)
}

func (s workflowProviderServer) PublishEvent(ctx context.Context, req *proto.PublishWorkflowProviderEventRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow publish event", s.provider.PublishEvent(ctx, req))
}
