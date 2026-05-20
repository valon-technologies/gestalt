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

func (s *ManagerServer) PlanDeployment(ctx context.Context, req *proto.WorkflowManagerPlanDeploymentRequest) (*proto.PlanWorkflowResponse, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	spec := workflowDeploymentSpecFromProto(req.GetSpec())
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDeploymentsCreate); err != nil {
		return nil, err
	}
	plan, err := s.manager.PlanDeployment(ctx, tokenCtx.Principal(), workflowmanager.DeploymentPlan{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Spec:             spec,
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return workflowPlanResponseToProto(plan), nil
}

func (s *ManagerServer) ApplyDeployment(ctx context.Context, req *proto.WorkflowManagerApplyDeploymentRequest) (*proto.ManagedWorkflowDeployment, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDeploymentsCreate); err != nil {
		return nil, err
	}
	managed, err := s.manager.ApplyDeployment(ctx, tokenCtx.Principal(), workflowmanager.DeploymentApply{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Spec:             workflowDeploymentSpecFromProto(req.GetSpec()),
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDeploymentToProto(managed.ProviderName, managed.Deployment)
}

func (s *ManagerServer) GetDeployment(ctx context.Context, req *proto.WorkflowManagerGetDeploymentRequest) (*proto.ManagedWorkflowDeployment, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDeploymentsGet); err != nil {
		return nil, err
	}
	managed, err := s.manager.GetDeployment(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDeploymentId()))
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDeploymentToProto(managed.ProviderName, managed.Deployment)
}

func (s *ManagerServer) ListDeployments(ctx context.Context, req *proto.WorkflowManagerListDeploymentsRequest) (*proto.WorkflowManagerListDeploymentsResponse, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDeploymentsGet); err != nil {
		return nil, err
	}
	values, err := s.manager.ListDeployments(ctx, tokenCtx.Principal())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	out := &proto.WorkflowManagerListDeploymentsResponse{}
	for _, value := range values {
		managed, err := managedWorkflowDeploymentToProto(value.ProviderName, value.Deployment)
		if err != nil {
			return nil, err
		}
		if providerName := strings.TrimSpace(req.GetProviderName()); providerName == "" || strings.TrimSpace(managed.GetProviderName()) == providerName {
			out.Deployments = append(out.Deployments, managed)
		}
	}
	return out, nil
}

func (s *ManagerServer) DeleteDeployment(ctx context.Context, req *proto.WorkflowManagerDeleteDeploymentRequest) (*emptypb.Empty, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDeploymentsDelete); err != nil {
		return nil, err
	}
	if err := s.manager.DeleteDeployment(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDeploymentId())); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ManagerServer) SetDeploymentPaused(ctx context.Context, req *proto.WorkflowManagerSetDeploymentPausedRequest) (*proto.ManagedWorkflowDeployment, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	operation := workflowgrants.OperationDeploymentsResume
	if req.GetPaused() {
		operation = workflowgrants.OperationDeploymentsPause
	}
	if err := s.requireWorkflowGrant(tokenCtx, operation); err != nil {
		return nil, err
	}
	managed, err := s.manager.SetDeploymentPaused(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDeploymentId()), req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDeploymentToProto(managed.ProviderName, managed.Deployment)
}

func (s *ManagerServer) SetActivationPaused(ctx context.Context, req *proto.WorkflowManagerSetActivationPausedRequest) (*proto.ManagedWorkflowDeployment, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	operation := workflowgrants.OperationDeploymentsResume
	if req.GetPaused() {
		operation = workflowgrants.OperationDeploymentsPause
	}
	if err := s.requireWorkflowGrant(tokenCtx, operation); err != nil {
		return nil, err
	}
	managed, err := s.manager.SetActivationPaused(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDeploymentId()), strings.TrimSpace(req.GetActivationId()), req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDeploymentToProto(managed.ProviderName, managed.Deployment)
}

