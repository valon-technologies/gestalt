package appaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AppServer struct {
	proto.UnimplementedAppServer

	invoker      invocation.Invoker
	callerName   string
	agentAppAuth interface {
		AuthorizeAppInvocation(context.Context, invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error)
	}
}

type AppServerOption func(*AppServer)

func WithAgentAppInvocationAuthorizer(authorizer interface {
	AuthorizeAppInvocation(context.Context, invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error)
}) AppServerOption {
	return func(s *AppServer) {
		s.agentAppAuth = authorizer
	}
}

func WithCallerApp(callerApp string) AppServerOption {
	return func(s *AppServer) {
		s.callerName = strings.TrimSpace(callerApp)
	}
}

func NewAppServer(invoker invocation.Invoker, opts ...AppServerOption) *AppServer {
	s := &AppServer{
		invoker: invoker,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func NewServer(invoker invocation.Invoker, opts ...AppServerOption) proto.AppServer {
	return NewAppServer(invoker, opts...)
}

func (s *AppServer) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	targetApp := strings.TrimSpace(req.GetApp())
	targetOperation := strings.TrimSpace(req.GetOperation())
	if targetApp == "" {
		return nil, status.Error(codes.InvalidArgument, "app is required")
	}
	if targetOperation == "" {
		return nil, status.Error(codes.InvalidArgument, "operation is required")
	}
	callCtx, err := s.requestContextForInvoke(ctx, req.GetContext(), targetApp, targetOperation, req.GetConnection(), req.GetInstance(), req.GetCredentialMode(), req.GetRunAs())
	if err != nil {
		return nil, err
	}

	invokeCtx, instance, err := prepareInvocationSelectors(ctx, callCtx, req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, err
	}
	invokeCtx = invocation.WithIdempotencyKey(invokeCtx, req.GetIdempotencyKey())
	params := map[string]any{}
	if raw := req.GetParams(); raw != nil {
		params = raw.AsMap()
	}
	invokePrincipal := callCtx.principal
	invokeCtx, invokePrincipal = invocation.ApplyDelegation(
		invokeCtx,
		invokePrincipal,
		callCtx.delegationRunAs,
	)

	result, err := s.invoker.Invoke(invokeCtx, invokePrincipal, targetApp, instance, targetOperation, params)
	return appOperationResult(result, err)
}

func (s *AppServer) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	targetApp := strings.TrimSpace(req.GetApp())
	if targetApp == "" {
		return nil, status.Error(codes.InvalidArgument, "app is required")
	}
	document := strings.TrimSpace(req.GetDocument())
	if document == "" {
		return nil, status.Error(codes.InvalidArgument, "document is required")
	}
	callCtx, err := s.requestContextForSurfaceInvoke(ctx, req.GetContext(), targetApp, "graphql")
	if err != nil {
		return nil, err
	}
	graphQLInvoker, ok := s.invoker.(interface {
		InvokeGraphQL(context.Context, *principal.Principal, string, string, invocation.GraphQLRequest) (*core.OperationResult, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "plugin graphql invocation is not available")
	}

	invokeCtx, instance, err := prepareInvocationSelectors(ctx, callCtx, req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, err
	}
	invokeCtx = invocation.WithIdempotencyKey(invokeCtx, req.GetIdempotencyKey())

	variables := map[string]any{}
	if raw := req.GetVariables(); raw != nil {
		variables = raw.AsMap()
	}
	result, err := graphQLInvoker.InvokeGraphQL(invokeCtx, callCtx.principal, targetApp, instance, invocation.GraphQLRequest{
		Document:  document,
		Variables: variables,
	})
	return appOperationResult(result, err)
}

type requestInvocationContext struct {
	callerName             string
	callerProviderKind     invocation.ProviderKind
	principal              *principal.Principal
	credential             invocation.CredentialContext
	credentialModeOverride core.ConnectionMode
	delegationRunAs        *core.RunAsSubject
	workflow               map[string]any
	agent                  invocation.AgentInvocationContext
	agentToolRefs          []coreagent.ToolRef
	agentToolRefsSet       bool
	authorizedConnection   string
	authorizedInstance     string
	authorizedSelectors    bool
	requestContext         *proto.RequestContext
}

func (s *AppServer) requestContextForInvoke(ctx context.Context, reqCtx *proto.RequestContext, targetApp, targetOperation, rawConnection, rawInstance, rawCredentialMode string, requestRunAs *proto.SubjectContext) (requestInvocationContext, error) {
	callCtx, err := s.requestContext(reqCtx)
	if err != nil {
		return requestInvocationContext{}, err
	}
	credentialMode, err := appInvokeCredentialMode(rawCredentialMode)
	if err != nil {
		return requestInvocationContext{}, err
	}
	if err := rejectRequestRunAsForSpecialCallers(callCtx, requestRunAs); err != nil {
		return requestInvocationContext{}, err
	}
	if callCtx.agent != (invocation.AgentInvocationContext{}) {
		authorized, err := s.authorizeAgentAppInvocation(ctx, callCtx, targetApp, targetOperation, rawConnection, rawInstance, credentialMode)
		if err != nil {
			return requestInvocationContext{}, err
		}
		if authorized.Principal != nil {
			callCtx.principal = authorized.Principal
		}
		if authorized.CredentialMode != "" {
			callCtx.credentialModeOverride = authorized.CredentialMode
		}
		callCtx.authorizedConnection = strings.TrimSpace(authorized.Connection)
		callCtx.authorizedInstance = strings.TrimSpace(authorized.Instance)
		callCtx.authorizedSelectors = true
		if runAs := core.NormalizeRunAsSubject(authorized.RunAs); runAs != nil {
			callCtx.delegationRunAs = runAs
		}
		if authorized.ToolRefsSet {
			callCtx.agentToolRefs = append([]coreagent.ToolRef(nil), authorized.ToolRefs...)
			callCtx.agentToolRefsSet = true
		}
		return callCtx, nil
	}
	if callCtx.callerProviderKind == invocation.ProviderKindWorkflow {
		if err := authorizeWorkflowAppInvocation(callCtx, targetApp, targetOperation, rawConnection, rawInstance, credentialMode); err != nil {
			return requestInvocationContext{}, err
		}
		if credentialMode != "" {
			callCtx.credentialModeOverride = credentialMode
		}
		return callCtx, nil
	}
	if callCtx.callerProviderKind == invocation.ProviderKindAgent {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "agent app invocation context is required")
	}
	if credentialMode != "" {
		callCtx.credentialModeOverride = credentialMode
	}
	if runAs := agentwire.RunAsSubjectFromProto(requestRunAs); runAs != nil {
		callCtx.delegationRunAs = runAs
	}
	return callCtx, nil
}

