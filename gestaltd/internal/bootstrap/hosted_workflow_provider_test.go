package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/pluginruntime"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestHostedWorkflowProviderPoolStartsWorkersFromWorkflowProviderStartup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })

	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Metadata: map[string]string{
				"workload": "temporal-workers",
			},
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   2,
				MaxReadyInstances:   2,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "50ms",
			},
		},
	}

	workers, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
		"config":  map[string]any{"namespace": "default"},
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	control := &recordingWorkflowControlProvider{}
	provider := wrapWorkflowProviderWithRuntimeWorkers(control, workers)
	result := &Result{ExtraWorkflows: []workflow.Provider{provider}}
	t.Cleanup(func() { _ = provider.Close() })
	assertPublicHostServicesVerified(t, deps.PublicHostServices, "workflow_host", workflowservice.DefaultHostSocketEnv)
	executionRefs, ok := provider.(workflow.ExecutionReferenceStore)
	if !ok {
		t.Fatalf("hosted workflow pool does not expose ExecutionReferenceStore")
	}
	ref, err := executionRefs.PutExecutionReference(ctx, &workflow.ExecutionReference{
		ID:           "workflow_schedule:sched-test:ref-test",
		ProviderName: "temporal",
		SubjectID:    "subject-test",
		SubjectKind:  "user",
		Target: workflow.Target{
			Plugin: &workflow.PluginTarget{
				PluginName: "roadmap",
				Operation:  "sync_items",
			},
		},
	})
	if err != nil {
		t.Fatalf("PutExecutionReference: %v", err)
	}
	if ref.ID != "workflow_schedule:sched-test:ref-test" {
		t.Fatalf("PutExecutionReference id = %q, want workflow_schedule:sched-test:ref-test", ref.ID)
	}
	if _, err := executionRefs.GetExecutionReference(ctx, "workflow_schedule:sched-test:ref-test"); err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}

	if got := runtimeProvider.startProviderCalls(); got != 0 {
		t.Fatalf("StartProvider calls before StartWorkflowProviders = %d, want 0", got)
	}
	if got := len(runtimeProvider.startPluginRequestsCopy()); got != 0 {
		t.Fatalf("StartPlugin requests before StartWorkflowProviders = %d, want 0", got)
	}

	if err := result.Start(ctx); err != nil {
		t.Fatalf("Result.Start: %v", err)
	}
	if got := runtimeProvider.startProviderCalls(); got != 0 {
		t.Fatalf("StartProvider calls after Result.Start = %d, want 0", got)
	}
	if err := result.StartWorkflowProviders(ctx); err != nil {
		t.Fatalf("StartWorkflowProviders: %v", err)
	}
	workerProvider, ok := provider.(runtimeWorkerWorkflowProvider)
	if !ok {
		t.Fatalf("provider does not expose runtime worker readiness")
	}
	if err := workerProvider.WaitRuntimeWorkersReady(ctx); err != nil {
		t.Fatalf("WaitRuntimeWorkersReady: %v", err)
	}
	if got := runtimeProvider.startProviderCalls(); got != 2 {
		t.Fatalf("StartProvider calls after StartWorkflowProviders = %d, want worker pool size 2", got)
	}

	startRequests := runtimeProvider.startPluginRequestsCopy()
	if len(startRequests) != 2 {
		t.Fatalf("StartPlugin requests after StartWorkflowProviders = %d, want 2 workers", len(startRequests))
	}
	workerReq := startRequests[0]
	if got := workerReq.Env[workflowservice.DefaultHostSocketEnv]; got != "tcp://127.0.0.1:8080" {
		t.Fatalf("worker env %s = %q, want public relay target", workflowservice.DefaultHostSocketEnv, got)
	}
	if got := workerReq.Env[workflowservice.HostSocketTokenEnv()]; got == "" {
		t.Fatalf("worker env missing %s", workflowservice.HostSocketTokenEnv())
	}
	sessions := runtimeProvider.startSessionRequestsCopy()
	if len(sessions) != 2 {
		t.Fatalf("StartSession requests = %d, want 2 workers", len(sessions))
	}
	if got := sessions[0].Metadata["provider_kind"]; got != providermanifestKindWorkflow {
		t.Fatalf("worker session provider_kind = %q, want %q", got, providermanifestKindWorkflow)
	}
	if got := sessions[0].Metadata["provider_name"]; got != "temporal" {
		t.Fatalf("worker session provider_name = %q, want temporal", got)
	}
	if got := sessions[0].Metadata["workload"]; got != "temporal-workers" {
		t.Fatalf("worker session workload = %q, want temporal-workers", got)
	}
}

