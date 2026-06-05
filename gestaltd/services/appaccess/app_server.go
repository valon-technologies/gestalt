package appaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowrunauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	appInvocationAuthorizationSubjectTypeApp        = "app"
	appInvocationAuthorizationResourceTypeOperation = "gestalt.app.operation"
	appInvocationAuthorizationResourceTypeSurface   = "gestalt.app.surface"
	appInvocationAuthorizationActionInvoke          = "invoke"
)

type AppServer struct {
	proto.UnimplementedAppServer

	invoker        invocation.Invoker
	tokens         *InvocationTokenManager
	workflowRuns   WorkflowRunResolver
	authorization  core.AuthorizationProvider
	callerApp      string
	accessProfiles AppAccessProfiles
}

type WorkflowRunResolver = workflowrunauth.Resolver

type AppServerOption func(*AppServer)

func WithInvocationTokenManager(tokens *InvocationTokenManager) AppServerOption {
	return func(s *AppServer) {
		s.tokens = tokens
	}
}

func WithWorkflowRunResolver(resolver WorkflowRunResolver) AppServerOption {
	return func(s *AppServer) {
		s.workflowRuns = resolver
	}
}

func WithAuthorizationProvider(provider core.AuthorizationProvider) AppServerOption {
	return func(s *AppServer) {
		s.authorization = provider
	}
}

