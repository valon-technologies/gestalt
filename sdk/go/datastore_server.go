package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type DatastoreServer struct {
	proto.UnimplementedDatastorePluginServer
	store DatastoreProvider
}

func NewDatastoreProviderServer(store DatastoreProvider) *DatastoreServer {
	return &DatastoreServer{store: store}
}

func (s *DatastoreServer) Migrate(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.store.Migrate(ctx); err != nil {
		return nil, providerRPCError("migrate", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) GetUser(ctx context.Context, req *proto.GetUserRequest) (*proto.StoredUser, error) {
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
	return storedUserToProto(user), nil
}

func (s *DatastoreServer) FindOrCreateUser(ctx context.Context, req *proto.FindOrCreateUserRequest) (*proto.StoredUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.store.FindOrCreateUser(ctx, req.GetEmail())
	if err != nil {
		return nil, providerRPCError("find or create user", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "datastore plugin returned nil user")
	}
	return storedUserToProto(user), nil
}

func (s *DatastoreServer) PutStoredIntegrationToken(ctx context.Context, req *proto.StoredIntegrationToken) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.PutIntegrationToken(ctx, storedIntegrationTokenFromProto(req)); err != nil {
		return nil, providerRPCError("put integration token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) GetStoredIntegrationToken(ctx context.Context, req *proto.GetStoredIntegrationTokenRequest) (*proto.StoredIntegrationToken, error) {
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
	return storedIntegrationTokenToProto(token), nil
}

func (s *DatastoreServer) ListStoredIntegrationTokens(ctx context.Context, req *proto.ListStoredIntegrationTokensRequest) (*proto.ListStoredIntegrationTokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokens, err := s.store.ListIntegrationTokens(ctx, req.GetUserId(), req.GetIntegration(), req.GetConnection())
	if err != nil {
		return nil, providerRPCError("list integration tokens", err)
	}
	protoTokens := make([]*proto.StoredIntegrationToken, len(tokens))
	for i, token := range tokens {
		protoTokens[i] = storedIntegrationTokenToProto(token)
	}
	return &proto.ListStoredIntegrationTokensResponse{Tokens: protoTokens}, nil
}

func (s *DatastoreServer) DeleteStoredIntegrationToken(ctx context.Context, req *proto.DeleteStoredIntegrationTokenRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.DeleteIntegrationToken(ctx, req.GetId()); err != nil {
		return nil, providerRPCError("delete integration token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) PutAPIToken(ctx context.Context, req *proto.StoredAPIToken) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.PutAPIToken(ctx, storedAPITokenFromProto(req)); err != nil {
		return nil, providerRPCError("put api token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) GetAPITokenByHash(ctx context.Context, req *proto.GetAPITokenByHashRequest) (*proto.StoredAPIToken, error) {
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
	return storedAPITokenToProto(token), nil
}

func (s *DatastoreServer) ListAPITokens(ctx context.Context, req *proto.ListAPITokensRequest) (*proto.ListAPITokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tokens, err := s.store.ListAPITokens(ctx, req.GetUserId())
	if err != nil {
		return nil, providerRPCError("list api tokens", err)
	}
	protoTokens := make([]*proto.StoredAPIToken, len(tokens))
	for i, token := range tokens {
		protoTokens[i] = storedAPITokenToProto(token)
	}
	return &proto.ListAPITokensResponse{Tokens: protoTokens}, nil
}

func (s *DatastoreServer) RevokeAPIToken(ctx context.Context, req *proto.RevokeAPITokenRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.store.RevokeAPIToken(ctx, req.GetUserId(), req.GetId()); err != nil {
		return nil, providerRPCError("revoke api token", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) RevokeAllAPITokens(ctx context.Context, req *proto.RevokeAllAPITokensRequest) (*proto.RevokeAllAPITokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	revoked, err := s.store.RevokeAllAPITokens(ctx, req.GetUserId())
	if err != nil {
		return nil, providerRPCError("revoke all api tokens", err)
	}
	return &proto.RevokeAllAPITokensResponse{Revoked: revoked}, nil
}

func (s *DatastoreServer) GetOAuthRegistration(ctx context.Context, req *proto.GetOAuthRegistrationRequest) (*proto.OAuthRegistration, error) {
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
	return oauthRegistrationToProto(registration), nil
}

func (s *DatastoreServer) PutOAuthRegistration(ctx context.Context, req *proto.OAuthRegistration) (*emptypb.Empty, error) {
	store, ok := s.store.(OAuthRegistrationStore)
	if !ok {
		return nil, providerRPCError("put oauth registration", ErrOAuthRegistrationStoreUnsupported)
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := store.PutOAuthRegistration(ctx, oauthRegistrationFromProto(req)); err != nil {
		return nil, providerRPCError("put oauth registration", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DatastoreServer) DeleteOAuthRegistration(ctx context.Context, req *proto.DeleteOAuthRegistrationRequest) (*emptypb.Empty, error) {
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

func storedUserToProto(user *StoredUser) *proto.StoredUser {
	if user == nil {
		return nil
	}
	return &proto.StoredUser{
		Id:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		CreatedAt:   timeToProto(user.CreatedAt),
		UpdatedAt:   timeToProto(user.UpdatedAt),
	}
}

func storedIntegrationTokenToProto(token *StoredIntegrationToken) *proto.StoredIntegrationToken {
	if token == nil {
		return nil
	}
	return &proto.StoredIntegrationToken{
		Id:                 token.ID,
		UserId:             token.UserID,
		Integration:        token.Integration,
		Connection:         token.Connection,
		Instance:           token.Instance,
		AccessTokenSealed:  append([]byte(nil), token.AccessTokenSealed...),
		RefreshTokenSealed: append([]byte(nil), token.RefreshTokenSealed...),
		Scopes:             token.Scopes,
		ExpiresAt:          timePtrToProto(token.ExpiresAt),
		LastRefreshedAt:    timePtrToProto(token.LastRefreshedAt),
		RefreshErrorCount:  token.RefreshErrorCount,
		ConnectionParams:   cloneStringMap(token.ConnectionParams),
		CreatedAt:          timeToProto(token.CreatedAt),
		UpdatedAt:          timeToProto(token.UpdatedAt),
	}
}

func storedIntegrationTokenFromProto(token *proto.StoredIntegrationToken) *StoredIntegrationToken {
	if token == nil {
		return nil
	}
	return &StoredIntegrationToken{
		ID:                 token.GetId(),
		UserID:             token.GetUserId(),
		Integration:        token.GetIntegration(),
		Connection:         token.GetConnection(),
		Instance:           token.GetInstance(),
		AccessTokenSealed:  append([]byte(nil), token.GetAccessTokenSealed()...),
		RefreshTokenSealed: append([]byte(nil), token.GetRefreshTokenSealed()...),
		Scopes:             token.GetScopes(),
		ExpiresAt:          protoToTimePtr(token.GetExpiresAt()),
		LastRefreshedAt:    protoToTimePtr(token.GetLastRefreshedAt()),
		RefreshErrorCount:  token.GetRefreshErrorCount(),
		ConnectionParams:   cloneStringMap(token.GetConnectionParams()),
		CreatedAt:          protoToTime(token.GetCreatedAt()),
		UpdatedAt:          protoToTime(token.GetUpdatedAt()),
	}
}

func storedAPITokenToProto(token *StoredAPIToken) *proto.StoredAPIToken {
	if token == nil {
		return nil
	}
	return &proto.StoredAPIToken{
		Id:          token.ID,
		UserId:      token.UserID,
		Name:        token.Name,
		HashedToken: token.HashedToken,
		Scopes:      token.Scopes,
		ExpiresAt:   timePtrToProto(token.ExpiresAt),
		CreatedAt:   timeToProto(token.CreatedAt),
		UpdatedAt:   timeToProto(token.UpdatedAt),
	}
}

func storedAPITokenFromProto(token *proto.StoredAPIToken) *StoredAPIToken {
	if token == nil {
		return nil
	}
	return &StoredAPIToken{
		ID:          token.GetId(),
		UserID:      token.GetUserId(),
		Name:        token.GetName(),
		HashedToken: token.GetHashedToken(),
		Scopes:      token.GetScopes(),
		ExpiresAt:   protoToTimePtr(token.GetExpiresAt()),
		CreatedAt:   protoToTime(token.GetCreatedAt()),
		UpdatedAt:   protoToTime(token.GetUpdatedAt()),
	}
}

func oauthRegistrationToProto(registration *OAuthRegistration) *proto.OAuthRegistration {
	if registration == nil {
		return nil
	}
	return &proto.OAuthRegistration{
		AuthServerUrl:         registration.AuthServerURL,
		RedirectUri:           registration.RedirectURI,
		ClientId:              registration.ClientID,
		ClientSecretSealed:    append([]byte(nil), registration.ClientSecretSealed...),
		ExpiresAt:             timePtrToProto(registration.ExpiresAt),
		AuthorizationEndpoint: registration.AuthorizationEndpoint,
		TokenEndpoint:         registration.TokenEndpoint,
		ScopesSupported:       registration.ScopesSupported,
		DiscoveredAt:          timeToProto(registration.DiscoveredAt),
	}
}

func oauthRegistrationFromProto(registration *proto.OAuthRegistration) *OAuthRegistration {
	if registration == nil {
		return nil
	}
	return &OAuthRegistration{
		AuthServerURL:         registration.GetAuthServerUrl(),
		RedirectURI:           registration.GetRedirectUri(),
		ClientID:              registration.GetClientId(),
		ClientSecretSealed:    append([]byte(nil), registration.GetClientSecretSealed()...),
		ExpiresAt:             protoToTimePtr(registration.GetExpiresAt()),
		AuthorizationEndpoint: registration.GetAuthorizationEndpoint(),
		TokenEndpoint:         registration.GetTokenEndpoint(),
		ScopesSupported:       registration.GetScopesSupported(),
		DiscoveredAt:          protoToTime(registration.GetDiscoveredAt()),
	}
}
