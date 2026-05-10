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
	run, err := s.provider.StartRun(ctx, startWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow start run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow start run", err)
}

func (s workflowProviderServer) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run, err := s.provider.GetRun(ctx, &GetWorkflowProviderRunRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, providerRPCError("workflow get run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow get run", err)
}

func (s workflowProviderServer) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	resp, err := s.provider.ListRuns(ctx, &ListWorkflowProviderRunsRequest{})
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	runs, err := workflowRunInputsToProto(resp.GetRuns())
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	return &proto.ListWorkflowProviderRunsResponse{Runs: runs}, nil
}

func (s workflowProviderServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.BoundWorkflowRun, error) {
	run, err := s.provider.CancelRun(ctx, &CancelWorkflowProviderRunRequest{RunID: req.GetRunId(), Reason: req.GetReason()})
	if err != nil {
		return nil, providerRPCError("workflow cancel run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow cancel run", err)
}

func (s workflowProviderServer) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	resp, err := s.provider.SignalRun(ctx, signalWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal run", err)
	}
	out, err := signalWorkflowRunResponseToProto(resp)
	return out, providerRPCError("workflow signal run", err)
}

func (s workflowProviderServer) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	resp, err := s.provider.SignalOrStartRun(ctx, signalOrStartWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal or start run", err)
	}
	out, err := signalWorkflowRunResponseToProto(resp)
	return out, providerRPCError("workflow signal or start run", err)
}

func (s workflowProviderServer) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.UpsertSchedule(ctx, upsertWorkflowProviderScheduleRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow upsert schedule", err)
	}
	out, err := workflowScheduleInputToProto(schedule)
	return out, providerRPCError("workflow upsert schedule", err)
}

func (s workflowProviderServer) GetSchedule(ctx context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.GetSchedule(ctx, &GetWorkflowProviderScheduleRequest{ScheduleID: req.GetScheduleId()})
	if err != nil {
		return nil, providerRPCError("workflow get schedule", err)
	}
	out, err := workflowScheduleInputToProto(schedule)
	return out, providerRPCError("workflow get schedule", err)
}

func (s workflowProviderServer) ListSchedules(ctx context.Context, req *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	resp, err := s.provider.ListSchedules(ctx, &ListWorkflowProviderSchedulesRequest{})
	if err != nil {
		return nil, providerRPCError("workflow list schedules", err)
	}
	schedules, err := workflowScheduleInputsToProto(resp.GetSchedules())
	if err != nil {
		return nil, providerRPCError("workflow list schedules", err)
	}
	return &proto.ListWorkflowProviderSchedulesResponse{Schedules: schedules}, nil
}

func (s workflowProviderServer) DeleteSchedule(ctx context.Context, req *proto.DeleteWorkflowProviderScheduleRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow delete schedule", s.provider.DeleteSchedule(ctx, &DeleteWorkflowProviderScheduleRequest{ScheduleID: req.GetScheduleId()}))
}

func (s workflowProviderServer) PauseSchedule(ctx context.Context, req *proto.PauseWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.PauseSchedule(ctx, &PauseWorkflowProviderScheduleRequest{ScheduleID: req.GetScheduleId()})
	if err != nil {
		return nil, providerRPCError("workflow pause schedule", err)
	}
	out, err := workflowScheduleInputToProto(schedule)
	return out, providerRPCError("workflow pause schedule", err)
}

func (s workflowProviderServer) ResumeSchedule(ctx context.Context, req *proto.ResumeWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	schedule, err := s.provider.ResumeSchedule(ctx, &ResumeWorkflowProviderScheduleRequest{ScheduleID: req.GetScheduleId()})
	if err != nil {
		return nil, providerRPCError("workflow resume schedule", err)
	}
	out, err := workflowScheduleInputToProto(schedule)
	return out, providerRPCError("workflow resume schedule", err)
}

func (s workflowProviderServer) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.UpsertEventTrigger(ctx, upsertWorkflowProviderEventTriggerRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow upsert event trigger", err)
	}
	out, err := workflowEventTriggerInputToProto(trigger)
	return out, providerRPCError("workflow upsert event trigger", err)
}

func (s workflowProviderServer) GetEventTrigger(ctx context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.GetEventTrigger(ctx, &GetWorkflowProviderEventTriggerRequest{TriggerID: req.GetTriggerId()})
	if err != nil {
		return nil, providerRPCError("workflow get event trigger", err)
	}
	out, err := workflowEventTriggerInputToProto(trigger)
	return out, providerRPCError("workflow get event trigger", err)
}

func (s workflowProviderServer) ListEventTriggers(ctx context.Context, req *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	resp, err := s.provider.ListEventTriggers(ctx, &ListWorkflowProviderEventTriggersRequest{})
	if err != nil {
		return nil, providerRPCError("workflow list event triggers", err)
	}
	triggers, err := workflowEventTriggerInputsToProto(resp.GetTriggers())
	if err != nil {
		return nil, providerRPCError("workflow list event triggers", err)
	}
	return &proto.ListWorkflowProviderEventTriggersResponse{Triggers: triggers}, nil
}

