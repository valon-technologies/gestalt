package invocation_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/invocation"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/registry"
	"github.com/valon-technologies/toolshed/internal/testutil"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation {
	return s.ops
}

func TestInvoke_Success(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"op":%q,"token":%q}`, op, token),
				}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{AccessToken: "stored-access-token"}, nil
		},
	}

	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
		Source:   principal.SourceSession,
	}

	result, err := b.Invoke(context.Background(), p, "test-int", "do_thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestInvoke_ProviderNotFound(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(&reg.Providers, ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "nonexistent", "op", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestInvoke_OperationNotFound(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "nonexistent", nil)
	if !errors.Is(err, invocation.ErrOperationNotFound) {
		t.Fatalf("expected ErrOperationNotFound, got %v", err)
	}
}

func TestInvoke_NilPrincipal(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	_, err := b.Invoke(context.Background(), nil, "test-int", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNotAuthenticated) {
		t.Fatalf("expected ErrNotAuthenticated, got %v", err)
	}
}

func TestInvoke_NoStoredToken(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestInvoke_UserIDResolution(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test-int",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "resolved-id", Email: email}, nil
		},
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{AccessToken: "tok"}, nil
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		Source:   principal.SourceSession,
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "do_thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.UserID != "resolved-id" {
		t.Fatalf("expected UserID to be resolved, got %q", p.UserID)
	}
}

func TestInvoke_NilTokenResponse(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test-int"},
		ops:             []core.Operation{{Name: "do_thing", Method: "GET"}},
	}

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return nil, nil
		},
	}
	b := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)

	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "user@example.com"},
		UserID:   "u1",
	}

	_, err := b.Invoke(context.Background(), p, "test-int", "do_thing", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}
