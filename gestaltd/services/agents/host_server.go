package agents

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ExecuteToolFunc func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error)
type SearchToolsFunc func(context.Context, coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error)
type ListToolsFunc func(context.Context, coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error)

type HostServer struct {
	proto.UnimplementedAgentHostServer
	providerName string
	searchTools  SearchToolsFunc
	listTools    ListToolsFunc
	executeTool  ExecuteToolFunc
}

func NewHostServer(providerName string, searchTools SearchToolsFunc, listTools ListToolsFunc, executeTool ExecuteToolFunc) *HostServer {
	return &HostServer{
		providerName: providerName,
		searchTools:  searchTools,
		listTools:    listTools,
		executeTool:  executeTool,
	}
}

func (s *HostServer) SearchTools(ctx context.Context, req *proto.SearchAgentToolsRequest) (*proto.SearchAgentToolsResponse, error) {
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

func (s *HostServer) ListTools(ctx context.Context, req *proto.ListAgentToolsRequest) (*proto.ListAgentToolsResponse, error) {
	if s == nil || s.listTools == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent host tool listing is not configured")
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
	resp, err := s.listTools(ctx, coreagent.ListToolsRequest{
		ProviderName: strings.TrimSpace(s.providerName),
		SessionID:    sessionID,
		TurnID:       turnID,
		PageSize:     int(req.GetPageSize()),
		PageToken:    strings.TrimSpace(req.GetPageToken()),
		ToolGrant:    strings.TrimSpace(req.GetToolGrant()),
	})
	if err != nil {
		return nil, status.Errorf(agentHostErrorCode(err), "agent list tools: %v", err)
	}
	out := &proto.ListAgentToolsResponse{}
	if resp == nil {
		return out, nil
	}
	out.Tools = listedAgentToolsToProto(resp.Tools)
	out.NextPageToken = resp.NextPageToken
	return out, nil
}

func (s *HostServer) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (out *proto.ExecuteAgentToolResponse, err error) {
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
	spanAttrs, metricAttrs := genAIToolExecutionAttrs(
		strings.TrimSpace(s.providerName),
		sessionID,
		turnID,
		toolID,
		toolCallID,
	)
	ctx, span := observability.StartSpan(ctx, "execute_tool "+toolID, spanAttrs...)
	startedAt := time.Now()
	defer func() {
		attrs := metricAttrs
		if err != nil {
			if errorAttr, ok := genAIErrorAttr(err); ok {
				observability.SetSpanAttributes(ctx, errorAttr)
				attrs = append(attrs, errorAttr)
			}
		}
		observability.EndSpan(span, err)
		observability.RecordGenAIClientOperationDuration(ctx, startedAt, attrs...)
	}()

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

func genAIToolExecutionAttrs(providerName, sessionID, turnID, toolID, toolCallID string) ([]attribute.KeyValue, []attribute.KeyValue) {
	agentName := strings.TrimSpace(providerName)
	toolName := strings.TrimSpace(toolID)
	metricAttrs := []attribute.KeyValue{
		observability.AttrGenAIOperationName.String("execute_tool"),
		observability.AttrGenAIProviderName.String("gestalt"),
		observability.AttrGenAIToolName.String(toolName),
		observability.AttrGenAIToolType.String("extension"),
	}
	if agentName != "" {
		metricAttrs = append(metricAttrs, observability.AttrGenAIAgentName.String(agentName))
	}
	spanAttrs := append([]attribute.KeyValue{}, metricAttrs...)
	if sessionID != "" {
		spanAttrs = append(spanAttrs, observability.AttrGenAIConversationID.String(sessionID))
	}
	if turnID != "" {
		spanAttrs = append(spanAttrs, attribute.String("gestalt.agent.turn_id", turnID))
	}
	if toolCallID != "" {
		spanAttrs = append(spanAttrs, observability.AttrGenAIToolCallID.String(toolCallID))
	}
	return spanAttrs, metricAttrs
}

func genAIErrorAttr(err error) (attribute.KeyValue, bool) {
	if err == nil {
		return attribute.KeyValue{}, false
	}
	if st, ok := status.FromError(err); ok {
		return observability.AttrErrorType.String(st.Code().String()), true
	}
	if typ := reflect.TypeOf(err); typ != nil {
		return observability.AttrErrorType.String(typ.String()), true
	}
	return observability.AttrErrorType.String("_OTHER"), true
}

var _ proto.AgentHostServer = (*HostServer)(nil)
