package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestStartupProviderProxyLazilyActivatesAndFailsFast(t *testing.T) {
	t.Parallel()

	proxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "deferred",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, nil)

	var triggers atomic.Int32
	// The install runs in the background; the trigger does not publish synchronously.
	proxy.setActivationTrigger(func(context.Context) { triggers.Add(1) })

	// First request triggers activation and fails fast rather than blocking.
	_, err := proxy.await(context.Background())
	if !errors.Is(err, core.ErrProviderActivating) {
		t.Fatalf("await while activating = %v, want ErrProviderActivating", err)
	}
	if triggers.Load() == 0 {
		t.Fatal("expected the first request to trigger activation")
	}

	// Still installing: keep failing fast.
	if _, err := proxy.await(context.Background()); !errors.Is(err, core.ErrProviderActivating) {
		t.Fatalf("await still activating = %v, want ErrProviderActivating", err)
	}

	// Install completes: subsequent requests resolve.
	proxy.publish(&coretesting.StubIntegration{N: "deferred"})
	provider, err := proxy.await(context.Background())
	if err != nil {
		t.Fatalf("await after install completed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected a resolved provider once activation finished")
	}
}

func TestStartupProviderProxySurfacesActivationFailure(t *testing.T) {
	t.Parallel()

	proxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "deferred",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, nil)
	proxy.setActivationTrigger(func(context.Context) {})

	if _, err := proxy.await(context.Background()); !errors.Is(err, core.ErrProviderActivating) {
		t.Fatalf("await while activating = %v, want ErrProviderActivating", err)
	}

	installErr := errors.New("boom: bad manifest")
	proxy.fail(installErr)

	_, err := proxy.await(context.Background())
	if errors.Is(err, core.ErrProviderActivating) {
		t.Fatalf("await after install failure still returns ErrProviderActivating: %v", err)
	}
	if !errors.Is(err, installErr) {
		t.Fatalf("await after install failure = %v, want the underlying install error", err)
	}
}

func TestStartupProviderProxyDoesNotActivateWhenAlreadyResolved(t *testing.T) {
	t.Parallel()

	proxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "eager",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, nil)

	var triggers atomic.Int32
	proxy.setActivationTrigger(func(context.Context) { triggers.Add(1) })
	proxy.publish(&coretesting.StubIntegration{N: "eager"})

	if _, err := proxy.await(context.Background()); err != nil {
		t.Fatalf("await: %v", err)
	}
	if got := triggers.Load(); got != 0 {
		t.Fatalf("activation trigger fired %d times for an already-resolved provider, want 0", got)
	}
}

func TestStartupProviderProxyRejectsAppAgentCycle(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	agentHandle := newAgentProviderHandle("managed", tracker)
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "caller",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	appCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	appWaitDone, err := agentHandle.beginCallerWait(appCtx)
	if err != nil {
		t.Fatalf("begin app->agent wait: %v", err)
	}
	defer appWaitDone()

	agentCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindAgent, "managed")
	_, err = appProxy.Execute(agentCtx, "sync", nil, "")
	if err == nil {
		t.Fatal("Execute returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `agent "managed" -> app "caller" -> agent "managed"`) {
		t.Fatalf("Execute error = %v, want app-agent cycle path", err)
	}
}

func TestStartupWorkflowProviderProxyTracksReadCallerWithoutTarget(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	workflowHandle := newWorkflowProviderHandle("temporal", tracker)
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "caller",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	workflowCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindWorkflow, "temporal")
	workflowWaitDone, err := appProxy.beginCallerWait(workflowCtx)
	if err != nil {
		t.Fatalf("begin workflow->app wait: %v", err)
	}
	defer workflowWaitDone()

	appCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	_, err = workflowHandle.await(appCtx)
	if err == nil {
		t.Fatal("ListSchedules returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `app "caller" -> workflow "temporal" -> app "caller"`) {
		t.Fatalf("ListSchedules error = %v, want app-workflow cycle path", err)
	}
}

func TestStartupWaitTrackerUsesProviderKind(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	agentHandle := newAgentProviderHandle("shared", tracker)
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "shared",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	appCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "shared")
	appWaitDone, err := agentHandle.beginCallerWait(appCtx)
	if err != nil {
		t.Fatalf("begin shared app->agent wait: %v", err)
	}
	defer appWaitDone()

	agentCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindAgent, "shared")
	_, err = appProxy.Execute(agentCtx, "sync", nil, "")
	if err == nil {
		t.Fatal("Execute returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `agent "shared" -> app "shared" -> agent "shared"`) {
		t.Fatalf("Execute error = %v, want kinded cycle path", err)
	}
}
