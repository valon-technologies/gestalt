package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowInvokeFunc func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error)
type workflowAllowFunc func(providerName, pluginName, operation string) bool

type WorkflowHostServer struct {
	proto.UnimplementedWorkflowHostServer
	providerName string
	invoke       workflowInvokeFunc
	allow        workflowAllowFunc
}

func NewWorkflowHostServer(providerName string, invoke workflowInvokeFunc, allow workflowAllowFunc) *WorkflowHostServer {
	return &WorkflowHostServer{
		providerName: providerName,
		invoke:       invoke,
		allow:        allow,
	}
}

func (s *WorkflowHostServer) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	if s == nil || s.invoke == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow host invoker is not configured")
	}
	value, err := workflowInvokeRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "workflow invoke operation: %v", err)
	}
	value.Target.PluginName = strings.TrimSpace(value.Target.PluginName)
	if value.Target.PluginName == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.plugin_name is required")
	}
	value.Target.Operation = strings.TrimSpace(value.Target.Operation)
	if value.Target.Operation == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.operation is required")
	}
	value.PluginName = strings.TrimSpace(req.GetPluginName())
	if value.PluginName == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: plugin_name is required")
	}
	if value.Target.PluginName != value.PluginName {
		return nil, status.Errorf(codes.PermissionDenied, "workflow invoke operation target plugin %q is outside scoped plugin %q", value.Target.PluginName, value.PluginName)
	}
	value.ProviderName = s.providerName
	if s.allow != nil && !s.allow(s.providerName, value.PluginName, value.Target.Operation) {
		return nil, status.Errorf(codes.PermissionDenied, "workflow invoke operation %q on plugin %q is not allowed", value.Target.Operation, value.PluginName)
	}
	resp, err := s.invoke(ctx, value)
	if err != nil {
		return nil, status.Errorf(workflowInvokeErrorCode(err), "workflow invoke operation: %v", err)
	}
	return workflowInvokeResponseToProto(resp), nil
}

func workflowInvokeErrorCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if existing, ok := status.FromError(err); ok {
		return existing.Code()
	}
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound):
		return codes.NotFound
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return codes.PermissionDenied
	case errors.Is(err, invocation.ErrNotAuthenticated), errors.Is(err, invocation.ErrNoToken):
		return codes.Unauthenticated
	case errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return codes.FailedPrecondition
	case errors.Is(err, invocation.ErrInternal):
		return codes.Internal
	default:
		return codes.Unknown
	}
}

var _ proto.WorkflowHostServer = (*WorkflowHostServer)(nil)
