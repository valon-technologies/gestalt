package appaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowrunauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type AppServer struct {
	proto.UnimplementedAppServer

	invoker                     invocation.Invoker
	tokens                      *InvocationTokenManager
	workflowRuns                WorkflowRunResolver
	workflowAppInvocationGrants map[string]InvocationGrants
}

type WorkflowRunResolver = workflowrunauth.Resolver

type AppServerOption func(*AppServer)

func WithWorkflowRunResolver(resolver WorkflowRunResolver) AppServerOption {
	return func(s *AppServer) {
		s.workflowRuns = resolver
	}
}

func WithWorkflowAppInvocationGrants(grants map[string]InvocationGrants) AppServerOption {
	return func(s *AppServer) {
		if len(grants) == 0 {
			s.workflowAppInvocationGrants = nil
			return
		}
		s.workflowAppInvocationGrants = make(map[string]InvocationGrants, len(grants))
		for app, appGrants := range grants {
			app = strings.TrimSpace(app)
			if app == "" {
				continue
			}
			if cloned := cloneInvocationGrants(appGrants); len(cloned) > 0 {
				s.workflowAppInvocationGrants[app] = cloned
			}
		}
	}
}

func NewAppServer(invoker invocation.Invoker, tokens *InvocationTokenManager, opts ...AppServerOption) *AppServer {
	s := &AppServer{
		invoker: invoker,
		tokens:  tokens,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func NewServer(invoker invocation.Invoker, tokens *InvocationTokenManager, opts ...AppServerOption) proto.AppServer {
	return NewAppServer(invoker, tokens, opts...)
}

func (s *AppServer) ExchangeInvocationToken(_ context.Context, req *proto.ExchangeInvocationTokenRequest) (*proto.ExchangeInvocationTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grants, err := decodeAppInvocationGrantProto(req.GetGrants())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
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
	tokenCtx, err := s.tokenContextForInvoke(ctx, req, targetApp, targetOperation)
	if err != nil {
		return nil, err
	}

	invokeCtx, instance, err := prepareInvocationSelectors(ctx, tokenCtx, req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, err
	}
	invokeCtx = invocation.WithIdempotencyKey(invokeCtx, req.GetIdempotencyKey())
	params := map[string]any{}
	if raw := req.GetParams(); raw != nil {
		params = raw.AsMap()
	}
	invokePrincipal := tokenCtx.principal
	invokeCtx, invokePrincipal = invocation.ApplyDelegation(
		invokeCtx,
		invokePrincipal,
		tokenCtx.operationProfile.Delegation.RunAs,
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
	tokenCtx, err := s.tokenContextForSurfaceInvoke(req, targetApp, "graphql")
	if err != nil {
		return nil, err
	}
	graphQLInvoker, ok := s.invoker.(interface {
		InvokeGraphQL(context.Context, *principal.Principal, string, string, invocation.GraphQLRequest) (*core.OperationResult, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "plugin graphql invocation is not available")
	}

	invokeCtx, instance, err := prepareInvocationSelectors(ctx, tokenCtx, req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, err
	}
	invokeCtx = invocation.WithIdempotencyKey(invokeCtx, req.GetIdempotencyKey())

	variables := map[string]any{}
	if raw := req.GetVariables(); raw != nil {
		variables = raw.AsMap()
	}
	result, err := graphQLInvoker.InvokeGraphQL(invokeCtx, tokenCtx.principal, targetApp, instance, invocation.GraphQLRequest{
		Document:  document,
		Variables: variables,
	})
	return appOperationResult(result, err)
}

func (s *AppServer) tokenContextForInvoke(ctx context.Context, req *proto.AppInvokeRequest, targetApp, targetOperation string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return workflowRunAsInvocationContext(ctx, s.workflowRuns, req.GetWorkflow(), targetApp, targetOperation, req.GetCredentialMode(), s.workflowAppInvocationGrants)
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, "")
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if workflow := req.GetWorkflow(); workflow != nil {
		tokenCtx.workflow = invocation.CloneWorkflowContext(workflow.AsMap())
	}
	callerApp := strings.TrimSpace(tokenCtx.callerApp)
	profile, ok := EffectiveOperationProfile(tokenCtx.grants, targetApp, targetOperation)
	if !ok {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", callerApp, targetApp, targetOperation)
	}
	credentialMode, err := appInvokeCredentialMode(req.GetCredentialMode())
	if err != nil {
		return invocationTokenContext{}, err
	}
	if credentialMode != "" {
		if !appInvokeCredentialModeAllowed(credentialMode, profile) {
			return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s with credential_mode %q", callerApp, targetApp, targetOperation, credentialMode)
		}
		tokenCtx.credentialModeOverride = credentialMode
		tokenCtx.operationProfile = profile
		return tokenCtx, nil
	}
	tokenCtx.credentialModeOverride = profile.CredentialMode
	tokenCtx.operationProfile = profile
	return tokenCtx, nil
}

func WorkflowRunAsTokenContext(ctx context.Context, resolver WorkflowRunResolver, workflow *structpb.Struct) (TokenContext, error) {
	tokenCtx, err := workflowRunAsInvocationContext(ctx, resolver, workflow, "", "", "", nil)
	if err != nil {
		return TokenContext{}, err
	}
	return TokenContext{inner: tokenCtx}, nil
}

func workflowRunAsInvocationContext(ctx context.Context, resolver WorkflowRunResolver, workflow *structpb.Struct, targetApp, targetOperation, rawCredentialMode string, appInvocationGrants map[string]InvocationGrants) (invocationTokenContext, error) {
	resolved, err := workflowrunauth.ResolveInvocationFromWorkflowRun(ctx, resolver, workflow)
	if err != nil {
		return invocationTokenContext{}, err
	}
	grants := invocationGrantsFromWorkflowTargetAuth(resolved.Auth)
	addWorkflowStepAppInvocationGrants(grants, resolved.Auth, appInvocationGrants)
	permissions := principal.ClonePermissionSet(resolved.Auth.Permissions)
	permissions = addWorkflowStepAppInvocationPermissions(permissions, resolved.Auth, appInvocationGrants)
	p := principal.Canonicalize(&principal.Principal{
		SubjectID:           resolved.RunAs.SubjectID,
		CredentialSubjectID: resolved.RunAs.CredentialSubjectID,
		DisplayName:         resolved.RunAs.DisplayName,
		Kind:                principal.Kind(resolved.RunAs.SubjectKind),
		TokenPermissions:    permissions,
	})
	principal.SetAuthSource(p, resolved.RunAs.AuthSource)

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
		if !appInvokeCredentialModeAllowed(credentialMode, profile) {
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

func addWorkflowStepAppInvocationGrants(grants InvocationGrants, auth workflowrunauth.TargetAuth, appInvocationGrants map[string]InvocationGrants) {
	if len(grants) == 0 || len(auth.StepApps) == 0 || len(appInvocationGrants) == 0 {
		return
	}
	for appName := range auth.StepApps {
		mergeWorkflowInvocationGrants(grants, appInvocationGrants[appName])
	}
}

func mergeWorkflowInvocationGrants(dst, src InvocationGrants) {
	for appName, grant := range src {
		appName = strings.TrimSpace(appName)
		if appName == "" || invocationGrantEmpty(grant) {
			continue
		}
		dst[appName] = mergeInvocationGrant(dst[appName], grant)
	}
}

func addWorkflowStepAppInvocationPermissions(perms principal.PermissionSet, auth workflowrunauth.TargetAuth, appInvocationGrants map[string]InvocationGrants) principal.PermissionSet {
	if len(auth.StepApps) == 0 || len(appInvocationGrants) == 0 {
		return perms
	}
	if perms == nil {
		perms = principal.PermissionSet{}
	}
	for appName := range auth.StepApps {
		addInvocationGrantPermissions(perms, appInvocationGrants[appName])
	}
	if len(perms) == 0 {
		return nil
	}
	return perms
}

func addInvocationGrantPermissions(perms principal.PermissionSet, grants InvocationGrants) {
	for appName, grant := range grants {
		appName = strings.TrimSpace(appName)
		if appName == "" || invocationGrantEmpty(grant) {
			continue
		}
		if grant.AllOperations {
			perms[appName] = nil
			continue
		}
		ops, ok := perms[appName]
		if ok && ops == nil {
			continue
		}
		if ops == nil {
			ops = map[string]struct{}{}
		}
		for operation := range grant.Operations {
			if operation = strings.TrimSpace(operation); operation != "" {
				ops[operation] = struct{}{}
			}
		}
		for surface := range grant.Surfaces {
			if surface = strings.ToLower(strings.TrimSpace(surface)); surface != "" {
				ops[surface] = struct{}{}
			}
		}
		if len(ops) > 0 {
			perms[appName] = ops
		}
	}
}

func invocationGrantsFromWorkflowTargetAuth(auth workflowrunauth.TargetAuth) InvocationGrants {
	grants := InvocationGrants{}
	for appName, operations := range auth.Operations {
		for operation, credentialMode := range operations {
			addWorkflowOperationGrant(grants, appName, operation, credentialMode)
		}
	}
	return grants
}

func addWorkflowOperationGrant(grants InvocationGrants, appName, operation string, credentialMode core.ConnectionMode) {
	appName = strings.TrimSpace(appName)
	operation = strings.TrimSpace(operation)
	if appName == "" || operation == "" {
		return
	}
	grant := grants[appName]
	if grant.Operations == nil {
		grant.Operations = map[string]core.ConnectionMode{}
	}
	grant.Operations[operation] = core.NormalizeOptionalConnectionMode(credentialMode)
	grants[appName] = grant
}

func (s *AppServer) tokenContextForSurfaceInvoke(req *proto.AppInvokeGraphQLRequest, targetApp, surface string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, "")
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	callerApp := strings.TrimSpace(tokenCtx.callerApp)
	if !allowsSurface(tokenCtx.grants, targetApp, surface) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s surface %q", callerApp, targetApp, surface)
	}
	// Surface invocations do not carry operation delegation metadata. Config
	// validation rejects runAs on surfaces so GraphQL cannot silently ignore it.
	return tokenCtx, nil
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

func appInvokeCredentialModeAllowed(mode core.ConnectionMode, profile OperationProfile) bool {
	mode = core.NormalizeOptionalConnectionMode(mode)
	if mode == "" {
		return true
	}
	if profile.CredentialMode != "" {
		return profile.CredentialMode == mode
	}
	return true
}

func prepareInvocationSelectors(ctx context.Context, tokenCtx invocationTokenContext, rawConnection, rawInstance string) (context.Context, string, error) {
	connection := strings.TrimSpace(rawConnection)
	instance := strings.TrimSpace(rawInstance)
	if instance == "" && connection == "" {
		instance = tokenCtx.credential.Instance
	}
	return restoreInvocationTokenContext(ctx, tokenCtx, connection), instance, nil
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
