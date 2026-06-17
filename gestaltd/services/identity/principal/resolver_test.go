package principal

import (
	"context"
	"errors"
	"testing"
	"time"

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
