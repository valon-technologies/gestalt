package principal

import (
	"context"
	"errors"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestPermissionSetFromScopesExplicitEmptyDeniesAll(t *testing.T) {
	t.Parallel()

	perms := PermissionSetFromScopes(nil)
	if perms != nil {
		t.Fatalf("permissions = %#v, want nil for empty scopes", perms)
	}
}

func TestResolveTokenPropagatesIntrospectError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider unavailable")
	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return nil, wantErr
		},
	}
	resolver := NewResolver(auth)

	_, err := resolver.ResolveToken(context.Background(), "token")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ResolveToken() error = %v, want %v", err, wantErr)
	}
}

func TestResolveTokenInactiveTokenReturnsInvalidToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: false}, nil
		},
	}
	resolver := NewResolver(auth)

	_, err := resolver.ResolveToken(context.Background(), "token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResolveToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestResolveTokenInactiveDoesNotFallbackToHostSessionJWT(t *testing.T) {
	t.Parallel()

	secret := []byte("resolver-test-session-secret")
	jwt, err := session.IssueToken(&core.UserIdentity{
		Email:       "user@example.com",
		DisplayName: "User",
	}, secret, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: false}, nil
		},
	}
	resolver := NewResolver(auth)

	_, err = resolver.ResolveToken(context.Background(), jwt)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResolveToken() error = %v, want ErrInvalidToken without session fallback", err)
	}
}

func TestResolveTokenActiveValidSubjectSucceeds(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:someone@example.com",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
	}
	resolver := NewResolver(auth)

	p, err := resolver.ResolveToken(context.Background(), "token")
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if p == nil || p.SubjectID != "user:someone@example.com" {
		t.Fatalf("principal = %+v, want user:someone@example.com", p)
	}
}

func TestResolveTokenEnrichesPrincipalWithUserInfo(t *testing.T) {
	t.Parallel()

	const accessToken = "session-access-token"
	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req.Token != accessToken {
				return &core.IntrospectResponse{Active: false}, nil
			}
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:someone@example.com",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
		UserInfoFn: func(ctx context.Context, _ *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			call := gestalt.AuthCallContextFromContext(ctx)
			if call.CallerBearerToken != accessToken {
				t.Fatalf("UserInfo() caller bearer token = %q, want %q", call.CallerBearerToken, accessToken)
			}
			return &core.UserInfoResponse{
				SubjectID: "user:someone@example.com",
				Email:     "someone@example.com",
				Name:      "Someone Example",
			}, nil
		},
	}
	resolver := NewResolver(auth)

	p, err := resolver.ResolveToken(context.Background(), accessToken)
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if p == nil || p.Identity == nil {
		t.Fatalf("principal = %+v, want identity", p)
	}
	if p.Identity.Email != "someone@example.com" {
		t.Fatalf("email = %q, want someone@example.com", p.Identity.Email)
	}
	if p.Identity.DisplayName != "Someone Example" {
		t.Fatalf("display name = %q, want Someone Example", p.Identity.DisplayName)
	}
}

func TestResolveTokenUserInfoNotFoundContinues(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:someone@example.com",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
		UserInfoFn: func(context.Context, *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			return nil, core.ErrNotFound
		},
	}
	resolver := NewResolver(auth)

	p, err := resolver.ResolveToken(context.Background(), "token")
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if p == nil || p.SubjectID != "user:someone@example.com" {
		t.Fatalf("principal = %+v, want user:someone@example.com", p)
	}
	if p.Identity != nil && p.Identity.DisplayName != "" {
		t.Fatalf("display name = %q, want empty when userinfo is missing", p.Identity.DisplayName)
	}
}

func TestResolveTokenUserInfoErrorContinues(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:someone@example.com",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
		UserInfoFn: func(context.Context, *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			return nil, errors.New("provider unavailable")
		},
	}
	resolver := NewResolver(auth)

	p, err := resolver.ResolveToken(context.Background(), "token")
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if p == nil || p.SubjectID != "user:someone@example.com" {
		t.Fatalf("principal = %+v, want user:someone@example.com", p)
	}
}

func TestResolveTokenUserInfoSubjectMismatchIgnored(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:someone@example.com",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
		UserInfoFn: func(context.Context, *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			return &core.UserInfoResponse{
				SubjectID: "user:other@example.com",
				Email:     "other@example.com",
				Name:      "Other Example",
			}, nil
		},
	}
	resolver := NewResolver(auth)

	p, err := resolver.ResolveToken(context.Background(), "token")
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if p == nil || p.SubjectID != "user:someone@example.com" {
		t.Fatalf("principal = %+v, want user:someone@example.com", p)
	}
	if p.Identity != nil && p.Identity.DisplayName == "Other Example" {
		t.Fatal("expected mismatched userinfo to be ignored")
	}
}

func TestResolveTokenActiveEmptySubjectReturnsInvalidToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: true, Subject: "   "}, nil
		},
	}
	resolver := NewResolver(auth)

	_, err := resolver.ResolveToken(context.Background(), "token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResolveToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestResolveTokenActiveMalformedSubjectReturnsInvalidToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: true, Subject: "raw-oidc-sub"}, nil
		},
	}
	resolver := NewResolver(auth)

	_, err := resolver.ResolveToken(context.Background(), "token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("ResolveToken() error = %v, want ErrInvalidToken", err)
	}
}
