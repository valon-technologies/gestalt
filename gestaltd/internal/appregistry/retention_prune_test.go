package appregistry_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestEvaluateRetentionPrune(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := appregistry.RetentionPolicy{UnusedRetention: 72 * time.Hour, DeployedRetention: 720 * time.Hour}
	index := appregistry.NewEmptyIndex()
	index.Apps["g-issues"] = appregistry.AppVersions{
		Versions: map[string]appregistry.IndexVersion{
			"v-unused": {PublishedAt: now.Add(-96 * time.Hour)},
			"v-old":    {PublishedAt: now.Add(-800 * time.Hour)},
		},
	}
	retention := appregistry.NewEmptyRetentionIndex()
	appregistry.UpsertPublishedRetention(retention, "v-unused", now.Add(-96*time.Hour))
	retention.Versions["v-old"] = appregistry.RetentionVersion{
		PublishedAt:       now.Add(-800 * time.Hour),
		EverDeployed:      true,
		DeployableUntil:   ptrTime(now.Add(-1 * time.Hour)),
		FirstDeployedAt:   ptrTime(now.Add(-800 * time.Hour)),
		LastDeactivatedAt: ptrTime(now.Add(-40 * 24 * time.Hour)),
	}

	actions := appregistry.EvaluateRetentionPrune(index, retention, "g-issues", "v-current", map[string]struct{}{"v-old": {}}, policy, now)
	if len(actions) < 2 {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestDeployedVersionsFromChangeRequests(t *testing.T) {
	t.Parallel()

	versions := appregistry.DeployedVersionsFromChangeRequests([]*core.AppVersionChangeRequest{
		{FromVersion: appregistry.FirstInstallFromVersion, ToVersion: "v1"},
		{FromVersion: "v1", ToVersion: "v2"},
	})
	if _, ok := versions["v1"]; !ok || len(versions) != 2 {
		t.Fatalf("versions = %#v", versions)
	}
}

func ptrTime(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
