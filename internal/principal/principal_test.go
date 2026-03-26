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

	t.Run("prefixed api token", func(t *testing.T) {
		t.Parallel()

		plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}

		auth := &coretesting.StubAuthProvider{N: "auth-provider"}
		ds := &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "user-123", Name: "api-token"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				if id != "user-123" {
					t.Fatalf("GetUser called with %q", id)
				}
				return &core.User{ID: id, Email: "api-user@example.test", DisplayName: "API User"}, nil
			},
		}
		r := principal.NewResolver(auth, ds)

		p, err := r.ResolveToken(context.Background(), plaintext)
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

	t.Run("unprefixed token rejected even if valid in database", func(t *testing.T) {
		t.Parallel()

		auth := &coretesting.StubAuthProvider{
			N: "auth-provider",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				return nil, errors.New("not a session")
			},
		}
		ds := &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, _ string) (*core.APIToken, error) {
				return &core.APIToken{UserID: "user-1", Name: "legacy-key"}, nil
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "legacy@example.test"}, nil
			},
		}
		r := principal.NewResolver(auth, ds)

		_, err := r.ResolveToken(context.Background(), "unprefixed-legacy-token")
		if !errors.Is(err, principal.ErrInvalidToken) {
			t.Fatalf("err = %v, want ErrInvalidToken (unprefixed tokens must be rejected)", err)
		}
	})

	t.Run("prefixed egress client token", func(t *testing.T) {
		t.Parallel()

		plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeEgressClient)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}

		auth := &coretesting.StubAuthProvider{N: "auth-provider"}
		ecs := &coretesting.StubDatastore{
			ValidateEgressClientTokenFn: func(_ context.Context, h string) (*core.EgressClientToken, error) {
				if h == hashed {
					return &core.EgressClientToken{ID: "tok-1", ClientID: "ec-1"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetEgressClientFn: func(_ context.Context, id string) (*core.EgressClient, error) {
				if id == "ec-1" {
					return &core.EgressClient{ID: "ec-1", Name: "ci-bot"}, nil
				}
				return nil, core.ErrNotFound
			},
		}
		r := principal.NewResolver(auth, &coretesting.StubDatastore{}, principal.WithEgressClientStore(ecs))

		p, err := r.ResolveToken(context.Background(), plaintext)
		if err != nil {
			t.Fatalf("ResolveToken: %v", err)
		}
		if p.Source != principal.SourceEgressClient {
			t.Fatalf("Source = %v, want SourceEgressClient", p.Source)
		}
		if p.EgressClientID != "ec-1" {
			t.Fatalf("EgressClientID = %q, want ec-1", p.EgressClientID)
		}
	})

	t.Run("prefixed api token skips oauth", func(t *testing.T) {
		t.Parallel()

		plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}

		auth := &coretesting.StubAuthProvider{
			N: "auth-provider",
			ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
				t.Fatal("OAuth should not be called for prefixed API tokens")
				return nil, nil
			},
		}
		ds := &coretesting.StubDatastore{
			ValidateAPITokenFn: func(_ context.Context, h string) (*core.APIToken, error) {
				if h == hashed {
					return &core.APIToken{UserID: "user-1", Name: "key"}, nil
				}
				return nil, core.ErrNotFound
			},
			GetUserFn: func(_ context.Context, id string) (*core.User, error) {
				return &core.User{ID: id, Email: "api@example.test"}, nil
			},
		}
		r := principal.NewResolver(auth, ds)

		p, err := r.ResolveToken(context.Background(), plaintext)
		if err != nil {
			t.Fatalf("ResolveToken: %v", err)
		}
		if p.Source != principal.SourceAPIToken {
			t.Fatalf("Source = %v, want SourceAPIToken", p.Source)
		}
	})
}
