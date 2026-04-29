package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentExecuteToolFunc func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error)
type agentSearchToolsFunc func(context.Context, coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)

type AgentHostServer struct {
	proto.UnimplementedAgentHostServer
	providerName string
	searchTools  agentSearchToolsFunc
	executeTool  agentExecuteToolFunc
}

func NewAgentHostServer(providerName string, searchTools agentSearchToolsFunc, executeTool agentExecuteToolFunc) *AgentHostServer {
	return &AgentHostServer{
		providerName: providerName,
		searchTools:  searchTools,
		executeTool:  executeTool,
	}
}

func (s *AgentHostServer) SearchTools(ctx context.Context, req *proto.SearchAgentToolsRequest) (*proto.SearchAgentToolsResponse, error) {
	if s == nil || s.searchTools == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent host tool search is not configured")
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
	resp, err := s.searchTools(ctx, coreagent.SearchToolsRequest{
		ProviderName:   strings.TrimSpace(s.providerName),
		SessionID:      sessionID,
		TurnID:         turnID,
		Query:          strings.TrimSpace(req.GetQuery()),
		MaxResults:     int(req.GetMaxResults()),
		CandidateLimit: int(req.GetCandidateLimit()),
		LoadRefs:       agentToolRefsFromProto(req.GetLoadRefs()),
		ToolGrant:      strings.TrimSpace(req.GetToolGrant()),
	})
	if err != nil {
		return nil, status.Errorf(agentHostErrorCode(err), "agent search tools: %v", err)
	}
	out := &proto.SearchAgentToolsResponse{}
	if resp == nil {
		return out, nil
	}
	if len(resp.Tools) > 0 {
		tools, err := agentToolsToProto(resp.Tools)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent search tools: %v", err)
		}
		out.Tools = tools
	}
	out.Candidates = agentToolCandidatesToProto(resp.Candidates)
	out.HasMore = resp.HasMore
	return out, nil
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
	toolCallID := strings.TrimSpace(req.GetToolCallId())
	idempotencyKey := strings.TrimSpace(req.GetIdempotencyKey())
	if toolCallID == "" && idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_call_id or idempotency_key is required")
	}
	resp, err := s.executeTool(ctx, coreagent.ExecuteToolRequest{
		ProviderName:   strings.TrimSpace(s.providerName),
		SessionID:      sessionID,
		TurnID:         turnID,
		ToolCallID:     toolCallID,
		ToolID:         toolID,
		Arguments:      mapFromStruct(req.GetArguments()),
		ToolGrant:      strings.TrimSpace(req.GetToolGrant()),
		IdempotencyKey: idempotencyKey,
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
	case errors.Is(err, invocation.ErrNotAuthenticated), errors.Is(err, invocation.ErrNoCredential):
		return codes.Unauthenticated
	case errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return codes.FailedPrecondition
	case errors.Is(err, invocation.ErrInvalidInvocation):
		return codes.InvalidArgument
	case errors.Is(err, agentmanager.ErrAgentWorkflowToolsNotConfigured):
		return codes.FailedPrecondition
	case errors.Is(err, invocation.ErrInternal):
		return codes.Internal
	default:
		return codes.Unknown
	}
}

var _ proto.AgentHostServer = (*AgentHostServer)(nil)
