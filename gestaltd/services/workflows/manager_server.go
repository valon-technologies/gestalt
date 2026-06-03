package workflows

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type InvocationTokenManager = appaccessservice.InvocationTokenManager
type ManagerService = workflowmanager.Service

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return appaccessservice.NewInvocationTokenManager(secret)
}

type ProviderServer struct {
	proto.UnimplementedWorkflowProviderServer

	appName string
	manager ManagerService
	tokens  *InvocationTokenManager
}

func NewProviderServer(appName string, manager ManagerService, tokens *InvocationTokenManager) *ProviderServer {
	return &ProviderServer{
		appName: strings.TrimSpace(appName),
		manager: manager,
		tokens:  tokens,
	}
}

func (s *ProviderServer) managerContext(ctx context.Context, tokenCtx appaccessservice.TokenContext) context.Context {
	ctx = appaccessservice.RestoreTokenContext(ctx, tokenCtx, "")
	return workflowmanager.WithCallerAppName(ctx, tokenCtx.CallerApp())
}

func (s *ProviderServer) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsApply); err != nil {
		return nil, err
	}
	if req.GetSpec().GetRunAs() != nil {
		return nil, status.Error(codes.PermissionDenied, "workflow run_as is only supported for config-managed workflows")
	}
	spec, err := workflowwire.DefinitionSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "definition spec: %v", err)
	}
	managed, err := s.manager.ApplyDefinition(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), workflowmanager.DefinitionApply{
		ProviderName:   strings.TrimSpace(req.GetProviderName()),
		Spec:           *spec,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		CallerAppName:  tokenCtx.CallerApp(),
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

func (s *ProviderServer) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
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
	managed, err := s.manager.GetDefinition(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), definitionID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsList); err != nil {
		return nil, err
	}
	managed, err := s.manager.ListDefinitions(s.managerContext(ctx, tokenCtx), tokenCtx.Principal())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	out := &proto.ListWorkflowProviderDefinitionsResponse{}
	for _, definition := range managed.Definitions {
		definitionProto, err := managedWorkflowDefinitionToProto(definition)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
		}
		out.Definitions = append(out.Definitions, definitionProto)
	}
	return out, nil
}

func (s *ProviderServer) SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsSetPaused); err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	managed, err := s.manager.SetDefinitionPaused(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), definitionID, req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationDefinitionsSetActivationPaused); err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	activationID := strings.TrimSpace(req.GetActivationId())
	if activationID == "" {
		return nil, status.Error(codes.InvalidArgument, "activation_id is required")
	}
	managed, err := s.manager.SetActivationPaused(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), definitionID, activationID, req.GetPaused())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) (*emptypb.Empty, error) {
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
	if err := s.manager.DeleteDefinition(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), definitionID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ProviderServer) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
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
	if req.GetRunAs() != nil {
		return nil, status.Error(codes.PermissionDenied, "workflow run_as is only supported for config-managed workflows")
	}
	managed, err := s.manager.StartRun(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), workflowmanager.RunStart{
		ProviderName:                 strings.TrimSpace(req.GetProviderName()),
		DefinitionID:                 strings.TrimSpace(req.GetDefinitionId()),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        protoutil.MapFromStruct(req.GetInput()),
		IdempotencyKey:               strings.TrimSpace(req.GetIdempotencyKey()),
		WorkflowKey:                  strings.TrimSpace(req.GetWorkflowKey()),
		CallerAppName:                tokenCtx.CallerApp(),
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

func (s *ProviderServer) ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsList); err != nil {
		return nil, err
	}
	statusFilter, err := workflowwire.RunStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "status: %v", err)
	}
	managed, err := s.manager.ListRuns(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), coreworkflow.ListRunsRequest{
		PageSize:  int(req.GetPageSize()),
		PageToken: strings.TrimSpace(req.GetPageToken()),
		TargetApp: strings.TrimSpace(req.GetTargetApp()),
		Status:    statusFilter,
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	out := &proto.ListWorkflowProviderRunsResponse{
		NextPageToken: managed.NextPageToken,
	}
	for _, run := range managed.Runs {
		runProto, err := managedWorkflowRunToProto(run)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode workflow run: %v", err)
		}
		out.Runs = append(out.Runs, runProto)
	}
	return out, nil
}

func (s *ProviderServer) GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsGet); err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.GetRun(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), runID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow run: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsGetEvents); err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	out, err := s.manager.GetRunEvents(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), runID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return out, nil
}

func (s *ProviderServer) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsGetOutput); err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	out, err := s.manager.GetRunOutput(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), runID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return out, nil
}

