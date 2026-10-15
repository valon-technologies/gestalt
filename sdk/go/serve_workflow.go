package gestalt

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeWorkflowProvider starts a gRPC server for a [WorkflowProvider].
func ServeWorkflowProvider(ctx context.Context, provider WorkflowProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindWorkflow, provider))
		proto.RegisterWorkflowServer(srv, workflowProviderServer{provider: provider})
	})
}

type workflowProviderServer struct {
	proto.UnimplementedWorkflowServer
	provider WorkflowProvider
}

func (s workflowProviderServer) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	input, err := applyWorkflowProviderDefinitionRequestFromProto(req)
	if err != nil {
		return nil, providerRPCError("workflow apply definition", err)
	}
	definition, err := s.provider.ApplyDefinition(ctx, input)
	if err != nil {
		return nil, providerRPCError("workflow apply definition", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow apply definition", err)
}

func (s workflowProviderServer) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	definition, err := s.provider.GetDefinition(ctx, &GetWorkflowProviderDefinitionRequest{DefinitionID: req.GetDefinitionId()})
	if err != nil {
		return nil, providerRPCError("workflow get definition", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow get definition", err)
}

func (s workflowProviderServer) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListDefinitions(ctx, &ListWorkflowProviderDefinitionsRequest{})
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	definitions, err := convertProtoSliceErr(resp.GetDefinitions(), workflowDefinitionToProto)
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	return &proto.ListWorkflowProviderDefinitionsResponse{Definitions: definitions}, nil
}

func (s workflowProviderServer) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	definition, err := s.provider.SetDefinitionPaused(ctx, &SetWorkflowProviderDefinitionPausedRequest{
		DefinitionID: req.GetDefinitionId(),
		Paused:       req.GetPaused(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set definition paused", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow set definition paused", err)
}

func (s workflowProviderServer) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	definition, err := s.provider.SetActivationPaused(ctx, &SetWorkflowProviderActivationPausedRequest{
		DefinitionID: req.GetDefinitionId(),
		ActivationID: req.GetActivationId(),
		Paused:       req.GetPaused(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set activation paused", err)
	}
	out, err := workflowDefinitionInputToProto(definition)
	return out, providerRPCError("workflow set activation paused", err)
}

func (s workflowProviderServer) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) (*emptypb.Empty, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	return &emptypb.Empty{}, providerRPCError("workflow delete definition", s.provider.DeleteDefinition(ctx, &DeleteWorkflowProviderDefinitionRequest{DefinitionID: req.GetDefinitionId()}))
}

func (s workflowProviderServer) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	run, err := s.provider.StartRun(ctx, startWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow start run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow start run", err)
}

func (s workflowProviderServer) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	run, err := s.provider.GetRun(ctx, &GetWorkflowProviderRunRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, providerRPCError("workflow get run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow get run", err)
}

func (s workflowProviderServer) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.ListRuns(ctx, &ListWorkflowProviderRunsRequest{
		PageSize:     int(req.GetPageSize()),
		PageToken:    req.GetPageToken(),
		Status:       WorkflowRunStatus(req.GetStatus()),
		TargetApp:    req.GetTargetApp(),
		KnownApps:    append([]string(nil), req.GetKnownApps()...),
		DefinitionID: req.GetDefinitionId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	runs, err := convertProtoSliceErr(resp.GetRuns(), workflowRunToProto)
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	out := &proto.ListWorkflowProviderRunsResponse{
		Runs:          runs,
		NextPageToken: resp.GetNextPageToken(),
	}
	if total, ok := resp.GetTotalCount(); ok {
		out.TotalCount = &total
	}
	if counts := resp.GetStatusCounts(); counts != nil {
		out.StatusCounts = &proto.WorkflowRunStatusCounts{
			Pending:   counts.Pending,
			Running:   counts.Running,
			Succeeded: counts.Succeeded,
			Failed:    counts.Failed,
			Canceled:  counts.Canceled,
		}
	}
	return out, nil
}

func (s workflowProviderServer) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.GetRunEvents(ctx, &GetWorkflowProviderRunEventsRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, providerRPCError("workflow get run events", err)
	}
	events, err := convertProtoSliceErr(resp.GetEvents(), workflowRunEventToProto)
	if err != nil {
		return nil, providerRPCError("workflow get run events", err)
	}
	return &proto.GetWorkflowProviderRunEventsResponse{Events: events}, nil
}

func (s workflowProviderServer) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.GetRunOutput(ctx, &GetWorkflowProviderRunOutputRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, providerRPCError("workflow get run output", err)
	}
	var outputValue any
	if resp != nil {
		outputValue = resp.Output
	}
	output, err := valueFromAny(outputValue)
	if err != nil {
		return nil, providerRPCError("workflow get run output", fmt.Errorf("output: %w", err))
	}
	return &proto.GetWorkflowProviderRunOutputResponse{Output: output}, nil
}

func (s workflowProviderServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	run, err := s.provider.CancelRun(ctx, &CancelWorkflowProviderRunRequest{RunID: req.GetRunId(), Reason: req.GetReason()})
	if err != nil {
		return nil, providerRPCError("workflow cancel run", err)
	}
	out, err := workflowRunInputToProto(run)
	return out, providerRPCError("workflow cancel run", err)
}

func (s workflowProviderServer) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.SignalRun(ctx, signalWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal run", err)
	}
	out, err := signalWorkflowRunResponseToProto(resp)
	return out, providerRPCError("workflow signal run", err)
}

