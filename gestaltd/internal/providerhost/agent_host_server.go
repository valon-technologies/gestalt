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
)

type agentExecuteToolFunc func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error)

type AgentHostServer struct {
	proto.UnimplementedAgentHostServer
	providerName string
	executeTool  agentExecuteToolFunc
}

func NewAgentHostServer(providerName string, executeTool agentExecuteToolFunc) *AgentHostServer {
	return &AgentHostServer{
		providerName: providerName,
		executeTool:  executeTool,
	}
}

func (s *AgentHostServer) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	if s == nil || s.executeTool == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent host executor is not configured")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	toolID := strings.TrimSpace(req.GetToolId())
	if toolID == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_id is required")
	}
	resp, err := s.executeTool(ctx, coreagent.ExecuteToolRequest{
		ProviderName: strings.TrimSpace(s.providerName),
		SessionID:    sessionID,
		TurnID:       turnID,
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