func TestHostedWorkflowProviderPoolStartupDoesNotBlockWorkflowReadiness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := &blockingStartSessionWorkflowRuntime{
		recordingHostedWorkflowRuntime: newRecordingHostedWorkflowRuntime(t),
		started:                        make(chan struct{}),
	}
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   1,
				MaxReadyInstances:   1,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "50ms",
			},
		},
	}

	workers, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	provider := wrapWorkflowProviderWithRuntimeWorkers(&recordingWorkflowControlProvider{}, workers)
	result := &Result{ExtraWorkflows: []workflow.Provider{provider}}
	t.Cleanup(func() { _ = provider.Close() })

	done := make(chan error, 1)
	go func() {
		done <- result.StartWorkflowProviders(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StartWorkflowProviders: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StartWorkflowProviders blocked on runtime worker startup")
	}
	select {
	case <-runtimeProvider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime worker startup did not begin")
	}
}

func TestWorkflowConfigReconciliationWaitsForRuntimeWorkers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   1,
				MaxReadyInstances:   1,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "50ms",
			},
		},
	}

	workers, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	provider := wrapWorkflowProviderWithRuntimeWorkers(&recordingWorkflowControlProvider{}, workers)
	workflowRuntime, err := newWorkflowRuntime(&config.Config{})
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	workflowRuntime.PublishProvider("temporal", provider)
	reconciled := make(chan struct{})
	result := &Result{
		ExtraWorkflows: []workflow.Provider{provider},
		workflowConfigReconcileTasks: []workflowConfigReconcileTask{{
			name: "temporal",
			reconcile: func(context.Context) error {
				if err := waitRuntimeWorkflowProviderReady(ctx, workflowRuntime, "temporal"); err != nil {
					return err
				}
				close(reconciled)
				return nil
			},
		}},
	}
	t.Cleanup(func() { _ = provider.Close() })

	result.StartWorkflowConfigReconciliation(ctx)
	select {
	case <-reconciled:
		t.Fatal("workflow config reconciliation ran before runtime workers were started")
	case <-time.After(50 * time.Millisecond):
	}
	if err := result.StartWorkflowProviders(ctx); err != nil {
		t.Fatalf("StartWorkflowProviders: %v", err)
	}
	select {
	case <-reconciled:
	case <-time.After(2 * time.Second):
		t.Fatal("workflow config reconciliation did not run after runtime workers became ready")
	}
}

func TestWorkflowConfigReconciliationReconcilesReadyRuntimeProvidersIndependently(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"ready": {
					Source: config.ProviderSource{Path: "stub"},
					Runtime: &config.RuntimePlacementConfig{
						Provider: "gke",
						Pool: &config.RuntimePlacementPoolConfig{
							MinReadyInstances:   1,
							MaxReadyInstances:   1,
							StartupTimeout:      "5s",
							HealthCheckInterval: "1m",
							RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
							DrainTimeout:        "50ms",
						},
					},
				},
				"stuck": {
					Source: config.ProviderSource{Path: "stub"},
					Runtime: &config.RuntimePlacementConfig{
						Provider: "gke",
						Pool: &config.RuntimePlacementPoolConfig{
							MinReadyInstances:   1,
							MaxReadyInstances:   1,
							StartupTimeout:      "5s",
							HealthCheckInterval: "1m",
							RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
							DrainTimeout:        "50ms",
						},
					},
				},
			},
		},
		Workflows: config.WorkflowsConfig{
			Schedules: map[string]config.WorkflowScheduleConfig{
				"ready_schedule": {
					Provider: "ready",
					Target:   workflowConfigTestAgentTarget(),
					Cron:     "* * * * *",
				},
			},
		},
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	readyProvider := &notifyingRuntimeWorkflowControlProvider{
		recordingWorkflowControlProvider: &recordingWorkflowControlProvider{},
		upsertedSchedule:                 make(chan struct{}),
	}
	stuckProvider := &blockingRuntimeWorkflowControlProvider{
		recordingWorkflowControlProvider: &recordingWorkflowControlProvider{},
		waitStarted:                      make(chan struct{}),
	}
	workflowRuntime.PublishProvider("ready", readyProvider)
	workflowRuntime.PublishProvider("stuck", stuckProvider)
	reconcileWorkflowConfig := func(ctx context.Context, includeProvider workflowConfigProviderFilter) error {
		if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, includeProvider); err != nil {
			return err
		}
		return reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, includeProvider)
	}
	result := &Result{
		workflowConfigReconcileTasks: runtimeWorkflowConfigReconcileTasks(workflowRuntime, runtimePlacedWorkflowProviderNames(cfg), reconcileWorkflowConfig),
	}

	result.StartWorkflowConfigReconciliation(ctx)
	select {
	case <-stuckProvider.waitStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stuck provider readiness wait did not start")
	}
	select {
	case <-readyProvider.upsertedSchedule:
	case <-time.After(2 * time.Second):
		t.Fatalf("ready provider schedules = %d, want 1 while another provider is stuck", len(readyProvider.upsertedSchedules))
	}
	if got := len(stuckProvider.upsertedSchedules); got != 0 {
		t.Fatalf("stuck provider schedules = %d, want 0 before readiness", got)
	}
}

