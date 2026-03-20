package invocation_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"
)

func newConnectionModeBroker(t *testing.T, prov core.Provider, ds *coretesting.StubDatastore) *invocation.Broker {
	t.Helper()
	return invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)
}

func connectionModeProvider(name string, mode core.ConnectionMode) *stubProviderWithOps {
	return &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			ConnMode: mode,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: token}, nil
			},
		},
		ops: []core.Operation{{Name: "do", Method: "POST"}},
	}
}

func TestInvoke_ConnectionModeIdentity(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal && integration == "svc" && instance == "default" {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeIdentity), ds)
	result, err := b.Invoke(context.Background(), &principal.Principal{}, "svc", "do", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Fatalf("expected identity-tok, got %q", result.Body)
	}
}

func TestInvoke_ConnectionModeIdentity_NoToken(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(context.Context, string, string, string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeIdentity), ds)
	_, err := b.Invoke(context.Background(), &principal.Principal{}, "svc", "do", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestInvoke_ConnectionModeEither_PrefersUser(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			switch userID {
			case "user-1":
				return &core.IntegrationToken{AccessToken: "user-tok"}, nil
			case principal.IdentityPrincipal:
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			default:
				return nil, core.ErrNotFound
			}
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeEither), ds)
	result, err := b.Invoke(context.Background(), &principal.Principal{UserID: "user-1"}, "svc", "do", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "user-tok" {
		t.Fatalf("expected user-tok, got %q", result.Body)
	}
}

func TestInvoke_ConnectionModeEither_FallsBackToIdentity(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeEither), ds)
	result, err := b.Invoke(context.Background(), &principal.Principal{UserID: "user-1"}, "svc", "do", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Fatalf("expected identity-tok, got %q", result.Body)
	}
}

func TestInvoke_ConnectionModeEither_EmptyPrincipalFallsBackToIdentity(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeEither), ds)
	result, err := b.Invoke(context.Background(), &principal.Principal{}, "svc", "do", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Fatalf("expected identity-tok, got %q", result.Body)
	}
}

func TestInvoke_ConnectionModeEither_NoTokens(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(context.Context, string, string, string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("svc", core.ConnectionModeEither), ds)
	_, err := b.Invoke(context.Background(), &principal.Principal{UserID: "user-1"}, "svc", "do", nil)
	if !errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestInvoke_ConnectionModeEither_InfraErrorNotSwallowed(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _ string, _ string) (*core.IntegrationToken, error) {
			if userID == "user-1" {
				return nil, fmt.Errorf("database connection refused")
			}
			return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
		},
	}

	b := newConnectionModeBroker(t, connectionModeProvider("alpha", core.ConnectionModeEither), ds)
	_, err := b.Invoke(context.Background(), &principal.Principal{UserID: "user-1"}, "alpha", "do", nil)
	if err == nil {
		t.Fatal("expected infrastructure error")
	}
	if errors.Is(err, invocation.ErrNoToken) {
		t.Fatalf("expected infrastructure error, got %v", err)
	}
}
