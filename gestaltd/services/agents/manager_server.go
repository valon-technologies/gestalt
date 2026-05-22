package agents

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	appinvokerservice "github.com/valon-technologies/gestalt/server/services/appinvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvocationTokenManager = appinvokerservice.InvocationTokenManager

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return appinvokerservice.NewInvocationTokenManager(secret)
}

type ManagerService interface {
	CreateSession(context.Context, *principal.Principal, coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error)
	GetSession(context.Context, *principal.Principal, string) (*coreagent.Session, error)
	ListSessions(context.Context, *principal.Principal, coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error)
	UpdateSession(context.Context, *principal.Principal, coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error)
	CreateTurn(context.Context, *principal.Principal, coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error)
	GetTurn(context.Context, *principal.Principal, string) (*coreagent.Turn, error)
	ListTurns(context.Context, *principal.Principal, coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error)
	CancelTurn(context.Context, *principal.Principal, string, string) (*coreagent.Turn, error)
	ListTurnEvents(context.Context, *principal.Principal, string, int64, int) ([]*coreagent.TurnEvent, error)
	ListInteractions(context.Context, *principal.Principal, string) ([]*coreagent.Interaction, error)
	ResolveInteraction(context.Context, *principal.Principal, string, string, map[string]any) (*coreagent.Interaction, error)
}

type ProviderServer struct {
	proto.UnimplementedAgentProviderServer

	pluginName string
	manager    ManagerService
	tokens     *InvocationTokenManager
}

func NewProviderServer(pluginName string, manager ManagerService, tokens *InvocationTokenManager) *ProviderServer {
	return &ProviderServer{
		pluginName: pluginName,
		manager:    manager,
		tokens:     tokens,
	}
}

func (s *ProviderServer) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	session, err := s.manager.CreateSession(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerCreateSessionRequest{
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		ProviderName:   strings.TrimSpace(req.GetProviderName()),
		Model:          strings.TrimSpace(req.GetModel()),
		ClientRef:      strings.TrimSpace(req.GetClientRef()),
		Metadata:       mapFromStruct(req.GetMetadata()),
		Workspace:      agentWorkspaceFromProto(req.GetWorkspace()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	session, err := s.manager.GetSession(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), sessionID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetLimit() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be non-negative")
	}
	sessions, err := s.manager.ListSessions(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerListSessionsRequest{
		ProviderName: strings.TrimSpace(req.GetProviderName()),
		State:        state,
		Limit:        int(req.GetLimit()),
		SummaryOnly:  req.GetSummaryOnly(),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp := &proto.ListAgentProviderSessionsResponse{Sessions: make([]*proto.AgentSession, 0, len(sessions))}
	for _, session := range sessions {
		encoded, err := agentSessionToProto(session)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent session: %v", err)
		}
		resp.Sessions = append(resp.Sessions, encoded)
	}
	return resp, nil
}

func (s *ProviderServer) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	state, err := agentSessionStateFromProto(req.GetState())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	session, err := s.manager.UpdateSession(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerUpdateSessionRequest{
		SessionID: sessionID,
		ClientRef: strings.TrimSpace(req.GetClientRef()),
		State:     state,
		Metadata:  mapFromStruct(req.GetMetadata()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	turn, err := s.manager.CreateTurn(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerCreateTurnRequest{
		CallerAppName:  strings.TrimSpace(s.pluginName),
		IdempotencyKey:    strings.TrimSpace(req.GetIdempotencyKey()),
		Model:             strings.TrimSpace(req.GetModel()),
		SessionID:         sessionID,
		Messages:          agentMessagesFromProto(req.GetMessages()),
		ToolRefs:          agentToolRefsFromProto(req.GetToolRefs()),
		ToolRefsSet:       req.GetToolRefsSet() || len(req.GetToolRefs()) > 0,
		ToolSource:        agentToolSourceModeFromProtoStrict(req.GetToolSource()),
		ResponseSchema:    mapFromStruct(req.GetResponseSchema()),
		ResponseSchemaSet: req.ResponseSchema != nil,
		Metadata:          mapFromStruct(req.GetMetadata()),
		ModelOptions:      mapFromStruct(req.GetModelOptions()),
		TimeoutSeconds:    int(req.GetTimeoutSeconds()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	turn, err := s.manager.GetTurn(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	statusFilter, err := agentExecutionStatusFromProto(req.GetStatus())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetLimit() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be non-negative")
	}
	turns, err := s.manager.ListTurns(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerListTurnsRequest{
		SessionID:   sessionID,
		Status:      statusFilter,
		Limit:       int(req.GetLimit()),
		SummaryOnly: req.GetSummaryOnly(),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp := &proto.ListAgentProviderTurnsResponse{Turns: make([]*proto.AgentTurn, 0, len(turns))}
	for _, turn := range turns {
		encoded, err := agentTurnToProto(turn)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent turn: %v", err)
		}
		resp.Turns = append(resp.Turns, encoded)
	}
	return resp, nil
}

func (s *ProviderServer) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	turn, err := s.manager.CancelTurn(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	events, err := s.manager.ListTurnEvents(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, req.GetAfterSeq(), int(req.GetLimit()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.ListAgentProviderTurnEventsResponse{Events: turnEventsToProto(events)}, nil
}

func (s *ProviderServer) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	return nil, status.Error(codes.Unimplemented, "agent get interaction is not available through the public provider facade")
}

func (s *ProviderServer) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) (*proto.ListAgentProviderInteractionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	interactions, err := s.manager.ListInteractions(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.ListAgentProviderInteractionsResponse{Interactions: interactionsToProto(interactions)}, nil
}

func (s *ProviderServer) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	interactionID := strings.TrimSpace(req.GetInteractionId())
	if interactionID == "" {
		return nil, status.Error(codes.InvalidArgument, "interaction_id is required")
	}
	interaction, err := s.manager.ResolveInteraction(appinvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, interactionID, mapFromStruct(req.GetResolution()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentInteractionToProto(interaction)
}

func (s *ProviderServer) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	return nil, status.Error(codes.Unimplemented, "agent get capabilities is not available through the public provider facade")
}

func (s *ProviderServer) tokenContext(token string) (appinvokerservice.TokenContext, error) {
	if s == nil || s.tokens == nil {
		return appinvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return appinvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.ResolveToken(token, s.pluginName)
	if err != nil {
		return appinvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	return tokenCtx, nil
}

func agentManagerStatusError(err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := status.FromError(err); ok {
		return existing.Err()
	}
	switch {
	case errors.Is(err, agentmanager.ErrAgentNotConfigured), errors.Is(err, agentmanager.ErrAgentProviderRequired), errors.Is(err, agentmanager.ErrAgentProviderNotAvailable), errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported), errors.Is(err, agentmanager.ErrAgentSessionStartUnsupported), errors.Is(err, agentmanager.ErrAgentWorkspaceUnsupported), errors.Is(err, agentmanager.ErrAgentStructuredOutputUnsupported), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired), errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool), errors.Is(err, agentmanager.ErrAgentInteractionRequired), errors.Is(err, agentmanager.ErrAgentSessionMetadataInvalid), errors.Is(err, agentmanager.ErrAgentWorkspaceInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInvalidListRequest), errors.Is(err, invocation.ErrInvalidInvocation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired), errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, invocation.ErrInternal):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInteractionNotFound), errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound), errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Unknown, err.Error())
	}
}

var _ proto.AgentProviderServer = (*ProviderServer)(nil)