func TestHostedWorkflowProviderPoolRejectsIncompatibleStartupSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := &staleSessionWorkflowRuntime{
		recordingHostedWorkflowRuntime: newRecordingHostedWorkflowRuntime(t),
	}
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   1,
				MaxReadyInstances:   1,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "50ms",
			},
		},
	}

	pool, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("pool.Start: %v", err)
	}
	waitForHostedWorkflowRuntimeStartSessionRequests(t, runtimeProvider.recordingHostedWorkflowRuntime, 1)
	if got := len(runtimeProvider.startPluginRequestsCopy()); got != 0 {
		t.Fatalf("StartPlugin requests = %d, want 0 for incompatible runtime session", got)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := pool.WaitReady(waitCtx); err == nil {
		t.Fatal("pool.WaitReady: expected timeout for incompatible runtime session")
	}
	if got := len(pool.readyWorkers()); got != 0 {
		t.Fatalf("ready workers = %d, want 0 for incompatible runtime session", got)
	}
}

func TestHostedWorkflowProviderPoolClosedStartLoopDoesNotMarkReady(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := &hostedWorkflowWorkerPool{
		name:   "temporal",
		ctx:    ctx,
		cancel: cancel,
		ready:  make(chan struct{}),
		policy: config.RuntimePlacementLifecyclePolicy{
			MinReadyInstances:   1,
			HealthCheckInterval: time.Hour,
			RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
		},
	}
	pool.mu.Lock()
	pool.closed = true
	pool.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.startLoop()
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		select {
		case <-pool.ready:
			t.Fatal("pool marked ready after it was closed")
		default:
		}
		t.Fatal("startLoop did not exit after pool closed")
	}
	select {
	case <-pool.ready:
		t.Fatal("pool marked ready after it was closed")
	default:
	}
}

