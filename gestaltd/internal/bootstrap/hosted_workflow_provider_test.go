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
	"testing/synctest"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"google.golang.org/grpc"
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
		Runtime:            runtimeProvider,
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
		Name:           "indexeddb",
		MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	control := &recordingWorkflowControlProvider{}
	provider := wrapWorkflowProviderWithRuntimeWorkers(control, workers)
	result := &Result{ExtraWorkflows: []workflow.Provider{provider}}
	t.Cleanup(func() { _ = provider.Close() })
	assertPublicHostServicesVerified(t, deps.PublicHostServices, "indexeddb")

	if got := runtimeProvider.startProviderCalls(); got != 0 {
		t.Fatalf("StartProvider calls before StartWorkflowProviders = %d, want 0", got)
	}
	if got := len(runtimeProvider.startAppRequestsCopy()); got != 0 {
		t.Fatalf("StartApp requests before StartWorkflowProviders = %d, want 0", got)
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

	startRequests := runtimeProvider.startAppRequestsCopy()
	if len(startRequests) != 2 {
		t.Fatalf("StartApp requests after StartWorkflowProviders = %d, want 2 workers", len(startRequests))
	}
	workerReq := startRequests[0]
	if got := workerReq.GetEnv()[runtimehost.HostServiceSocketEnv]; got != "tcp://127.0.0.1:8080" {
		t.Fatalf("worker env %s = %q, want public relay target", runtimehost.HostServiceSocketEnv, got)
	}
	if got := workerReq.GetEnv()[runtimehost.HostServiceTokenEnv]; got == "" {
		t.Fatalf("worker env missing %s", runtimehost.HostServiceTokenEnv)
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

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runtimeProvider := &blockingStartSessionWorkflowRuntime{
			recordingHostedWorkflowRuntime: newRecordingHostedWorkflowRuntime(t),
			started:                        make(chan struct{}),
		}
		defer func() { _ = runtimeProvider.Close() }()
		deps := Deps{
			BaseURL:            "http://127.0.0.1:8080",
			EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
			Runtime:            runtimeProvider,
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
			Name:           "indexeddb",
			MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
		}}, deps)
		if err != nil {
			t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
		}
		provider := wrapWorkflowProviderWithRuntimeWorkers(&recordingWorkflowControlProvider{}, workers)
		defer func() { _ = provider.Close() }()
		result := &Result{ExtraWorkflows: []workflow.Provider{provider}}

		done := make(chan error, 1)
		go func() {
			done <- result.StartWorkflowProviders(ctx)
		}()
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("StartWorkflowProviders: %v", err)
			}
		default:
			t.Fatal("StartWorkflowProviders blocked on runtime worker startup")
		}
		<-runtimeProvider.started
	})
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
		Runtime:            runtimeProvider,
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
		Name:           "indexeddb",
		MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
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
	readinessWaitStarted := make(chan struct{})
	var readinessWaitStartedOnce sync.Once
	result := &Result{
		ExtraWorkflows: []workflow.Provider{provider},
		workflowConfigReconcileTasks: []workflowConfigReconcileTask{{
			name: "temporal",
			reconcile: func(context.Context) error {
				readinessWaitStartedOnce.Do(func() {
					close(readinessWaitStarted)
				})
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
	<-readinessWaitStarted
	select {
	case <-reconciled:
		t.Fatal("workflow config reconciliation ran before runtime workers were started")
	default:
	}
	if err := result.StartWorkflowProviders(ctx); err != nil {
		t.Fatalf("StartWorkflowProviders: %v", err)
	}
	<-reconciled
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
					Target:   workflowConfigTestAgentStepTarget(),
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
		if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, nil, includeProvider); err != nil {
			return err
		}
		return reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, nil, includeProvider)
	}
	result := &Result{
		workflowConfigReconcileTasks: runtimeWorkflowConfigReconcileTasks(workflowRuntime, runtimePlacedWorkflowProviderNames(cfg), reconcileWorkflowConfig),
	}

	result.StartWorkflowConfigReconciliation(ctx)
	<-stuckProvider.waitStarted
	<-readyProvider.upsertedSchedule
	if got := len(stuckProvider.upsertedSchedules); got != 0 {
		t.Fatalf("stuck provider schedules = %d, want 0 before readiness", got)
	}
}

