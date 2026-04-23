package providerhost

import (
	"context"
	"errors"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentManagerService interface {
	Run(context.Context, *principal.Principal, coreagent.ManagerRunRequest) (*coreagent.ManagedRun, error)
	GetRun(context.Context, *principal.Principal, string) (*coreagent.ManagedRun, error)
	ListRuns(context.Context, *principal.Principal) ([]*coreagent.ManagedRun, error)
	CancelRun(context.Context, *principal.Principal, string, string) (*coreagent.ManagedRun, error)
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

func (s *AgentManagerServer) Run(ctx context.Context, req *proto.AgentManagerRunRequest) (*proto.ManagedAgentRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent manager is not configured")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	managed, err := s.manager.Run(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, coreagent.ManagerRunRequest{
		CallerPluginName: strings.TrimSpace(s.pluginName),
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Model:            strings.TrimSpace(req.GetModel()),
		Messages:         agentMessagesFromProto(req.GetMessages()),
		ToolRefs:         agentToolRefsFromProto(req.GetToolRefs()),
		ToolSource:       agentToolSourceModeFromProto(req.GetToolSource()),
		ResponseSchema:   mapFromStruct(req.GetResponseSchema()),
		SessionRef:       strings.TrimSpace(req.GetSessionRef()),
		Metadata:         mapFromStruct(req.GetMetadata()),
		ProviderOptions:  mapFromStruct(req.GetProviderOptions()),
		IdempotencyKey:   strings.TrimSpace(req.GetIdempotencyKey()),
	})
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp, err := managedAgentRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode agent run: %v", err)
	}
	return resp, nil
}

func (s *AgentManagerServer) GetRun(ctx context.Context, req *proto.AgentManagerGetRunRequest) (*proto.ManagedAgentRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent manager is not configured")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.GetRun(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, runID)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp, err := managedAgentRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode agent run: %v", err)
	}
	return resp, nil
}

func (s *AgentManagerServer) ListRuns(ctx context.Context, req *proto.AgentManagerListRunsRequest) (*proto.AgentManagerListRunsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent manager is not configured")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	runs, err := s.manager.ListRuns(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal)
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp := &proto.AgentManagerListRunsResponse{Runs: make([]*proto.ManagedAgentRun, 0, len(runs))}
	for _, run := range runs {
		encoded, err := managedAgentRunToProto(run)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode agent run: %v", err)
		}
		resp.Runs = append(resp.Runs, encoded)
	}
	return resp, nil
}

func (s *AgentManagerServer) CancelRun(ctx context.Context, req *proto.AgentManagerCancelRunRequest) (*proto.ManagedAgentRun, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "agent manager is not configured")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	managed, err := s.manager.CancelRun(restoreInvocationTokenContext(ctx, tokenCtx, ""), tokenCtx.principal, runID, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, agentManagerStatusError(err)
	}
	resp, err := managedAgentRunToProto(managed)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode agent run: %v", err)
	}
	return resp, nil
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
	case errors.Is(err, agentmanager.ErrAgentRunCreationInProgress):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, agentmanager.ErrAgentNotConfigured), errors.Is(err, agentmanager.ErrAgentRunMetadataNotConfigured), errors.Is(err, invocation.ErrNoToken), errors.Is(err, invocation.ErrAmbiguousInstance), errors.Is(err, invocation.ErrUserResolution):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired), errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired), errors.Is(err, invocation.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, invocation.ErrInternal):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, invocation.ErrAuthorizationDenied), errors.Is(err, invocation.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound), errors.Is(err, invocation.ErrOperationNotFound), errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Unknown, err.Error())
	}
}

var _ proto.AgentManagerHostServer = (*AgentManagerServer)(nil)