func TestHostedWorkflowProviderPoolCloseUnblocksWaitReady(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	pool := &hostedWorkflowWorkerPool{
		name:   "temporal",
		ctx:    ctx,
		cancel: cancel,
		ready:  make(chan struct{}),
		policy: config.RuntimePlacementLifecyclePolicy{
			MinReadyInstances:   1,
			HealthCheckInterval: time.Hour,
			RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
		},
	}
	t.Cleanup(func() { _ = pool.Close() })

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- pool.WaitReady(context.Background())
	}()
	select {
	case err := <-waitDone:
		t.Fatalf("WaitReady returned before pool close: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("pool.Close: %v", err)
	}
	select {
	case err := <-waitDone:
		if !errors.Is(err, errHostedWorkflowWorkerPoolClosed) {
			t.Fatalf("WaitReady error = %v, want hosted workflow worker pool closed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitReady did not unblock after pool close")
	}
}

func TestWorkflowConfigReconciliationFiltersRuntimePlacedProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"local": {Source: config.ProviderSource{Path: "stub"}},
				"runtime": {
					Source: config.ProviderSource{Path: "stub"},
					Runtime: &config.RuntimePlacementConfig{
						Provider: "gke",
						Pool: &config.RuntimePlacementPoolConfig{
							MinReadyInstances:   1,
							MaxReadyInstances:   1,
							StartupTimeout:      "5s",
							HealthCheckInterval: "1m",
							RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
							DrainTimeout:        "50ms",
						},
					},
				},
			},
		},
		Workflows: config.WorkflowsConfig{
			Schedules: map[string]config.WorkflowScheduleConfig{
				"local_schedule": {
					Provider: "local",
					Target:   workflowConfigTestAgentTarget(),
					Cron:     "* * * * *",
				},
				"runtime_schedule": {
					Provider: "runtime",
					Target:   workflowConfigTestAgentTarget(),
					Cron:     "* * * * *",
				},
			},
			EventTriggers: map[string]config.WorkflowEventTriggerConfig{
				"local_trigger": {
					Provider: "local",
					Target:   workflowConfigTestAgentTarget(),
					Match:    config.WorkflowEventMatch{Type: "local.changed"},
				},
				"runtime_trigger": {
					Provider: "runtime",
					Target:   workflowConfigTestAgentTarget(),
					Match:    config.WorkflowEventMatch{Type: "runtime.changed"},
				},
			},
		},
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	localProvider := &recordingWorkflowControlProvider{}
	runtimeProvider := &recordingWorkflowControlProvider{}
	workflowRuntime.PublishProvider("local", localProvider)
	workflowRuntime.PublishProvider("runtime", runtimeProvider)

	runtimePlaced := runtimePlacedWorkflowProviderNames(cfg)
	localFilter := func(providerName string) bool {
		_, ok := runtimePlaced[providerName]
		return !ok
	}
	runtimeFilter := func(providerName string) bool {
		_, ok := runtimePlaced[providerName]
		return ok
	}

	if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, localFilter); err != nil {
		t.Fatalf("reconcile local schedules: %v", err)
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, localFilter); err != nil {
		t.Fatalf("reconcile local event triggers: %v", err)
	}
	if got := len(localProvider.upsertedSchedules); got != 1 {
		t.Fatalf("local upserted schedules = %d, want 1", got)
	}
	if got := len(localProvider.upsertedEventTriggers); got != 1 {
		t.Fatalf("local upserted event triggers = %d, want 1", got)
	}
	if got := len(runtimeProvider.upsertedSchedules); got != 0 {
		t.Fatalf("runtime upserted schedules during local reconcile = %d, want 0", got)
	}
	if got := len(runtimeProvider.upsertedEventTriggers); got != 0 {
		t.Fatalf("runtime upserted event triggers during local reconcile = %d, want 0", got)
	}

	if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, runtimeFilter); err != nil {
		t.Fatalf("reconcile runtime schedules: %v", err)
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, runtimeFilter); err != nil {
		t.Fatalf("reconcile runtime event triggers: %v", err)
	}
	if got := len(runtimeProvider.upsertedSchedules); got != 1 {
		t.Fatalf("runtime upserted schedules = %d, want 1", got)
	}
	if got := len(runtimeProvider.upsertedEventTriggers); got != 1 {
		t.Fatalf("runtime upserted event triggers = %d, want 1", got)
	}
}

func workflowConfigTestAgentTarget() *config.WorkflowTargetConfig {
	return &config.WorkflowTargetConfig{
		Agent: &config.WorkflowAgentConfig{
			Prompt: "summarize",
		},
	}
}

func TestHostedWorkflowAllowedHostsFiltersLoopbackRelayTargets(t *testing.T) {
	t.Parallel()

	allowed := hostedWorkflowAllowedHosts([]string{"localhost", "127.0.0.1", "api.example.com"}, RuntimePlacementPlan{
		Resolved: RuntimeBehavior{
			HostServiceAccess: RuntimeHostServiceAccessRelay,
			EgressMode:        RuntimeEgressModeNone,
		},
	})
	if !slices.Equal(allowed, []string{"api.example.com"}) {
		t.Fatalf("hostedWorkflowAllowedHosts = %#v, want api.example.com only", allowed)
	}
}

func TestHostedWorkflowProviderKeepsSharedRuntimeOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   1,
				MaxReadyInstances:   1,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "50ms",
			},
		},
	}

	workers, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	if err := workers.Close(); err != nil {
		t.Fatalf("workers.Close: %v", err)
	}
	if got := runtimeProvider.closeCalls.Load(); got != 0 {
		t.Fatalf("runtime Close calls after workers.Close = %d, want 0 for shared runtime", got)
	}
}

