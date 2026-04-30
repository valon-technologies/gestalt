package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentManagerService interface {
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

type AgentManagerServer struct {
	proto.UnimplementedAgentManagerHostServer

	pluginName string
	manager    AgentManagerService
	tokens     *InvocationTokenManager
}

func NewAgentManagerServer(pluginName string, manager AgentManagerService, tokens *InvocationTokenManager) *AgentManagerServer {
	return &AgentManagerServer{
		pluginName: pluginName,
		manager:    manager,
		tokens:     tokens,
	}
}

func (s *AgentManagerServer) CreateSession(ctx context.Context, req *proto.AgentManagerCreateSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	session, err := s.manager.CreateSession(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerCreateSessionRequest{
		IdempotencyKey:  strings.TrimSpace(req.GetIdempotencyKey()),
		ProviderName:    strings.TrimSpace(req.GetProviderName()),
		Model:           strings.TrimSpace(req.GetModel()),
		ClientRef:       strings.TrimSpace(req.GetClientRef()),
		Metadata:        mapFromStruct(req.GetMetadata()),
		ProviderOptions: mapFromStruct(req.GetProviderOptions()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *AgentManagerServer) GetSession(ctx context.Context, req *proto.AgentManagerGetSessionRequest) (*proto.AgentSession, error) {
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
	session, err := s.manager.GetSession(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, sessionID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *AgentManagerServer) ListSessions(ctx context.Context, req *proto.AgentManagerListSessionsRequest) (*proto.AgentManagerListSessionsResponse, error) {
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
	sessions, err := s.manager.ListSessions(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerListSessionsRequest{
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

func (s *AgentManagerServer) UpdateSession(ctx context.Context, req *proto.AgentManagerUpdateSessionRequest) (*proto.AgentSession, error) {
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
	session, err := s.manager.UpdateSession(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerUpdateSessionRequest{
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

func (s *AgentManagerServer) CreateTurn(ctx context.Context, req *proto.AgentManagerCreateTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.CreateTurn(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerCreateTurnRequest{
		CallerPluginName: strings.TrimSpace(s.pluginName),
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
		Model:            strings.TrimSpace(req.GetModel()),
		SessionID:        sessionID,
		Messages:         agentMessagesFromProto(req.GetMessages()),
		ToolRefs:         agentToolRefsFromProto(req.GetToolRefs()),
		ToolSource:       agentToolSourceModeFromProto(req.GetToolSource()),
		ResponseSchema:   mapFromStruct(req.GetResponseSchema()),
		Metadata:         mapFromStruct(req.GetMetadata()),
		ProviderOptions:  mapFromStruct(req.GetProviderOptions()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *AgentManagerServer) GetTurn(ctx context.Context, req *proto.AgentManagerGetTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.GetTurn(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *AgentManagerServer) ListTurns(ctx context.Context, req *proto.AgentManagerListTurnsRequest) (*proto.AgentManagerListTurnsResponse, error) {
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
	turns, err := s.manager.ListTurns(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerListTurnsRequest{
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

func (s *AgentManagerServer) CancelTurn(ctx context.Context, req *proto.AgentManagerCancelTurnRequest) (*proto.AgentTurn, error) {
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
	turn, err := s.manager.CancelTurn(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, turnID, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *AgentManagerServer) ListTurnEvents(ctx context.Context, req *proto.AgentManagerListTurnEventsRequest) (*proto.AgentManagerListTurnEventsResponse, error) {
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
	events, err := s.manager.ListTurnEvents(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, turnID, req.GetAfterSeq(), int(req.GetLimit()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.AgentManagerListTurnEventsResponse{Events: turnEventsToProto(events)}, nil
}

func (s *AgentManagerServer) ListInteractions(ctx context.Context, req *proto.AgentManagerListInteractionsRequest) (*proto.AgentManagerListInteractionsResponse, error) {
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
	interactions, err := s.manager.ListInteractions(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, turnID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.AgentManagerListInteractionsResponse{Interactions: interactionsToProto(interactions)}, nil
}

func (s *AgentManagerServer) ResolveInteraction(ctx context.Context, req *proto.AgentManagerResolveInteractionRequest) (*proto.AgentInteraction, error) {
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
	interaction, err := s.manager.ResolveInteraction(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, turnID, interactionID, mapFromStruct(req.GetResolution()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentInteractionToProto(interaction)
}

func (s *AgentManagerServer) tokenContext(token string) (invocationTokenContext, error) {
	if s == nil || s.tokens == nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, "invocation token is required")
	}
	tokenCtx, err := s.tokens.resolveToken(token, s.pluginName)
	if err != nil {
		return invocationTokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
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
	case errors.Is(err, agentmanager.ErrAgentNotConfigured), errors.Is(err, agentmanager.ErrAgentProviderRequired), errors.Is(err, agentmanager.ErrAgentProviderNotAvailable), errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired), errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool), errors.Is(err, agentmanager.ErrAgentInteractionRequired):
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

var _ proto.AgentManagerHostServer = (*AgentManagerServer)(nil)
