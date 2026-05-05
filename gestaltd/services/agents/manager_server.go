package agents

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvocationTokenManager = plugininvokerservice.InvocationTokenManager

func NewInvocationTokenManager(secret []byte) (*InvocationTokenManager, error) {
	return plugininvokerservice.NewInvocationTokenManager(secret)
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

type ManagerServer struct {
	proto.UnimplementedAgentManagerHostServer

	pluginName string
	manager    ManagerService
	tokens     *InvocationTokenManager
}

func NewManagerServer(pluginName string, manager ManagerService, tokens *InvocationTokenManager) *ManagerServer {
	return &ManagerServer{
		pluginName: pluginName,
		manager:    manager,
		tokens:     tokens,
	}
}

func (s *ManagerServer) CreateSession(ctx context.Context, req *proto.AgentManagerCreateSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	session, err := s.manager.CreateSession(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerCreateSessionRequest{
		IdempotencyKey: strings.TrimSpace(req.GetIdempotencyKey()),
		ProviderName:   strings.TrimSpace(req.GetProviderName()),
		Model:          strings.TrimSpace(req.GetModel()),
		ClientRef:      strings.TrimSpace(req.GetClientRef()),
		Metadata:       mapFromStruct(req.GetMetadata()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ManagerServer) GetSession(ctx context.Context, req *proto.AgentManagerGetSessionRequest) (*proto.AgentSession, error) {
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
	session, err := s.manager.GetSession(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), sessionID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ManagerServer) ListSessions(ctx context.Context, req *proto.AgentManagerListSessionsRequest) (*proto.AgentManagerListSessionsResponse, error) {
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
	sessions, err := s.manager.ListSessions(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerListSessionsRequest{
		ProviderName: strings.TrimSpace(req.GetProviderName()),
		State:        state,
		Limit:        int(req.GetLimit()),
		SummaryOnly:  req.GetSummaryOnly(),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp := &proto.AgentManagerListSessionsResponse{Sessions: make([]*proto.AgentSession, 0, len(sessions))}
	for _, session := range sessions {
		encoded, err := agentSessionToProto(session)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent session: %v", err)
		}
		resp.Sessions = append(resp.Sessions, encoded)
	}
	return resp, nil
}

func (s *ManagerServer) UpdateSession(ctx context.Context, req *proto.AgentManagerUpdateSessionRequest) (*proto.AgentSession, error) {
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
	session, err := s.manager.UpdateSession(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerUpdateSessionRequest{
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

func (s *ManagerServer) CreateTurn(ctx context.Context, req *proto.AgentManagerCreateTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.CreateTurn(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerCreateTurnRequest{
		CallerPluginName: strings.TrimSpace(s.pluginName),
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		Model:            strings.TrimSpace(req.GetModel()),
		SessionID:        sessionID,
		Messages:         agentMessagesFromProto(req.GetMessages()),
		ToolRefs:         agentToolRefsFromProto(req.GetToolRefs()),
		ToolSource:       agentToolSourceModeFromProto(req.GetToolSource()),
		ResponseSchema:   mapFromStruct(req.GetResponseSchema()),
		Metadata:         mapFromStruct(req.GetMetadata()),
		ModelOptions:     mapFromStruct(req.GetModelOptions()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ManagerServer) GetTurn(ctx context.Context, req *proto.AgentManagerGetTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.GetTurn(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ManagerServer) ListTurns(ctx context.Context, req *proto.AgentManagerListTurnsRequest) (*proto.AgentManagerListTurnsResponse, error) {
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
	turns, err := s.manager.ListTurns(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coreagent.ManagerListTurnsRequest{
		SessionID:   sessionID,
		Status:      statusFilter,
		Limit:       int(req.GetLimit()),
		SummaryOnly: req.GetSummaryOnly(),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp := &proto.AgentManagerListTurnsResponse{Turns: make([]*proto.AgentTurn, 0, len(turns))}
	for _, turn := range turns {
		encoded, err := agentTurnToProto(turn)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent turn: %v", err)
		}
		resp.Turns = append(resp.Turns, encoded)
	}
	return resp, nil
}

func (s *ManagerServer) CancelTurn(ctx context.Context, req *proto.AgentManagerCancelTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.CancelTurn(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ManagerServer) ListTurnEvents(ctx context.Context, req *proto.AgentManagerListTurnEventsRequest) (*proto.AgentManagerListTurnEventsResponse, error) {
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
	events, err := s.manager.ListTurnEvents(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, req.GetAfterSeq(), int(req.GetLimit()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.AgentManagerListTurnEventsResponse{Events: turnEventsToProto(events)}, nil
}

func (s *ManagerServer) ListInteractions(ctx context.Context, req *proto.AgentManagerListInteractionsRequest) (*proto.AgentManagerListInteractionsResponse, error) {
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
	interactions, err := s.manager.ListInteractions(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.AgentManagerListInteractionsResponse{Interactions: interactionsToProto(interactions)}, nil
}

func (s *ManagerServer) ResolveInteraction(ctx context.Context, req *proto.AgentManagerResolveInteractionRequest) (*proto.AgentInteraction, error) {
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
	interaction, err := s.manager.ResolveInteraction(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), turnID, interactionID, mapFromStruct(req.GetResolution()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentInteractionToProto(interaction)
}

func (s *ManagerServer) tokenContext(token string) (plugininvokerservice.TokenContext, error) {
	if s == nil || s.tokens == nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.ResolveToken(token, s.pluginName)
	if err != nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
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
	case errors.Is(err, agentmanager.ErrAgentNotConfigured), errors.Is(err, agentmanager.ErrAgentProviderRequired), errors.Is(err, agentmanager.ErrAgentProviderNotAvailable), errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported), errors.Is(err, agentmanager.ErrAgentSessionStartUnsupported), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired), errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool), errors.Is(err, agentmanager.ErrAgentInteractionRequired), errors.Is(err, agentmanager.ErrAgentSessionMetadataInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInvalidListRequest):
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

var _ proto.AgentManagerHostServer = (*ManagerServer)(nil)
