package bootstrap

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestHostedWorkflowProviderPoolStartupDoesNotBlockWorkflowReadiness(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		poolCtx, poolCancel := context.WithCancel(context.Background())
		defer poolCancel()

		pool := &hostedWorkflowWorkerPool{
			ctx:    poolCtx,
			cancel: poolCancel,
			ready:  make(chan struct{}),
			policy: config.RuntimePlacementLifecyclePolicy{
				RestartPolicy: config.RuntimePlacementRestartPolicyNever,
			},
		}
		provider := wrapWorkflowProviderWithRuntimeWorkers(&noopWorkflowProvider{}, pool)
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
	})
}