func (s *ManagerServer) StartRun(ctx context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	tokenCtx, spec, providerName, err := s.deploymentRunContext(ctx, req.GetInvocationToken(), req.GetProviderName(), req.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsStart); err != nil {
		return nil, err
	}
	managed, err := s.manager.StartRun(ctx, tokenCtx.Principal(), workflowmanager.RunStart{
		ProviderName:         providerName,
		DeploymentID:         strings.TrimSpace(req.GetDeploymentId()),
		DeploymentGeneration: req.GetDeploymentGeneration(),
		ActivationID:         strings.TrimSpace(req.GetActivationId()),
		Target:               spec.Target,
		Input:                structMap(req.GetInput()),
		IdempotencyKey:       strings.TrimSpace(req.GetIdempotencyKey()),
		WorkflowKey:          strings.TrimSpace(req.GetWorkflowKey()),
		CallerPluginName:     strings.TrimSpace(s.pluginName),
		Permissions:          append([]core.AccessPermission(nil), spec.Permissions...),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowRunToProto(managed)
}

func (s *ManagerServer) SignalRun(ctx context.Context, req *proto.WorkflowManagerSignalRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsSignal); err != nil {
		return nil, err
	}
	signal := workflowSignalFromProto(req.GetSignal())
	managed, err := s.manager.SignalRun(ctx, tokenCtx.Principal(), workflowmanager.RunSignal{RunID: strings.TrimSpace(req.GetRunId()), Signal: signal})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowRunSignalToProto(managed)
}

func (s *ManagerServer) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	tokenCtx, spec, providerName, err := s.deploymentRunContext(ctx, req.GetInvocationToken(), req.GetProviderName(), req.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsSignalOrStart); err != nil {
		return nil, err
	}
	signal := workflowSignalFromProto(req.GetSignal())
	managed, err := s.manager.SignalOrStartRun(ctx, tokenCtx.Principal(), workflowmanager.RunSignalOrStart{
		ProviderName:         providerName,
		DeploymentID:         strings.TrimSpace(req.GetDeploymentId()),
		DeploymentGeneration: req.GetDeploymentGeneration(),
		ActivationID:         strings.TrimSpace(req.GetActivationId()),
		WorkflowKey:          strings.TrimSpace(req.GetWorkflowKey()),
		Target:               spec.Target,
		Input:                structMap(req.GetInput()),
		IdempotencyKey:       strings.TrimSpace(req.GetIdempotencyKey()),
		Signal:               signal,
		CallerPluginName:     strings.TrimSpace(s.pluginName),
		Permissions:          append([]core.AccessPermission(nil), spec.Permissions...),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowRunSignalToProto(managed)
}

func (s *ManagerServer) CancelRun(ctx context.Context, req *proto.WorkflowManagerCancelRunRequest) (*proto.ManagedWorkflowRun, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsCancel); err != nil {
		return nil, err
	}
	managed, err := s.manager.CancelRun(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetRunId()), strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowRunToProto(managed)
}

func (s *ManagerServer) DeliverEvent(ctx context.Context, req *proto.WorkflowManagerDeliverEventRequest) (*proto.WorkflowManagerDeliverEventResponse, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventsPublish); err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	event, err := workflowEventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp, err := s.manager.DeliverEvent(ctx, tokenCtx.Principal(), workflowmanager.EventPublish{
		ProviderName:   strings.TrimSpace(req.GetProviderName()),
		PluginName:     strings.TrimSpace(s.pluginName),
		Event:          event,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	var results []*proto.WorkflowEventDeliveryResult
	if resp != nil {
		results, err = workflowEventDeliveryResultsToProto(resp.Results)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &proto.WorkflowManagerDeliverEventResponse{Results: results}, nil
}

func (s *ManagerServer) deploymentRunContext(ctx context.Context, invocationToken, requestedProviderName, deploymentID string) (plugininvokerservice.TokenContext, coreworkflow.DeploymentSpec, string, error) {
	tokenCtx, err := s.tokenContext(invocationToken)
	if err != nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DeploymentSpec{}, "", err
	}
	if s == nil || s.manager == nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DeploymentSpec{}, "", status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	deploymentID = strings.TrimSpace(deploymentID)
	managed, err := s.manager.GetDeployment(ctx, tokenCtx.Principal(), deploymentID)
	if err != nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DeploymentSpec{}, "", workflowManagerStatusError(err)
	}
	spec := managed.Deployment.Spec
	providerName := strings.TrimSpace(managed.ProviderName)
	if requested := strings.TrimSpace(requestedProviderName); requested != "" && requested != providerName {
		return plugininvokerservice.TokenContext{}, coreworkflow.DeploymentSpec{}, "", status.Errorf(codes.InvalidArgument, "workflow deployment belongs to provider %q, not %q", providerName, requested)
	}
	return tokenCtx, spec, providerName, nil
}

func (s *ManagerServer) tokenContext(token string) (plugininvokerservice.TokenContext, error) {
	if s == nil || s.tokens == nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, "workflow manager token resolver is not configured")
	}
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