func WithCallerApp(callerApp string, profiles AppAccessProfiles) AppServerOption {
	return func(s *AppServer) {
		s.callerApp = strings.TrimSpace(callerApp)
		s.accessProfiles = CloneAppAccessProfiles(profiles)
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

func (s *AppServer) ExchangeInvocationToken(_ context.Context, req *proto.ExchangeInvocationTokenRequest) (*proto.ExchangeInvocationTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grants, err := decodeAppInvocationGrantProto(req.GetGrants())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if s == nil || s.tokens == nil {
		return nil, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	exchangedToken, err := s.tokens.ExchangeToken(
		req.GetParentInvocationToken(),
		grants,
		time.Duration(req.GetTtlSeconds())*time.Second,
	)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	return &proto.ExchangeInvocationTokenResponse{
		InvocationToken: exchangedToken,
	}, nil
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
	callCtx, err := s.requestContextForInvoke(ctx, req, targetApp, targetOperation)
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
		callCtx.operationProfile.Delegation.RunAs,
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
	callCtx, err := s.requestContextForSurfaceInvoke(ctx, req, targetApp, "graphql")
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
	callerApp              string
	callerProviderKind     invocation.ProviderKind
	principal              *principal.Principal
	credential             invocation.CredentialContext
	credentialModeOverride core.ConnectionMode
	operationProfile       AppOperationProfile
	workflow               map[string]any
	requestContext         *proto.RequestContext
	legacyTokenContext     *invocationTokenContext
}

func (s *AppServer) requestContextForInvoke(ctx context.Context, req *proto.AppInvokeRequest, targetApp, targetOperation string) (requestInvocationContext, error) {
	reqCtx := req.GetContext()
	if reqCtx == nil {
		return s.tokenContextForInvoke(ctx, req, targetApp, targetOperation, req.GetCredentialMode())
	}
	if strings.TrimSpace(req.GetInvocationToken()) != "" {
		callCtx, err := s.tokenContextForInvoke(ctx, req, targetApp, targetOperation, req.GetCredentialMode())
		if err != nil {
			return requestInvocationContext{}, err
		}
		if parsedCtx, err := s.requestContext(reqCtx); err == nil {
			callCtx.requestContext = parsedCtx.requestContext
		}
		return callCtx, nil
	}
	callCtx, err := s.requestContext(reqCtx)
	if err != nil {
		return requestInvocationContext{}, err
	}
	if err := s.authorizeAppInvocation(ctx, callCtx, appInvocationAuthorizationResourceTypeOperation, appInvocationOperationResourceID(targetApp, targetOperation), targetApp, targetOperation); err != nil {
		return requestInvocationContext{}, err
	}
	profile, profileOK := EffectiveAppOperationProfile(s.accessProfiles, targetApp, targetOperation)
	credentialMode, err := appInvokeCredentialMode(req.GetCredentialMode())
	if err != nil {
		return requestInvocationContext{}, err
	}
	if credentialMode != "" {
		if !profileOK {
			return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s with credential_mode %q", callCtx.callerApp, targetApp, targetOperation, credentialMode)
		}
		if profileOK && !appInvokeCredentialModeAllowed(credentialMode, profile) {
			return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s with credential_mode %q", callCtx.callerApp, targetApp, targetOperation, credentialMode)
		}
		callCtx.credentialModeOverride = credentialMode
		callCtx.operationProfile = profile
		return callCtx, nil
	}
	if profileOK {
		callCtx.credentialModeOverride = profile.CredentialMode
		callCtx.operationProfile = profile
	}
	return callCtx, nil
}

func (s *AppServer) requestContextForSurfaceInvoke(ctx context.Context, req *proto.AppInvokeGraphQLRequest, targetApp, surface string) (requestInvocationContext, error) {
	reqCtx := req.GetContext()
	if reqCtx == nil {
		return s.tokenContextForSurfaceInvoke(req, targetApp, surface)
	}
	if strings.TrimSpace(req.GetInvocationToken()) != "" {
		callCtx, err := s.tokenContextForSurfaceInvoke(req, targetApp, surface)
		if err != nil {
			return requestInvocationContext{}, err
		}
		if parsedCtx, err := s.requestContext(reqCtx); err == nil {
			callCtx.requestContext = parsedCtx.requestContext
		}
		return callCtx, nil
	}
	callCtx, err := s.requestContext(reqCtx)
	if err != nil {
		return requestInvocationContext{}, err
	}
	if err := s.authorizeAppInvocation(ctx, callCtx, appInvocationAuthorizationResourceTypeSurface, appInvocationSurfaceResourceID(targetApp, surface), targetApp, surface); err != nil {
		return requestInvocationContext{}, err
	}
	if profile, ok := EffectiveAppSurfaceProfile(s.accessProfiles, targetApp, surface); ok {
		callCtx.credentialModeOverride = profile.CredentialMode
	}
	// Surface invocations do not carry delegation metadata. Config validation
	// rejects runAs on surfaces so GraphQL cannot silently ignore it.
	return callCtx, nil
}

func (s *AppServer) tokenContextForSurfaceInvoke(req *proto.AppInvokeGraphQLRequest, targetApp, surface string) (requestInvocationContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	if s == nil || s.tokens == nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, "")
	if err != nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	callerApp := strings.TrimSpace(tokenCtx.callerApp)
	if !allowsSurface(tokenCtx.grants, targetApp, surface) {
		return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s surface %q", callerApp, targetApp, surface)
	}
	return requestContextFromLegacyToken(tokenCtx), nil
}

func (s *AppServer) tokenContextForInvoke(ctx context.Context, req *proto.AppInvokeRequest, targetApp, targetOperation, rawCredentialMode string) (requestInvocationContext, error) {
	invocationToken := ""
	var workflow *structpb.Struct
	if req != nil {
		invocationToken = strings.TrimSpace(req.GetInvocationToken())
		workflow = req.GetWorkflow()
	}
	if invocationToken == "" {
		tokenCtx, err := workflowRunAsInvocationContext(ctx, s.workflowRuns, workflow, targetApp, targetOperation, rawCredentialMode)
		if err != nil {
			return requestInvocationContext{}, err
		}
		return requestContextFromLegacyToken(tokenCtx), nil
	}
	if s == nil || s.tokens == nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, "")
	if err != nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if workflow != nil {
		tokenCtx.workflow = invocation.CloneWorkflowContext(workflow.AsMap())
	}
	callerApp := strings.TrimSpace(tokenCtx.callerApp)
	profile, ok := EffectiveOperationProfile(tokenCtx.grants, targetApp, targetOperation)
	if !ok {
		return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", callerApp, targetApp, targetOperation)
	}
	credentialMode, err := appInvokeCredentialMode(rawCredentialMode)
	if err != nil {
		return requestInvocationContext{}, err
	}
	if credentialMode != "" {
		if !appInvokeLegacyCredentialModeAllowed(credentialMode, profile) {
			return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s with credential_mode %q", callerApp, targetApp, targetOperation, credentialMode)
		}
		tokenCtx.credentialModeOverride = credentialMode
		tokenCtx.operationProfile = profile
		return requestContextFromLegacyToken(tokenCtx), nil
	}
	tokenCtx.credentialModeOverride = profile.CredentialMode
	tokenCtx.operationProfile = profile
	return requestContextFromLegacyToken(tokenCtx), nil
}

func WorkflowRunAsTokenContext(ctx context.Context, resolver WorkflowRunResolver, workflow *structpb.Struct) (TokenContext, error) {
	tokenCtx, err := workflowRunAsInvocationContext(ctx, resolver, workflow, "", "", "")
	if err != nil {
		return TokenContext{}, err
	}
	return TokenContext{inner: tokenCtx}, nil
}

func workflowRunAsInvocationContext(ctx context.Context, resolver WorkflowRunResolver, workflow *structpb.Struct, targetApp, targetOperation, rawCredentialMode string) (invocationTokenContext, error) {
	resolved, err := workflowrunauth.ResolveInvocationFromWorkflowRun(ctx, resolver, workflow)
	if err != nil {
		return invocationTokenContext{}, err
	}
	grants := AllInvocationGrants()
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           resolved.RunAs.SubjectID,
		CredentialSubjectID: resolved.RunAs.CredentialSubjectID,
	})

	tokenCtx := invocationTokenContext{
		callerApp:          "workflow:" + strings.TrimSpace(resolved.ProviderName),
		callerProviderKind: invocation.ProviderKindWorkflow,
		principal:          p,
		grants:             grants,
		workflow:           resolved.Workflow,
	}
	if targetApp == "" && targetOperation == "" {
		return tokenCtx, nil
	}
	profile, ok := EffectiveOperationProfile(grants, targetApp, targetOperation)
	if !ok {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "workflow run %q may not invoke %s.%s", resolved.RunID, targetApp, targetOperation)
	}
	credentialMode, err := appInvokeCredentialMode(rawCredentialMode)
	if err != nil {
		return invocationTokenContext{}, err
	}
	if credentialMode != "" {
		if !appInvokeLegacyCredentialModeAllowed(credentialMode, profile) {
			return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "workflow run %q may not invoke %s.%s with credential_mode %q", resolved.RunID, targetApp, targetOperation, credentialMode)
		}
		tokenCtx.credentialModeOverride = credentialMode
		tokenCtx.operationProfile = profile
		return tokenCtx, nil
	}
	tokenCtx.credentialModeOverride = profile.CredentialMode
	tokenCtx.operationProfile = profile
	return tokenCtx, nil
}