func TestHostedWorkflowProviderPoolDrainWaitsBeforeClosingWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := newRecordingHostedWorkflowRuntime(t)
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		PluginRuntime:      runtimeProvider,
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	entry := &config.ProviderEntry{
		Runtime: &config.RuntimePlacementConfig{
			Provider: "gke",
			Pool: &config.RuntimePlacementPoolConfig{
				MinReadyInstances:   1,
				MaxReadyInstances:   1,
				StartupTimeout:      "5s",
				HealthCheckInterval: "1m",
				RestartPolicy:       config.RuntimePlacementRestartPolicyNever,
				DrainTimeout:        "150ms",
			},
		},
	}

	pool, err := buildHostedWorkflowWorkerPool(ctx, "temporal", entry, mustNode(t, map[string]any{
		"command": "/bin/temporal-provider",
	}), []runtimehost.HostService{{
		Name:   "workflow_host",
		EnvVar: workflowservice.DefaultHostSocketEnv,
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("pool.Start: %v", err)
	}
	if err := pool.WaitReady(ctx); err != nil {
		t.Fatalf("pool.WaitReady: %v", err)
	}
	workers := pool.readyWorkers()
	if len(workers) != 1 {
		t.Fatalf("ready workers = %d, want 1", len(workers))
	}
	pool.mu.Lock()
	workers[0].active = 1
	pool.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- pool.drainAndCloseWorker(workers[0])
	}()
	select {
	case err := <-done:
		t.Fatalf("drainAndCloseWorker finished before drain timeout with error %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	pool.mu.Lock()
	workers[0].active = 0
	pool.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drainAndCloseWorker: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drainAndCloseWorker did not finish after drain timeout")
	}
}

const providermanifestKindWorkflow = "workflow"

type recordingHostedWorkflowRuntime struct {
	provider *pluginruntime.LocalProvider
	t        *testing.T

	mu                  sync.Mutex
	startRequests       []pluginruntime.StartSessionRequest
	startPluginRequests []pluginruntime.StartPluginRequest
	servers             map[string]*recordingHostedWorkflowServer
	closeCalls          atomic.Int32
}

type blockingStartSessionWorkflowRuntime struct {
	*recordingHostedWorkflowRuntime
	started chan struct{}
	once    sync.Once
}

func (r *blockingStartSessionWorkflowRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	r.once.Do(func() {
		close(r.started)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

type staleSessionWorkflowRuntime struct {
	*recordingHostedWorkflowRuntime
}

func (r *staleSessionWorkflowRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	session, err := r.recordingHostedWorkflowRuntime.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	for key, value := range staleRuntimeSessionForTest().Metadata {
		session.Metadata[key] = value
	}
	return session, nil
}

type notifyingRuntimeWorkflowControlProvider struct {
	*recordingWorkflowControlProvider
	upsertedSchedule chan struct{}
	once             sync.Once
}

func (p *notifyingRuntimeWorkflowControlProvider) WaitRuntimeWorkersReady(context.Context) error {
	return nil
}

func (p *notifyingRuntimeWorkflowControlProvider) UpsertSchedule(ctx context.Context, req workflow.UpsertScheduleRequest) (*workflow.Schedule, error) {
	schedule, err := p.recordingWorkflowControlProvider.UpsertSchedule(ctx, req)
	if err == nil {
		p.once.Do(func() {
			close(p.upsertedSchedule)
		})
	}
	return schedule, err
}

type blockingRuntimeWorkflowControlProvider struct {
	*recordingWorkflowControlProvider
	waitStarted chan struct{}
	once        sync.Once
}

func (p *blockingRuntimeWorkflowControlProvider) WaitRuntimeWorkersReady(ctx context.Context) error {
	p.once.Do(func() {
		close(p.waitStarted)
	})
	<-ctx.Done()
	return ctx.Err()
}

type recordingWorkflowControlProvider struct {
	noopWorkflowProvider
	refs                  map[string]*workflow.ExecutionReference
	schedules             map[string]*workflow.Schedule
	upsertedSchedules     []workflow.UpsertScheduleRequest
	eventTriggers         map[string]*workflow.EventTrigger
	upsertedEventTriggers []workflow.UpsertEventTriggerRequest
}

func (p *recordingWorkflowControlProvider) PutExecutionReference(_ context.Context, ref *workflow.ExecutionReference) (*workflow.ExecutionReference, error) {
	if p.refs == nil {
		p.refs = map[string]*workflow.ExecutionReference{}
	}
	stored := cloneWorkflowExecutionReference(ref)
	p.refs[stored.ID] = stored
	return cloneWorkflowExecutionReference(stored), nil
}

func (p *recordingWorkflowControlProvider) GetExecutionReference(_ context.Context, id string) (*workflow.ExecutionReference, error) {
	ref := p.refs[id]
	if ref == nil {
		return nil, status.Error(codes.NotFound, "execution reference not found")
	}
	return cloneWorkflowExecutionReference(ref), nil
}

func (p *recordingWorkflowControlProvider) ListExecutionReferences(_ context.Context, subjectID string) ([]*workflow.ExecutionReference, error) {
	out := make([]*workflow.ExecutionReference, 0, len(p.refs))
	for _, ref := range p.refs {
		if ref == nil {
			continue
		}
		if subjectID != "" && ref.SubjectID != subjectID {
			continue
		}
		out = append(out, cloneWorkflowExecutionReference(ref))
	}
	return out, nil
}

func (p *recordingWorkflowControlProvider) GetSchedule(_ context.Context, req workflow.GetScheduleRequest) (*workflow.Schedule, error) {
	if schedule := p.schedules[req.ScheduleID]; schedule != nil {
		return cloneWorkflowSchedule(schedule), nil
	}
	return nil, core.ErrNotFound
}

func (p *recordingWorkflowControlProvider) UpsertSchedule(_ context.Context, req workflow.UpsertScheduleRequest) (*workflow.Schedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, req)
	if p.schedules == nil {
		p.schedules = map[string]*workflow.Schedule{}
	}
	schedule := &workflow.Schedule{
		ID:           req.ScheduleID,
		Cron:         req.Cron,
		Timezone:     req.Timezone,
		Target:       req.Target,
		Paused:       req.Paused,
		CreatedBy:    req.RequestedBy,
		ExecutionRef: req.ExecutionRef,
	}
	p.schedules[req.ScheduleID] = schedule
	return cloneWorkflowSchedule(schedule), nil
}

func (p *recordingWorkflowControlProvider) ListSchedules(context.Context, workflow.ListSchedulesRequest) ([]*workflow.Schedule, error) {
	out := make([]*workflow.Schedule, 0, len(p.schedules))
	for _, schedule := range p.schedules {
		out = append(out, cloneWorkflowSchedule(schedule))
	}
	return out, nil
}

func (p *recordingWorkflowControlProvider) GetEventTrigger(_ context.Context, req workflow.GetEventTriggerRequest) (*workflow.EventTrigger, error) {
	if trigger := p.eventTriggers[req.TriggerID]; trigger != nil {
		return cloneWorkflowEventTrigger(trigger), nil
	}
	return nil, core.ErrNotFound
}

func (p *recordingWorkflowControlProvider) UpsertEventTrigger(_ context.Context, req workflow.UpsertEventTriggerRequest) (*workflow.EventTrigger, error) {
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, req)
	if p.eventTriggers == nil {
		p.eventTriggers = map[string]*workflow.EventTrigger{}
	}
	trigger := &workflow.EventTrigger{
		ID:           req.TriggerID,
		Match:        req.Match,
		Target:       req.Target,
		Paused:       req.Paused,
		CreatedBy:    req.RequestedBy,
		ExecutionRef: req.ExecutionRef,
	}
	p.eventTriggers[req.TriggerID] = trigger
	return cloneWorkflowEventTrigger(trigger), nil
}

func (p *recordingWorkflowControlProvider) ListEventTriggers(context.Context, workflow.ListEventTriggersRequest) ([]*workflow.EventTrigger, error) {
	out := make([]*workflow.EventTrigger, 0, len(p.eventTriggers))
	for _, trigger := range p.eventTriggers {
		out = append(out, cloneWorkflowEventTrigger(trigger))
	}
	return out, nil
}

func cloneWorkflowExecutionReference(ref *workflow.ExecutionReference) *workflow.ExecutionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	return &clone
}

func cloneWorkflowSchedule(schedule *workflow.Schedule) *workflow.Schedule {
	if schedule == nil {
		return nil
	}
	clone := *schedule
	return &clone
}

func cloneWorkflowEventTrigger(trigger *workflow.EventTrigger) *workflow.EventTrigger {
	if trigger == nil {
		return nil
	}
	clone := *trigger
	return &clone
}

func newRecordingHostedWorkflowRuntime(t *testing.T) *recordingHostedWorkflowRuntime {
	t.Helper()
	return &recordingHostedWorkflowRuntime{
		provider: pluginruntime.NewLocalProvider(),
		t:        t,
		servers:  map[string]*recordingHostedWorkflowServer{},
	}
}

func (r *recordingHostedWorkflowRuntime) Support(context.Context) (pluginruntime.Support, error) {
	return pluginruntime.Support{
		CanHostPlugins: true,
		EgressMode:     pluginruntime.EgressModeHostname,
	}, nil
}

func (r *recordingHostedWorkflowRuntime) StartSession(ctx context.Context, req pluginruntime.StartSessionRequest) (*pluginruntime.Session, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, pluginruntime.StartSessionRequest{
		PluginName:    req.PluginName,
		Template:      req.Template,
		Image:         req.Image,
		ImagePullAuth: cloneImagePullAuth(req.ImagePullAuth),
		Metadata:      cloneRuntimeMetadata(req.Metadata),
	})
	r.mu.Unlock()
	return r.provider.StartSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) ListSessions(ctx context.Context) ([]pluginruntime.Session, error) {
	return r.provider.ListSessions(ctx)
}

