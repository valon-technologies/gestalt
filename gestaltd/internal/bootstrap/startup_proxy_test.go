package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestStartupProviderProxyRejectsAppAgentCycle(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	agentProxy := newStartupAgentProviderProxy("managed", tracker)
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "caller",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	appCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	appWaitDone, err := agentProxy.beginCallerWait(appCtx)
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
	workflowProxy := newStartupWorkflowProviderProxy("temporal", tracker)
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
	_, err = workflowProxy.ListSchedules(appCtx, coreworkflow.ListSchedulesRequest{})
	if err == nil {
		t.Fatal("ListSchedules returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `app "caller" -> workflow "temporal" -> app "caller"`) {
		t.Fatalf("ListSchedules error = %v, want app-workflow cycle path", err)
	}
}

func TestStartupProviderProxyPrefersWorkflowContext(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "caller",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	ctx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	ctx = invocation.WithWorkflowContext(ctx, map[string]any{"provider": "temporal"})
	done, err := appProxy.beginCallerWait(ctx)
	if err != nil {
		t.Fatalf("begin workflow app wait: %v", err)
	}
	defer done()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	workflowEdge := tracker.waits[newStartupProviderNode(invocation.ProviderKindWorkflow, "temporal")][newStartupProviderNode(invocation.ProviderKindApp, "caller")]
	appEdge := tracker.waits[newStartupProviderNode(invocation.ProviderKindApp, "caller")][newStartupProviderNode(invocation.ProviderKindApp, "caller")]
	if workflowEdge != 1 {
		t.Fatalf("workflow edge count = %d, want 1", workflowEdge)
	}
	if appEdge != 0 {
		t.Fatalf("app self edge count = %d, want 0", appEdge)
	}
}

func TestStartupWorkflowProviderProxyTracksPingCaller(t *testing.T) {
	t.Parallel()

	proxy := newStartupWorkflowProviderProxy("temporal", newStartupWaitTracker())
	ctx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindWorkflow, "temporal")
	err := proxy.Ping(ctx)
	if err == nil {
		t.Fatal("Ping returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `workflow "temporal" -> workflow "temporal"`) {
		t.Fatalf("Ping error = %v, want workflow self-cycle path", err)
	}
}

func TestStartupAgentProviderProxyPingReportsUnavailableWhilePending(t *testing.T) {
	t.Parallel()

	proxy := newStartupAgentProviderProxy("managed", nil)
	done := make(chan error, 1)
	go func() {
		done <- proxy.Ping(context.Background())
	}()

	select {
	case err := <-done:
		if !errors.Is(err, agentmanager.ErrAgentProviderNotAvailable) {
			t.Fatalf("Ping error = %v, want ErrAgentProviderNotAvailable", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ping blocked on pending startup agent proxy")
	}
}

func TestStartupWaitTrackerUsesProviderKind(t *testing.T) {
	t.Parallel()

	tracker := newStartupWaitTracker()
	agentProxy := newStartupAgentProviderProxy("shared", tracker)
	appProxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "shared",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, tracker)

	appCtx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "shared")
	appWaitDone, err := agentProxy.beginCallerWait(appCtx)
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

func TestStartupWaitTrackerRejectsSelfWait(t *testing.T) {
	t.Parallel()

	proxy := newStartupProviderProxy(appservice.StaticProviderSpec{
		Name:           "caller",
		ConnectionMode: core.ConnectionModeNone,
	}, startupOperationRouting{}, newStartupWaitTracker())

	ctx := invocation.WithCallerProvider(context.Background(), invocation.ProviderKindApp, "caller")
	_, err := proxy.Execute(ctx, "sync", nil, "")
	if err == nil {
		t.Fatal("Execute returned nil error, want startup dependency cycle")
	}
	if !strings.Contains(err.Error(), `app "caller" -> app "caller"`) {
		t.Fatalf("Execute error = %v, want self-cycle path", err)
	}
}

func TestStartupAgentProviderProxyRejectsWorkspaceAfterNonWorkspacePublish(t *testing.T) {
	t.Parallel()

	proxy := newStartupAgentProviderProxy("basic", nil)
	proxy.publish(coreagent.UnimplementedProvider{})

	_, err := proxy.CreateSession(context.Background(), coreagent.CreateSessionRequest{
		Workspace: &coreagent.Workspace{
			Checkouts: []coreagent.WorkspaceGitCheckout{{
				URL:  "https://github.com/valon-technologies/gestalt.git",
				Path: "repo",
			}},
			CWD: "repo",
		},
	})
	if err == nil {
		t.Fatal("CreateSession returned nil error, want workspace unsupported")
	}
	if !errors.Is(err, agentmanager.ErrAgentWorkspaceUnsupported) {
		t.Fatalf("CreateSession error = %v, want ErrAgentWorkspaceUnsupported", err)
	}
}

func TestStartupAgentProviderProxyWorkspaceSupportDelegatesAfterPublish(t *testing.T) {
	t.Parallel()

	proxy := newStartupAgentProviderProxy("managed", nil)
	if !proxy.SupportsWorkspaceRequests() {
		t.Fatal("pending startup agent proxy rejected workspace requests")
	}

	proxy.publish(providerBuildOrderingAgentProvider{})
	if !proxy.SupportsWorkspaceRequests() {
		t.Fatal("published workspace provider rejected workspace requests")
	}

	proxy = newStartupAgentProviderProxy("basic", nil)
	proxy.publish(coreagent.UnimplementedProvider{})
	if proxy.SupportsWorkspaceRequests() {
		t.Fatal("published non-workspace provider accepted workspace requests")
	}
}