func TestHostedWorkflowProviderPoolRejectsIncompatibleStartupSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeProvider := &staleSessionWorkflowRuntime{
		recordingHostedWorkflowRuntime: newRecordingHostedWorkflowRuntime(t),
		started:                        make(chan struct{}),
	}
	t.Cleanup(func() { _ = runtimeProvider.Close() })
	deps := Deps{
		BaseURL:            "http://127.0.0.1:8080",
		EncryptionKey:      []byte("0123456789abcdef0123456789abcdef"),
		Runtime:            runtimeProvider,
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
		Name:           "indexeddb",
		MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
	}}, deps)
	if err != nil {
		t.Fatalf("buildHostedWorkflowWorkerPool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("pool.Start: %v", err)
	}
	<-runtimeProvider.started
	if got := len(runtimeProvider.startAppRequestsCopy()); got != 0 {
		t.Fatalf("StartApp requests = %d, want 0 for incompatible runtime session", got)
	}
	select {
	case <-pool.ready:
		t.Fatal("pool marked ready for incompatible runtime session")
	default:
	}
	waitCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := pool.WaitReady(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pool.WaitReady canceled error = %v, want %v", err, context.Canceled)
	}
	if got := len(pool.readyWorkers()); got != 0 {
		t.Fatalf("ready workers = %d, want 0 for incompatible runtime session", got)
	}
}

func TestHostedWorkflowProviderPoolClosedStartLoopDoesNotMarkReady(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("startLoop did not exit after pool closed")
		}
		select {
		case <-pool.ready:
			t.Fatal("pool marked ready after it was closed")
		default:
		}
	})
}