func requestContextFromLegacyToken(tokenCtx invocationTokenContext) requestInvocationContext {
	return requestInvocationContext{
		callerApp:              tokenCtx.callerApp,
		callerProviderKind:     tokenCtx.callerProviderKind,
		principal:              tokenCtx.principal,
		credential:             tokenCtx.credential,
		credentialModeOverride: tokenCtx.credentialModeOverride,
		operationProfile: AppOperationProfile{
			CredentialMode: tokenCtx.operationProfile.CredentialMode,
			Delegation: AppAccessDelegation{
				RunAs: tokenCtx.operationProfile.Delegation.RunAs,
			},
		},
		workflow:           tokenCtx.workflow,
		legacyTokenContext: &tokenCtx,
	}
}

func (s *AppServer) requestContext(reqCtx *proto.RequestContext) (requestInvocationContext, error) {
	if reqCtx == nil {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "request context is required")
	}
	caller := reqCtx.GetCaller()
	callerKind := invocation.ProviderKind(strings.TrimSpace(caller.GetKind()))
	callerApp := strings.TrimSpace(caller.GetName())
	if callerKind != invocation.ProviderKindApp || callerApp == "" {
		return requestInvocationContext{}, status.Error(codes.FailedPrecondition, "app caller context is required")
	}
	if expected := strings.TrimSpace(s.callerApp); expected != "" && callerApp != expected {
		return requestInvocationContext{}, status.Errorf(codes.PermissionDenied, "app caller context %q does not match host service caller %q", callerApp, expected)
	}
	return requestInvocationContext{
		callerApp:          callerApp,
		callerProviderKind: callerKind,
		principal:          PrincipalFromSubjectContext(reqCtx.GetSubject()),
		credential:         credentialFromRequestContext(reqCtx),
		workflow:           workflowFromRequestContext(reqCtx),
		requestContext:     reqCtx,
	}, nil
}

