package workflows

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowauth"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ManagerService = workflowmanager.Service

type ProviderServer struct {
	proto.UnimplementedWorkflowServer

	appName              string
	workflowProviderName string
	manager              ManagerService
	authorization        core.AuthorizationProvider
	agentAuth            interface {
		AuthorizeWorkflowInvocation(context.Context, invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error)
	}
}

type ProviderServerOption func(*ProviderServer)

func WithWorkflowProviderName(name string) ProviderServerOption {
	return func(s *ProviderServer) {
		if name = strings.TrimSpace(name); name != "" {
			s.workflowProviderName = name
		}
	}
}

func WithAgentWorkflowInvocationAuthorizer(authorizer interface {
	AuthorizeWorkflowInvocation(context.Context, invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error)
}) ProviderServerOption {
	return func(s *ProviderServer) {
		s.agentAuth = authorizer
	}
}

func NewProviderServer(appName string, manager ManagerService, authorization core.AuthorizationProvider, opts ...ProviderServerOption) *ProviderServer {
	s := &ProviderServer{
		appName:              strings.TrimSpace(appName),
		workflowProviderName: "default",
		manager:              manager,
		authorization:        authorization,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

type workflowProviderAuthRequest interface {
	GetContext() *proto.RequestContext
}

type workflowManagerAuthContext struct {
	request appaccessservice.ProviderRequestContext
	raw     *proto.RequestContext
}

func (c workflowManagerAuthContext) Principal() *principal.Principal {
	return c.request.Principal()
}

func (c workflowManagerAuthContext) Caller() invocation.CallerProvider {
	return invocation.CallerProvider{Kind: c.request.CallerKind(), Name: c.request.CallerName()}
}

func (c workflowManagerAuthContext) CallerName() string {
	return c.request.CallerName()
}

func (c workflowManagerAuthContext) Agent() invocation.AgentInvocationContext {
	return c.request.Agent()
}

func (c workflowManagerAuthContext) Restore(ctx context.Context) context.Context {
	return c.request.Restore(ctx, "")
}

func (s *ProviderServer) managerContext(ctx context.Context, authCtx workflowManagerAuthContext) context.Context {
	return authCtx.Restore(ctx)
}

func workflowManagerCaller(authCtx workflowManagerAuthContext) invocation.CallerProvider {
	return authCtx.Caller()
}

func (s *ProviderServer) ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsApply)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.GetSpec().GetRunAs()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow run_as is required")
	}
	kind, subjectID, ok := core.ParseSubjectID(req.GetSpec().GetRunAs())
	if !ok || kind != "service_account" || subjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow run_as must be service_account:<id>")
	}
	if err := s.requireServiceAccountManagement(ctx, authCtx, p, req.GetSpec().GetRunAs()); err != nil {
		return nil, err
	}
	if s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow manager is not configured")
	}
	spec, err := workflowwire.DefinitionSpecFromProto(req.GetSpec())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "definition spec: %v", err)
	}
	managed, err := s.manager.ApplyDefinition(s.managerContext(ctx, authCtx), p, workflowmanager.DefinitionApply{
		Spec:           *spec,
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		Caller:         workflowManagerCaller(authCtx),
	})
	if err != nil {
		err = workflowManagerStatusError(err)
		return nil, err
	}
	resp, err := managedWorkflowDefinitionToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode workflow definition: %v", err)
	}
	return resp, nil
}

func (s *ProviderServer) requireServiceAccountManagement(ctx context.Context, authCtx workflowManagerAuthContext, p *principal.Principal, runAs string) error {
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok && authCtx.CallerName() == "gestaltd" && origin.FullMethod != "" {
		return nil
	}
	if s == nil || s.authorization == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is required for workflow run_as management")
	}
	subjectID := ""
	if p != nil {
		subjectID = strings.TrimSpace(p.SubjectID)
	}
	if subjectID == "" {
		subjectID = strings.TrimSpace(authCtx.CallerName())
	}
	resp, err := s.authorization.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: subjectID},
		Action:   &proto.Action{Name: "manages"},
		Resource: &proto.Resource{Type: "service_account", Id: strings.TrimSpace(runAs)},
	})
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "subject %q cannot manage %q: %v", subjectID, runAs, err)
	}
	if resp == nil || !resp.GetAllowed() {
		return status.Errorf(codes.PermissionDenied, "subject %q cannot manage %q", subjectID, runAs)
	}
	return nil
}

