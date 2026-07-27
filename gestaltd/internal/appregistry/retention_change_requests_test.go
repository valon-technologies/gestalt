package appregistry_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestVersionDeploymentState_usesChangeRequestDeadlineOnly(t *testing.T) {
	t.Parallel()

	const (
		vOld     = "0.0.0-snapshot.g86f9e963e901d57fdc6a6f0c896380dde6dc7358"
		vCurrent = "0.0.0-snapshot.gb32ef9983cfb32d8657315ec78f2e4eda8a976b4"
	)
	deployedAt := time.Date(2026, 7, 27, 20, 20, 12, 967000000, time.UTC)
	deployableUntil := deployedAt.Add(720 * time.Hour)

	index := appregistry.NewEmptyRetentionIndex()
	appregistry.UpsertPublishedRetention(index, vOld, deployedAt.Add(-24*time.Hour))
	index.Versions[vOld] = appregistry.RetentionVersion{
		PublishedAt:       deployedAt.Add(-24 * time.Hour),
		EverDeployed:      true,
		FirstDeployedAt:   ptrTime(deployedAt.Add(-24 * time.Hour)),
		LastDeactivatedAt: ptrTime(deployedAt),
	}

	chain := appregistry.VersionDeploymentChainFromChangeRequests([]*core.AppVersionChangeRequest{
		{
			FromVersion:                vOld,
			ToVersion:                  vCurrent,
			Timestamp:                  deployedAt,
			FromVersionDeployableUntil: ptrTime(deployableUntil),
		},
		{
			FromVersion: "registry:first-install",
			ToVersion:   vOld,
			Timestamp:   deployedAt.Add(-24 * time.Hour),
		},
	})

	state, until := appregistry.VersionDeploymentState(vOld, vCurrent, index, appregistry.RetentionPolicy{
		UnusedRetention:   72 * time.Hour,
		DeployedRetention: 720 * time.Hour,
	}, deployedAt.Add(time.Minute), chain)
	if state != appregistry.DeploymentStateRedeployable {
		t.Fatalf("outgoing version state = %q, want redeployable", state)
	}
	if until == nil || !until.Equal(deployableUntil) {
		t.Fatalf("deployableUntil = %#v, want %s", until, deployableUntil)
	}
}

func TestVersionDeploymentState_ignoresRetentionDeployableUntil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 27, 20, 30, 0, 0, time.UTC)
	retention := appregistry.NewEmptyRetentionIndex()
	retention.Versions["v-old"] = appregistry.RetentionVersion{
		PublishedAt:       now.Add(-48 * time.Hour),
		EverDeployed:      true,
		DeployableUntil:   ptrTime(now.Add(20 * 24 * time.Hour)),
		LastDeactivatedAt: ptrTime(now.Add(-time.Hour)),
	}
	chain := appregistry.VersionDeploymentChain{
		Deployed: map[string]struct{}{"v-old": {}},
		Deadlines: map[string]time.Time{
			"v-old": now.Add(-time.Hour),
		},
	}

	state, _ := appregistry.VersionDeploymentState("v-old", "v-current", retention, appregistry.RetentionPolicy{
		UnusedRetention:   72 * time.Hour,
		DeployedRetention: 720 * time.Hour,
	}, now, chain)
	if state != appregistry.DeploymentStateLocked {
		t.Fatalf("state = %q, want locked from change-request deadline", state)
	}
}

func TestDeployableUntilDeadlinesFromChangeRequests_usesLatestDeactivation(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	second := first.Add(24 * time.Hour)
	deadlines := appregistry.DeployableUntilDeadlinesFromChangeRequests([]*core.AppVersionChangeRequest{
		{
			FromVersion:                "v1",
			ToVersion:                  "v2",
			Timestamp:                  first,
			FromVersionDeployableUntil: ptrTime(first.Add(720 * time.Hour)),
		},
		{
			FromVersion:                "v1",
			ToVersion:                  "v3",
			Timestamp:                  second,
			FromVersionDeployableUntil: ptrTime(second.Add(720 * time.Hour)),
		},
	})
	if got := deadlines["v1"]; !got.Equal(second.Add(720 * time.Hour)) {
		t.Fatalf("deadline = %s, want %s", got, second.Add(720*time.Hour))
	}
}
