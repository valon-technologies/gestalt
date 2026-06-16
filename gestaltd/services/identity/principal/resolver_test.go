package principal

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
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
