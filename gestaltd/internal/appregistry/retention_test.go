package appregistry_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestVersionDeploymentState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := appregistry.RetentionPolicy{
		UnusedRetention:   72 * time.Hour,
		DeployedRetention: 720 * time.Hour,
	}
	retention := appregistry.NewEmptyRetentionIndex()
	appregistry.UpsertPublishedRetention(retention, "v-new", now.Add(-96*time.Hour))
	retention.Versions["v-old"] = appregistry.RetentionVersion{
		PublishedAt:       now.Add(-800 * time.Hour),
		EverDeployed:      true,
		FirstDeployedAt:   ptrTime(now.Add(-800 * time.Hour)),
		LastDeactivatedAt: ptrTime(now.Add(-100 * time.Hour)),
		DeployableUntil:   ptrTime(now.Add(-10 * time.Hour)),
	}

	state, _ := appregistry.VersionDeploymentState("v-current", "v-current", retention, policy, now)
	if state != appregistry.DeploymentStateDesired {
		t.Fatalf("desired state = %q", state)
	}
	state, _ = appregistry.VersionDeploymentState("v-new", "v-current", retention, policy, now)
	if state != appregistry.DeploymentStateExpired {
		t.Fatalf("expired state = %q", state)
	}
	state, _ = appregistry.VersionDeploymentState("v-old", "v-current", retention, policy, now)
	if state != appregistry.DeploymentStateLocked {
		t.Fatalf("locked state = %q", state)
	}
}

func ptrTime(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
