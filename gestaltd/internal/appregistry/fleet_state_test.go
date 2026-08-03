package appregistry

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestEvaluateFleetState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-45 * time.Second)
	healthy := func(id string, at time.Time) *core.GestaltdInstanceHeartbeat {
		return heartbeatForFleet(id, "source", at, map[string]core.GestaltdInstanceAppHeartbeat{
			"app": {State: core.GestaltdInstanceAppStateRunning, RunningVersion: "v2"},
		})
	}
	activeRollout := &core.AppRollout{
		App:                 "app",
		Version:             "v2",
		State:               core.AppRolloutStateRestarting,
		TargetSourceVersion: "source",
		Deadline:            now.Add(time.Minute),
	}

	tests := []struct {
		name       string
		minimum    int
		heartbeats []*core.GestaltdInstanceHeartbeat
		rollout    *core.AppRollout
		wantState  core.AppFleetState
		wantLive   int
		wantRun    int
		wantMis    int
		wantErrors int
	}{
		{
			name:    "source filtering and TTL boundary",
			minimum: 1,
			heartbeats: []*core.GestaltdInstanceHeartbeat{
				healthy("boundary", cutoff),
				heartbeatForFleet("old-source", "old", now, map[string]core.GestaltdInstanceAppHeartbeat{}),
				healthy("stale", cutoff.Add(-time.Nanosecond)),
			},
			wantState: core.AppFleetStateHealthy,
			wantLive:  1,
			wantRun:   1,
		},
		{
			name:       "insufficient capacity",
			minimum:    2,
			heartbeats: []*core.GestaltdInstanceHeartbeat{healthy("one", now)},
			wantState:  core.AppFleetStateUnknown,
			wantLive:   1,
			wantRun:    1,
		},
		{
			name:    "missing app observation",
			minimum: 1,
			heartbeats: []*core.GestaltdInstanceHeartbeat{
				heartbeatForFleet("missing", "source", now, map[string]core.GestaltdInstanceAppHeartbeat{}),
			},
			wantState:  core.AppFleetStateDegraded,
			wantLive:   1,
			wantErrors: 1,
		},
		{
			name:    "version mismatch and runtime error",
			minimum: 2,
			heartbeats: []*core.GestaltdInstanceHeartbeat{
				heartbeatForFleet("mismatch", "source", now, map[string]core.GestaltdInstanceAppHeartbeat{
					"app": {State: core.GestaltdInstanceAppStateRunning, RunningVersion: "v1"},
				}),
				heartbeatForFleet("error", "source", now, map[string]core.GestaltdInstanceAppHeartbeat{
					"app": {State: core.GestaltdInstanceAppStateError, LastError: "failed"},
				}),
			},
			wantState:  core.AppFleetStateDegraded,
			wantLive:   2,
			wantMis:    1,
			wantErrors: 1,
		},
		{
			name:       "autoscaling requires every replica healthy",
			minimum:    2,
			heartbeats: []*core.GestaltdInstanceHeartbeat{healthy("one", now), healthy("two", now), healthy("three", now)},
			wantState:  core.AppFleetStateHealthy,
			wantLive:   3,
			wantRun:    3,
		},
		{
			name:    "matching active rollout overlays converging",
			minimum: 1,
			heartbeats: []*core.GestaltdInstanceHeartbeat{
				heartbeatForFleet("mismatch", "source", now, map[string]core.GestaltdInstanceAppHeartbeat{
					"app": {State: core.GestaltdInstanceAppStateRunning, RunningVersion: "v1"},
				}),
			},
			rollout:   activeRollout,
			wantState: core.AppFleetStateConverging,
			wantLive:  1,
			wantMis:   1,
		},
		{
			name:    "expired rollout does not overlay",
			minimum: 1,
			heartbeats: []*core.GestaltdInstanceHeartbeat{
				heartbeatForFleet("mismatch", "source", now, map[string]core.GestaltdInstanceAppHeartbeat{
					"app": {State: core.GestaltdInstanceAppStateRunning, RunningVersion: "v1"},
				}),
			},
			rollout: &core.AppRollout{
				App:                 "app",
				Version:             "v2",
				State:               core.AppRolloutStateRestarting,
				TargetSourceVersion: "source",
				Deadline:            now,
			},
			wantState: core.AppFleetStateDegraded,
			wantLive:  1,
			wantMis:   1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateFleetState(FleetEvaluation{
				App:                     "app",
				DesiredVersion:          "v2",
				SourceVersion:           "source",
				MinimumHealthyInstances: tc.minimum,
				Cutoff:                  cutoff,
				EvaluatedAt:             now,
				Heartbeats:              tc.heartbeats,
				ActiveRollout:           tc.rollout,
			})
			if got.State != tc.wantState ||
				got.LiveInstances != tc.wantLive ||
				got.RunningDesiredVersion != tc.wantRun ||
				got.Mismatched != tc.wantMis ||
				got.Errors != tc.wantErrors {
				t.Fatalf("projection = %#v", got)
			}
			if len(got.Replicas) != got.LiveInstances {
				t.Fatalf("replicas len = %d, liveInstances = %d", len(got.Replicas), got.LiveInstances)
			}
			var onDesired, mismatched, errors int
			for _, replica := range got.Replicas {
				switch replica.Class {
				case core.AppFleetReplicaClassOnDesired:
					onDesired++
				case core.AppFleetReplicaClassMismatched:
					mismatched++
				case core.AppFleetReplicaClassError:
					errors++
				default:
					t.Fatalf("unexpected replica class %q in %#v", replica.Class, replica)
				}
			}
			if onDesired != got.RunningDesiredVersion || mismatched != got.Mismatched || errors != got.Errors {
				t.Fatalf("replica class counts on=%d mis=%d err=%d; aggregates run=%d mis=%d err=%d",
					onDesired, mismatched, errors, got.RunningDesiredVersion, got.Mismatched, got.Errors)
			}
		})
	}
}

func TestEvaluateFleetStateRequiresDesiredSourceAndMinimum(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		version string
		source  string
		minimum int
	}{
		{name: "missing desired", source: "source", minimum: 1},
		{name: "missing source", version: "v2", minimum: 1},
		{name: "missing minimum", version: "v2", source: "source"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateFleetState(FleetEvaluation{
				App:                     "app",
				DesiredVersion:          tc.version,
				SourceVersion:           tc.source,
				MinimumHealthyInstances: tc.minimum,
				Cutoff:                  now.Add(-time.Minute),
				EvaluatedAt:             now,
			})
			if got.State != core.AppFleetStateUnknown {
				t.Fatalf("state = %q, want unknown", got.State)
			}
		})
	}
}

func heartbeatForFleet(
	instanceID string,
	sourceVersion string,
	heartbeatAt time.Time,
	apps map[string]core.GestaltdInstanceAppHeartbeat,
) *core.GestaltdInstanceHeartbeat {
	return &core.GestaltdInstanceHeartbeat{
		InstanceID:    instanceID,
		SourceVersion: sourceVersion,
		HeartbeatAt:   heartbeatAt,
		Apps:          apps,
	}
}
