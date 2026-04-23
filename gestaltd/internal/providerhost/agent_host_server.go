package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type agentExecuteToolFunc func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error)
type agentEmitEventFunc func(context.Context, coreagent.EmitEventRequest) (*coreagent.RunEvent, error)

type AgentHostServer struct {
	proto.UnimplementedAgentHostServer
	providerName string
	executeTool  agentExecuteToolFunc
	emitEvent    agentEmitEventFunc
}

func NewAgentHostServer(providerName string, executeTool agentExecuteToolFunc, emitEvent agentEmitEventFunc) *AgentHostServer {
	return &AgentHostServer{
		providerName: providerName,
		executeTool:  executeTool,
		emitEvent:    emitEvent,
	}
}

func (s *AgentHostServer) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	if s == nil || s.executeTool == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent host executor is not configured")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	toolID := strings.TrimSpace(req.GetToolId())
	if toolID == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_id is required")
	}
	resp, err := s.executeTool(ctx, coreagent.ExecuteToolRequest{
		ProviderName: strings.TrimSpace(s.providerName),
		RunID:        runID,
		ToolCallID:   strings.TrimSpace(req.GetToolCallId()),
		ToolID:       toolID,
		Arguments:    mapFromStruct(req.GetArguments()),
	})
	if err != nil {
		return nil, status.Errorf(agentHostErrorCode(err), "agent execute tool: %v", err)
	}
	if resp == nil {
		return &proto.ExecuteAgentToolResponse{}, nil
	}
	return &proto.ExecuteAgentToolResponse{
		Status: int32(resp.Status),
		Body:   resp.Body,
	}, nil
}

func (s *AgentHostServer) EmitEvent(ctx context.Context, req *proto.EmitAgentEventRequest) (*emptypb.Empty, error) {
	if s == nil || s.emitEvent == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent host event emitter is not configured")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	eventType := strings.TrimSpace(req.GetType())
	if eventType == "" {
		return nil, status.Error(codes.InvalidArgument, "type is required")
	}
	if _, err := s.emitEvent(ctx, coreagent.EmitEventRequest{
		ProviderName: strings.TrimSpace(s.providerName),
		RunID:        runID,
		Type:         eventType,
		Visibility:   strings.TrimSpace(req.GetVisibility()),
		Data:         mapFromStruct(req.GetData()),
	}); err != nil {
		return nil, status.Errorf(agentHostErrorCode(err), "agent emit event: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func agentHostErrorCode(err error) codes.Code {
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

var _ proto.AgentHostServer = (*AgentHostServer)(nil)