func (s workflowProviderServer) DeleteEventTrigger(ctx context.Context, req *proto.DeleteWorkflowProviderEventTriggerRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow delete event trigger", s.provider.DeleteEventTrigger(ctx, &DeleteWorkflowProviderEventTriggerRequest{TriggerID: req.GetTriggerId()}))
}

func (s workflowProviderServer) PauseEventTrigger(ctx context.Context, req *proto.PauseWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.PauseEventTrigger(ctx, &PauseWorkflowProviderEventTriggerRequest{TriggerID: req.GetTriggerId()})
	if err != nil {
		return nil, providerRPCError("workflow pause event trigger", err)
	}
	out, err := workflowEventTriggerInputToProto(trigger)
	return out, providerRPCError("workflow pause event trigger", err)
}

func (s workflowProviderServer) ResumeEventTrigger(ctx context.Context, req *proto.ResumeWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	trigger, err := s.provider.ResumeEventTrigger(ctx, &ResumeWorkflowProviderEventTriggerRequest{TriggerID: req.GetTriggerId()})
	if err != nil {
		return nil, providerRPCError("workflow resume event trigger", err)
	}
	out, err := workflowEventTriggerInputToProto(trigger)
	return out, providerRPCError("workflow resume event trigger", err)
}

func (s workflowProviderServer) PutExecutionReference(ctx context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	input, err := workflowExecutionReferenceInputPtrFromReference(req.GetReference())
	if err != nil {
		return nil, providerRPCError("workflow put execution reference", err)
	}
	ref, err := s.provider.PutExecutionReference(ctx, &PutWorkflowExecutionReferenceRequest{Reference: input})
	if err != nil {
		return nil, providerRPCError("workflow put execution reference", err)
	}
	out, err := workflowExecutionReferenceInputToProto(ref)
	return out, providerRPCError("workflow put execution reference", err)
}

func (s workflowProviderServer) GetExecutionReference(ctx context.Context, req *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref, err := s.provider.GetExecutionReference(ctx, &GetWorkflowExecutionReferenceRequest{ID: req.GetId()})
	if err != nil {
		return nil, providerRPCError("workflow get execution reference", err)
	}
	out, err := workflowExecutionReferenceInputToProto(ref)
	return out, providerRPCError("workflow get execution reference", err)
}

func (s workflowProviderServer) ListExecutionReferences(ctx context.Context, req *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	resp, err := s.provider.ListExecutionReferences(ctx, &ListWorkflowExecutionReferencesRequest{SubjectID: req.GetSubjectId()})
	if err != nil {
		return nil, providerRPCError("workflow list execution references", err)
	}
	refs, err := workflowExecutionReferenceInputsToProto(resp.GetReferences())
	if err != nil {
		return nil, providerRPCError("workflow list execution references", err)
	}
	return &proto.ListWorkflowExecutionReferencesResponse{References: refs}, nil
}

func (s workflowProviderServer) PublishEvent(ctx context.Context, req *proto.PublishWorkflowProviderEventRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow publish event", s.provider.PublishEvent(ctx, publishWorkflowProviderEventRequestFromProto(req)))
}

func startWorkflowProviderRunRequestFromProto(req *proto.StartWorkflowProviderRunRequest) *StartWorkflowProviderRunRequest {
	if req == nil {
		return &StartWorkflowProviderRunRequest{}
	}
	return &StartWorkflowProviderRunRequest{
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorInputPtrFromActor(req.GetCreatedBy()),
		ExecutionRef:   req.GetExecutionRef(),
		WorkflowKey:    req.GetWorkflowKey(),
	}
}

func signalWorkflowProviderRunRequestFromProto(req *proto.SignalWorkflowProviderRunRequest) *SignalWorkflowProviderRunRequest {
	if req == nil {
		return &SignalWorkflowProviderRunRequest{}
	}
	var signal *WorkflowSignalInput
	if req.GetSignal() != nil {
		input := WorkflowSignalInputFromSignal(req.GetSignal())
		signal = &input
	}
	return &SignalWorkflowProviderRunRequest{
		RunID:  req.GetRunId(),
		Signal: signal,
	}
}

func signalOrStartWorkflowProviderRunRequestFromProto(req *proto.SignalOrStartWorkflowProviderRunRequest) *SignalOrStartWorkflowProviderRunRequest {
	if req == nil {
		return &SignalOrStartWorkflowProviderRunRequest{}
	}
	var signal *WorkflowSignalInput
	if req.GetSignal() != nil {
		input := WorkflowSignalInputFromSignal(req.GetSignal())
		signal = &input
	}
	return &SignalOrStartWorkflowProviderRunRequest{
		WorkflowKey:    req.GetWorkflowKey(),
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorInputPtrFromActor(req.GetCreatedBy()),
		ExecutionRef:   req.GetExecutionRef(),
		Signal:         signal,
	}
}

