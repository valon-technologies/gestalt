package gestalt

import (
	"context"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeWorkflowProvider starts a gRPC server for a [WorkflowProvider].
func ServeWorkflowProvider(ctx context.Context, provider WorkflowProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindWorkflow, provider))
		proto.RegisterWorkflowProviderServer(srv, workflowProviderServer{provider: provider})
	}, grpc.UnaryInterceptor(workflowProviderInvocationUnaryInterceptor))
}

type workflowProviderServer struct {
	proto.UnimplementedWorkflowProviderServer
	provider WorkflowProvider
}

type workflowProviderInvocationTokenRequest interface {
	GetInvocationToken() string
}

func workflowProviderInvocationUnaryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if tokenReq, ok := req.(workflowProviderInvocationTokenRequest); ok {
		ctx = workflowProviderInvocationContext(ctx, tokenReq.GetInvocationToken())
	}
	return handler(ctx, req)
}

func (s workflowProviderServer) CreateDefinition(ctx context.Context, req *proto.CreateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	definition, err := s.provider.CreateDefinition(ctx, createWorkflowProviderDefinitionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow create definition", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow create definition", err)
}

func (s workflowProviderServer) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	definition, err := s.provider.GetDefinition(ctx, &GetWorkflowProviderDefinitionRequest{DefinitionID: req.GetDefinitionId()})
	if err != nil {
		return nil, providerRPCError("workflow get definition", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow get definition", err)
}

func (s workflowProviderServer) UpdateDefinition(ctx context.Context, req *proto.UpdateWorkflowProviderDefinitionRequest) (*proto.BoundWorkflowDefinition, error) {
	definition, err := s.provider.UpdateDefinition(ctx, updateWorkflowProviderDefinitionRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow update definition", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow update definition", err)
}

func (s workflowProviderServer) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, providerRPCError("workflow delete definition", s.provider.DeleteDefinition(ctx, &DeleteWorkflowProviderDefinitionRequest{DefinitionID: req.GetDefinitionId()}))
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
	resp, err := s.provider.ListRuns(ctx, &ListWorkflowProviderRunsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
		Status:    WorkflowRunStatus(req.GetStatus()),
		TargetApp: req.GetTargetApp(),
	})
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	runs, err := workflowRunInputsToProto(resp.GetRuns())
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	return &proto.ListWorkflowProviderRunsResponse{
		Runs:          runs,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
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

func (s workflowProviderServer) PublishEvent(ctx context.Context, req *proto.PublishWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	input := publishWorkflowProviderEventRequestFromProto(req)
	eventInput, err := s.provider.PublishEvent(ctx, input)
	if err != nil {
		return nil, providerRPCError("workflow publish event", err)
	}
	if eventInput == nil {
		eventInput = input.Event
	}
	if eventInput == nil {
		eventInput = &WorkflowEvent{}
	}
	event, err := workflowEventToProto(*eventInput)
	return event, providerRPCError("workflow publish event", err)
}

func workflowProviderInvocationContext(ctx context.Context, token string) context.Context {
	if strings.TrimSpace(token) == "" {
		return ctx
	}
	return withInvocationToken(ctx, token)
}

func createWorkflowProviderDefinitionRequestFromProto(req *proto.CreateWorkflowProviderDefinitionRequest) *CreateWorkflowProviderDefinitionRequest {
	if req == nil {
		return &CreateWorkflowProviderDefinitionRequest{}
	}
	return &CreateWorkflowProviderDefinitionRequest{
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorInputPtrFromActor(req.GetCreatedBy()),
	}
}

func updateWorkflowProviderDefinitionRequestFromProto(req *proto.UpdateWorkflowProviderDefinitionRequest) *UpdateWorkflowProviderDefinitionRequest {
	if req == nil {
		return &UpdateWorkflowProviderDefinitionRequest{}
	}
	return &UpdateWorkflowProviderDefinitionRequest{
		DefinitionID: req.GetDefinitionId(),
		Target:       workflowTargetInputPtrFromTarget(req.GetTarget()),
		RequestedBy:  workflowActorInputPtrFromActor(req.GetRequestedBy()),
	}
}

func startWorkflowProviderRunRequestFromProto(req *proto.StartWorkflowProviderRunRequest) *StartWorkflowProviderRunRequest {
	if req == nil {
		return &StartWorkflowProviderRunRequest{}
	}
	return &StartWorkflowProviderRunRequest{
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorInputPtrFromActor(req.GetCreatedBy()),
		RunAs:          subjectFromProto(req.GetRunAs()),
		WorkflowKey:    req.GetWorkflowKey(),
		DefinitionID:   req.GetDefinitionId(),
	}
}

func signalWorkflowProviderRunRequestFromProto(req *proto.SignalWorkflowProviderRunRequest) *SignalWorkflowProviderRunRequest {
	if req == nil {
		return &SignalWorkflowProviderRunRequest{}
	}
	var signal *WorkflowSignal
	if req.GetSignal() != nil {
		input := workflowSignalFromProto(req.GetSignal())
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
	var signal *WorkflowSignal
	if req.GetSignal() != nil {
		input := workflowSignalFromProto(req.GetSignal())
		signal = &input
	}
	return &SignalOrStartWorkflowProviderRunRequest{
		WorkflowKey:    req.GetWorkflowKey(),
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		IdempotencyKey: req.GetIdempotencyKey(),
		CreatedBy:      workflowActorInputPtrFromActor(req.GetCreatedBy()),
		RunAs:          subjectFromProto(req.GetRunAs()),
		Signal:         signal,
		DefinitionID:   req.GetDefinitionId(),
	}
}

func upsertWorkflowProviderScheduleRequestFromProto(req *proto.UpsertWorkflowProviderScheduleRequest) *UpsertWorkflowProviderScheduleRequest {
	if req == nil {
		return &UpsertWorkflowProviderScheduleRequest{}
	}
	return &UpsertWorkflowProviderScheduleRequest{
		ScheduleID:     req.GetScheduleId(),
		Cron:           req.GetCron(),
		Timezone:       req.GetTimezone(),
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		Paused:         req.GetPaused(),
		RequestedBy:    workflowActorInputPtrFromActor(req.GetRequestedBy()),
		RunAs:          subjectFromProto(req.GetRunAs()),
		IdempotencyKey: req.GetIdempotencyKey(),
		DefinitionID:   req.GetDefinitionId(),
	}
}

func upsertWorkflowProviderEventTriggerRequestFromProto(req *proto.UpsertWorkflowProviderEventTriggerRequest) *UpsertWorkflowProviderEventTriggerRequest {
	if req == nil {
		return &UpsertWorkflowProviderEventTriggerRequest{}
	}
	return &UpsertWorkflowProviderEventTriggerRequest{
		TriggerID:      req.GetTriggerId(),
		Match:          workflowEventMatchInputPtrFromMatch(req.GetMatch()),
		Target:         workflowTargetInputPtrFromTarget(req.GetTarget()),
		Paused:         req.GetPaused(),
		RequestedBy:    workflowActorInputPtrFromActor(req.GetRequestedBy()),
		RunAs:          subjectFromProto(req.GetRunAs()),
		IdempotencyKey: req.GetIdempotencyKey(),
		DefinitionID:   req.GetDefinitionId(),
	}
}

func publishWorkflowProviderEventRequestFromProto(req *proto.PublishWorkflowProviderEventRequest) *PublishWorkflowProviderEventRequest {
	if req == nil {
		return &PublishWorkflowProviderEventRequest{}
	}
	var event *WorkflowEvent
	if req.GetEvent() != nil {
		input := workflowEventFromProto(req.GetEvent())
		event = &input
	}
	return &PublishWorkflowProviderEventRequest{
		AppName:     req.GetAppName(),
		Event:       event,
		PublishedBy: workflowActorInputPtrFromActor(req.GetPublishedBy()),
	}
}

func workflowRunInputToProto(input *BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowRunToProto(*input)
}

func workflowDefinitionInputToProto(input *BoundWorkflowDefinition) (*proto.BoundWorkflowDefinition, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowDefinitionToProto(*input)
}

func workflowRunInputsToProto(values []BoundWorkflowRun) ([]*proto.BoundWorkflowRun, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowRun, 0, len(values))
	for _, value := range values {
		run, err := boundWorkflowRunToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func workflowScheduleInputToProto(input *BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowScheduleToProto(*input)
}

func workflowScheduleInputsToProto(values []BoundWorkflowSchedule) ([]*proto.BoundWorkflowSchedule, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowSchedule, 0, len(values))
	for _, value := range values {
		schedule, err := boundWorkflowScheduleToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, schedule)
	}
	return out, nil
}

func workflowEventTriggerInputToProto(input *BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowEventTriggerToProto(*input)
}

func workflowEventTriggerInputsToProto(values []BoundWorkflowEventTrigger) ([]*proto.BoundWorkflowEventTrigger, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.BoundWorkflowEventTrigger, 0, len(values))
	for _, value := range values {
		trigger, err := boundWorkflowEventTriggerToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, trigger)
	}
	return out, nil
}

func workflowSignalInputToProto(input *WorkflowSignal) (*proto.WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return workflowSignalToProto(*input)
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
