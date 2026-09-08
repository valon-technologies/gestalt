package appregistry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type FleetChangeRequests interface {
	ListKnownVersionsByApp(context.Context, string) ([]*core.AppInstallation, error)
}

type FleetSourceVersions interface {
	Get(context.Context) (*core.GestaltdSourceVersionState, error)
}

type FleetHeartbeats interface {
	ListFreshBySourceVersion(context.Context, string, time.Time) ([]*core.GestaltdInstanceHeartbeat, error)
}

type FleetRollouts interface {
	Get(context.Context, string) (*core.AppRollout, error)
}

type FleetProjector struct {
	ChangeRequests FleetChangeRequests
	SourceVersions FleetSourceVersions
	Heartbeats     FleetHeartbeats
	Rollouts       FleetRollouts
	HeartbeatTTL   time.Duration
	Now            func() time.Time
}

func (p *FleetProjector) Project(ctx context.Context, app string) (*core.AppFleetProjection, error) {
	if p == nil || p.ChangeRequests == nil || p.SourceVersions == nil || p.Heartbeats == nil {
		return nil, fmt.Errorf("fleet projector is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("fleet projector: app is required")
	}
	now := p.now()
	ttl := p.HeartbeatTTL
	if ttl <= 0 {
		return nil, fmt.Errorf("fleet projector: heartbeat TTL must be positive")
	}
	known, err := p.ChangeRequests.ListKnownVersionsByApp(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("project fleet state: load desired version: %w", err)
	}
	desiredVersion := coredata.LatestKnownVersion(known)

	sourceState, err := p.SourceVersions.Get(ctx)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			projection := EvaluateFleetState(FleetEvaluation{
				App:            app,
				DesiredVersion: desiredVersion,
				Cutoff:         now.Add(-ttl),
				EvaluatedAt:    now,
			})
			projection.HeartbeatTTL = ttl
			return &projection, nil
		}
		return nil, fmt.Errorf("project fleet state: load source version: %w", err)
	}
	sourceVersion := strings.TrimSpace(sourceState.CurrentSourceVersion)
	minimum := sourceState.MinimumHealthyInstances
	var heartbeats []*core.GestaltdInstanceHeartbeat
	if sourceVersion != "" {
		heartbeats, err = p.Heartbeats.ListFreshBySourceVersion(ctx, sourceVersion, now.Add(-ttl))
		if err != nil {
			return nil, fmt.Errorf("project fleet state: load fresh heartbeats: %w", err)
		}
	}
	var rollout *core.AppRollout
	if p.Rollouts != nil {
		rollout, err = p.Rollouts.Get(ctx, app)
		if err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("project fleet state: load active rollout: %w", err)
		}
		if errors.Is(err, core.ErrNotFound) {
			rollout = nil
		}
	}
	projection := EvaluateFleetState(FleetEvaluation{
		App:                     app,
		DesiredVersion:          desiredVersion,
		SourceVersion:           sourceVersion,
		MinimumHealthyInstances: minimum,
		Cutoff:                  now.Add(-ttl),
		EvaluatedAt:             now,
		Heartbeats:              heartbeats,
		ActiveRollout:           rollout,
	})
	projection.HeartbeatTTL = ttl
	return &projection, nil
}

// ProjectForRollout evaluates the fleet using the rollout's admission
// snapshot. A terminal rollout must not be judged using a later source or
// capacity change, because that would answer a different rollout's question.
func (p *FleetProjector) ProjectForRollout(ctx context.Context, rollout *core.AppRollout) (*core.AppFleetProjection, error) {
	if p == nil || p.Heartbeats == nil {
		return nil, fmt.Errorf("fleet projector is not configured")
	}
	if rollout == nil {
		return nil, fmt.Errorf("fleet projector: rollout is required")
	}
	app := strings.TrimSpace(rollout.App)
	if app == "" {
		return nil, fmt.Errorf("fleet projector: rollout app is required")
	}
	// Enrollment rollouts predate the heartbeat snapshot fields. Preserve
	// their existing current-fleet behavior; heartbeat rollouts have the
	// explicit inputs needed for an exact historical evaluation.
	if rollout.Mode != core.AppRolloutModeHeartbeat ||
		strings.TrimSpace(rollout.TargetSourceVersion) == "" || rollout.MinimumHealthyInstances <= 0 {
		return p.Project(ctx, app)
	}
	now := p.now()
	ttl := p.HeartbeatTTL
	if ttl <= 0 {
		return nil, fmt.Errorf("fleet projector: heartbeat TTL must be positive")
	}
	heartbeats, err := p.Heartbeats.ListFreshBySourceVersion(
		ctx,
		rollout.TargetSourceVersion,
		now.Add(-ttl),
	)
	if err != nil {
		return nil, fmt.Errorf("project rollout fleet: load fresh heartbeats: %w", err)
	}
	projection := EvaluateFleetState(FleetEvaluation{
		App:                     app,
		DesiredVersion:          rollout.Version,
		SourceVersion:           rollout.TargetSourceVersion,
		MinimumHealthyInstances: rollout.MinimumHealthyInstances,
		Cutoff:                  now.Add(-ttl),
		EvaluatedAt:             now,
		Heartbeats:              heartbeats,
	})
	projection.HeartbeatTTL = ttl
	return &projection, nil
}

