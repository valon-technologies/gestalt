package agents

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ManagerService interface {
	CreateSession(context.Context, *principal.Principal, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error)
	GetSession(context.Context, *principal.Principal, *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error)
	ListSessions(context.Context, *principal.Principal, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error)
	UpdateSession(context.Context, *principal.Principal, *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error)
	CreateTurn(context.Context, *principal.Principal, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error)
	GetTurn(context.Context, *principal.Principal, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error)
	ListTurns(context.Context, *principal.Principal, *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error)
	CancelTurn(context.Context, *principal.Principal, *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error)
	ListTurnEvents(context.Context, *principal.Principal, *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error)
	ListInteractions(context.Context, *principal.Principal, *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error)
	ResolveInteraction(context.Context, *principal.Principal, *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error)
}

type ProviderServer struct {
	proto.UnimplementedAgentServer

	pluginName string
	manager    ManagerService
}

type ProviderServerOption func(*ProviderServer)

func NewProviderServer(pluginName string, manager ManagerService, opts ...ProviderServerOption) *ProviderServer {
	s := &ProviderServer{
		pluginName: pluginName,
		manager:    manager,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *ProviderServer) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflowAgentRequest(reqCtx, req.GetProviderName(), req.GetModel()); err != nil {
		return nil, err
	}
	session, err := s.manager.CreateSession(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "get agent sessions"); err != nil {
		return nil, err
	}
	session, err := s.manager.GetSession(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	if req.GetLimit() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be non-negative")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "list agent sessions"); err != nil {
		return nil, err
	}
	sessions, err := s.manager.ListSessions(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
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
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "update agent sessions"); err != nil {
		return nil, err
	}
	session, err := s.manager.UpdateSession(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentSessionToProto(session)
}

func (s *ProviderServer) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := s.authorizeWorkflowAgentRequest(reqCtx, req.GetProviderName(), req.GetModel()); err != nil {
		return nil, err
	}
	turn, err := s.manager.CreateTurn(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	if err := s.authorizeWorkflowAgentRequest(reqCtx, req.GetProviderName(), ""); err != nil {
		return nil, err
	}
	turn, err := s.manager.GetTurn(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.GetLimit() < 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be non-negative")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "list agent turns"); err != nil {
		return nil, err
	}
	turns, err := s.manager.ListTurns(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
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
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	if err := s.authorizeWorkflowAgentRequest(reqCtx, req.GetProviderName(), ""); err != nil {
		return nil, err
	}
	turn, err := s.manager.CancelTurn(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentTurnToProto(turn)
}

func (s *ProviderServer) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "list agent turn events"); err != nil {
		return nil, err
	}
	events, err := s.manager.ListTurnEvents(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
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
	reqCtx, err := s.requestContext(ctx, req.GetContext())
	if err != nil {
		return nil, err
	}
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		return nil, status.Error(codes.InvalidArgument, "turn_id is required")
	}
	if err := rejectWorkflowAgentRequest(reqCtx, "list agent interactions"); err != nil {
		return nil, err
	}
	interactions, err := s.manager.ListInteractions(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return &proto.ListAgentProviderInteractionsResponse{Interactions: interactionsToProto(interactions)}, nil
}

func (s *ProviderServer) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	reqCtx, err := s.requestContext(ctx, req.GetContext())
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
	if err := rejectWorkflowAgentRequest(reqCtx, "resolve agent interactions"); err != nil {
		return nil, err
	}
	interaction, err := s.manager.ResolveInteraction(s.restoreRequestContext(ctx, reqCtx), reqCtx.Principal(), req)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	return agentInteractionToProto(interaction)
}

func (s *ProviderServer) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	return nil, status.Error(codes.Unimplemented, "agent get capabilities is not available through the public provider facade")
}

func (s *ProviderServer) requestContext(ctx context.Context, reqCtx *proto.RequestContext) (appaccessservice.ProviderRequestContext, error) {
	out, err := appaccessservice.ProviderRequestContextFromProto(ctx, reqCtx, "", s.pluginName)
	if err != nil {
		return out, err
	}
	switch out.CallerKind() {
	case invocation.ProviderKindApp, invocation.ProviderKindWorkflow:
		return out, nil
	default:
		return out, status.Errorf(codes.FailedPrecondition, "%s caller context is not supported for agent provider invocation", out.CallerKind())
	}
}

func (s *ProviderServer) restoreRequestContext(ctx context.Context, reqCtx appaccessservice.ProviderRequestContext) context.Context {
	return reqCtx.Restore(ctx, "")
}

func (s *ProviderServer) authorizeWorkflowAgentRequest(reqCtx appaccessservice.ProviderRequestContext, providerName, model string) error {
	if reqCtx.CallerKind() != invocation.ProviderKindWorkflow {
		return nil
	}
	step, err := appaccessservice.WorkflowStepInvocationFromContext(reqCtx.Workflow())
	if err != nil {
		return err
	}
	if step.ProviderName != reqCtx.CallerName() {
		return status.Errorf(codes.PermissionDenied, "workflow caller %q does not match workflow context provider %q", reqCtx.CallerName(), step.ProviderName)
	}
	if step.Kind != "agent" {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke agent provider", reqCtx.CallerName(), step.ID)
	}
	if providerName = strings.TrimSpace(providerName); providerName != "" && step.AgentProvider != providerName {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke agent provider %q", reqCtx.CallerName(), step.ID, providerName)
	}
	if model = strings.TrimSpace(model); model != "" && step.Model != model {
		return status.Errorf(codes.PermissionDenied, "workflow %q step %q may not invoke agent model %q", reqCtx.CallerName(), step.ID, model)
	}
	return nil
}

func rejectWorkflowAgentRequest(reqCtx appaccessservice.ProviderRequestContext, operation string) error {
	if reqCtx.CallerKind() != invocation.ProviderKindWorkflow {
		return nil
	}
	return status.Errorf(codes.PermissionDenied, "workflow callers may not %s", operation)
}

func agentManagerStatusError(err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := status.FromError(err); ok {
		return existing.Err()
	}
	switch {
	case errors.Is(err, agentmanager.ErrAgentNotConfigured), errors.Is(err, agentmanager.ErrAgentProviderRequired), errors.Is(err, agentmanager.ErrAgentProviderNotAvailable), errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported), errors.Is(err, agentmanager.ErrAgentSessionStartUnsupported), errors.Is(err, agentmanager.ErrAgentWorkspaceUnsupported), errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool), errors.Is(err, agentmanager.ErrAgentInteractionRequired), errors.Is(err, agentmanager.ErrAgentSessionMetadataInvalid), errors.Is(err, agentmanager.ErrAgentWorkspaceInvalid):
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

var _ proto.AgentServer = (*ProviderServer)(nil)
