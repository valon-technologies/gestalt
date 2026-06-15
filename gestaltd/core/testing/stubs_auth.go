package coretesting

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type StubAuthProvider struct {
	N             string
	AuthorizeFn   func(context.Context, *core.AuthorizeRequest) (*core.AuthorizeResponse, error)
	TokenFn       func(context.Context, *core.TokenRequest) (*core.TokenResponse, error)
	IntrospectFn  func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error)
	ListGrantsFn  func(context.Context, *core.ListGrantsRequest) (*core.ListGrantsResponse, error)
	GetGrantFn    func(context.Context, *core.GetGrantRequest) (*core.GetGrantResponse, error)
	RevokeGrantFn func(context.Context, *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error)

	// Legacy test hooks mapped onto Token/Introspect.
	HandleCallbackFn func(context.Context, string) (*core.UserIdentity, error)
	ValidateTokenFn  func(context.Context, string) (*core.UserIdentity, error)
	LoginURL         string
	lastAuthorize    *core.AuthorizeRequest
}

type StubAuthenticationProvider = StubAuthProvider

func (s *StubAuthProvider) Authorize(ctx context.Context, req *core.AuthorizeRequest) (*core.AuthorizeResponse, error) {
	if s.AuthorizeFn != nil {
		return s.AuthorizeFn(ctx, req)
	}
	if req == nil {
		return nil, fmt.Errorf("authorize request is required")
	}
	s.lastAuthorize = req
	redirect := strings.TrimSpace(s.LoginURL)
	if redirect == "" {
		redirect = "https://idp.example.test/login"
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	if req.State != "" {
		query.Set("state", req.State)
	}
	query.Set("code", "stub-auth-code")
	parsed.RawQuery = query.Encode()
	return &core.AuthorizeResponse{RedirectURI: parsed.String()}, nil
}

func (s *StubAuthProvider) Token(ctx context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
	if s.TokenFn != nil {
		return s.TokenFn(ctx, req)
	}
	if req == nil {
		return nil, fmt.Errorf("token request is required")
	}
	grantType := strings.TrimSpace(req.GrantType)
	if grantType == "" {
		grantType = core.GrantTypeAuthorizationCode
	}
	if grantType == core.GrantTypeTokenExchange {
		intro, err := s.Introspect(ctx, &core.IntrospectRequest{Token: req.SubjectToken})
		if err != nil || intro == nil || !intro.Active {
			return nil, fmt.Errorf("inactive subject token")
		}
		scope := strings.TrimSpace(req.Scope)
		grantID := "grant-exchange"
		return &core.TokenResponse{
			AccessToken: "grant-access-" + grantID,
			TokenType:   "Bearer",
			ExpiresIn:   30 * 24 * 3600,
			GrantID:     grantID,
			Scope:       scope,
		}, nil
	}
	if strings.TrimSpace(req.Code) == "" {
		return nil, fmt.Errorf("authorization code is required")
	}
	if req.Code == "stub-auth-code" {
		grantID := "grant-stub"
		scope := ""
		if s.lastAuthorize != nil {
			scope = strings.TrimSpace(s.lastAuthorize.Scope)
		}
		return &core.TokenResponse{
			AccessToken: "grant-access-" + grantID,
			TokenType:   "Bearer",
			ExpiresIn:   30 * 24 * 3600,
			GrantID:     grantID,
			Scope:       scope,
		}, nil
	}
	if s.HandleCallbackFn != nil {
		identity, err := s.HandleCallbackFn(ctx, req.Code)
		if err != nil {
			return nil, err
		}
		token := "dev-token"
		if identity != nil && identity.Email != "" {
			token = "dev-token-" + identity.Email
		}
		return &core.TokenResponse{
			AccessToken: token,
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			GrantID:     "grant-" + req.Code,
		}, nil
	}
	if req.Code == "valid-code" || req.Code == "good-code" {
		return &core.TokenResponse{
			AccessToken: "valid-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			GrantID:     "grant-valid",
		}, nil
	}
	return nil, fmt.Errorf("invalid authorization code")
}

func (s *StubAuthProvider) Introspect(ctx context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
	if s.IntrospectFn != nil {
		return s.IntrospectFn(ctx, req)
	}
	if req == nil || strings.TrimSpace(req.Token) == "" {
		return &core.IntrospectResponse{Active: false}, nil
	}
	if s.ValidateTokenFn != nil {
		identity, err := s.ValidateTokenFn(ctx, req.Token)
		if err != nil || identity == nil {
			return &core.IntrospectResponse{Active: false}, nil
		}
		subject := "user:test"
		if identity.Email != "" {
			subject = "user:" + strings.TrimSpace(identity.Email)
		}
		return &core.IntrospectResponse{
			Active:   true,
			Subject:  subject,
			ClientID: core.DefaultOAuthClientID,
		}, nil
	}
	switch req.Token {
	case "valid-token", "valid-cookie-token", "valid-header-token":
		return &core.IntrospectResponse{
			Active:   true,
			Subject:  "user:test",
			ClientID: core.DefaultOAuthClientID,
		}, nil
	default:
		if strings.HasPrefix(req.Token, "dev-token-") {
			email := strings.TrimPrefix(req.Token, "dev-token-")
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:" + email,
				ClientID: core.DefaultOAuthClientID,
			}, nil
		}
		return &core.IntrospectResponse{Active: false}, nil
	}
}

func (s *StubAuthProvider) ListGrants(ctx context.Context, req *core.ListGrantsRequest) (*core.ListGrantsResponse, error) {
	if s.ListGrantsFn != nil {
		return s.ListGrantsFn(ctx, req)
	}
	return &core.ListGrantsResponse{}, nil
}

func (s *StubAuthProvider) GetGrant(ctx context.Context, req *core.GetGrantRequest) (*core.GetGrantResponse, error) {
	if s.GetGrantFn != nil {
		return s.GetGrantFn(ctx, req)
	}
	return &core.GetGrantResponse{}, nil
}

func (s *StubAuthProvider) RevokeGrant(ctx context.Context, req *core.RevokeGrantRequest) (*core.RevokeGrantResponse, error) {
	if s.RevokeGrantFn != nil {
		return s.RevokeGrantFn(ctx, req)
	}
	return &core.RevokeGrantResponse{}, nil
}
