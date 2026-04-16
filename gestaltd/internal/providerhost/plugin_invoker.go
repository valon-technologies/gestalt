package providerhost

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	snapshots  *RequestSnapshotStore
	allowed    map[string]map[string]struct{}
}

func NewPluginInvokerServer(pluginName string, deps []config.PluginInvocationDependency, invoker invocation.Invoker, snapshots *RequestSnapshotStore) *PluginInvokerServer {
	allowed := make(map[string]map[string]struct{}, len(deps))
	for i := range deps {
		targetPlugin := strings.TrimSpace(deps[i].Plugin)
		targetOperation := strings.TrimSpace(deps[i].Operation)
		if targetPlugin == "" || targetOperation == "" {
			continue
		}
		ops, ok := allowed[targetPlugin]
		if !ok {
			ops = make(map[string]struct{})
			allowed[targetPlugin] = ops
		}
		ops[targetOperation] = struct{}{}
	}

	return &PluginInvokerServer{
		pluginName: pluginName,
		invoker:    invoker,
		snapshots:  snapshots,
		allowed:    allowed,
	}
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
	if !s.allows(targetPlugin, targetOperation) {
		return nil, status.Errorf(codes.PermissionDenied, "plugin %q may not invoke %s.%s", s.pluginName, targetPlugin, targetOperation)
	}

	snapshot, err := s.snapshots.snapshot(req.GetRequestHandle())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	connection := strings.TrimSpace(req.GetConnection())
	invokeCtx := restoreRequestSnapshotContext(ctx, snapshot, connection)
	params := map[string]any{}
	if raw := req.GetParams(); raw != nil {
		params = raw.AsMap()
	}

	instance := strings.TrimSpace(req.GetInstance())
	if instance == "" && connection == "" && shouldInheritCredentialSelectors(snapshot) {
		instance = snapshot.credential.Instance
	}

	result, err := s.invoker.Invoke(invokeCtx, snapshot.principal, targetPlugin, instance, targetOperation, params)
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