func upsertWorkflowProviderScheduleRequestFromProto(req *proto.UpsertWorkflowProviderScheduleRequest) *UpsertWorkflowProviderScheduleRequest {
	if req == nil {
		return &UpsertWorkflowProviderScheduleRequest{}
	}
	return &UpsertWorkflowProviderScheduleRequest{
		ScheduleID:   req.GetScheduleId(),
		Cron:         req.GetCron(),
		Timezone:     req.GetTimezone(),
		Target:       workflowTargetInputPtrFromTarget(req.GetTarget()),
		Paused:       req.GetPaused(),
		RequestedBy:  workflowActorInputPtrFromActor(req.GetRequestedBy()),
		ExecutionRef: req.GetExecutionRef(),
	}
}

func upsertWorkflowProviderEventTriggerRequestFromProto(req *proto.UpsertWorkflowProviderEventTriggerRequest) *UpsertWorkflowProviderEventTriggerRequest {
	if req == nil {
		return &UpsertWorkflowProviderEventTriggerRequest{}
	}
	return &UpsertWorkflowProviderEventTriggerRequest{
		TriggerID:    req.GetTriggerId(),
		Match:        workflowEventMatchInputPtrFromMatch(req.GetMatch()),
		Target:       workflowTargetInputPtrFromTarget(req.GetTarget()),
		Paused:       req.GetPaused(),
		RequestedBy:  workflowActorInputPtrFromActor(req.GetRequestedBy()),
		ExecutionRef: req.GetExecutionRef(),
	}
}

func publishWorkflowProviderEventRequestFromProto(req *proto.PublishWorkflowProviderEventRequest) *PublishWorkflowProviderEventRequest {
	if req == nil {
		return &PublishWorkflowProviderEventRequest{}
	}
	var event *WorkflowEventInput
	if req.GetEvent() != nil {
		input := WorkflowEventInputFromEvent(req.GetEvent())
		event = &input
	}
	return &PublishWorkflowProviderEventRequest{
		PluginName:  req.GetPluginName(),
		Event:       event,
		PublishedBy: workflowActorInputPtrFromActor(req.GetPublishedBy()),
	}
}

func workflowRunInputToProto(input *BoundWorkflowRunInput) (*proto.BoundWorkflowRun, error) {
	if input == nil {
		return nil, nil
	}
	return NewBoundWorkflowRun(*input)
}

func workflowRunInputsToProto(values []BoundWorkflowRunInput) ([]*proto.BoundWorkflowRun, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowRun, 0, len(values))
	for _, value := range values {
		run, err := NewBoundWorkflowRun(value)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func workflowScheduleInputToProto(input *BoundWorkflowScheduleInput) (*proto.BoundWorkflowSchedule, error) {
	if input == nil {
		return nil, nil
	}
	return NewBoundWorkflowSchedule(*input)
}

func workflowScheduleInputsToProto(values []BoundWorkflowScheduleInput) ([]*proto.BoundWorkflowSchedule, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowSchedule, 0, len(values))
	for _, value := range values {
		schedule, err := NewBoundWorkflowSchedule(value)
		if err != nil {
			return nil, err
		}
		out = append(out, schedule)
	}
	return out, nil
}

func workflowEventTriggerInputToProto(input *BoundWorkflowEventTriggerInput) (*proto.BoundWorkflowEventTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return NewBoundWorkflowEventTrigger(*input)
}

func workflowEventTriggerInputsToProto(values []BoundWorkflowEventTriggerInput) ([]*proto.BoundWorkflowEventTrigger, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowEventTrigger, 0, len(values))
	for _, value := range values {
		trigger, err := NewBoundWorkflowEventTrigger(value)
		if err != nil {
			return nil, err
		}
		out = append(out, trigger)
	}
	return out, nil
}

func workflowExecutionReferenceInputPtrFromReference(value *proto.WorkflowExecutionReference) (*WorkflowExecutionReferenceInput, error) {
	if value == nil {
		return nil, nil
	}
	input, err := WorkflowExecutionReferenceInputFromReference(value)
	if err != nil {
		return nil, err
	}
	return &input, nil
}

func workflowExecutionReferenceInputToProto(input *WorkflowExecutionReferenceInput) (*proto.WorkflowExecutionReference, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowExecutionReference(*input)
}

func workflowExecutionReferenceInputsToProto(values []WorkflowExecutionReferenceInput) ([]*proto.WorkflowExecutionReference, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowExecutionReference, 0, len(values))
	for _, value := range values {
		ref, err := NewWorkflowExecutionReference(value)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}

func workflowSignalInputToProto(input *WorkflowSignalInput) (*proto.WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowSignal(*input)
}

func signalWorkflowRunResponseToProto(resp *SignalWorkflowRunResponse) (*proto.SignalWorkflowRunResponse, error) {
	if resp == nil {
		return nil, nil
	}
	run, err := workflowRunInputToProto(resp.Run)
	if err != nil {
		return nil, err
	}
	signal, err := workflowSignalInputToProto(resp.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowRunResponse{
		Run:         run,
		Signal:      signal,
		StartedRun:  resp.StartedRun,
		WorkflowKey: resp.WorkflowKey,
	}, nil
}