func (s *AppServer) authorizeAppInvocation(ctx context.Context, callCtx requestInvocationContext, resourceType, resourceID, targetApp, target string) error {
	if s == nil || s.authorization == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is required for app invocation")
	}
	resp, err := s.authorization.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject: &proto.Subject{
			Type: appInvocationAuthorizationSubjectTypeApp,
			Id:   callCtx.callerApp,
		},
		Action: &proto.Action{Name: appInvocationAuthorizationActionInvoke},
		Resource: &proto.Resource{
			Type: resourceType,
			Id:   resourceID,
		},
	})
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s: %v", callCtx.callerApp, targetApp, target, err)
	}
	if resp == nil || !resp.GetAllowed() {
		return status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", callCtx.callerApp, targetApp, target)
	}
	return nil
}

func appInvocationOperationResourceID(appName, operation string) string {
	return strings.TrimSpace(appName) + "/operations/" + strings.TrimSpace(operation)
}

func appInvocationSurfaceResourceID(appName, surface string) string {
	return strings.TrimSpace(appName) + "/surfaces/" + strings.ToLower(strings.TrimSpace(surface))
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

func appInvokeCredentialModeAllowed(mode core.ConnectionMode, profile AppOperationProfile) bool {
	mode = core.NormalizeOptionalConnectionMode(mode)
	if mode == "" {
		return true
	}
	if profile.CredentialMode != "" {
		return profile.CredentialMode == mode
	}
	return true
}

func appInvokeLegacyCredentialModeAllowed(mode core.ConnectionMode, profile OperationProfile) bool {
	mode = core.NormalizeOptionalConnectionMode(mode)
	if mode == "" {
		return true
	}
	if profile.CredentialMode != "" {
		return profile.CredentialMode == mode
	}
	return true
}

func prepareInvocationSelectors(ctx context.Context, callCtx requestInvocationContext, rawConnection, rawInstance string) (context.Context, string, error) {
	connection := strings.TrimSpace(rawConnection)
	instance := strings.TrimSpace(rawInstance)
	if instance == "" && connection == "" {
		instance = callCtx.credential.Instance
	}
	return restoreRequestInvocationContext(ctx, callCtx, connection), instance, nil
}

func restoreRequestInvocationContext(ctx context.Context, callCtx requestInvocationContext, connectionOverride string) context.Context {
	if callCtx.legacyTokenContext != nil {
		if callCtx.requestContext != nil {
			ctx = restoreRequestContextInvocationContext(ctx, callCtx, "")
		}
		return restoreInvocationTokenContext(ctx, *callCtx.legacyTokenContext, connectionOverride)
	}
	return restoreRequestContextInvocationContext(ctx, callCtx, connectionOverride)
}

func restoreRequestContextInvocationContext(ctx context.Context, callCtx requestInvocationContext, connectionOverride string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if callCtx.principal != nil {
		ctx = principal.WithPrincipal(ctx, principal.Canonicalized(callCtx.principal))
	}
	if callCtx.callerApp != "" && callCtx.callerProviderKind != "" {
		ctx = invocation.WithCallerProvider(ctx, callCtx.callerProviderKind, callCtx.callerApp)
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
		if inv.GetInternalConnectionAccess() {
			ctx = invocation.WithInternalConnectionAccess(ctx)
		}
	}
	if callCtx.workflow != nil {
		ctx = invocation.WithWorkflowContext(ctx, invocation.CloneWorkflowContext(callCtx.workflow))
	}
	ctx = applyInheritedRequestContext(ctx, callCtx.requestContext)

	connection := strings.TrimSpace(connectionOverride)
	if connection == "" && callCtx.requestContext.GetInvocation() != nil {
		connection = strings.TrimSpace(callCtx.requestContext.GetInvocation().GetConnection())
	}
	if connection == "" {
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
		// Some target invocation errors are operation-level failures so SDK callers can
		// inspect status/body; transport, authz, and malformed invoke errors remain RPC errors.
		if statusCode, ok := invocation.OperationErrorResultStatus(err); ok {
			return &proto.OperationResult{
				Status: int32(statusCode),
				Body:   err.Error(),
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
