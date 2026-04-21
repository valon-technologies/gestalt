package providerhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DefaultPluginInvokerSocketEnv = proto.EnvPluginInvokerSocket

type PluginInvokerServer struct {
	proto.UnimplementedPluginInvokerServer

	pluginName string
	invoker    invocation.Invoker
	tokens     *InvocationTokenManager
	allowed    map[string]map[string]struct{}
}

func NewPluginInvokerServer(pluginName string, deps []config.PluginInvocationDependency, invoker invocation.Invoker, tokens *InvocationTokenManager) *PluginInvokerServer {
	return &PluginInvokerServer{
		pluginName: pluginName,
		invoker:    invoker,
		tokens:     tokens,
		allowed:    InvocationDependencyGrants(deps),
	}
}

func (s *PluginInvokerServer) ExchangeInvocationToken(_ context.Context, req *proto.ExchangeInvocationTokenRequest) (*proto.ExchangeInvocationTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grants := decodePluginInvocationGrantProto(req.GetGrants())
	if len(grants) > 0 && !operationMapSubset(grants, s.allowed) {
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

	connection := strings.TrimSpace(req.GetConnection())
	invokeCtx := restoreInvocationTokenContext(ctx, tokenCtx, connection)
	params := map[string]any{}
	if raw := req.GetParams(); raw != nil {
		params = raw.AsMap()
	}

	instance := strings.TrimSpace(req.GetInstance())
	if instance == "" && connection == "" && shouldInheritCredentialSelectors(tokenCtx.principal) {
		instance = tokenCtx.credential.Instance
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

func (s *PluginInvokerServer) allows(plugin, operation string) bool {
	if s == nil {
		return false
	}
	ops, ok := s.allowed[plugin]
	if !ok {
		return false
	}
	_, ok = ops[operation]
	return ok
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
	return tokenCtx, nil
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
	case errors.Is(err, invocation.ErrNoToken):
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
