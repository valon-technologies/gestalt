package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// AuthenticatedUser is the authenticated principal returned by an
// authentication provider.
type AuthenticatedUser struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarUrl     string
	Claims        map[string]string
}

// GetSubject returns the subject field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetSubject() string {
	if u == nil {
		return ""
	}
	return u.Subject
}

// GetEmail returns the email field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetEmail() string {
	if u == nil {
		return ""
	}
	return u.Email
}

// GetEmailVerified returns the email verified field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetEmailVerified() bool {
	if u == nil {
		return false
	}
	return u.EmailVerified
}

// GetDisplayName returns the display name field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetDisplayName() string {
	if u == nil {
		return ""
	}
	return u.DisplayName
}

// GetAvatarUrl returns the avatar url field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetAvatarUrl() string {
	if u == nil {
		return ""
	}
	return u.AvatarUrl
}

// GetClaims returns the claims field; it is safe to call on a nil receiver.
func (u *AuthenticatedUser) GetClaims() map[string]string {
	if u == nil {
		return nil
	}
	return u.Claims
}

// BeginLoginRequest starts an interactive provider login flow.
type BeginLoginRequest struct {
	CallbackUrl string
	HostState   string
	Scopes      []string
	Options     map[string]string
}

// GetCallbackUrl returns the callback url field; it is safe to call on a nil receiver.
func (r *BeginLoginRequest) GetCallbackUrl() string {
	if r == nil {
		return ""
	}
	return r.CallbackUrl
}

// GetHostState returns the host state field; it is safe to call on a nil receiver.
func (r *BeginLoginRequest) GetHostState() string {
	if r == nil {
		return ""
	}
	return r.HostState
}

// GetScopes returns the scopes field; it is safe to call on a nil receiver.
func (r *BeginLoginRequest) GetScopes() []string {
	if r == nil {
		return nil
	}
	return r.Scopes
}

// GetOptions returns the options field; it is safe to call on a nil receiver.
func (r *BeginLoginRequest) GetOptions() map[string]string {
	if r == nil {
		return nil
	}
	return r.Options
}

// BeginLoginResponse contains the provider-managed authorization URL and
// opaque state.
type BeginLoginResponse struct {
	AuthorizationUrl string
	ProviderState    []byte
}

// GetAuthorizationUrl returns the authorization url field; it is safe to call on a nil receiver.
func (r *BeginLoginResponse) GetAuthorizationUrl() string {
	if r == nil {
		return ""
	}
	return r.AuthorizationUrl
}

// GetProviderState returns the provider state field; it is safe to call on a nil receiver.
func (r *BeginLoginResponse) GetProviderState() []byte {
	if r == nil {
		return nil
	}
	return r.ProviderState
}

// CompleteLoginRequest finishes an interactive login flow.
type CompleteLoginRequest struct {
	Query         map[string]string
	ProviderState []byte
	CallbackUrl   string
}

// GetQuery returns the query field; it is safe to call on a nil receiver.
func (r *CompleteLoginRequest) GetQuery() map[string]string {
	if r == nil {
		return nil
	}
	return r.Query
}

// GetProviderState returns the provider state field; it is safe to call on a nil receiver.
func (r *CompleteLoginRequest) GetProviderState() []byte {
	if r == nil {
		return nil
	}
	return r.ProviderState
}

// GetCallbackUrl returns the callback url field; it is safe to call on a nil receiver.
func (r *CompleteLoginRequest) GetCallbackUrl() string {
	if r == nil {
		return ""
	}
	return r.CallbackUrl
}

// AuthSessionSettings contains provider-owned authentication session hints.
type AuthSessionSettings struct {
	SessionTtlSeconds int64
}

// GetSessionTtlSeconds returns the session ttl seconds field; it is safe to call on a nil receiver.
func (s *AuthSessionSettings) GetSessionTtlSeconds() int64 {
	if s == nil {
		return 0
	}
	return s.SessionTtlSeconds
}

// AuthenticationProvider serves the Gestalt authentication protocol.
type AuthenticationProvider interface {
	Provider
	BeginLogin(ctx context.Context, req *BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*AuthenticatedUser, error)
}

// ExternalTokenValidator is implemented by authentication providers that can
// validate tokens minted outside the interactive login flow.
type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

// SessionTTLProvider is implemented by authentication providers that want the
// host to persist sessions for a fixed amount of time.
type SessionTTLProvider interface {
	SessionTTL() time.Duration
}

func authenticatedUserToProto(user *AuthenticatedUser) *proto.AuthenticatedUser {
	if user == nil {
		return nil
	}
	return &proto.AuthenticatedUser{
		Subject:       user.Subject,
		Email:         user.Email,
		EmailVerified: user.EmailVerified,
		DisplayName:   user.DisplayName,
		AvatarUrl:     user.AvatarUrl,
		Claims:        cloneStringMap(user.Claims),
	}
}

func beginLoginRequestFromProto(req *proto.BeginLoginRequest) *BeginLoginRequest {
	if req == nil {
		return nil
	}
	return &BeginLoginRequest{
		CallbackUrl: req.GetCallbackUrl(),
		HostState:   req.GetHostState(),
		Scopes:      append([]string(nil), req.GetScopes()...),
		Options:     cloneStringMap(req.GetOptions()),
	}
}

func beginLoginResponseToProto(resp *BeginLoginResponse) *proto.BeginLoginResponse {
	if resp == nil {
		return nil
	}
	return &proto.BeginLoginResponse{
		AuthorizationUrl: resp.AuthorizationUrl,
		ProviderState:    append([]byte(nil), resp.ProviderState...),
	}
}

func completeLoginRequestFromProto(req *proto.CompleteLoginRequest) *CompleteLoginRequest {
	if req == nil {
		return nil
	}
	return &CompleteLoginRequest{
		Query:         cloneStringMap(req.GetQuery()),
		ProviderState: append([]byte(nil), req.GetProviderState()...),
		CallbackUrl:   req.GetCallbackUrl(),
	}
}