func (s *ProviderServer) GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsGet)
	if err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	managed, err := s.manager.GetDefinition(s.managerContext(ctx, authCtx), p, definitionID)
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsList)
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.ListDefinitions(s.managerContext(ctx, authCtx), p)
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsSetPaused)
	if err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	managed, err := s.manager.SetDefinitionPaused(s.managerContext(ctx, authCtx), p, definitionID, req.GetPaused())
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsSetActivationPaused)
	if err != nil {
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
	managed, err := s.manager.SetActivationPaused(s.managerContext(ctx, authCtx), p, definitionID, activationID, req.GetPaused())
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationDefinitionsDelete)
	if err != nil {
		return nil, err
	}
	definitionID := strings.TrimSpace(req.GetDefinitionId())
	if definitionID == "" {
		return nil, status.Error(codes.InvalidArgument, "definition_id is required")
	}
	if err := s.manager.DeleteDefinition(s.managerContext(ctx, authCtx), p, definitionID); err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ProviderServer) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsStart)
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.StartRun(s.managerContext(ctx, authCtx), p, workflowmanager.RunStart{
		DefinitionID:                 strings.TrimSpace(req.GetDefinitionId()),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        protoutil.MapFromStruct(req.GetInput()),
		IdempotencyKey:               strings.TrimSpace(req.GetIdempotencyKey()),
		WorkflowKey:                  strings.TrimSpace(req.GetWorkflowKey()),
		Caller:                       workflowManagerCaller(authCtx),
	})
	if err != nil {
		err = workflowManagerStatusError(err)
		return nil, err
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsList)
	if err != nil {
		return nil, err
	}
	statusFilter, err := workflowwire.RunStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "status: %v", err)
	}
	managed, err := s.manager.ListRuns(s.managerContext(ctx, authCtx), p, coreworkflow.ListRunsRequest{
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsGet)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.GetRun(s.managerContext(ctx, authCtx), p, runID)
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsGetEvents)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	out, err := s.manager.GetRunEvents(s.managerContext(ctx, authCtx), p, runID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return out, nil
}

func (s *ProviderServer) GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsGetOutput)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	out, err := s.manager.GetRunOutput(s.managerContext(ctx, authCtx), p, runID)
	if err != nil {
		return nil, workflowManagerStatusError(err)
	}
	return out, nil
}

func (s *ProviderServer) CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsCancel)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.CancelRun(s.managerContext(ctx, authCtx), p, runID, req.GetReason())
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsSignal)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.SignalRun(s.managerContext(ctx, authCtx), p, workflowmanager.RunSignal{
		RunID:  runID,
		Signal: workflowwire.SignalFromProto(req.GetSignal()),
	})
	if err != nil {
		err = workflowManagerStatusError(err)
		return nil, err
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
	dims := s.workflowManagerSignalOrStartMetricDims(req, nil)
	ctx, span := observability.StartSpan(ctx, "workflow.manager.operation", observability.WorkflowMetricAttributes(dims)...)
	defer func() {
		finalDims := s.workflowManagerSignalOrStartMetricDims(req, managed)
		observability.SetSpanAttributes(ctx, observability.WorkflowMetricAttributes(finalDims)...)
		observability.EndSpan(span, err)
		observability.RecordWorkflowManagerOperation(ctx, startedAt, err, finalDims)
	}()
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationRunsSignalOrStart)
	if err != nil {
		return nil, err
	}
	managed, err = s.manager.SignalOrStartRun(s.managerContext(ctx, authCtx), p, workflowmanager.RunSignalOrStart{
		WorkflowKey:                  strings.TrimSpace(req.GetWorkflowKey()),
		DefinitionID:                 strings.TrimSpace(req.GetDefinitionId()),
		ExpectedDefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		Input:                        protoutil.MapFromStruct(req.GetInput()),
		IdempotencyKey:               strings.TrimSpace(req.GetIdempotencyKey()),
		Signal:                       workflowwire.SignalFromProto(req.GetSignal()),
		Caller:                       workflowManagerCaller(authCtx),
	})
	if err != nil {
		err = workflowManagerStatusError(err)
		return nil, err
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
	authCtx, err := s.requestContext(req)
	if err != nil {
		return nil, err
	}
	p, err := s.authorizeWorkflowAccess(ctx, authCtx, workflowauth.OperationEventsDeliver)
	if err != nil {
		return nil, err
	}
	event, err := workflowwire.EventFromProto(req.GetEvent())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "event: %v", err)
	}
	appName := authCtx.CallerName()
	delivered, err := s.manager.DeliverEvent(s.managerContext(ctx, authCtx), p, workflowmanager.EventDeliver{
		AppName: appName,
		Event:   event,
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

func (s *ProviderServer) requestContext(req workflowProviderAuthRequest) (workflowManagerAuthContext, error) {
	raw := req.GetContext()
	authCtx, err := appaccessservice.ProviderRequestContextFromProto(raw, "", "")
	if err != nil {
		return workflowManagerAuthContext{}, err
	}
	if agent := authCtx.Agent(); agent != (invocation.AgentInvocationContext{}) {
		if strings.TrimSpace(agent.ProviderName) != strings.TrimSpace(s.appName) {
			return workflowManagerAuthContext{}, status.Errorf(codes.PermissionDenied, "agent invocation context provider %q does not match serving provider %q", strings.TrimSpace(agent.ProviderName), strings.TrimSpace(s.appName))
		}
		return workflowManagerAuthContext{request: authCtx, raw: raw}, nil
	}
	if authCtx.CallerKind() != invocation.ProviderKindApp {
		return workflowManagerAuthContext{}, status.Error(codes.FailedPrecondition, "app caller context is required")
	}
	if strings.TrimSpace(authCtx.CallerName()) != strings.TrimSpace(s.appName) {
		return workflowManagerAuthContext{}, status.Errorf(codes.PermissionDenied, "provider caller context %q does not match serving provider %q", authCtx.CallerName(), strings.TrimSpace(s.appName))
	}
	return workflowManagerAuthContext{request: authCtx, raw: raw}, nil
}

func (s *ProviderServer) authorizeWorkflowAccess(ctx context.Context, authCtx workflowManagerAuthContext, operation string) (*principal.Principal, error) {
	if authCtx.Agent() != (invocation.AgentInvocationContext{}) {
		if s == nil || s.agentAuth == nil {
			return nil, status.Error(codes.FailedPrecondition, "agent workflow invocation authorizer is required")
		}
		authorized, err := s.agentAuth.AuthorizeWorkflowInvocation(ctx, invocation.AgentWorkflowAuthorizationRequest{
			AgentProviderName: strings.TrimSpace(s.appName),
			CallerKind:        authCtx.Caller().Kind,
			CallerName:        authCtx.Caller().Name,
			Agent:             authCtx.Agent(),
			Principal:         authCtx.Principal(),
			Operation:         operation,
			RequestContext:    authCtx.raw,
		})
		if err != nil {
			return nil, err
		}
		if authorized.Principal != nil {
			return authorized.Principal, nil
		}
		return authCtx.Principal(), nil
	}
	if err := s.requireWorkflowAccess(ctx, authCtx, operation); err != nil {
		return nil, err
	}
	return authCtx.Principal(), nil
}

func (s *ProviderServer) requireWorkflowAccess(ctx context.Context, authCtx workflowManagerAuthContext, operation string) error {
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok && authCtx.CallerName() == "gestaltd" && origin.FullMethod != "" {
		return nil
	}
	if s == nil || s.authorization == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is required for workflow manager operation")
	}
	action := workflowRPCAction(operation)
	subjectID := ""
	if p := authCtx.Principal(); p != nil {
		subjectID = strings.TrimSpace(p.SubjectID)
	}
	if subjectID == "" {
		subjectID = strings.TrimSpace(authCtx.CallerName())
	}
	resp, err := s.authorization.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject: &proto.Subject{
			Type: "subject",
			Id:   subjectID,
		},
		Action: &proto.Action{Name: action},
		Resource: &proto.Resource{
			Type: "workflow",
			Id:   strings.TrimSpace(s.workflowProviderName),
		},
	})
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "workflow RPC %q is not allowed for subject %q: %v", action, subjectID, err)
	}
	if resp == nil || !resp.GetAllowed() {
		return status.Errorf(codes.PermissionDenied, "workflow RPC %q is not allowed for subject %q", action, subjectID)
	}
	return nil
}

