package bootstrap

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestResultStartRegistryAppsReconcilesAfterProvidersReady(t *testing.T) {
	t.Parallel()

	startup := newDeferredProviders()
	release := make(chan struct{})
	var started atomic.Bool
	var reconciled atomic.Int32
	wantErr := errors.New("reconcile failed")
	result := &Result{
		StartupProvidersReady: startup.ready(),
		startup:               startup,
		startAppProviders: func() {
			if !started.CompareAndSwap(false, true) {
				return
			}
			startup.set(nil, nil)
			go func() {
				<-release
				startup.finish()
			}()
		},
		startupWorkflowConfigReconcile: func(context.Context) error {
			if reconciled.Add(1) == 1 {
				return wantErr
			}
			return nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- result.StartAppProviders(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("StartAppProviders returned before providers were ready: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if got := reconciled.Load(); got != 0 {
		t.Fatalf("workflow reconciliations before providers were ready = %d, want 0", got)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StartAppProviders error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartAppProviders did not finish after providers became ready")
	}
	if err := result.StartRegistryApps(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("StartRegistryApps error = %v, want %v", err, wantErr)
	}
	deadline := time.After(5 * time.Second)
	for reconciled.Load() != 2 {
		select {
		case <-deadline:
			t.Fatal("failed registry workflow reconciliation was not retried")
		case <-time.After(time.Millisecond):
		}
	}
	if err := result.StartRegistryApps(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("second StartRegistryApps error = %v, want %v", err, wantErr)
	}
	if got := reconciled.Load(); got != 2 {
		t.Fatalf("workflow reconciliations after second start = %d, want 2", got)
	}
}

func TestResultStartupOperationsWaitForRegistryWorkflowReconcile(t *testing.T) {
	t.Parallel()

	reconcileStarted := make(chan struct{})
	reconcileRelease := make(chan struct{})
	result := &Result{
		startupWorkflowConfigReconcile: func(context.Context) error {
			close(reconcileStarted)
			<-reconcileRelease
			return nil
		},
	}

	startDone := make(chan error, 1)
	go func() { startDone <- result.StartRegistryApps(context.Background()) }()
	select {
	case <-reconcileStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("workflow reconciliation did not start")
	}

	waitDone := make(chan struct{})
	go func() {
		result.startupOperations.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		t.Fatal("startup operation completed while workflow reconciliation was running")
	case <-time.After(100 * time.Millisecond):
	}

	close(reconcileRelease)
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("StartRegistryApps: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartRegistryApps did not finish")
	}
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("startup operation wait did not finish after reconciliation")
	}
}

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
		_ func() []error,
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