func (s *AppServer) requestContextForSurfaceInvoke(ctx context.Context, reqCtx *proto.RequestContext, targetApp, surface string) (requestInvocationContext, error) {
	callCtx, err := s.requestContext(reqCtx)
	if err != nil {
		return requestInvocationContext{}, err
	}
	if callCtx.agent != (invocation.AgentInvocationContext{}) || callCtx.callerProviderKind == invocation.ProviderKindAgent {
		return requestInvocationContext{}, status.Error(codes.PermissionDenied, "agent callers may only invoke listed app operations")
	}
	if callCtx.callerProviderKind == invocation.ProviderKindWorkflow {
		return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "%s callers may only invoke authorized target app operations", callCtx.callerProviderKind)
	}
	_ = surface
	_ = targetApp
	return callCtx, nil
}

func rejectRequestRunAsForSpecialCallers(callCtx requestInvocationContext, requestRunAs *proto.SubjectContext) error {
	if requestRunAs == nil || strings.TrimSpace(requestRunAs.GetId()) == "" {
		return nil
	}
	switch callCtx.callerProviderKind {
	case invocation.ProviderKindWorkflow:
		return status.Error(codes.PermissionDenied, "workflow callers may not set request run_as")
	case invocation.ProviderKindAgent:
		return status.Error(codes.PermissionDenied, "agent callers may not set request run_as")
	default:
		if callCtx.agent != (invocation.AgentInvocationContext{}) {
			return status.Error(codes.PermissionDenied, "agent callers may not set request run_as")
		}
	}
	return nil
}