func workflowRPCAction(operation string) string {
	if strings.HasSuffix(operation, "definitions.apply") {
		return "ApplyDefinition"
	}
	if strings.HasSuffix(operation, "definitions.get") {
		return "GetDefinition"
	}
	if strings.HasSuffix(operation, "definitions.list") {
		return "ListDefinitions"
	}
	if strings.HasSuffix(operation, "definitions.set_paused") {
		return "SetDefinitionPaused"
	}
	if strings.HasSuffix(operation, "definitions.set_activation_paused") {
		return "SetActivationPaused"
	}
	if strings.HasSuffix(operation, "definitions.delete") {
		return "DeleteDefinition"
	}
	if strings.HasSuffix(operation, "runs.start") {
		return "StartRun"
	}
	if strings.HasSuffix(operation, "runs.list") {
		return "ListRuns"
	}
	if strings.HasSuffix(operation, "runs.get") {
		return "GetRun"
	}
	if strings.HasSuffix(operation, "runs.get_events") {
		return "GetRunEvents"
	}
	if strings.HasSuffix(operation, "runs.get_output") {
		return "GetRunOutput"
	}
	if strings.HasSuffix(operation, "runs.cancel") {
		return "CancelRun"
	}
	if strings.HasSuffix(operation, "runs.signal") {
		return "SignalRun"
	}
	if strings.HasSuffix(operation, "runs.signal_or_start") {
		return "SignalOrStartRun"
	}
	if strings.HasSuffix(operation, "events.deliver") {
		return "DeliverEvent"
	}
	return operation
}

func (s *ProviderServer) workflowManagerSignalOrStartMetricDims(req *proto.SignalOrStartWorkflowProviderRunRequest, managed *workflowmanager.ManagedRunSignal) observability.WorkflowMetricDims {
	providerName := s.workflowProviderName
	runStatus := observability.WorkflowRunStatusUnknown
	targetKind := observability.WorkflowTargetKindUnknown
	if req != nil {
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

var _ proto.WorkflowServer = (*ProviderServer)(nil)
