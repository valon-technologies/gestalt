package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
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

func (s workflowProviderServer) ApplyWorkflowDefinition(ctx context.Context, req *proto.ApplyWorkflowDefinitionRequest) (*proto.WorkflowDefinition, error) {
	input, err := applyWorkflowDefinitionRequestFromProto(req)
	if err != nil {
		return nil, providerRPCError("workflow apply deployment", err)
	}
	deployment, err := s.provider.ApplyWorkflowDefinition(ctx, input)
	if err != nil {
		return nil, providerRPCError("workflow apply deployment", err)
	}
	out, err := workflowDefinitionToProto(deployment)
	return out, providerRPCError("workflow apply deployment", err)
}

func (s workflowProviderServer) GetWorkflowDefinition(ctx context.Context, req *proto.GetWorkflowDefinitionRequest) (*proto.WorkflowDefinition, error) {
	deployment, err := s.provider.GetWorkflowDefinition(ctx, &GetWorkflowDefinitionRequest{DefinitionID: req.GetDefinitionId()})
	if err != nil {
		return nil, providerRPCError("workflow get deployment", err)
	}
	out, err := workflowDefinitionToProto(deployment)
	return out, providerRPCError("workflow get deployment", err)
}

func (s workflowProviderServer) ListWorkflowDefinitions(ctx context.Context, req *proto.ListWorkflowDefinitionsRequest) (*proto.ListWorkflowDefinitionsResponse, error) {
	resp, err := s.provider.ListWorkflowDefinitions(ctx, &ListWorkflowDefinitionsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
		Labels:    cloneStringMap(req.GetLabels()),
	})
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	definitions, err := workflowDefinitionsToProto(resp.GetDefinitions())
	if err != nil {
		return nil, providerRPCError("workflow list definitions", err)
	}
	return &proto.ListWorkflowDefinitionsResponse{
		Definitions:   definitions,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (s workflowProviderServer) DeleteWorkflowDefinition(ctx context.Context, req *proto.DeleteWorkflowDefinitionRequest) (*emptypb.Empty, error) {
	err := s.provider.DeleteWorkflowDefinition(ctx, &DeleteWorkflowDefinitionRequest{
		DefinitionID: req.GetDefinitionId(),
		Generation:   req.GetGeneration(),
		RequestID:    req.GetRequestId(),
	})
	return &emptypb.Empty{}, providerRPCError("workflow delete deployment", err)
}

func (s workflowProviderServer) SetWorkflowDefinitionPaused(ctx context.Context, req *proto.SetWorkflowDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	deployment, err := s.provider.SetWorkflowDefinitionPaused(ctx, &SetWorkflowDefinitionPausedRequest{
		DefinitionID: req.GetDefinitionId(),
		Paused:       req.GetPaused(),
		RequestID:    req.GetRequestId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set deployment paused", err)
	}
	out, err := workflowDefinitionToProto(deployment)
	return out, providerRPCError("workflow set deployment paused", err)
}

func (s workflowProviderServer) SetWorkflowActivationPaused(ctx context.Context, req *proto.SetWorkflowActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	deployment, err := s.provider.SetWorkflowActivationPaused(ctx, &SetWorkflowActivationPausedRequest{
		DefinitionID: req.GetDefinitionId(),
		ActivationID: req.GetActivationId(),
		Paused:       req.GetPaused(),
		RequestID:    req.GetRequestId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set activation paused", err)
	}
	out, err := workflowDefinitionToProto(deployment)
	return out, providerRPCError("workflow set activation paused", err)
}

func (s workflowProviderServer) StartWorkflowRun(ctx context.Context, req *proto.StartWorkflowRunRequest) (*proto.WorkflowRun, error) {
	run, err := s.provider.StartWorkflowRun(ctx, startWorkflowRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow start run", err)
	}
	out, err := workflowRunToProto(run)
	return out, providerRPCError("workflow start run", err)
}

func (s workflowProviderServer) SignalWorkflowRun(ctx context.Context, req *proto.SignalWorkflowRunRequest) (*proto.WorkflowRunSignal, error) {
	resp, err := s.provider.SignalWorkflowRun(ctx, signalWorkflowRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal run", err)
	}
	out, err := workflowRunSignalToProto(resp)
	return out, providerRPCError("workflow signal run", err)
}

func (s workflowProviderServer) SignalOrStartWorkflowRun(ctx context.Context, req *proto.SignalOrStartWorkflowRunRequest) (*proto.WorkflowRunSignal, error) {
	resp, err := s.provider.SignalOrStartWorkflowRun(ctx, signalOrStartWorkflowRunRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow signal or start run", err)
	}
	out, err := workflowRunSignalToProto(resp)
	return out, providerRPCError("workflow signal or start run", err)
}

func (s workflowProviderServer) CancelWorkflowRun(ctx context.Context, req *proto.CancelWorkflowRunRequest) (*proto.WorkflowRun, error) {
	run, err := s.provider.CancelWorkflowRun(ctx, &CancelWorkflowRunRequest{RunID: req.GetRunId(), Reason: req.GetReason()})
	if err != nil {
		return nil, providerRPCError("workflow cancel run", err)
	}
	out, err := workflowRunToProto(run)
	return out, providerRPCError("workflow cancel run", err)
}

func (s workflowProviderServer) DeliverWorkflowEvent(ctx context.Context, req *proto.DeliverWorkflowEventRequest) (*proto.DeliverWorkflowEventResponse, error) {
	resp, err := s.provider.DeliverWorkflowEvent(ctx, deliverWorkflowEventRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow deliver event", err)
	}
	results, err := workflowEventDeliveryResultsToProto(resp.GetResults())
	if err != nil {
		return nil, providerRPCError("workflow deliver event", err)
	}
	return &proto.DeliverWorkflowEventResponse{Results: results}, nil
}

func (s workflowProviderServer) GetWorkflowRun(ctx context.Context, req *proto.GetWorkflowRunRequest) (*proto.WorkflowRun, error) {
	run, err := s.provider.GetWorkflowRun(ctx, &GetWorkflowRunRequest{RunID: req.GetRunId()})
	if err != nil {
		return nil, providerRPCError("workflow get run", err)
	}
	out, err := workflowRunToProto(run)
	return out, providerRPCError("workflow get run", err)
}

func (s workflowProviderServer) ListWorkflowRuns(ctx context.Context, req *proto.ListWorkflowRunsRequest) (*proto.ListWorkflowRunsResponse, error) {
	resp, err := s.provider.ListWorkflowRuns(ctx, &ListWorkflowRunsRequest{
		DefinitionID: req.GetDefinitionId(),
		PageSize:     int(req.GetPageSize()),
		PageToken:    req.GetPageToken(),
		Status:       WorkflowRunStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	runs, err := workflowRunsToProto(resp.GetRuns())
	if err != nil {
		return nil, providerRPCError("workflow list runs", err)
	}
	return &proto.ListWorkflowRunsResponse{
		Runs:          runs,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (s workflowProviderServer) GetWorkflowRunEvents(ctx context.Context, req *proto.GetWorkflowRunEventsRequest) (*proto.ListWorkflowRunEventsResponse, error) {
	resp, err := s.provider.GetWorkflowRunEvents(ctx, &GetWorkflowRunEventsRequest{
		RunID:     req.GetRunId(),
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, providerRPCError("workflow get run events", err)
	}
	return &proto.ListWorkflowRunEventsResponse{
		Events:        workflowRunEventsToProto(resp.GetEvents()),
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (s workflowProviderServer) GetWorkflowRunOutput(ctx context.Context, req *proto.GetWorkflowRunOutputRequest) (*proto.WorkflowRunOutput, error) {
	output, err := s.provider.GetWorkflowRunOutput(ctx, &GetWorkflowRunOutputRequest{
		RunID:     req.GetRunId(),
		OutputRef: req.GetOutputRef(),
		StepID:    req.GetStepId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow get run output", err)
	}
	out, err := workflowRunOutputToProto(output)
	return out, providerRPCError("workflow get run output", err)
}

func (s workflowProviderServer) GetExecutionReference(ctx context.Context, req *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref, err := s.provider.GetExecutionReference(ctx, &GetWorkflowExecutionReferenceRequest{ID: req.GetId()})
	if err != nil {
		return nil, providerRPCError("workflow get execution reference", err)
	}
	out, err := workflowExecutionReferenceToProto(ref)
	return out, providerRPCError("workflow get execution reference", err)
}

func (s workflowProviderServer) ListExecutionReferences(ctx context.Context, req *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	resp, err := s.provider.ListExecutionReferences(ctx, &ListWorkflowExecutionReferencesRequest{SubjectID: req.GetSubjectId()})
	if err != nil {
		return nil, providerRPCError("workflow list execution references", err)
	}
	refs, err := workflowExecutionReferencesToProto(resp.GetExecutionRefs())
	if err != nil {
		return nil, providerRPCError("workflow list execution references", err)
	}
	return &proto.ListWorkflowExecutionReferencesResponse{ExecutionRefs: refs}, nil
}

func applyWorkflowDefinitionRequestFromProto(req *proto.ApplyWorkflowDefinitionRequest) (*ApplyWorkflowDefinitionRequest, error) {
	if req == nil {
		return &ApplyWorkflowDefinitionRequest{}, nil
	}
	ref, err := workflowExecutionReferenceFromProto(req.GetExecutionRef())
	if err != nil {
		return nil, err
	}
	return &ApplyWorkflowDefinitionRequest{
		Spec:         workflowDefinitionSpecInputPtrFromSpec(req.GetSpec()),
		Binding:      workflowDefinitionBindingFromProto(req.GetBinding()),
		ExecutionRef: ref,
		RequestID:    req.GetRequestId(),
	}, nil
}

func startWorkflowRunRequestFromProto(req *proto.StartWorkflowRunRequest) *StartWorkflowRunRequest {
	if req == nil {
		return &StartWorkflowRunRequest{}
	}
	return &StartWorkflowRunRequest{
		DefinitionID:         req.GetDefinitionId(),
		DefinitionGeneration: req.GetDefinitionGeneration(),
		ActivationID:         req.GetActivationId(),
		WorkflowKey:          req.GetWorkflowKey(),
		Input:                mapFromStruct(req.GetInput()),
		IdempotencyKey:       req.GetIdempotencyKey(),
		CreatedBy:            workflowActorInputPtrFromActor(req.GetCreatedBy()),
	}
}

func signalWorkflowRunRequestFromProto(req *proto.SignalWorkflowRunRequest) *SignalWorkflowRunRequest {
	if req == nil {
		return &SignalWorkflowRunRequest{}
	}
	var signal *WorkflowSignal
	if req.GetSignal() != nil {
		input := workflowSignalFromProto(req.GetSignal())
		signal = &input
	}
	return &SignalWorkflowRunRequest{
		RunID:  req.GetRunId(),
		Signal: signal,
	}
}

func signalOrStartWorkflowRunRequestFromProto(req *proto.SignalOrStartWorkflowRunRequest) *SignalOrStartWorkflowRunRequest {
	if req == nil {
		return &SignalOrStartWorkflowRunRequest{}
	}
	var signal *WorkflowSignal
	if req.GetSignal() != nil {
		input := workflowSignalFromProto(req.GetSignal())
		signal = &input
	}
	return &SignalOrStartWorkflowRunRequest{
		DefinitionID:         req.GetDefinitionId(),
		DefinitionGeneration: req.GetDefinitionGeneration(),
		ActivationID:         req.GetActivationId(),
		WorkflowKey:          req.GetWorkflowKey(),
		Input:                mapFromStruct(req.GetInput()),
		IdempotencyKey:       req.GetIdempotencyKey(),
		Signal:               signal,
		CreatedBy:            workflowActorInputPtrFromActor(req.GetCreatedBy()),
	}
}

func deliverWorkflowEventRequestFromProto(req *proto.DeliverWorkflowEventRequest) *DeliverWorkflowEventRequest {
	if req == nil {
		return &DeliverWorkflowEventRequest{}
	}
	return &DeliverWorkflowEventRequest{
		DeliveryID:     req.GetDeliveryId(),
		Event:          workflowEventInputPtrFromEvent(req.GetEvent()),
		PublishedBy:    workflowActorInputPtrFromActor(req.GetPublishedBy()),
		IdempotencyKey: req.GetIdempotencyKey(),
	}
}