func TestHostedWorkflowProviderPoolCloseUnblocksWaitReady(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
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

		waitDone := make(chan error, 1)
		go func() {
			waitDone <- pool.WaitReady(context.Background())
		}()
		synctest.Wait()
		select {
		case err := <-waitDone:
			t.Fatalf("WaitReady returned before pool close: %v", err)
		default:
		}
		if err := pool.Close(); err != nil {
			t.Fatalf("pool.Close: %v", err)
		}
		err := <-waitDone
		if !errors.Is(err, errHostedWorkflowWorkerPoolClosed) {
			t.Fatalf("WaitReady error = %v, want hosted workflow worker pool closed", err)
		}
	})
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
					Target:   workflowConfigTestAgentStepTarget(),
					Cron:     "* * * * *",
				},
				"runtime_schedule": {
					Provider: "runtime",
					Target:   workflowConfigTestAgentStepTarget(),
					Cron:     "* * * * *",
				},
			},
			EventTriggers: map[string]config.WorkflowEventTriggerConfig{
				"local_trigger": {
					Provider: "local",
					Target:   workflowConfigTestAgentStepTarget(),
					Match:    config.WorkflowEventMatch{Type: "local.changed"},
				},
				"runtime_trigger": {
					Provider: "runtime",
					Target:   workflowConfigTestAgentStepTarget(),
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

	if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, nil, localFilter); err != nil {
		t.Fatalf("reconcile local schedules: %v", err)
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, nil, localFilter); err != nil {
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

	if err := reconcileWorkflowConfigSchedules(ctx, cfg, workflowRuntime, nil, runtimeFilter); err != nil {
		t.Fatalf("reconcile runtime schedules: %v", err)
	}
	if err := reconcileWorkflowConfigEventTriggers(ctx, cfg, workflowRuntime, nil, runtimeFilter); err != nil {
		t.Fatalf("reconcile runtime event triggers: %v", err)
	}
	if got := len(runtimeProvider.upsertedSchedules); got != 1 {
		t.Fatalf("runtime upserted schedules = %d, want 1", got)
	}
	if got := len(runtimeProvider.upsertedEventTriggers); got != 1 {
		t.Fatalf("runtime upserted event triggers = %d, want 1", got)
	}
}

func workflowConfigTestAgentStepTarget() *config.WorkflowTargetConfig {
	return &config.WorkflowTargetConfig{
		Steps: []config.WorkflowStepConfig{{
			ID: "main",
			Agent: &config.WorkflowStepAgentConfig{
				Prompt: config.WorkflowTextConfig{Template: "summarize"},
				Output: &config.WorkflowAgentOutputConfig{Text: &config.WorkflowAgentTextOutputConfig{}},
			},
		}},
	}
}

func TestHostedWorkflowAllowedHostsFiltersLoopbackRelayTargets(t *testing.T) {
	t.Parallel()

	allowed := hostedWorkflowAllowedHosts([]string{"localhost", "127.0.0.1", "api.example.com"}, RuntimePlacementPlan{
		EgressMode: RuntimeEgressModeNone,
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
		Runtime:            runtimeProvider,
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
		Name:           "indexeddb",
		MethodPrefixes: []string{grpcMethodPrefix(proto.IndexedDB_ServiceDesc.ServiceName)},
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

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		worker := &hostedWorkflowWorker{
			id:       1,
			provider: &noopWorkflowProvider{},
			active:   1,
		}
		pool := &hostedWorkflowWorkerPool{
			name:    "temporal",
			ctx:     ctx,
			cancel:  cancel,
			ready:   make(chan struct{}),
			workers: []*hostedWorkflowWorker{worker},
			policy: config.RuntimePlacementLifecyclePolicy{
				DrainTimeout: 150 * time.Millisecond,
			},
		}

		done := make(chan error, 1)
		go func() {
			done <- pool.drainAndCloseWorker(worker)
		}()
		time.Sleep(30 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("drainAndCloseWorker finished while worker was active: %v", err)
		default:
		}
		pool.mu.Lock()
		worker.active = 0
		pool.mu.Unlock()
		releasedAt := time.Now()
		err := <-done
		if err != nil {
			t.Fatalf("drainAndCloseWorker: %v", err)
		}
		if elapsed := time.Since(releasedAt); elapsed > 25*time.Millisecond {
			t.Fatalf("drainAndCloseWorker elapsed after worker release = %v, want at most one poll interval", elapsed)
		}
	})
}

const providermanifestKindWorkflow = "workflow"

type recordingHostedWorkflowRuntime struct {
	provider *runtimeprovider.LocalProvider
	t        *testing.T

	mu               sync.Mutex
	startRequests    []*proto.StartRuntimeSessionRequest
	startAppRequests []*proto.StartHostedAppRequest
	servers          map[string]*recordingHostedWorkflowServer
	closeCalls       atomic.Int32
}

type blockingStartSessionWorkflowRuntime struct {
	*recordingHostedWorkflowRuntime
	started chan struct{}
	once    sync.Once
}

func (r *blockingStartSessionWorkflowRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	r.once.Do(func() {
		close(r.started)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

type staleSessionWorkflowRuntime struct {
	*recordingHostedWorkflowRuntime
	started chan struct{}
	once    sync.Once
}

func (r *staleSessionWorkflowRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	session, err := r.recordingHostedWorkflowRuntime.StartSession(ctx, req)
	if err == nil {
		r.once.Do(func() {
			close(r.started)
		})
	}
	return session, err
}

func (r *staleSessionWorkflowRuntime) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
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

func (p *notifyingRuntimeWorkflowControlProvider) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
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
	schedules             map[string]*workflow.Schedule
	upsertedSchedules     []*proto.UpsertWorkflowProviderScheduleRequest
	eventTriggers         map[string]*workflow.EventTrigger
	upsertedEventTriggers []*proto.UpsertWorkflowProviderEventTriggerRequest
}

func (p *recordingWorkflowControlProvider) GetSchedule(_ context.Context, req *proto.GetWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	if schedule := p.schedules[req.GetScheduleId()]; schedule != nil {
		return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
	}
	return nil, core.ErrNotFound
}

func (p *recordingWorkflowControlProvider) UpsertSchedule(_ context.Context, req *proto.UpsertWorkflowProviderScheduleRequest) (*proto.BoundWorkflowSchedule, error) {
	p.upsertedSchedules = append(p.upsertedSchedules, gproto.Clone(req).(*proto.UpsertWorkflowProviderScheduleRequest))
	if p.schedules == nil {
		p.schedules = map[string]*workflow.Schedule{}
	}
	schedule := &workflow.Schedule{
		ID:           req.GetScheduleId(),
		Cron:         req.GetCron(),
		Timezone:     req.GetTimezone(),
		Target:       workflowwire.TargetFromProto(req.GetTarget()),
		Paused:       req.GetPaused(),
		CreatedBy:    workflowwire.ActorFromProto(req.GetRequestedBy()),
		DefinitionID: req.GetDefinitionId(),
	}
	p.schedules[req.GetScheduleId()] = schedule
	return workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
}

func (p *recordingWorkflowControlProvider) ListSchedules(context.Context, *proto.ListWorkflowProviderSchedulesRequest) (*proto.ListWorkflowProviderSchedulesResponse, error) {
	out := &proto.ListWorkflowProviderSchedulesResponse{}
	for _, schedule := range p.schedules {
		pb, err := workflowwire.ScheduleToProto(cloneWorkflowSchedule(schedule))
		if err != nil {
			return nil, err
		}
		out.Schedules = append(out.Schedules, pb)
	}
	return out, nil
}

func (p *recordingWorkflowControlProvider) GetEventTrigger(_ context.Context, req *proto.GetWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	if trigger := p.eventTriggers[req.GetTriggerId()]; trigger != nil {
		return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
	}
	return nil, core.ErrNotFound
}

func (p *recordingWorkflowControlProvider) UpsertEventTrigger(_ context.Context, req *proto.UpsertWorkflowProviderEventTriggerRequest) (*proto.BoundWorkflowEventTrigger, error) {
	p.upsertedEventTriggers = append(p.upsertedEventTriggers, gproto.Clone(req).(*proto.UpsertWorkflowProviderEventTriggerRequest))
	if p.eventTriggers == nil {
		p.eventTriggers = map[string]*workflow.EventTrigger{}
	}
	trigger := &workflow.EventTrigger{
		ID:           req.GetTriggerId(),
		Match:        workflowwire.EventMatchFromProto(req.GetMatch()),
		Target:       workflowwire.TargetFromProto(req.GetTarget()),
		Paused:       req.GetPaused(),
		CreatedBy:    workflowwire.ActorFromProto(req.GetRequestedBy()),
		DefinitionID: req.GetDefinitionId(),
	}
	p.eventTriggers[req.GetTriggerId()] = trigger
	return workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
}

func (p *recordingWorkflowControlProvider) ListEventTriggers(context.Context, *proto.ListWorkflowProviderEventTriggersRequest) (*proto.ListWorkflowProviderEventTriggersResponse, error) {
	out := &proto.ListWorkflowProviderEventTriggersResponse{}
	for _, trigger := range p.eventTriggers {
		pb, err := workflowwire.EventTriggerToProto(cloneWorkflowEventTrigger(trigger))
		if err != nil {
			return nil, err
		}
		out.Triggers = append(out.Triggers, pb)
	}
	return out, nil
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
		provider: runtimeprovider.NewLocalProvider(),
		t:        t,
		servers:  map[string]*recordingHostedWorkflowServer{},
	}
}

func (r *recordingHostedWorkflowRuntime) Support(context.Context) (*proto.RuntimeSupport, error) {
	return &proto.RuntimeSupport{
		CanHostApps: true,
		EgressMode:  proto.RuntimeEgressMode_RUNTIME_EGRESS_MODE_HOSTNAME,
	}, nil
}

func (r *recordingHostedWorkflowRuntime) StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	r.mu.Lock()
	r.startRequests = append(r.startRequests, cloneStartRuntimeSessionRequest(req))
	r.mu.Unlock()
	return r.provider.StartSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error) {
	return r.provider.ListSessions(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error) {
	return r.provider.GetSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error {
	r.cleanupServer(req.GetSessionId())
	return r.provider.StopSession(ctx, req)
}

func (r *recordingHostedWorkflowRuntime) StartApp(_ context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error) {
	r.mu.Lock()
	r.startAppRequests = append(r.startAppRequests, cloneStartHostedAppRequest(req))
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
	r.servers[req.GetSessionId()] = workflowServer
	r.mu.Unlock()
	r.t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
		_ = os.RemoveAll(dir)
	})
	return &proto.HostedApp{
		Id:         "fake-" + req.GetSessionId(),
		SessionId:  req.GetSessionId(),
		AppName:    req.GetAppName(),
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

func (r *recordingHostedWorkflowRuntime) startSessionRequestsCopy() []*proto.StartRuntimeSessionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*proto.StartRuntimeSessionRequest, len(r.startRequests))
	for i, req := range r.startRequests {
		out[i] = cloneStartRuntimeSessionRequest(req)
	}
	return out
}

func (r *recordingHostedWorkflowRuntime) startAppRequestsCopy() []*proto.StartHostedAppRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*proto.StartHostedAppRequest, len(r.startAppRequests))
	for i, req := range r.startAppRequests {
		out[i] = cloneStartHostedAppRequest(req)
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

func (r *recordingHostedWorkflowRuntime) cleanupServer(sessionID string) {
	r.mu.Lock()
	delete(r.servers, sessionID)
	r.mu.Unlock()
}

type recordingHostedWorkflowServer struct {
	proto.UnimplementedProviderLifecycleServer
	proto.UnimplementedWorkflowProviderServer

	startProviderCalls atomic.Int32
}

func newRecordingHostedWorkflowServer() *recordingHostedWorkflowServer {
	return &recordingHostedWorkflowServer{}
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