func (s *AppServer) requestContext(reqCtx *proto.RequestContext) (requestInvocationContext, error) {
	if reqCtx == nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "request context is required")
	}
	caller := reqCtx.GetCaller()
	callerKind := invocation.ProviderKind(strings.TrimSpace(caller.GetKind()))
	callerName := strings.TrimSpace(caller.GetName())
	if callerName == "" {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "provider caller context is required")
	}
	if callerKind != invocation.ProviderKindApp && callerKind != invocation.ProviderKindWorkflow && callerKind != invocation.ProviderKindAgent {
		return requestInvocationContext{}, status.Errorf(codes.FailedPrecondition, "%s caller context is not supported for app invocation", callerKind)
	}
	agent := agentFromRequestContext(reqCtx)
	if expected := strings.TrimSpace(s.callerName); expected != "" {
		if agent != (invocation.AgentInvocationContext{}) {
			if strings.TrimSpace(agent.ProviderName) != expected {
				return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "agent invocation context provider %q does not match serving provider %q", strings.TrimSpace(agent.ProviderName), expected)
			}
		} else if callerName != expected {
			return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "provider caller context %q does not match serving provider %q", callerName, expected)
		}
	}
	return requestInvocationContext{
		callerName:         callerName,
		callerProviderKind: callerKind,
		principal:          PrincipalFromSubjectContext(reqCtx.GetSubject()),
		credential:         credentialFromRequestContext(reqCtx),
		workflow:           workflowFromRequestContext(reqCtx),
		agent:              agent,
		requestContext:     reqCtx,
	}, nil
}

func (s *AppServer) authorizeAgentAppInvocation(ctx context.Context, callCtx requestInvocationContext, targetApp, targetOperation, rawConnection, rawInstance string, credentialMode core.ConnectionMode) (invocation.AgentAppAuthorization, error) {
	if s == nil || s.agentAppAuth == nil {
		return invocation.AgentAppAuthorization{}, status.Error(codes.FailedPrecondition, "agent app invocation authorizer is required")
	}
	return s.agentAppAuth.AuthorizeAppInvocation(ctx, invocation.AgentAppAuthorizationRequest{
		AgentProviderName: strings.TrimSpace(s.callerName),
		CallerKind:        callCtx.callerProviderKind,
		CallerName:        callCtx.callerName,
		Agent:             callCtx.agent,
		Principal:         callCtx.principal,
		App:               targetApp,
		Operation:         targetOperation,
		Connection:        strings.TrimSpace(rawConnection),
		Instance:          strings.TrimSpace(rawInstance),
		CredentialMode:    credentialMode,
		RequestContext:    callCtx.requestContext,
	})
}

func authorizeWorkflowAppInvocation(callCtx requestInvocationContext, targetApp, targetOperation, rawConnection, rawInstance string, credentialMode core.ConnectionMode) error {
	step, err := WorkflowStepInvocationFromContext(callCtx.workflow)
	if err != nil {
		return err
	}
	if step.ProviderName != callCtx.callerName {
		return status.Errorf(codes.PermissionDenied, "workflow caller %q does not match workflow context provider %q", callCtx.callerName, step.ProviderName)
	}
	if step.Kind != "app" || step.App != targetApp || step.Operation != targetOperation {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke %s.%s", callCtx.callerName, step.ID, targetApp, targetOperation)
	}
	if strings.TrimSpace(rawConnection) != step.Connection {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke %s.%s with connection %q", callCtx.callerName, step.ID, targetApp, targetOperation, strings.TrimSpace(rawConnection))
	}
	if strings.TrimSpace(rawInstance) != step.Instance {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke %s.%s with instance %q", callCtx.callerName, step.ID, targetApp, targetOperation, strings.TrimSpace(rawInstance))
	}
	stepCredentialMode, err := appInvokeCredentialMode(step.CredentialMode)
	if err != nil {
		return err
	}
	if stepCredentialMode != credentialMode {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke %s.%s with credential_mode %q", callCtx.callerName, step.ID, targetApp, targetOperation, credentialMode)
	}
	return nil
}

