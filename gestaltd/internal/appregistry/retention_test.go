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
	appregistry.UpsertPublishedRetention(retention, "v-new", now.Add(-96*time.Hour), policy)
	retention.Versions["v-old"] = appregistry.RetentionVersion{
		PublishedAt:  now.Add(-800 * time.Hour),
		EverDeployed: true,
		ExpiresAt:    ptrTime(now.Add(-10 * time.Hour)),
	}
	retention.Versions["v-stale"] = appregistry.RetentionVersion{
		PublishedAt:  now.Add(-800 * time.Hour),
		EverDeployed: true,
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
	state, _ = appregistry.VersionDeploymentState("v-stale", "v-current", retention, policy, now)
	if state != appregistry.DeploymentStateRedeployable {
		t.Fatalf("missing expiresAt should lean redeployable, got %q", state)
	}
}

func TestUpsertPublishedRetentionSetsExpiresAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := appregistry.RetentionPolicy{UnusedRetention: 72 * time.Hour}
	retention := appregistry.NewEmptyRetentionIndex()
	appregistry.UpsertPublishedRetention(retention, "v1", now, policy)
	entry := retention.Versions["v1"]
	if entry.ExpiresAt == nil {
		t.Fatal("expected expiresAt to be set on publish")
	}
	expected := now.Add(72 * time.Hour)
	if !entry.ExpiresAt.Equal(expected) {
		t.Fatalf("expiresAt = %s, want %s", entry.ExpiresAt, expected)
	}
}

func TestApplyDesiredVersionTransitionExpiresAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := appregistry.RetentionPolicy{DeployedRetention: 720 * time.Hour}
	retention := appregistry.NewEmptyRetentionIndex()
	appregistry.ApplyDesiredVersionTransition(retention, "v1", "v2", policy, now)

	from := retention.Versions["v1"]
	if from.ExpiresAt == nil {
		t.Fatal("expected outgoing expiresAt")
	}
	if !from.ExpiresAt.Equal(now.Add(720 * time.Hour)) {
		t.Fatalf("outgoing expiresAt = %s", from.ExpiresAt)
	}
	to := retention.Versions["v2"]
	if to.ExpiresAt != nil {
		t.Fatalf("incoming expiresAt = %v, want cleared", to.ExpiresAt)
	}
	if !to.EverDeployed {
		t.Fatal("incoming version should be marked everDeployed")
	}
}

func ptrTime(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
