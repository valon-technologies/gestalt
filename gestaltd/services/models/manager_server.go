package models

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/models/modelgrants"
	"github.com/valon-technologies/gestalt/server/services/models/modelmanager"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultManagerSocketEnv = "GESTALT_MODEL_MANAGER_SOCKET"
)

type InvocationTokenManager = plugininvokerservice.InvocationTokenManager

type ManagerService interface {
	Generate(context.Context, *principal.Principal, coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error)
}

type ManagerServer struct {
	proto.UnimplementedModelManagerHostServer

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

func (s *ManagerServer) Generate(ctx context.Context, req *proto.ModelManagerGenerateRequest) (*proto.GenerateModelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokenCtx, err := s.tokenContext(req.GetInvocationToken())
	if err != nil {
		return nil, err
	}
	if err := s.requireModelGrant(tokenCtx, modelgrants.OperationGenerate); err != nil {
		return nil, err
	}
	resp, err := s.manager.Generate(plugininvokerservice.RestoreTokenContext(ctx, tokenCtx, ""), tokenCtx.Principal(), coremodel.ManagerGenerateRequest{
		ProviderName:     strings.TrimSpace(req.GetProviderName()),
		Model:            strings.TrimSpace(req.GetModel()),
		Messages:         modelMessagesFromProto(req.GetMessages()),
		ResponseSchema:   mapFromStruct(req.GetResponseSchema()),
		ModelOptions:     mapFromStruct(req.GetModelOptions()),
		Metadata:         mapFromStruct(req.GetMetadata()),
		CallerPluginName: strings.TrimSpace(s.pluginName),
	})
	if err != nil {
		return nil, modelManagerStatusError(err)
	}
	encoded, err := modelGenerateResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode model response: %v", err)
	}
	return encoded, nil
}

func (s *ManagerServer) tokenContext(token string) (plugininvokerservice.TokenContext, error) {
	tokenCtx, err := s.tokens.ResolveToken(token, s.pluginName)
	if err != nil {
		return plugininvokerservice.TokenContext{}, status.Error(codes.FailedPrecondition, err.Error())
	}
	return tokenCtx, nil
}

func (s *ManagerServer) requireModelGrant(tokenCtx plugininvokerservice.TokenContext, operation string) error {
	if tokenCtx.AllowsModelManagerOperation(operation) {
		return nil
	}
	return status.Errorf(codes.PermissionDenied, "model manager operation %q is not allowed for plugin %q", operation, strings.TrimSpace(s.pluginName))
}

func modelManagerStatusError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, modelmanager.ErrInvalidGenerateRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.FailedPrecondition, err.Error())
	}
}