func (r *recordingHostedWorkflowRuntime) GetSession(ctx context.Context, req pluginruntime.GetSessionRequest) (*pluginruntime.Session, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StopSession(ctx context.Context, req pluginruntime.StopSessionRequest) error {
	r.cleanupServer(req.SessionID)
	return r.provider.StopSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StartPlugin(_ context.Context, req pluginruntime.StartPluginRequest) (*pluginruntime.HostedPlugin, error) {
	r.mu.Lock()
	r.startPluginRequests = append(r.startPluginRequests, pluginruntime.StartPluginRequest{
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		Command:    req.Command,
		Args:       slices.Clone(req.Args),
		Env:        cloneRuntimeMetadata(req.Env),
		Egress:     cloneRuntimeEgressPolicy(req.Egress),
		HostBinary: req.HostBinary,
	})
	r.mu.Unlock()

	dir, err := runtimehost.NewPluginTempDir("gst-workflow-runtime-*")
	if err != nil {
		return nil, fmt.Errorf("create fake hosted workflow dir: %w", err)
	}
	socketPath := filepath.Join(dir, "workflow.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for fake hosted workflow: %w", err)
	}
	workflowServer := newRecordingHostedWorkflowServer()
	grpcServer := grpc.NewServer()
	proto.RegisterProviderLifecycleServer(grpcServer, workflowServer)
	proto.RegisterWorkflowProviderServer(grpcServer, workflowServer)
	go func() {
		_ = grpcServer.Serve(lis)
	}()

	r.mu.Lock()
	r.servers[req.SessionID] = workflowServer
	r.mu.Unlock()
	r.t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
		_ = os.RemoveAll(dir)
	})
	return &pluginruntime.HostedPlugin{
		ID:         "fake-" + req.SessionID,
		SessionID:  req.SessionID,
		PluginName: req.PluginName,
		DialTarget: "unix://" + socketPath,
	}, nil
}

