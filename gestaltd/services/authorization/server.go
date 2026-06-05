package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type forwardingServer struct {
	proto.UnimplementedAuthorizationProviderServer
	provider core.AuthorizationProvider
}

type hostServer struct {
	*forwardingServer
	enforcer *access.Enforcer
	tokens   *appaccess.InvocationTokenManager
}

func NewHostServer(provider core.AuthorizationProvider, enforcer *access.Enforcer, tokens *appaccess.InvocationTokenManager) proto.AuthorizationProviderServer {
	if enforcer == nil {
		enforcer = access.NewEnforcer(provider)
	}
	return &hostServer{
		forwardingServer: &forwardingServer{provider: provider},
		enforcer:         enforcer,
		tokens:           tokens,
	}
}

func (s *forwardingServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccess(ctx, req)
}

func (s *forwardingServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccessMany(ctx, req)
}

func (s *forwardingServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListRelationships(ctx, req)
}

func (s *hostServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if err := s.requireMutation(ctx, "AddRelationship"); err != nil {
		return nil, err
	}
	return s.provider.AddRelationship(ctx, req)
}

func (s *hostServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if err := s.requireMutation(ctx, "DeleteRelationship"); err != nil {
		return nil, err
	}
	return s.provider.DeleteRelationship(ctx, req)
}

func (s *hostServer) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	if err := s.requireMutation(ctx, "SetAuthorizationState"); err != nil {
		return nil, err
	}
	return s.provider.SetAuthorizationState(ctx, req)
}

func (s *forwardingServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.GetActiveModelRef(ctx)
}

func (s *hostServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if err := s.requireMutation(ctx, "SetActiveModel"); err != nil {
		return nil, err
	}
	return s.provider.SetActiveModel(ctx, req)
}

func (s *forwardingServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListActiveModelResourceTypes(ctx, req)
}

func (s *forwardingServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is not configured")
	}
	return nil
}

func (s *hostServer) requireMutation(ctx context.Context, method string) error {
	if err := s.requireProvider(); err != nil {
		return err
	}
	p, err := s.principal(ctx)
	if err != nil {
		return err
	}
	if err := s.enforcer.RequireAuthorizationMutation(ctx, p, method); err != nil {
		return accessStatusError(err)
	}
	return nil
}

func (s *hostServer) principal(ctx context.Context) (*principal.Principal, error) {
	if p := principal.FromContext(ctx); p != nil && access.SubjectFromPrincipal(p) != nil {
		return p, nil
	}
	token := invocationTokenFromMetadata(ctx)
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, access.ErrNotAuthenticated.Error())
	}
	if s == nil || s.tokens == nil {
		return nil, status.Error(codes.FailedPrecondition, "invocation tokens are not configured")
	}
	tokenCtx, err := s.tokens.ResolveToken(token, "")
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	p := tokenCtx.Principal()
	if access.SubjectFromPrincipal(p) == nil {
		return nil, status.Error(codes.Unauthenticated, access.ErrNotAuthenticated.Error())
	}
	return p, nil
}

func invocationTokenFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(appaccess.InvocationTokenMetadataKey) {
		if token := strings.TrimSpace(value); token != "" {
			return token
		}
	}
	return ""
}

func accessStatusError(err error) error {
	switch {
	case errors.Is(err, access.ErrNotAuthenticated):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, access.ErrDenied), errors.Is(err, access.ErrScopeDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case access.IsPolicyUnavailable(err):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
