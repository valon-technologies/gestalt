package workflows

import (
	"context"
	"errors"
	"time"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvokeFunc func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error)
type InvokeActionFunc func(context.Context, coreworkflow.InvokeActionRequest) (*coreworkflow.HostActionResponse, error)
type CancelHostActionFunc func(context.Context, coreworkflow.CancelHostActionRequest) (*coreworkflow.HostActionResponse, error)

type HostServer struct {
	proto.UnimplementedWorkflowHostServer
	providerName string
	invokeAction InvokeActionFunc
}

func NewHostServer(providerName string, _ InvokeFunc) *HostServer {
	return &HostServer{providerName: providerName}
}

func NewHostServerWithActions(providerName string, _ InvokeFunc, invokeAction InvokeActionFunc, _ CancelHostActionFunc) *HostServer {
	return &HostServer{
		providerName: providerName,
		invokeAction: invokeAction,
	}
}

func (s *HostServer) InvokeWorkflowAction(ctx context.Context, req *proto.InvokeWorkflowActionRequest) (out *proto.WorkflowActionResult, err error) {
	startedAt := time.Now()
	dims := workflowHostActionMetricDims(s, observability.WorkflowOperationInvokeWorkflowAction)
	ctx, span := observability.StartSpan(ctx, "workflow.host.action", observability.WorkflowMetricAttributes(dims)...)
	defer func() {
		observability.EndSpan(span, err)
		observability.RecordWorkflowHostOperation(ctx, startedAt, err, dims)
	}()
	if s == nil || s.invokeAction == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow host action invoker is not configured")
	}
	value, err := workflowActionRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "workflow action: %v", err)
	}
	value.ProviderName = s.providerName
	resp, err := s.invokeAction(ctx, value)
	if err != nil {
		return nil, status.Errorf(workflowInvokeErrorCode(err), "workflow action: %v", err)
	}
	return workflowHostActionResponseToProto(resp), nil
}

func workflowHostActionMetricDims(s *HostServer, operation string) observability.WorkflowMetricDims {
	providerName := ""
	if s != nil {
		providerName = s.providerName
	}
	return observability.WorkflowMetricDims{
		ProviderName:    providerName,
		OperationName:   operation,
		TriggerKind:     observability.WorkflowTriggerKindUnknown,
		TargetKind:      observability.WorkflowTargetKindSteps,
		TelemetrySource: observability.WorkflowTelemetrySourceCore,
	}
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
	case errors.Is(err, invocation.ErrNotAuthenticated), errors.Is(err, invocation.ErrNoCredential):
		return codes.Unauthenticated
	case errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return codes.FailedPrecondition
	case errors.Is(err, invocation.ErrInvalidInvocation):
		return codes.InvalidArgument
	case errors.Is(err, invocation.ErrInternal):
		return codes.Internal
	default:
		return codes.Unknown
	}
}

var _ proto.WorkflowHostServer = (*HostServer)(nil)