func (r *recordingHostedWorkflowRuntime) Close() error {
	r.closeCalls.Add(1)
	r.mu.Lock()
	sessionIDs := make([]string, 0, len(r.servers))
	for sessionID := range r.servers {
		sessionIDs = append(sessionIDs, sessionID)
	}
	r.mu.Unlock()
	for _, sessionID := range sessionIDs {
		r.cleanupServer(sessionID)
	}
	return r.provider.Close()
}

func (r *recordingHostedWorkflowRuntime) startSessionRequestsCopy() []pluginruntime.StartSessionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.StartSessionRequest, len(r.startRequests))
	for i, req := range r.startRequests {
		out[i] = pluginruntime.StartSessionRequest{
			PluginName:    req.PluginName,
			Template:      req.Template,
			Image:         req.Image,
			ImagePullAuth: cloneImagePullAuth(req.ImagePullAuth),
			Metadata:      cloneRuntimeMetadata(req.Metadata),
		}
	}
	return out
}

func (r *recordingHostedWorkflowRuntime) startPluginRequestsCopy() []pluginruntime.StartPluginRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]pluginruntime.StartPluginRequest, len(r.startPluginRequests))
	for i, req := range r.startPluginRequests {
		out[i] = pluginruntime.StartPluginRequest{
			SessionID:  req.SessionID,
			PluginName: req.PluginName,
			Command:    req.Command,
			Args:       slices.Clone(req.Args),
			Env:        cloneRuntimeMetadata(req.Env),
			Egress:     cloneRuntimeEgressPolicy(req.Egress),
			HostBinary: req.HostBinary,
		}
	}
	return out
}

