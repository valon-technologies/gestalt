package appaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AppServer struct {
	proto.UnimplementedAppServer

	invoker invocation.Invoker
	tokens  *InvocationTokenManager
}

func NewAppServer(invoker invocation.Invoker, tokens *InvocationTokenManager) *AppServer {
	return &AppServer{
		invoker: invoker,
		tokens:  tokens,
	}
}

func NewServer(invoker invocation.Invoker, tokens *InvocationTokenManager) proto.AppServer {
	return NewAppServer(invoker, tokens)
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
	tokenCtx, err := s.tokenContextForInvoke(req, targetApp, targetOperation)
	if err != nil {
		return nil, err
	}

	invokeCtx, instance, err := prepareInvocationSelectors(ctx, tokenCtx, req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, err
	}
	if workflow := req.GetWorkflow(); workflow != nil {
		invokeCtx = invocation.WithWorkflowContext(invokeCtx, invocation.CloneWorkflowContext(workflow.AsMap()))
	}
	invokeCtx = invocation.WithIdempotencyKey(invokeCtx, req.GetIdempotencyKey())
	params := map[string]any{}
	if raw := req.GetParams(); raw != nil {
		params = raw.AsMap()
	}

	result, err := s.invoker.Invoke(invokeCtx, tokenCtx.principal, targetApp, instance, targetOperation, params)
	if err != nil {
		return nil, invocationStatusError(err)
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "plugin invocation returned no result")
	}

	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: mapStringSlices(result.Headers),
		Body:    result.Body,
	}, nil
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
	if err != nil {
		return nil, invocationStatusError(err)
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "plugin invocation returned no result")
	}

	return &proto.OperationResult{
		Status:  int32(result.Status),
		Headers: mapStringSlices(result.Headers),
		Body:    result.Body,
	}, nil
}

func mapStringSlices[V ~map[string][]string](values V) map[string]*proto.StringList {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*proto.StringList, len(values))
	for key, item := range values {
		copied := make([]string, len(item))
		copy(copied, item)
		out[key] = &proto.StringList{Values: copied}
	}
	return out
}

func (s *AppServer) tokenContextForInvoke(req *proto.AppInvokeRequest, targetApp, targetOperation string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, "")
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	callerApp := strings.TrimSpace(tokenCtx.callerApp)
	if !allowsOperation(tokenCtx.grants, targetApp, targetOperation) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", callerApp, targetApp, targetOperation)
	}
	credentialMode, err := appInvokeCredentialMode(req.GetCredentialMode())
	if err != nil {
		return invocationTokenContext{}, err
	}
	if credentialMode != "" {
		if !appInvokeCredentialModeAllowed(credentialMode, tokenCtx.grants, targetApp, targetOperation) {
			return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s with credential_mode %q", callerApp, targetApp, targetOperation, credentialMode)
		}
		tokenCtx.credentialModeOverride = credentialMode
		return tokenCtx, nil
	}
	tokenCtx.credentialModeOverride = appInvokeCredentialModeOverride(tokenCtx.grants, targetApp, targetOperation)
	return tokenCtx, nil
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
	return tokenCtx, nil
}

func appInvokeCredentialMode(raw string) (core.ConnectionMode, error) {
	mode := core.NormalizeOptionalConnectionMode(core.ConnectionMode(raw))
	switch mode {
	case "", core.ConnectionModeNone, core.ConnectionModeUser:
		return mode, nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "credential_mode %q is not supported", strings.TrimSpace(raw))
	}
}

func appInvokeCredentialModeAllowed(mode core.ConnectionMode, tokenGrants InvocationGrants, appName, operation string) bool {
	mode = core.NormalizeOptionalConnectionMode(mode)
	if mode == "" {
		return true
	}
	if tokenMode := operationCredentialMode(tokenGrants, appName, operation); tokenMode != "" {
		return tokenMode == mode
	}
	return allowsOperation(tokenGrants, appName, operation)
}

func appInvokeCredentialModeOverride(tokenGrants InvocationGrants, appName, operation string) core.ConnectionMode {
	return operationCredentialMode(tokenGrants, appName, operation)
}

func prepareInvocationSelectors(ctx context.Context, tokenCtx invocationTokenContext, rawConnection, rawInstance string) (context.Context, string, error) {
	connection := strings.TrimSpace(rawConnection)
	instance := strings.TrimSpace(rawInstance)
	if instance == "" && connection == "" {
		instance = tokenCtx.credential.Instance
	}
	return restoreInvocationTokenContext(ctx, tokenCtx, connection), instance, nil
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
	case errors.Is(err, invocation.ErrNoCredential):
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