func (s workflowProviderServer) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	resp, err := s.provider.SignalOrStartRun(ctx, signalOrStartWorkflowProviderRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal or start run", err)
	}
	out, err := signalWorkflowRunResponseToProto(resp)
	return out, providerRPCError("workflow signal or start run", err)
}

func (s workflowProviderServer) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	ctx = withRequestContext(ctx, req.GetContext())
	input := deliverWorkflowProviderEventRequestFromProto(req)
	eventInput, err := s.provider.DeliverEvent(ctx, input)
	if err != nil {
		return nil, providerRPCError("workflow deliver event", err)
	}
	if eventInput == nil {
		eventInput = input.Event
	}
	if eventInput == nil {
		eventInput = &WorkflowEvent{}
	}
	event, err := workflowEventToProto(*eventInput)
	return event, providerRPCError("workflow deliver event", err)
}

func applyWorkflowProviderDefinitionRequestFromProto(req *proto.ApplyWorkflowProviderDefinitionRequest) (*ApplyWorkflowProviderDefinitionRequest, error) {
	if req == nil {
		return &ApplyWorkflowProviderDefinitionRequest{}, nil
	}
	var spec *WorkflowDefinitionSpec
	if req.GetSpec() != nil {
		input, err := workflowDefinitionSpecFromProto(req.GetSpec())
		if err != nil {
			return nil, err
		}
		spec = &input
	}
	return &ApplyWorkflowProviderDefinitionRequest{
		Spec:           spec,
		IdempotencyKey: req.GetIdempotencyKey(),
	}, nil
}

func startWorkflowProviderRunRequestFromProto(req *proto.StartWorkflowProviderRunRequest) *StartWorkflowProviderRunRequest {
	if req == nil {
		return &StartWorkflowProviderRunRequest{}
	}
	return &StartWorkflowProviderRunRequest{
		DefinitionID:                 req.GetDefinitionId(),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        mapFromStruct(req.GetInput()),
		IdempotencyKey:               req.GetIdempotencyKey(),
		WorkflowKey:                  req.GetWorkflowKey(),
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
		WorkflowKey:                  req.GetWorkflowKey(),
		DefinitionID:                 req.GetDefinitionId(),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        mapFromStruct(req.GetInput()),
		IdempotencyKey:               req.GetIdempotencyKey(),
		Signal:                       signal,
	}
}

func deliverWorkflowProviderEventRequestFromProto(req *proto.DeliverWorkflowProviderEventRequest) *DeliverWorkflowProviderEventRequest {
	if req == nil {
		return &DeliverWorkflowProviderEventRequest{}
	}
	var event *WorkflowEvent
	if req.GetEvent() != nil {
		input := workflowEventFromProto(req.GetEvent())
		event = &input
	}
	return &DeliverWorkflowProviderEventRequest{
		Event: event,
	}
}

func workflowRunInputToProto(input *WorkflowRun) (*proto.WorkflowRun, error) {
	if input == nil {
		return nil, nil
	}
	return workflowRunToProto(*input)
}

func workflowDefinitionInputToProto(input *WorkflowDefinition) (*proto.WorkflowDefinition, error) {
	if input == nil {
		return nil, nil
	}
	return workflowDefinitionToProto(*input)
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
