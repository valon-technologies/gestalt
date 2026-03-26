package principal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/principal"
)

func TestResolveToken(t *testing.T) {
	t.Parallel()

	t.Run("session token", func(t *testing.T) {
		t.Parallel()

		auth := &coretesting.StubAuthProvider{
			N: "auth-provider",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "session-token" {
					t.Fatalf("ValidateToken called with %q", token)
				}
				return &core.UserIdentity{Email: "session-user@example.test"}, nil
			},
		}
		r := principal.NewResolver(auth, &coretesting.StubDatastore{})

		p, err := r.ResolveToken(context.Background(), "session-token")
		if err != nil {
			t.Fatalf("ResolveToken: %v", err)
		}
		if p.Source != principal.SourceSession {
			t.Fatalf("Source = %v, want %v", p.Source, principal.SourceSession)
		}
		if p.Identity == nil || p.Identity.Email != "session-user@example.test" {
			t.Fatalf("Identity = %+v", p.Identity)
		}
		if p.UserID != "" {
			t.Fatalf("UserID = %q, want empty", p.UserID)
		}
	})

	t.Run("api token", func(t *testing.T) {
		t.Parallel()

		auth := &coretesting.StubAuthProvider{
			N: "auth-provider",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, errors.New("session token rejected")
			},
		}
		ds := &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, hashed string) (*core.APIToken, error) {
				if hashed == "" {
					t.Fatal("expected hashed token")
				}
				return &core.APIToken{UserID: "user-123", Name: "api-token"}, nil
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				if id != "user-123" {
					t.Fatalf("GetUser called with %q", id)
				}
				return &core.User{ID: id, Email: "api-user@example.test", DisplayName: "API User"}, nil
			},
		}
		r := principal.NewResolver(auth, ds)

		p, err := r.ResolveToken(context.Background(), "api-token")
		if err != nil {
			t.Fatalf("ResolveToken: %v", err)
		}
		if p.Source != principal.SourceAPIToken {
			t.Fatalf("Source = %v, want %v", p.Source, principal.SourceAPIToken)
		}
		if p.UserID != "user-123" {
			t.Fatalf("UserID = %q, want user-123", p.UserID)
		}
		if p.Identity == nil || p.Identity.Email != "api-user@example.test" {
			t.Fatalf("Identity = %+v", p.Identity)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		t.Parallel()

		auth := &coretesting.StubAuthProvider{
			N: "auth-provider",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, errors.New("session token rejected")
			},
		}
		ds := &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, _ string) (*core.APIToken, error) {
				return nil, core.ErrNotFound
			},
		}
		r := principal.NewResolver(auth, ds)

		_, err := r.ResolveToken(context.Background(), "bad-token")
		if !errors.Is(err, principal.ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken", err)
		}
	})
}
