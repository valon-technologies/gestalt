package workflows

import (
	"context"
	"errors"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvokeFunc func(context.Context, coreworkflow.InvokeOperationRequest) (*coreworkflow.InvokeOperationResponse, error)

type HostServer struct {
	proto.UnimplementedWorkflowHostServer
	providerName string
	invoke       InvokeFunc
}

func NewHostServer(providerName string, invoke InvokeFunc) *HostServer {
	return &HostServer{
		providerName: providerName,
		invoke:       invoke,
	}
}

func (s *HostServer) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (out *proto.InvokeWorkflowOperationResponse, err error) {
	startedAt := time.Now()
	providerName := ""
	if s != nil {
		providerName = s.providerName
	}
	dims := observability.WorkflowMetricDims{
		ProviderName:    providerName,
		OperationName:   observability.WorkflowOperationInvokeOperation,
		TriggerKind:     workflowProtoTriggerKind(req),
		TargetKind:      workflowProtoTargetKind(req.GetTarget()),
		TelemetrySource: observability.WorkflowTelemetrySourceCore,
	}
	ctx, span := observability.StartSpan(ctx, "workflow.host.operation", observability.WorkflowMetricAttributes(dims)...)
	defer func() {
		observability.EndSpan(span, err)
		observability.RecordWorkflowHostOperation(ctx, startedAt, err, dims)
	}()
	if s == nil || s.invoke == nil {
		return nil, status.Error(codes.FailedPrecondition, "workflow host invoker is not configured")
	}
	value, err := workflowInvokeRequestFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "workflow invoke operation: %v", err)
	}
	if value.Target.Agent == nil {
		if value.Target.Plugin == nil {
			return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.plugin.plugin_name is required")
		}
		value.Target.Plugin.PluginName = strings.TrimSpace(value.Target.Plugin.PluginName)
		if value.Target.Plugin.PluginName == "" {
			return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.plugin.plugin_name is required")
		}
		value.Target.Plugin.Operation = strings.TrimSpace(value.Target.Plugin.Operation)
		if value.Target.Plugin.Operation == "" {
			return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.plugin.operation is required")
		}
	} else if strings.TrimSpace(value.Target.Agent.ProviderName) == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow invoke operation: target.agent.provider_name is required")
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

func workflowProtoTargetKind(target *proto.BoundWorkflowTarget) string {
	if target.GetPlugin() != nil {
		return observability.WorkflowTargetKindPlugin
	}
	if target.GetAgent() != nil {
		return observability.WorkflowTargetKindAgent
	}
	return observability.WorkflowTargetKindUnknown
}

func workflowProtoTriggerKind(req *proto.InvokeWorkflowOperationRequest) string {
	if req == nil {
		return observability.WorkflowTriggerKindNone
	}
	switch {
	case req.GetTrigger().GetManual() != nil:
		return observability.WorkflowTriggerKindManual
	case req.GetTrigger().GetSchedule() != nil:
		return observability.WorkflowTriggerKindSchedule
	case req.GetTrigger().GetEvent() != nil:
		return observability.WorkflowTriggerKindEvent
	case len(req.GetSignals()) > 0:
		return observability.WorkflowTriggerKindSignal
	default:
		return observability.WorkflowTriggerKindNone
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
	case errors.Is(err, invocation.ErrInternal):
		return codes.Internal
	default:
		return codes.Unknown
	}
}

var _ proto.WorkflowHostServer = (*HostServer)(nil)