func (s *ProviderServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationRunsCancel); err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.CancelRun(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), runID, req.GetReason())
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	resp, err := managedWorkflowRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow run: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
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
	managed, err := s.manager.SignalRun(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), workflowmanager.RunSignal{
		RunID:  runID,
		Signal: workflowwire.SignalFromProto(req.GetSignal()),
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

func (s *ProviderServer) SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (out *proto.SignalWorkflowRunResponse, err error) {
	startedAt := time.Now()
	var managed *workflowmanager.ManagedRunSignal
	dims := workflowManagerSignalOrStartMetricDims(req, nil)
	ctx, span := observability.StartSpan(ctx, "workflow.manager.operation", observability.WorkflowMetricAttributes(dims)...)
	defer func() {
		finalDims := workflowManagerSignalOrStartMetricDims(req, managed)
		observability.SetSpanAttributes(ctx, observability.WorkflowMetricAttributes(finalDims)...)
		observability.EndSpan(span, err)
		observability.RecordWorkflowManagerOperation(ctx, startedAt, err, finalDims)
	}()
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
	if req.GetRunAs() != nil {
		return nil, status.Error(codes.PermissionDenied, "workflow run_as is only supported for config-managed workflows")
	}
	managed, err = s.manager.SignalOrStartRun(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), workflowmanager.RunSignalOrStart{
		ProviderName:                 strings.TrimSpace(req.GetProviderName()),
		WorkflowKey:                  strings.TrimSpace(req.GetWorkflowKey()),
		DefinitionID:                 strings.TrimSpace(req.GetDefinitionId()),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        protoutil.MapFromStruct(req.GetInput()),
		IdempotencyKey:               strings.TrimSpace(req.GetIdempotencyKey()),
		Signal:                       workflowwire.SignalFromProto(req.GetSignal()),
		CallerAppName:                tokenCtx.CallerApp(),
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

func (s *ProviderServer) DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflowGrant(tokenCtx, workflowgrants.OperationEventsDeliver); err != nil {
		return nil, err
	}
	event, err := workflowwire.EventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "event: %v", err)
	}
	delivered, err := s.manager.DeliverEvent(s.managerContext(ctx, tokenCtx), tokenCtx.Principal(), workflowmanager.EventDeliver{
		ProviderName: strings.TrimSpace(req.GetProviderName()),
		AppName:      tokenCtx.CallerApp(),
		Event:        event,
	})
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	out, err := workflowwire.EventToProto(delivered)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow event: %v", err)
	}
	return out, nil
}

func (s *ProviderServer) tokenContext(token string) (appaccessservice.TokenContext, error) {
	tokenCtx, err := s.tokens.ResolveToken(token, "")
	if err != nil {
		return appaccessservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if tokenCtx.CallerApp() == "" {
		return appaccessservice.TokenContext{}, status.Error(codes.FailedPrecondition, "invocation token caller app is required")
	}
	return tokenCtx, nil
}

func (s *ProviderServer) requireWorkflowGrant(tokenCtx appaccessservice.TokenContext, operation string) error {
	if tokenCtx.AllowsWorkflowManagerOperation(operation) {
		return nil
	}
	return status.Errorf(codes.PermissionDenied, "workflow manager operation %q is not allowed for app %q", operation, tokenCtx.CallerApp())
}

func workflowManagerSignalOrStartMetricDims(req *proto.SignalOrStartWorkflowProviderRunRequest, managed *workflowmanager.ManagedRunSignal) observability.WorkflowMetricDims {
	providerName := ""
	runStatus := observability.WorkflowRunStatusUnknown
	targetKind := observability.WorkflowTargetKindUnknown
	if req != nil {
		providerName = strings.TrimSpace(req.GetProviderName())
	}
	if managed != nil {
		if resolved := strings.TrimSpace(managed.ProviderName); resolved != "" {
			providerName = resolved
		}
		if managed.Run != nil {
			targetKind = workflowTargetKindFromCore(managed.Run.Target)
			runStatus = workflowRunStatusFromCore(managed.Run)
		}
	}
	return observability.WorkflowMetricDims{
		ProviderName:    providerName,
		OperationName:   observability.WorkflowOperationSignalOrStartRun,
		TriggerKind:     observability.WorkflowTriggerKindSignal,
		TargetKind:      targetKind,
		RunStatus:       runStatus,
		TelemetrySource: observability.WorkflowTelemetrySourceCore,
	}
}

func workflowTargetKindFromCore(target coreworkflow.Target) string {
	if len(target.Steps) > 0 {
		return observability.WorkflowTargetKindSteps
	}
	return observability.WorkflowTargetKindUnknown
}

func workflowRunStatusFromCore(run *coreworkflow.Run) string {
	if run == nil {
		return observability.WorkflowRunStatusUnknown
	}
	switch run.Status {
	case coreworkflow.RunStatusPending:
		return observability.WorkflowRunStatusPending
	case coreworkflow.RunStatusRunning:
		return observability.WorkflowRunStatusRunning
	case coreworkflow.RunStatusSucceeded:
		return observability.WorkflowRunStatusSucceeded
	case coreworkflow.RunStatusFailed:
		return observability.WorkflowRunStatusFailed
	case coreworkflow.RunStatusCanceled:
		return observability.WorkflowRunStatusCanceled
	default:
		return observability.WorkflowRunStatusUnknown
	}
}

func workflowManagerStatusError(err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := status.FromError(err); ok {
		return existing.Err()
	}
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowEventMatchRequired), errors.Is(err, workflowmanager.ErrWorkflowEventSourceRequired), errors.Is(err, workflowmanager.ErrWorkflowEventTypeRequired), errors.Is(err, workflowmanager.ErrWorkflowKeyRequired), errors.Is(err, workflowmanager.ErrWorkflowSignalNameRequired), errors.Is(err, invocation.ErrInvalidInvocation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowSubjectRequired), errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, workflowmanager.ErrDuplicateWorkflowObjects), errors.Is(err, invocation.ErrInternal):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound), errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Unknown, err.Error())
	}
}

var _ proto.WorkflowProviderServer = (*ProviderServer)(nil)
