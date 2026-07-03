package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
)

func TestBootstrapDefersActivationWhenAutoActivateDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	disabled := false
	cfg.Server.AutoActivate = &disabled

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	select {
	case <-result.ProvidersReady:
		t.Fatal("deferred providers should not activate before ActivateAppProviders is called")
	case <-time.After(300 * time.Millisecond):
	}

	result.ActivateAppProviders(ctx)

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("ProvidersReady should close after ActivateAppProviders")
	}
}

func TestActivateAppProvidersIgnoresCallerContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	disabled := false
	cfg.Server.AutoActivate = &disabled

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result.ActivateAppProviders(canceled)

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("activation should run under the server context, not the already-canceled caller context")
	}
}

func TestActivateAppProvidersAfterCloseIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	disabled := false
	cfg.Server.AutoActivate = &disabled

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := result.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	go func() {
		result.ActivateAppProviders(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ActivateAppProviders after Close should be a no-op, not block")
	}
}

func TestBootstrapAutoActivateEnabledStartsWithoutActivateCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	enabled := true
	cfg.Server.AutoActivate = &enabled

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() {
		if err := result.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	select {
	case <-result.ProvidersReady:
	case <-time.After(5 * time.Second):
		t.Fatal("ProvidersReady should close at startup when autoActivate is enabled")
	}
}