func appInvokeCredentialMode(raw string) (core.ConnectionMode, error) {
	mode := core.NormalizeOptionalConnectionMode(core.ConnectionMode(raw))
	switch mode {
	case "", core.ConnectionModeNone, core.ConnectionModeSubject:
		return mode, nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "credential_mode %q is not supported", strings.TrimSpace(raw))
	}
}

func prepareInvocationSelectors(ctx context.Context, callCtx requestInvocationContext, rawConnection, rawInstance string) (context.Context, string, error) {
	if callCtx.authorizedSelectors {
		return restoreRequestInvocationContext(ctx, callCtx, callCtx.authorizedConnection), callCtx.authorizedInstance, nil
	}
	connection := strings.TrimSpace(rawConnection)
	instance := strings.TrimSpace(rawInstance)
	if instance == "" && connection == "" {
		instance = callCtx.credential.Instance
	}
	return restoreRequestInvocationContext(ctx, callCtx, connection), instance, nil
}

func restoreRequestInvocationContext(ctx context.Context, callCtx requestInvocationContext, connectionOverride string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if callCtx.principal != nil {
		ctx = principal.WithPrincipal(ctx, principal.Canonicalized(callCtx.principal))
	}
	if callCtx.callerName != "" && callCtx.callerProviderKind != "" {
		ctx = invocation.WithCallerProvider(ctx, callCtx.callerProviderKind, callCtx.callerName)
	}
	if meta := invocationMetaFromRequestContext(callCtx.requestContext); meta != nil {
		ctx = invocation.ContextWithMeta(ctx, meta)
	}
	if reqMeta := requestMetaFromRequestContext(callCtx.requestContext); reqMeta != (invocation.RequestMeta{}) {
		ctx = invocation.WithRequestMeta(ctx, reqMeta)
	}
	if callCtx.credential != (invocation.CredentialContext{}) {
		ctx = invocation.WithCredentialContext(ctx, callCtx.credential)
	}
	if callCtx.credentialModeOverride != "" {
		ctx = invocation.WithCredentialModeOverride(ctx, callCtx.credentialModeOverride)
	}
	if inv := callCtx.requestContext.GetInvocation(); inv != nil {
		if surface := invocation.InvocationSurface(strings.TrimSpace(inv.GetSurface())); surface != "" {
			ctx = invocation.WithInvocationSurface(ctx, surface)
		}
		if inv.GetInternalConnectionAccess() && !callCtx.authorizedSelectors {
			ctx = invocation.WithInternalConnectionAccess(ctx)
		}
	}
	if callCtx.workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, invocation.CloneWorkflowContext(callCtx.workflow))
	}
	if callCtx.agent != (invocation.AgentInvocationContext{}) {
		ctx = invocation.WithAgentInvocationContext(ctx, callCtx.agent)
	}
	ctx = applyInheritedRequestContext(ctx, callCtx.requestContext)
	if callCtx.agentToolRefsSet {
		ctx = invocation.WithToolRefsContext(ctx, callCtx.agentToolRefs)
	}

	connection := strings.TrimSpace(connectionOverride)
	if !callCtx.authorizedSelectors && connection == "" && callCtx.requestContext.GetInvocation() != nil {
		connection = strings.TrimSpace(callCtx.requestContext.GetInvocation().GetConnection())
	}
	if !callCtx.authorizedSelectors && connection == "" {
		connection = strings.TrimSpace(callCtx.credential.Connection)
	}
	if connection != "" {
		ctx = invocation.WithConnection(ctx, connection)
	}
	return ctx
}

func credentialFromRequestContext(reqCtx *proto.RequestContext) invocation.CredentialContext {
	credential := reqCtx.GetCredential()
	if credential == nil {
		return invocation.CredentialContext{}
	}
	return invocation.CredentialContext{
		Mode:       core.ConnectionMode(credential.GetMode()),
		SubjectID:  credential.GetSubjectId(),
		Connection: credential.GetConnection(),
		Instance:   credential.GetInstance(),
	}
}

