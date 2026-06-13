package gestalt

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// authenticationHandler bridges the ergonomic [AuthenticationProvider] facade
// onto the generated transport handler; wire conversion lives in the generated
// adapter. providerRPCError preserves root sentinel-error mapping.
type authenticationHandler struct {
	client.UnimplementedAuthenticationProvider
	auth AuthenticationProvider
}

func (h authenticationHandler) BeginLogin(ctx context.Context, req *client.BeginLoginRequest) (*client.BeginLoginResponse, error) {
	rootReq := &BeginLoginRequest{
		CallbackUrl: req.CallbackURL,
		HostState:   req.HostState,
		Scopes:      append([]string(nil), req.Scopes...),
		Options:     cloneStringMap(req.Options),
	}
	resp, err := h.auth.BeginLogin(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("begin login", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return &client.BeginLoginResponse{
		AuthorizationURL: resp.AuthorizationUrl,
		ProviderState:    append([]byte(nil), resp.ProviderState...),
	}, nil
}

func (h authenticationHandler) CompleteLogin(ctx context.Context, req *client.CompleteLoginRequest) (*client.AuthenticatedUser, error) {
	rootReq := &CompleteLoginRequest{
		Query:         cloneStringMap(req.Query),
		ProviderState: append([]byte(nil), req.ProviderState...),
		CallbackUrl:   req.CallbackURL,
	}
	user, err := h.auth.CompleteLogin(ctx, rootReq)
	if err != nil {
		return nil, providerRPCError("complete login", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil user")
	}
	return rootAuthenticatedUserToClient(user), nil
}

func (h authenticationHandler) ValidateExternalToken(ctx context.Context, req *client.ValidateExternalTokenRequest) (*client.AuthenticatedUser, error) {
	validator, ok := h.auth.(ExternalTokenValidator)
	if !ok {
		return nil, providerRPCError("validate external token", ErrExternalTokenValidationUnsupported)
	}
	user, err := validator.ValidateExternalToken(ctx, req.Token)
	if err != nil {
		return nil, providerRPCError("validate external token", err)
	}
	if user == nil {
		return nil, status.Error(codes.NotFound, "token not recognized")
	}
	return rootAuthenticatedUserToClient(user), nil
}

func (h authenticationHandler) GetSessionSettings(ctx context.Context) (*client.AuthSessionSettings, error) {
	provider, ok := h.auth.(SessionTTLProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authentication provider does not expose session settings")
	}
	ttl := provider.SessionTTL()
	if ttl < 0 {
		ttl = 0
	}
	return &client.AuthSessionSettings{
		SessionTTLSeconds: int64(ttl / time.Second),
	}, nil
}

func rootAuthenticatedUserToClient(user *AuthenticatedUser) *client.AuthenticatedUser {
	if user == nil {
		return nil
	}
	return &client.AuthenticatedUser{
		Subject:       user.Subject,
		Email:         user.Email,
		EmailVerified: user.EmailVerified,
		DisplayName:   user.DisplayName,
		AvatarURL:     user.AvatarUrl,
		Claims:        cloneStringMap(user.Claims),
	}
}
