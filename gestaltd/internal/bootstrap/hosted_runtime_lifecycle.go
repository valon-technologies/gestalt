package bootstrap

import (
	"fmt"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
)

const hostedRuntimeLifecycleSafetyMargin = 5 * time.Second

func runtimeSessionRetirementReason(session *proto.RuntimeSession, drainAt *time.Time, now time.Time) string {
	if session == nil {
		return ""
	}
	switch session.GetState() {
	case runtimeprovider.SessionStateFailed, runtimeprovider.SessionStateStopped:
		return fmt.Sprintf("runtime session entered %q state", session.GetState())
	}
	if reason := hostedRuntimeSessionCompatibilityReason(session); reason != "" {
		return reason
	}
	if drainAt == nil || now.Before(*drainAt) {
		return ""
	}
	if expiresAt := runtimeSessionExpiresAt(session); expiresAt != nil && !now.Before(*expiresAt) {
		return fmt.Sprintf("runtime session expired at %s", expiresAt.Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("runtime session reached drain deadline %s", drainAt.Format(time.RFC3339Nano))
}

func runtimeSessionDrainAt(session *proto.RuntimeSession, now time.Time, policy config.RuntimePlacementLifecyclePolicy) *time.Time {
	lifecycle := session.GetLifecycle()
	if lifecycle == nil {
		return nil
	}
	var drainAt *time.Time
	if lifecycle.GetRecommendedDrainAt() != nil {
		recommended := lifecycle.GetRecommendedDrainAt().AsTime().UTC()
		drainAt = &recommended
	}
	if lifecycle.GetExpiresAt() != nil {
		expiryDrain := runtimeSessionExpiryDrainAt(lifecycle, now, policy)
		if drainAt == nil || expiryDrain.Before(*drainAt) {
			drainAt = &expiryDrain
		}
	}
	return drainAt
}

func runtimeSessionExpiryDrainAt(lifecycle *proto.RuntimeSessionLifecycle, now time.Time, policy config.RuntimePlacementLifecyclePolicy) time.Time {
	expiresAt := lifecycle.GetExpiresAt().AsTime().UTC()
	reserve := policy.StartupTimeout + policy.DrainTimeout + policy.HealthCheckInterval + hostedRuntimeLifecycleSafetyMargin
	drainAt := expiresAt.Add(-reserve).UTC()
	if lifecycle.GetStartedAt() != nil {
		startedAt := lifecycle.GetStartedAt().AsTime().UTC()
		lifetime := expiresAt.Sub(startedAt)
		if lifetime > 0 {
			minDrainAt := startedAt.Add(lifetime / 2).UTC()
			if drainAt.Before(minDrainAt) {
				return minDrainAt
			}
		}
	}
	if !now.IsZero() && expiresAt.After(now) && drainAt.Before(now) {
		return now.Add(expiresAt.Sub(now) / 2).UTC()
	}
	return drainAt
}

func runtimeSessionExpiresAt(session *proto.RuntimeSession) *time.Time {
	if session.GetLifecycle().GetExpiresAt() == nil {
		return nil
	}
	expiresAt := session.GetLifecycle().GetExpiresAt().AsTime().UTC()
	return &expiresAt
}
