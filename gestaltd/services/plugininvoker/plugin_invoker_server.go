package plugininvoker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PluginInvokerServer struct {
	proto.UnimplementedPluginInvokerServer

	pluginName string
	invoker    invocation.Invoker
	tokens     *InvocationTokenManager
	allowed    InvocationGrants
}

func NewPluginInvokerServer(pluginName string, deps []invocation.PluginInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) *PluginInvokerServer {
	return &PluginInvokerServer{
		pluginName: pluginName,
		invoker:    invoker,
		tokens:     tokens,
		allowed:    InvocationDependencyGrants(deps),
	}
}

func NewServer(pluginName string, deps []invocation.PluginInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) proto.PluginInvokerServer {
	return NewPluginInvokerServer(pluginName, deps, invoker, tokens)
}

func (s *PluginInvokerServer) ExchangeInvocationToken(_ context.Context, req *proto.ExchangeInvocationTokenRequest) (*proto.ExchangeInvocationTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grants, err := decodePluginInvocationGrantProto(req.GetGrants())
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

func (s *PluginInvokerServer) Invoke(ctx context.Context, req *proto.PluginInvokeRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	targetPlugin := strings.TrimSpace(req.GetPlugin())
	targetOperation := strings.TrimSpace(req.GetOperation())
	if targetPlugin == "" {
		return nil, status.Error(codes.InvalidArgument, "plugin is required")
	}
	if targetOperation == "" {
		return nil, status.Error(codes.InvalidArgument, "operation is required")
	}
	tokenCtx, err := s.tokenContextForInvoke(req, targetPlugin, targetOperation)
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

	result, err := s.invoker.Invoke(invokeCtx, tokenCtx.principal, targetPlugin, instance, targetOperation, params)
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

func (s *PluginInvokerServer) InvokeGraphQL(ctx context.Context, req *proto.PluginInvokeGraphQLRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	targetPlugin := strings.TrimSpace(req.GetPlugin())
	if targetPlugin == "" {
		return nil, status.Error(codes.InvalidArgument, "plugin is required")
	}
	document := strings.TrimSpace(req.GetDocument())
	if document == "" {
		return nil, status.Error(codes.InvalidArgument, "document is required")
	}
	operation := strings.TrimSpace(req.GetOperation())
	tokenCtx, err := s.tokenContextForSurfaceInvoke(req, targetPlugin, "graphql")
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
	result, err := graphQLInvoker.InvokeGraphQL(invokeCtx, tokenCtx.principal, targetPlugin, instance, invocation.GraphQLRequest{
		Operation: operation,
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

func (s *PluginInvokerServer) allows(plugin, operation string) bool {
	if s == nil {
		return false
	}
	return allowsOperation(s.allowed, plugin, operation)
}

func (s *PluginInvokerServer) allowsSurface(plugin, surface string) bool {
	if s == nil {
		return false
	}
	return allowsSurface(s.allowed, plugin, surface)
}

func (s *PluginInvokerServer) tokenContextForInvoke(req *proto.PluginInvokeRequest, targetPlugin, targetOperation string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, s.pluginName)
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if !allowsOperation(tokenCtx.grants, targetPlugin, targetOperation) || !s.allows(targetPlugin, targetOperation) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", s.pluginName, targetPlugin, targetOperation)
	}
	tokenCtx.credentialModeOverride = operationCredentialMode(tokenCtx.grants, targetPlugin, targetOperation)
	if tokenCtx.credentialModeOverride == "" {
		tokenCtx.credentialModeOverride = operationCredentialMode(s.allowed, targetPlugin, targetOperation)
	}
	return tokenCtx, nil
}

func (s *PluginInvokerServer) tokenContextForSurfaceInvoke(req *proto.PluginInvokeGraphQLRequest, targetPlugin, surface string) (invocationTokenContext, error) {
	invocationToken := strings.TrimSpace(req.GetInvocationToken())
	if invocationToken == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(invocationToken, s.pluginName)
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	if !allowsSurface(tokenCtx.grants, targetPlugin, surface) || !s.allowsSurface(targetPlugin, surface) {
		return invocationTokenContext{}, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s surface %q", s.pluginName, targetPlugin, surface)
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
