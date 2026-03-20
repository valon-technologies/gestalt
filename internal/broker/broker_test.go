package broker_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/broker"
	"github.com/valon-technologies/toolshed/internal/principal"
	"github.com/valon-technologies/toolshed/internal/registry"
)

func newTestBroker(t *testing.T, prov core.Provider, ds *coretesting.StubDatastore) *broker.Broker {
	t.Helper()
	reg := registry.New()
	if err := reg.Providers.Register(prov.Name(), prov); err != nil {
		t.Fatalf("registering provider: %v", err)
	}
	return broker.New(&reg.Providers, ds)
}

func identityProvider(name string, mode core.ConnectionMode) *stubProviderWithOps {
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

func TestResolveToken_Identity(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, integration, instance string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal && integration == "svc" && instance == "default" {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeIdentity), ds)

	result, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Errorf("expected identity-tok, got %q", result.Body)
	}
}

func TestResolveToken_Identity_NoToken(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(context.Context, string, string, string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeIdentity), ds)

	_, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var nce *broker.NoCredentialError
	if !errors.As(err, &nce) {
		t.Fatalf("expected NoCredentialError, got %T: %v", err, err)
	}
}

func TestResolveToken_Either_PrefersUser(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			switch userID {
			case "user-1":
				return &core.IntegrationToken{AccessToken: "user-tok"}, nil
			case principal.IdentityPrincipal:
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeEither), ds)

	result, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "user-tok" {
		t.Errorf("expected user-tok, got %q", result.Body)
	}
}

func TestResolveToken_Either_FallsBackToIdentity(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeEither), ds)

	result, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Errorf("expected identity-tok, got %q", result.Body)
	}
}

func TestResolveToken_Either_NoPrincipal(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _, _ string) (*core.IntegrationToken, error) {
			if userID == principal.IdentityPrincipal {
				return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
			}
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeEither), ds)

	result, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Body != "identity-tok" {
		t.Errorf("expected identity-tok, got %q", result.Body)
	}
}

func TestResolveToken_Either_NoTokens(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(context.Context, string, string, string) (*core.IntegrationToken, error) {
			return nil, core.ErrNotFound
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeEither), ds)

	_, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
		UserID:    "user-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var nce *broker.NoCredentialError
	if !errors.As(err, &nce) {
		t.Fatalf("expected NoCredentialError, got %T: %v", err, err)
	}
}

func TestResolveToken_User_NoPrincipal(t *testing.T) {
	t.Parallel()

	called := false
	ds := &coretesting.StubDatastore{
		TokenFn: func(context.Context, string, string, string) (*core.IntegrationToken, error) {
			called = true
			return &core.IntegrationToken{AccessToken: "tok"}, nil
		},
	}
	b := newTestBroker(t, identityProvider("svc", core.ConnectionModeUser), ds)

	_, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "svc",
		Operation: "do",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Error("datastore should not have been called for empty UserID")
	}
	var nce *broker.NoCredentialError
	if !errors.As(err, &nce) {
		t.Fatalf("expected NoCredentialError, got %T: %v", err, err)
	}
}

func TestResolveToken_Either_InfraErrorNotSwallowed(t *testing.T) {
	t.Parallel()

	ds := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, userID, _ string, _ string) (*core.IntegrationToken, error) {
			if userID == "user-1" {
				return nil, fmt.Errorf("database connection refused")
			}
			return &core.IntegrationToken{AccessToken: "identity-tok"}, nil
		},
	}

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeEither,
		},
		ops: []core.Operation{{Name: "ping"}},
	}

	b := newTestBroker(t, prov, ds)
	_, err := b.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
		UserID:    "user-1",
	})
	if err == nil {
		t.Fatal("expected infrastructure error to propagate, got nil")
	}
	var nce *broker.NoCredentialError
	if errors.As(err, &nce) {
		t.Fatal("infrastructure error should not be treated as missing credential")
	}
}
