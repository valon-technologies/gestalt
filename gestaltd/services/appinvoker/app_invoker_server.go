package appinvoker

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

type AppInvokerServer struct {
	proto.UnimplementedAppInvokerServer

	pluginName string
	invoker    invocation.Invoker
	tokens     *InvocationTokenManager
	allowed    InvocationGrants
}

func NewAppInvokerServer(pluginName string, deps []invocation.AppInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) *AppInvokerServer {
	return &AppInvokerServer{
		pluginName: pluginName,
		invoker:    invoker,
		tokens:     tokens,
		allowed:    InvocationDependencyGrants(deps),
	}
}

func NewServer(pluginName string, deps []invocation.AppInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) proto.AppInvokerServer {
	return NewAppInvokerServer(pluginName, deps, invoker, tokens)
}

func (s *AppInvokerServer) ExchangeInvocationToken(_ context.Context, req *proto.ExchangeInvocationTokenRequest) (*proto.ExchangeInvocationTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grants, err := decodeAppInvocationGrantProto(req.GetGrants())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if len(grants) > 0 && !invocationGrantSubset(grants, s.allowed) {
		return nil, status.Error(codes.PermissionDenied, "requested invocation grants exceed the plugin's declared invokes")
	}
	exchangedToken, err := s.tokens.ExchangeToken(
		req.GetParentInvocationToken(),
		s.pluginName,
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

func (s *AppInvokerServer) Invoke(ctx context.Context, req *proto.AppInvokeRequest) (*proto.OperationResult, error) {
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
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *AppInvokerServer) InvokeGraphQL(ctx context.Context, req *proto.AppInvokeGraphQLRequest) (*proto.OperationResult, error) {
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
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *AppInvokerServer) allows(plugin, operation string) bool {
	if s == nil {
		return false
	}
	return allowsOperation(s.allowed, plugin, operation)
}

func (s *AppInvokerServer) allowsSurface(plugin, surface string) bool {
	if s == nil {
		return false
	}
	return allowsSurface(s.allowed, plugin, surface)
}

func (s *AppInvokerServer) tokenContextForInvoke(req *proto.AppInvokeRequest, targetApp, targetOperation string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, s.pluginName)
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if !allowsOperation(tokenCtx.grants, targetApp, targetOperation) || !s.allows(targetApp, targetOperation) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", s.pluginName, targetApp, targetOperation)
	}
	tokenCtx.credentialModeOverride = operationCredentialMode(tokenCtx.grants, targetApp, targetOperation)
	if tokenCtx.credentialModeOverride == "" {
		tokenCtx.credentialModeOverride = operationCredentialMode(s.allowed, targetApp, targetOperation)
	}
	return tokenCtx, nil
}

func (s *AppInvokerServer) tokenContextForSurfaceInvoke(req *proto.AppInvokeGraphQLRequest, targetApp, surface string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, s.pluginName)
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if !allowsSurface(tokenCtx.grants, targetApp, surface) || !s.allowsSurface(targetApp, surface) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s surface %q", s.pluginName, targetApp, surface)
	}
	return tokenCtx, nil
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
