package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

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
