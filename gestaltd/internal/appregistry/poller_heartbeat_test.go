package appregistry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestHeartbeatRolloutUsesFreshTargetSourceFleetAndReplacementReplicas(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	rollout := heartbeatPollerRollout(t, services, start, 2)
	clock := start
	poller := heartbeatRolloutPoller(services, &clock)

	upsertRolloutHeartbeat(t, services, "departed", "source-a", start.Add(-time.Minute), "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "old-source", "source-old", start, "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "replacement-a", "source-a", start, "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "replacement-b", "source-a", start, "v2", core.GestaltdInstanceAppStateRunning)
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, rollout); err != nil || terminal {
		t.Fatalf("first evaluation terminal=%v err=%v", terminal, err)
	}
	current, err := services.AppRollouts.Get(ctx, rollout.App)
	if err != nil {
		t.Fatal(err)
	}
	if !current.HealthySince.Equal(start) {
		t.Fatalf("healthy_since = %v, want %v", current.HealthySince, start)
	}

	clock = start.Add(30 * time.Second)
	upsertRolloutHeartbeat(t, services, "autoscaled-unhealthy", "source-a", clock, "v1", core.GestaltdInstanceAppStateRunning)
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, current); err != nil || terminal {
		t.Fatalf("unhealthy evaluation terminal=%v err=%v", terminal, err)
	}
	current, _ = services.AppRollouts.Get(ctx, rollout.App)
	if !current.HealthySince.IsZero() {
		t.Fatalf("autoscaled unhealthy replica did not reset healthy_since: %v", current.HealthySince)
	}

	clock = start.Add(2 * time.Minute)
	upsertRolloutHeartbeat(t, services, "replacement-c", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "replacement-d", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, current); err != nil || terminal {
		t.Fatalf("replacement evaluation terminal=%v err=%v", terminal, err)
	}
	current, _ = services.AppRollouts.Get(ctx, rollout.App)
	if !current.HealthySince.Equal(clock) {
		t.Fatalf("replacement healthy_since = %v, want %v", current.HealthySince, clock)
	}

	clock = clock.Add(time.Minute)
	upsertRolloutHeartbeat(t, services, "replacement-c", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "replacement-d", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, evalErr := heartbeatRolloutPoller(services, &clock).updateHeartbeatRolloutOutcome(ctx, current); evalErr != nil {
				t.Errorf("concurrent poller evaluation: %v", evalErr)
			}
		}()
	}
	wg.Wait()
	current, err = services.AppRollouts.Get(ctx, rollout.App)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != core.AppRolloutStateComplete {
		t.Fatalf("rollout = %#v, want complete", current)
	}
	changeID, err := services.AppVersionChangeRequests.LatestRevisionIDForVersion(ctx, rollout.App, rollout.Version)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := services.AppVersionRolloutOutcomes.Get(ctx, changeID)
	if err != nil || outcome.CompletedAt.IsZero() {
		t.Fatalf("outcome = %#v, err=%v", outcome, err)
	}
}

func heartbeatPollerRollout(t *testing.T, services *coredata.Services, start time.Time, minimum int) *core.AppRollout {
	t.Helper()
	ctx := context.Background()
	if _, err := services.GestaltdSourceVersionState.ActivateWithRolloutMode(
		ctx, "source-a", start.Add(-time.Minute), false, 2*time.Minute, 15*time.Minute,
		core.AppRolloutModeHeartbeat, minimum,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		App: "g-issues", FromVersion: "v1", ToVersion: "v2", Timestamp: start,
	}); err != nil {
		t.Fatal(err)
	}
	rollout, err := services.GestaltdSourceVersionState.CreateAppRollout(ctx, &core.AppRollout{
		App: "g-issues", Version: "v2", State: core.AppRolloutStateRestarting,
		Mode: core.AppRolloutModeHeartbeat, CreatedAt: start,
		EnrollmentEndsAt: start.Add(2 * time.Minute), Deadline: start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rollout
}

func heartbeatRolloutPoller(services *coredata.Services, clock *time.Time) *CatalogPoller {
	return NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:         services.AppVersionChangeRequests,
		Materializations:       services.AppInstanceMaterializations,
		Rollouts:               services.AppRollouts,
		RolloutOutcomes:        services.AppVersionRolloutOutcomes,
		Heartbeats:             services.GestaltdInstanceHeartbeats,
		HeartbeatTTL:           45 * time.Second,
		HealthyStabilityWindow: time.Minute,
		Now:                    func() time.Time { return *clock },
	})
}

func upsertRolloutHeartbeat(
	t *testing.T,
	services *coredata.Services,
	instance, source string,
	at time.Time,
	version string,
	state core.GestaltdInstanceAppState,
) {
	t.Helper()
	_, err := services.GestaltdInstanceHeartbeats.Upsert(context.Background(), &core.GestaltdInstanceHeartbeat{
		InstanceID: instance, SourceVersion: source, StartedAt: at.Add(-time.Minute), HeartbeatAt: at,
		Apps: map[string]core.GestaltdInstanceAppHeartbeat{
			"g-issues": {
				State: state, DesiredVersion: "v2", RunningVersion: version, ObservedAt: at,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