type FleetEvaluation struct {
	App                     string
	DesiredVersion          string
	SourceVersion           string
	MinimumHealthyInstances int
	Cutoff                  time.Time
	EvaluatedAt             time.Time
	Heartbeats              []*core.GestaltdInstanceHeartbeat
	ActiveRollout           *core.AppRollout
}

// EvaluateFleetState is the pure projection used by current fleet-state reads.
// Its explicit source, minimum, and cutoff inputs also allow a future rollout
// evaluator to use values snapshotted at rollout admission.
func EvaluateFleetState(input FleetEvaluation) core.AppFleetProjection {
	app := strings.TrimSpace(input.App)
	desiredVersion := strings.TrimSpace(input.DesiredVersion)
	sourceVersion := strings.TrimSpace(input.SourceVersion)
	evaluatedAt := input.EvaluatedAt.UTC()
	projection := core.AppFleetProjection{
		App:                     app,
		State:                   core.AppFleetStateUnknown,
		SourceVersion:           sourceVersion,
		DesiredVersion:          desiredVersion,
		MinimumHealthyInstances: input.MinimumHealthyInstances,
		EvaluatedAt:             evaluatedAt,
	}
	if !input.Cutoff.IsZero() && !evaluatedAt.IsZero() {
		projection.HeartbeatTTL = evaluatedAt.Sub(input.Cutoff.UTC())
	}

	for _, heartbeat := range input.Heartbeats {
		if heartbeat == nil ||
			strings.TrimSpace(heartbeat.SourceVersion) != sourceVersion ||
			heartbeat.HeartbeatAt.Before(input.Cutoff) {
			continue
		}
		projection.LiveInstances++
		replica := core.AppFleetReplicaObservation{
			InstanceID:  strings.TrimSpace(heartbeat.InstanceID),
			StartedAt:   heartbeat.StartedAt.UTC(),
			HeartbeatAt: heartbeat.HeartbeatAt.UTC(),
			AppState:    core.GestaltdInstanceAppStateUnknown,
			Class:       core.AppFleetReplicaClassError,
		}
		observation, ok := heartbeat.Apps[app]
		if !ok {
			projection.Errors++
			projection.Replicas = append(projection.Replicas, replica)
			continue
		}
		replica.AppState = observation.State
		replica.RunningVersion = strings.TrimSpace(observation.RunningVersion)
		replica.ObservedDesiredVersion = strings.TrimSpace(observation.DesiredVersion)
		replica.ObservedAt = observation.ObservedAt.UTC()
		replica.LastError = strings.TrimSpace(observation.LastError)
		if observation.State == core.GestaltdInstanceAppStateRunning &&
			replica.RunningVersion == desiredVersion &&
			desiredVersion != "" {
			replica.Class = core.AppFleetReplicaClassOnDesired
			projection.RunningDesiredVersion++
			projection.Replicas = append(projection.Replicas, replica)
			continue
		}
		switch observation.State {
		case core.GestaltdInstanceAppStateError, core.GestaltdInstanceAppStateUnknown:
			replica.Class = core.AppFleetReplicaClassError
			projection.Errors++
		case core.GestaltdInstanceAppStateStarting, core.GestaltdInstanceAppStateNotRunning:
			// A live host can exist without a running provider, especially for
			// low-traffic Cloud Run apps. This is neutral fleet evidence: it is
			// neither proof of the desired version nor evidence of an old one.
			replica.Class = core.AppFleetReplicaClassNotRunning
			projection.NotRunning++
		default:
			replica.Class = core.AppFleetReplicaClassMismatched
			projection.Mismatched++
		}
		projection.Replicas = append(projection.Replicas, replica)
	}
	sortFleetReplicas(projection.Replicas)

	validBasis := desiredVersion != "" && sourceVersion != "" && input.MinimumHealthyInstances > 0
	switch {
	case !validBasis || projection.LiveInstances < input.MinimumHealthyInstances:
		projection.State = core.AppFleetStateUnknown
	case projection.RunningDesiredVersion >= input.MinimumHealthyInstances &&
		projection.Mismatched == 0 && projection.Errors == 0:
		projection.State = core.AppFleetStateHealthy
	default:
		projection.State = core.AppFleetStateDegraded
	}
	if validBasis && projection.State != core.AppFleetStateHealthy && rolloutMatches(input.ActiveRollout, app, desiredVersion, sourceVersion, evaluatedAt) {
		projection.State = core.AppFleetStateConverging
	}
	return projection
}

// sortFleetReplicas makes live replica order stable for API consumers:
// newest heartbeat first, then instance id.
func sortFleetReplicas(replicas []core.AppFleetReplicaObservation) {
	slices.SortFunc(replicas, func(a, b core.AppFleetReplicaObservation) int {
		if byHeartbeat := b.HeartbeatAt.Compare(a.HeartbeatAt); byHeartbeat != 0 {
			return byHeartbeat
		}
		return strings.Compare(a.InstanceID, b.InstanceID)
	})
}

func rolloutMatches(rollout *core.AppRollout, app, version, sourceVersion string, now time.Time) bool {
	if rollout == nil ||
		(rollout.State != core.AppRolloutStateEnrolling && rollout.State != core.AppRolloutStateRestarting) {
		return false
	}
	return strings.TrimSpace(rollout.App) == app &&
		strings.TrimSpace(rollout.Version) == version &&
		strings.TrimSpace(rollout.TargetSourceVersion) == sourceVersion &&
		rollout.Deadline.After(now)
}

func (p *FleetProjector) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
