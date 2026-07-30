package coredata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestHeartbeatRolloutEvaluationStabilityDeadlineAndSummary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	rollout := createHeartbeatRollout(t, services, start, start.Add(3*time.Minute))

	evaluate := func(at time.Time, healthy bool) (*core.AppRollout, bool) {
		t.Helper()
		current, err := services.AppRollouts.Get(ctx, rollout.App)
		if err != nil {
			t.Fatal(err)
		}
		updated, transitioned, err := services.AppRollouts.EvaluateHeartbeatRollout(ctx, current, coredata.HeartbeatRolloutEvaluation{
			Healthy:         healthy,
			StabilityWindow: time.Minute,
			EvaluatedAt:     at,
			FailureSummary: core.AppRolloutFailureSummary{
				LiveInstances:         2,
				RunningDesiredVersion: 1,
				Mismatched:            1,
			},
		})
		if err != nil {
			t.Fatalf("EvaluateHeartbeatRollout: %v", err)
		}
		return updated, transitioned
	}

	got, transitioned := evaluate(start, true)
	if transitioned || !got.HealthySince.Equal(start) {
		t.Fatalf("first healthy observation = %#v, transitioned=%v", got, transitioned)
	}
	got, _ = evaluate(start.Add(30*time.Second), true)
	if !got.HealthySince.Equal(start) {
		t.Fatalf("healthy_since moved to %v", got.HealthySince)
	}
	got, _ = evaluate(start.Add(45*time.Second), false)
	if !got.HealthySince.IsZero() {
		t.Fatalf("healthy_since was not reset: %v", got.HealthySince)
	}
	got, _ = evaluate(start.Add(2*time.Minute), true)
	if !got.HealthySince.Equal(start.Add(2 * time.Minute)) {
		t.Fatalf("healthy_since after reset = %v", got.HealthySince)
	}
	got, transitioned = evaluate(start.Add(3*time.Minute), true)
	if !transitioned || got.State != core.AppRolloutStateComplete {
		t.Fatalf("exact deadline completion = %#v, transitioned=%v", got, transitioned)
	}

	services = testutil.NewStubServices(t)
	rollout = createHeartbeatRollout(t, services, start, start.Add(3*time.Minute))
	_, _ = evaluate(start.Add(2*time.Minute+time.Second), true)
	got, transitioned = evaluate(start.Add(3*time.Minute), true)
	if !transitioned || got.State != core.AppRolloutStateFailed || got.FailureSummary == nil {
		t.Fatalf("deadline failure = %#v, transitioned=%v", got, transitioned)
	}
	summary := got.FailureSummary
	if summary.LiveInstances != 2 || summary.MinimumHealthyInstances != 2 ||
		summary.RunningDesiredVersion != 1 || summary.Mismatched != 1 ||
		summary.SourceVersion != "source-a" || summary.Version != "v2" ||
		!summary.EvaluatedAt.Equal(start.Add(3*time.Minute)) {
		t.Fatalf("failure summary = %#v", summary)
	}
}

func TestHeartbeatRolloutEvaluationEpochAndConcurrencyFences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	stale := createHeartbeatRollout(t, services, start, start.Add(15*time.Minute))
	if _, _, err := services.AppRollouts.EvaluateHeartbeatRollout(ctx, stale, coredata.HeartbeatRolloutEvaluation{
		Healthy: true, StabilityWindow: time.Minute, EvaluatedAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := services.GestaltdSourceVersionState.ActivateWithRolloutMode(
		ctx, "source-b", start.Add(2*time.Minute), false, 2*time.Minute, 15*time.Minute,
		core.AppRolloutModeHeartbeat, 3,
	); err != nil {
		t.Fatalf("ActivateWithRolloutMode: %v", err)
	}
	if _, _, err := services.AppRollouts.EvaluateHeartbeatRollout(ctx, stale, coredata.HeartbeatRolloutEvaluation{
		Healthy: true, StabilityWindow: time.Minute, EvaluatedAt: start.Add(3 * time.Minute),
	}); !errors.Is(err, coredata.ErrAppRolloutEpochMismatch) {
		t.Fatalf("stale evaluator error = %v", err)
	}
	retargeted, err := services.AppRollouts.Get(ctx, stale.App)
	if err != nil {
		t.Fatal(err)
	}
	if retargeted.Mode != core.AppRolloutModeHeartbeat || retargeted.State != core.AppRolloutStateRestarting ||
		retargeted.TargetSourceVersion != "source-b" || retargeted.MinimumHealthyInstances != 3 ||
		!retargeted.HealthySince.IsZero() || retargeted.FailureSummary != nil {
		t.Fatalf("retargeted rollout = %#v", retargeted)
	}
	retargeted, _, err = services.AppRollouts.EvaluateHeartbeatRollout(ctx, retargeted, coredata.HeartbeatRolloutEvaluation{
		Healthy: true, StabilityWindow: time.Minute, EvaluatedAt: start.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("seed healthy evaluation: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, transitioned, evalErr := services.AppRollouts.EvaluateHeartbeatRollout(ctx, retargeted, coredata.HeartbeatRolloutEvaluation{
				Healthy: true, StabilityWindow: time.Minute, EvaluatedAt: start.Add(4 * time.Minute),
			})
			if evalErr != nil {
				t.Errorf("concurrent evaluation: %v", evalErr)
			}
			results <- transitioned
		}()
	}
	wg.Wait()
	close(results)
	transitions := 0
	for transitioned := range results {
		if transitioned {
			transitions++
		}
	}
	if transitions != 1 {
		t.Fatalf("terminal transitions = %d, want 1", transitions)
	}
}

func createHeartbeatRollout(
	t *testing.T,
	services *coredata.Services,
	start, deadline time.Time,
) *core.AppRollout {
	t.Helper()
	ctx := context.Background()
	if _, err := services.GestaltdSourceVersionState.ActivateWithRolloutMode(
		ctx, "source-a", start.Add(-time.Minute), false, 2*time.Minute, 15*time.Minute,
		core.AppRolloutModeHeartbeat, 2,
	); err != nil {
		t.Fatalf("ActivateWithRolloutMode: %v", err)
	}
	rollout, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v2",
		State:            core.AppRolloutStateRestarting,
		Mode:             core.AppRolloutModeHeartbeat,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(2 * time.Minute),
		Deadline:         deadline,
	})
	if err != nil {
		t.Fatalf("CreateAppRollout: %v", err)
	}
	return rollout
}