func workflowFromRequestContext(reqCtx *proto.RequestContext) map[string]any {
	if workflow := reqCtx.GetWorkflow(); workflow != nil {
		return invocation.CloneWorkflowContext(workflow.AsMap())
	}
	return nil
}

func agentFromRequestContext(reqCtx *proto.RequestContext) invocation.AgentInvocationContext {
	if reqCtx == nil || reqCtx.GetAgent() == nil {
		return invocation.AgentInvocationContext{}
	}
	return invocation.AgentInvocationContext{
		ProviderName: strings.TrimSpace(reqCtx.GetAgent().GetProviderName()),
		SessionID:    strings.TrimSpace(reqCtx.GetAgent().GetSessionId()),
		TurnID:       strings.TrimSpace(reqCtx.GetAgent().GetTurnId()),
	}
}

func invocationMetaFromRequestContext(reqCtx *proto.RequestContext) *invocation.InvocationMeta {
	inv := reqCtx.GetInvocation()
	if inv == nil {
		return nil
	}
	return &invocation.InvocationMeta{
		RequestID: strings.TrimSpace(inv.GetRequestId()),
		Depth:     int(inv.GetDepth()),
		CallChain: append([]string(nil), inv.GetCallChain()...),
	}
}

func requestMetaFromRequestContext(reqCtx *proto.RequestContext) invocation.RequestMeta {
	meta := reqCtx.GetRequestMeta()
	if meta == nil {
		return invocation.RequestMeta{}
	}
	return invocation.RequestMeta{
		ClientIP:   meta.GetClientIp(),
		RemoteAddr: meta.GetRemoteAddr(),
		UserAgent:  meta.GetUserAgent(),
	}
}

func applyInheritedRequestContext(ctx context.Context, reqCtx *proto.RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	if agentSubject := reqCtx.GetAgentSubject(); agentSubject != nil {
		ctx = invocation.WithRunAsAudit(ctx, agentwire.RunAsSubjectFromProto(agentSubject), agentwire.RunAsSubjectFromProto(reqCtx.GetSubject()))
	}
	if host := reqCtx.GetHost(); host != nil {
		ctx = invocation.WithHostContext(ctx, invocation.HostContext{
			PublicBaseURL: host.GetPublicBaseUrl(),
		})
	}
	if reqCtx.GetToolRefsSet() {
		ctx = invocation.WithToolRefsContext(ctx, agentwire.ToolRefsFromProto(reqCtx.GetToolRefs()))
	}
	return ctx
}

func appOperationResult(result *core.OperationResult, err error) (*proto.OperationResult, error) {
	if err != nil {
		if statusCode, ok := invocation.OperationErrorResultStatus(err); ok {
			return &proto.OperationResult{
				Status: int32(statusCode),
				Body:   []byte(err.Error()),
			}, nil
		}
		return nil, invocationStatusError(err)
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "plugin invocation returned no result")
	}
	return coreOperationResultToProto(result), nil
}

func coreOperationResultToProto(result *core.OperationResult) *proto.OperationResult {
	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: protoutil.StringSlicesToProto(result.Headers),
		Body:    result.Body,
	}
}

func invocationStatusError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, invocation.ErrInvalidInvocation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrReconnectRequired):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, invocation.ErrAmbiguousInstance):
		return status.Error(codes.Aborted, err.Error())
	default:
		var maxDepthErr *invocation.MaxDepthError
		if errors.As(err, &maxDepthErr) {
			return status.Error(codes.ResourceExhausted, maxDepthErr.Error())
		}
		var rateLimitErr *invocation.RateLimitError
		if errors.As(err, &rateLimitErr) {
			return status.Error(codes.ResourceExhausted, rateLimitErr.Error())
		}
		var recursionErr *invocation.RecursionError
		if errors.As(err, &recursionErr) {
			return status.Error(codes.FailedPrecondition, recursionErr.Error())
		}
		return status.Error(codes.Unknown, fmt.Sprintf("plugin invocation failed: %v", err))
	}
}