func (r *recordingHostedWorkflowRuntime) startProviderCalls() int32 {
	r.mu.Lock()
	servers := make([]*recordingHostedWorkflowServer, 0, len(r.servers))
	for _, server := range r.servers {
		servers = append(servers, server)
	}
	r.mu.Unlock()
	var total int32
	for _, server := range servers {
		total += server.startProviderCalls.Load()
	}
	return total
}

func waitForHostedWorkflowRuntimeStartSessionRequests(t *testing.T, runtimeProvider *recordingHostedWorkflowRuntime, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := len(runtimeProvider.startSessionRequestsCopy()); got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("StartSession requests = %d, want at least %d", len(runtimeProvider.startSessionRequestsCopy()), want)
}

func (r *recordingHostedWorkflowRuntime) cleanupServer(sessionID string) {
	r.mu.Lock()
	delete(r.servers, sessionID)
	r.mu.Unlock()
}

type recordingHostedWorkflowServer struct {
	proto.UnimplementedProviderLifecycleServer
	proto.UnimplementedWorkflowProviderServer

	startProviderCalls atomic.Int32
	mu                 sync.Mutex
	executionRefs      map[string]*proto.WorkflowExecutionReference
}

func newRecordingHostedWorkflowServer() *recordingHostedWorkflowServer {
	return &recordingHostedWorkflowServer{
		executionRefs: map[string]*proto.WorkflowExecutionReference{},
	}
}

func (s *recordingHostedWorkflowServer) GetProviderIdentity(context.Context, *emptypb.Empty) (*proto.ProviderIdentity, error) {
	return &proto.ProviderIdentity{
		Kind:               proto.ProviderKind_PROVIDER_KIND_WORKFLOW,
		Name:               "temporal",
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *recordingHostedWorkflowServer) ConfigureProvider(context.Context, *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *recordingHostedWorkflowServer) HealthCheck(context.Context, *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (s *recordingHostedWorkflowServer) StartProvider(context.Context, *emptypb.Empty) (*proto.StartRuntimeProviderResponse, error) {
	s.startProviderCalls.Add(1)
	return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *recordingHostedWorkflowServer) PutExecutionReference(_ context.Context, req *proto.PutWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	ref := req.GetReference()
	if ref.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "missing execution reference id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executionRefs[ref.GetId()] = gproto.Clone(ref).(*proto.WorkflowExecutionReference)
	return gproto.Clone(ref).(*proto.WorkflowExecutionReference), nil
}

func (s *recordingHostedWorkflowServer) GetExecutionReference(_ context.Context, req *proto.GetWorkflowExecutionReferenceRequest) (*proto.WorkflowExecutionReference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := s.executionRefs[req.GetId()]
	if ref == nil {
		return nil, status.Error(codes.NotFound, "execution reference not found")
	}
	return gproto.Clone(ref).(*proto.WorkflowExecutionReference), nil
}

func (s *recordingHostedWorkflowServer) ListExecutionReferences(_ context.Context, req *proto.ListWorkflowExecutionReferencesRequest) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := make([]*proto.WorkflowExecutionReference, 0, len(s.executionRefs))
	for _, ref := range s.executionRefs {
		if req.GetSubjectId() == "" || ref.GetSubjectId() == req.GetSubjectId() {
			refs = append(refs, gproto.Clone(ref).(*proto.WorkflowExecutionReference))
		}
	}
	return &proto.ListWorkflowExecutionReferencesResponse{References: refs}, nil
}
