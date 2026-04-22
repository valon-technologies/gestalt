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

type WorkflowHostServer struct {
	proto.UnimplementedWorkflowHostServer
	providerName string
	invoke       workflowInvokeFunc
}

func NewWorkflowHostServer(providerName string, invoke workflowInvokeFunc) *WorkflowHostServer {
	return &WorkflowHostServer{
		providerName: providerName,
		invoke:       invoke,
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
	if strings.TrimSpace(value.ExecutionRef) == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: execution_ref is required")
	}
	value.ProviderName = s.providerName
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
