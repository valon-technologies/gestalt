package principal_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/principal"
)

func TestResolveToken_ValidSession(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token == "session-tok" {
				return &core.UserIdentity{Email: "session@example.com"}, nil
			}
			return nil, fmt.Errorf("invalid")
		},
	}
	ds := &coretesting.StubDatastore{}
	r := principal.NewResolver(auth, ds)

	p, err := r.ResolveToken(context.Background(), "session-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Source != principal.SourceSession {
		t.Fatalf("expected SourceSession, got %d", p.Source)
	}
	if p.Identity.Email != "session@example.com" {
		t.Fatalf("expected session@example.com, got %q", p.Identity.Email)
	}
	if p.UserID != "" {
		t.Fatalf("expected empty UserID for session, got %q", p.UserID)
	}
}

func TestResolveToken_ValidAPIToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
			return nil, fmt.Errorf("not a session token")
		},
	}
	ds := &coretesting.StubDatastore{
		ValidateAPITokenFn: func(_ context.Context, _ string) (*core.APIToken, error) {
			return &core.APIToken{UserID: "u1", Name: "my-key"}, nil
		},
		GetUserFn: func(_ context.Context, id string) (*core.User, error) {
			return &core.User{ID: id, Email: "api@example.com", DisplayName: "API User"}, nil
		},
	}
	r := principal.NewResolver(auth, ds)

	p, err := r.ResolveToken(context.Background(), "api-key-plaintext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Source != principal.SourceAPIToken {
		t.Fatalf("expected SourceAPIToken, got %d", p.Source)
	}
	if p.UserID != "u1" {
		t.Fatalf("expected UserID u1, got %q", p.UserID)
	}
	if p.Identity.Email != "api@example.com" {
		t.Fatalf("expected api@example.com, got %q", p.Identity.Email)
	}
}

func TestResolveToken_InvalidToken(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{
		N: "test",
		ValidateTokenFn: func(_ context.Context, _ string) (*core.UserIdentity, error) {
			return nil, fmt.Errorf("invalid")
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
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestResolveEmail(t *testing.T) {
	t.Parallel()

	auth := &coretesting.StubAuthProvider{N: "test"}
	ds := &coretesting.StubDatastore{}
	r := principal.NewResolver(auth, ds)

	p := r.ResolveEmail("dev@example.com")
	if p.Source != principal.SourceEnv {
		t.Fatalf("expected SourceEnv, got %d", p.Source)
	}
	if p.Identity.Email != "dev@example.com" {
		t.Fatalf("expected dev@example.com, got %q", p.Identity.Email)
	}
}

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "ctx@example.com"},
		Source:   principal.SourceSession,
	}
	ctx := principal.WithPrincipal(context.Background(), p)
	got := principal.FromContext(ctx)
	if got != p {
		t.Fatal("expected same principal from context")
	}
}

func TestFromContext_Empty(t *testing.T) {
	t.Parallel()

	got := principal.FromContext(context.Background())
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestHashToken(t *testing.T) {
	t.Parallel()

	h := principal.HashToken("test")
	if len(h) != 64 {
		t.Fatalf("expected 64 char hex, got len %d", len(h))
	}
	if principal.HashToken("test") != h {
		t.Fatal("expected deterministic hash")
	}
	if principal.HashToken("other") == h {
		t.Fatal("different inputs should produce different hashes")
	}
}
