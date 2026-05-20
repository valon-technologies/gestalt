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

func (s workflowProviderServer) PlanWorkflow(ctx context.Context, req *proto.PlanWorkflowRequest) (*proto.PlanWorkflowResponse, error) {
	resp, err := s.provider.PlanWorkflow(ctx, planWorkflowRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow plan", err)
	}
	return planWorkflowResponseToProto(resp), nil
}

func (s workflowProviderServer) ApplyWorkflowDeployment(ctx context.Context, req *proto.ApplyWorkflowDeploymentRequest) (*proto.WorkflowDeployment, error) {
	deployment, err := s.provider.ApplyWorkflowDeployment(ctx, applyWorkflowDeploymentRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("workflow apply deployment", err)
	}
	out, err := workflowDeploymentToProto(deployment)
	return out, providerRPCError("workflow apply deployment", err)
}

func (s workflowProviderServer) GetWorkflowDeployment(ctx context.Context, req *proto.GetWorkflowDeploymentRequest) (*proto.WorkflowDeployment, error) {
	deployment, err := s.provider.GetWorkflowDeployment(ctx, &GetWorkflowDeploymentRequest{DeploymentID: req.GetDeploymentId()})
	if err != nil {
		return nil, providerRPCError("workflow get deployment", err)
	}
	out, err := workflowDeploymentToProto(deployment)
	return out, providerRPCError("workflow get deployment", err)
}

func (s workflowProviderServer) ListWorkflowDeployments(ctx context.Context, req *proto.ListWorkflowDeploymentsRequest) (*proto.ListWorkflowDeploymentsResponse, error) {
	resp, err := s.provider.ListWorkflowDeployments(ctx, &ListWorkflowDeploymentsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
		Labels:    cloneStringMap(req.GetLabels()),
	})
	if err != nil {
		return nil, providerRPCError("workflow list deployments", err)
	}
	deployments, err := workflowDeploymentsToProto(resp.GetDeployments())
	if err != nil {
		return nil, providerRPCError("workflow list deployments", err)
	}
	return &proto.ListWorkflowDeploymentsResponse{
		Deployments:   deployments,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func (s workflowProviderServer) DeleteWorkflowDeployment(ctx context.Context, req *proto.DeleteWorkflowDeploymentRequest) (*emptypb.Empty, error) {
	err := s.provider.DeleteWorkflowDeployment(ctx, &DeleteWorkflowDeploymentRequest{
		DeploymentID: req.GetDeploymentId(),
		Generation:   req.GetGeneration(),
		RequestID:    req.GetRequestId(),
	})
	return &emptypb.Empty{}, providerRPCError("workflow delete deployment", err)
}

func (s workflowProviderServer) SetWorkflowDeploymentPaused(ctx context.Context, req *proto.SetWorkflowDeploymentPausedRequest) (*proto.WorkflowDeployment, error) {
	deployment, err := s.provider.SetWorkflowDeploymentPaused(ctx, &SetWorkflowDeploymentPausedRequest{
		DeploymentID: req.GetDeploymentId(),
		Paused:       req.GetPaused(),
		RequestID:    req.GetRequestId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set deployment paused", err)
	}
	out, err := workflowDeploymentToProto(deployment)
	return out, providerRPCError("workflow set deployment paused", err)
}

func (s workflowProviderServer) SetWorkflowActivationPaused(ctx context.Context, req *proto.SetWorkflowActivationPausedRequest) (*proto.WorkflowDeployment, error) {
	deployment, err := s.provider.SetWorkflowActivationPaused(ctx, &SetWorkflowActivationPausedRequest{
		DeploymentID: req.GetDeploymentId(),
		ActivationID: req.GetActivationId(),
		Paused:       req.GetPaused(),
		RequestID:    req.GetRequestId(),
	})
	if err != nil {
		return nil, providerRPCError("workflow set activation paused", err)
	}
	out, err := workflowDeploymentToProto(deployment)
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
		DeploymentID: req.GetDeploymentId(),
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

func (s workflowProviderServer) PutExecutionReference(ctx context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref, err := workflowExecutionReferenceFromProto(req.GetExecutionRef())
	if err != nil {
		return nil, providerRPCError("workflow put execution reference", err)
	}
	stored, err := s.provider.PutExecutionReference(ctx, &PutWorkflowExecutionReferenceRequest{
		ExecutionRef: ref,
	})
	if err != nil {
		return nil, providerRPCError("workflow put execution reference", err)
	}
	out, err := workflowExecutionReferenceToProto(stored)
	return out, providerRPCError("workflow put execution reference", err)
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

func planWorkflowRequestFromProto(req *proto.PlanWorkflowRequest) *PlanWorkflowRequest {
	if req == nil {
		return &PlanWorkflowRequest{}
	}
	return &PlanWorkflowRequest{
		Spec:                          workflowDeploymentSpecInputPtrFromSpec(req.GetSpec()),
		SpecDigest:                    req.GetSpecDigest(),
		TargetDigest:                  req.GetTargetDigest(),
		ActionTableDigest:             req.GetActionTableDigest(),
		TargetCanonicalizationVersion: req.GetTargetCanonicalizationVersion(),
		WorkflowSemanticsVersion:      req.GetWorkflowSemanticsVersion(),
	}
}

func applyWorkflowDeploymentRequestFromProto(req *proto.ApplyWorkflowDeploymentRequest) *ApplyWorkflowDeploymentRequest {
	if req == nil {
		return &ApplyWorkflowDeploymentRequest{}
	}
	return &ApplyWorkflowDeploymentRequest{
		Spec:         workflowDeploymentSpecInputPtrFromSpec(req.GetSpec()),
		Plan:         planWorkflowResponseFromProto(req.GetPlan()),
		Binding:      workflowDeploymentBindingFromProto(req.GetBinding()),
		RequestID:    req.GetRequestId(),
		ValidateOnly: req.GetValidateOnly(),
	}
}

func startWorkflowRunRequestFromProto(req *proto.StartWorkflowRunRequest) *StartWorkflowRunRequest {
	if req == nil {
		return &StartWorkflowRunRequest{}
	}
	return &StartWorkflowRunRequest{
		DeploymentID:         req.GetDeploymentId(),
		DeploymentGeneration: req.GetDeploymentGeneration(),
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
		DeploymentID:         req.GetDeploymentId(),
		DeploymentGeneration: req.GetDeploymentGeneration(),
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
