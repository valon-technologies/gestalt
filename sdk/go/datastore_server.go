package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type datastoreServer struct {
	proto.UnimplementedDatastoreProviderServer
	store DatastoreProvider
}

func newDatastoreProviderServer(store DatastoreProvider) *datastoreServer {
	return &datastoreServer{store: store}
}

func (s *datastoreServer) Migrate(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.store.Migrate(ctx); err != nil {
		return nil, providerRPCError("migrate", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) GetUser(ctx context.Context, req *proto.GetUserRequest) (*proto.StoredUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.store.GetUser(ctx, req.GetId())
	if err != nil {
		return nil, providerRPCError("get user", err)
	}
	if user == nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return user, nil
}

func (s *datastoreServer) FindOrCreateUser(ctx context.Context, req *proto.FindOrCreateUserRequest) (*proto.StoredUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.store.FindOrCreateUser(ctx, req.GetEmail())
	if err != nil {
		return nil, providerRPCError("find or create user", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "datastore provider returned nil user")
	}
	return user, nil
}

func (s *datastoreServer) PutStoredIntegrationToken(ctx context.Context, req *proto.StoredIntegrationToken) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.PutIntegrationToken(ctx, req); err != nil {
		return nil, providerRPCError("put integration token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) GetStoredIntegrationToken(ctx context.Context, req *proto.GetStoredIntegrationTokenRequest) (*proto.StoredIntegrationToken, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	token, err := s.store.GetIntegrationToken(ctx, req.GetUserId(), req.GetIntegration(), req.GetConnection(), req.GetInstance())
	if err != nil {
		return nil, providerRPCError("get integration token", err)
	}
	if token == nil {
		return nil, status.Error(codes.NotFound, "integration token not found")
	}
	return token, nil
}

func (s *datastoreServer) ListStoredIntegrationTokens(ctx context.Context, req *proto.ListStoredIntegrationTokensRequest) (*proto.ListStoredIntegrationTokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokens, err := s.store.ListIntegrationTokens(ctx, req.GetUserId(), req.GetIntegration(), req.GetConnection())
	if err != nil {
		return nil, providerRPCError("list integration tokens", err)
	}
	return &proto.ListStoredIntegrationTokensResponse{Tokens: tokens}, nil
}

func (s *datastoreServer) DeleteStoredIntegrationToken(ctx context.Context, req *proto.DeleteStoredIntegrationTokenRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.DeleteIntegrationToken(ctx, req.GetId()); err != nil {
		return nil, providerRPCError("delete integration token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) PutAPIToken(ctx context.Context, req *proto.StoredAPIToken) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.PutAPIToken(ctx, req); err != nil {
		return nil, providerRPCError("put api token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) GetAPITokenByHash(ctx context.Context, req *proto.GetAPITokenByHashRequest) (*proto.StoredAPIToken, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	token, err := s.store.GetAPITokenByHash(ctx, req.GetHashedToken())
	if err != nil {
		return nil, providerRPCError("get api token by hash", err)
	}
	if token == nil {
		return nil, status.Error(codes.NotFound, "api token not found")
	}
	return token, nil
}

func (s *datastoreServer) ListAPITokens(ctx context.Context, req *proto.ListAPITokensRequest) (*proto.ListAPITokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokens, err := s.store.ListAPITokens(ctx, req.GetUserId())
	if err != nil {
		return nil, providerRPCError("list api tokens", err)
	}
	return &proto.ListAPITokensResponse{Tokens: tokens}, nil
}

func (s *datastoreServer) RevokeAPIToken(ctx context.Context, req *proto.RevokeAPITokenRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.RevokeAPIToken(ctx, req.GetUserId(), req.GetId()); err != nil {
		return nil, providerRPCError("revoke api token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) RevokeAllAPITokens(ctx context.Context, req *proto.RevokeAllAPITokensRequest) (*proto.RevokeAllAPITokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	revoked, err := s.store.RevokeAllAPITokens(ctx, req.GetUserId())
	if err != nil {
		return nil, providerRPCError("revoke all api tokens", err)
	}
	return &proto.RevokeAllAPITokensResponse{Revoked: revoked}, nil
}

func (s *datastoreServer) GetOAuthRegistration(ctx context.Context, req *proto.GetOAuthRegistrationRequest) (*proto.OAuthRegistration, error) {
	store, ok := s.store.(OAuthRegistrationStore)
	if !ok {
		return nil, providerRPCError("get oauth registration", ErrOAuthRegistrationStoreUnsupported)
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	registration, err := store.GetOAuthRegistration(ctx, req.GetAuthServerUrl(), req.GetRedirectUri())
	if err != nil {
		return nil, providerRPCError("get oauth registration", err)
	}
	if registration == nil {
		return nil, status.Error(codes.NotFound, "oauth registration not found")
	}
	return registration, nil
}

func (s *datastoreServer) PutOAuthRegistration(ctx context.Context, req *proto.OAuthRegistration) (*emptypb.Empty, error) {
	store, ok := s.store.(OAuthRegistrationStore)
	if !ok {
		return nil, providerRPCError("put oauth registration", ErrOAuthRegistrationStoreUnsupported)
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := store.PutOAuthRegistration(ctx, req); err != nil {
		return nil, providerRPCError("put oauth registration", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *datastoreServer) DeleteOAuthRegistration(ctx context.Context, req *proto.DeleteOAuthRegistrationRequest) (*emptypb.Empty, error) {
	store, ok := s.store.(OAuthRegistrationStore)
	if !ok {
		return nil, providerRPCError("delete oauth registration", ErrOAuthRegistrationStoreUnsupported)
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := store.DeleteOAuthRegistration(ctx, req.GetAuthServerUrl(), req.GetRedirectUri()); err != nil {
		return nil, providerRPCError("delete oauth registration", err)
	}
	return &emptypb.Empty{}, nil
}
