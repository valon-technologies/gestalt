package bootstrap

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestNewProviderActivationStartsProvidersOnce(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"noop": {Source: config.ProviderSource{Path: "stub"}},
		},
	}
	builds, err := prepareProviderBuilds(cfg, NewFactoryRegistry(), Deps{})
	if err != nil {
		t.Fatalf("prepareProviderBuilds: %v", err)
	}
	t.Cleanup(func() { _ = CloseProviders(builds.providers) })

	var builderCalls atomic.Int32
	builder := func(context.Context, string, *config.ProviderEntry, Deps) (*ProviderBuildResult, error) {
		builderCalls.Add(1)
		return &ProviderBuildResult{Provider: &coretesting.StubIntegration{N: "noop"}}, nil
	}

	var gotReady <-chan struct{}
	activate := newProviderActivation(context.Background(), builds, Deps{}, builder, func(
		ready <-chan struct{},
		_ func() map[string]map[string]OAuthHandler,
		_ func() map[string]map[string]ManualTokenExchanger,
	) {
		gotReady = ready
	})

	done := make(chan struct{})
	go func() {
		activate()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ActivateAppProviders blocked instead of firing goroutines and returning")
	}

	if gotReady == nil {
		t.Fatal("expected onStart to be invoked with a ready channel")
	}
	select {
	case <-gotReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider build to complete")
	}
	if got := builderCalls.Load(); got != 1 {
		t.Fatalf("builder calls after first activation = %d, want 1", got)
	}

	activate()
	activate()
	if got := builderCalls.Load(); got != 1 {
		t.Fatalf("builder calls after repeated activation = %d, want 1 (idempotent)", got)
	}
}
