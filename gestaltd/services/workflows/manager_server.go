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

func (s *ManagerServer) ApplyDefinition(ctx context.Context, req *proto.WorkflowManagerApplyDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsCreate); err != nil {
		return nil, err
	}
	managed, err := s.manager.ApplyDefinition(ctx, tokenCtx.Principal(), workflowmanager.DefinitionApply{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Spec:             workflowDefinitionSpecFromProto(req.GetSpec()),
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDefinitionToProto(managed.ProviderName, managed.Definition)
}

func (s *ManagerServer) GetDefinition(ctx context.Context, req *proto.WorkflowManagerGetDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsGet); err != nil {
		return nil, err
	}
	managed, err := s.manager.GetDefinition(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDefinitionId()))
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDefinitionToProto(managed.ProviderName, managed.Definition)
}

func (s *ManagerServer) ListDefinitions(ctx context.Context, req *proto.WorkflowManagerListDefinitionsRequest) (*proto.WorkflowManagerListDefinitionsResponse, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsGet); err != nil {
		return nil, err
	}
	values, err := s.manager.ListDefinitions(ctx, tokenCtx.Principal())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	out := &proto.WorkflowManagerListDefinitionsResponse{}
	for _, value := range values {
		managed, err := managedWorkflowDefinitionToProto(value.ProviderName, value.Definition)
		if err != nil {
			return nil, err
		}
		if providerName := strings.TrimSpace(req.GetProviderName()); providerName == "" || strings.TrimSpace(managed.GetProviderName()) == providerName {
			out.Definitions = append(out.Definitions, managed)
		}
	}
	return out, nil
}

func (s *ManagerServer) DeleteDefinition(ctx context.Context, req *proto.WorkflowManagerDeleteDefinitionRequest) (*emptypb.Empty, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsDelete); err != nil {
		return nil, err
	}
	if err := s.manager.DeleteDefinition(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDefinitionId())); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ManagerServer) SetDefinitionPaused(ctx context.Context, req *proto.WorkflowManagerSetDefinitionPausedRequest) (*proto.ManagedWorkflowDefinition, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsUpdate); err != nil {
		return nil, err
	}
	managed, err := s.manager.SetDefinitionPaused(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDefinitionId()), req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDefinitionToProto(managed.ProviderName, managed.Definition)
}

func (s *ManagerServer) SetActivationPaused(ctx context.Context, req *proto.WorkflowManagerSetActivationPausedRequest) (*proto.ManagedWorkflowDefinition, error) {
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsUpdate); err != nil {
		return nil, err
	}
	managed, err := s.manager.SetActivationPaused(ctx, tokenCtx.Principal(), strings.TrimSpace(req.GetDefinitionId()), strings.TrimSpace(req.GetActivationId()), req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return managedWorkflowDefinitionToProto(managed.ProviderName, managed.Definition)
}

func (s *ManagerServer) StartRun(ctx context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	tokenCtx, spec, providerName, err := s.definitionRunContext(ctx, req.GetInvocationToken(), req.GetProviderName(), req.GetDefinitionId())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsStart); err != nil {
		return nil, err
	}
	managed, err := s.manager.StartRun(ctx, tokenCtx.Principal(), workflowmanager.RunStart{
		ProviderName:         providerName,
		DefinitionID:         strings.TrimSpace(req.GetDefinitionId()),
		DefinitionGeneration: req.GetDefinitionGeneration(),
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
	tokenCtx, spec, providerName, err := s.definitionRunContext(ctx, req.GetInvocationToken(), req.GetProviderName(), req.GetDefinitionId())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsSignalOrStart); err != nil {
		return nil, err
	}
	signal := workflowSignalFromProto(req.GetSignal())
	managed, err := s.manager.SignalOrStartRun(ctx, tokenCtx.Principal(), workflowmanager.RunSignalOrStart{
		ProviderName:         providerName,
		DefinitionID:         strings.TrimSpace(req.GetDefinitionId()),
		DefinitionGeneration: req.GetDefinitionGeneration(),
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

func (s *ManagerServer) definitionRunContext(ctx context.Context, invocationToken, requestedProviderName, definitionID string) (plugininvokerservice.TokenContext, coreworkflow.DefinitionSpec, string, error) {
	tokenCtx, err := s.tokenContext(invocationToken)
	if err != nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DefinitionSpec{}, "", err
	}
	if s == nil || s.manager == nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DefinitionSpec{}, "", status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	definitionID = strings.TrimSpace(definitionID)
	managed, err := s.manager.GetDefinition(ctx, tokenCtx.Principal(), definitionID)
	if err != nil {
		return plugininvokerservice.TokenContext{}, coreworkflow.DefinitionSpec{}, "", workflowManagerStatusError(err)
	}
	spec := managed.Definition.Spec
	providerName := strings.TrimSpace(managed.ProviderName)
	if requested := strings.TrimSpace(requestedProviderName); requested != "" && requested != providerName {
		return plugininvokerservice.TokenContext{}, coreworkflow.DefinitionSpec{}, "", status.Errorf(codes.InvalidArgument, "workflow definition belongs to provider %q, not %q", providerName, requested)
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
