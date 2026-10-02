package appregistry_test

import (
	"encoding/json"
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
	appregistry.UpsertPublishedRetention(retention, "v-unused", now.Add(-96*time.Hour), policy)
	retention.Versions["v-old"] = appregistry.RetentionVersion{
		PublishedAt:  now.Add(-800 * time.Hour),
		EverDeployed: true,
		ExpiresAt:    ptrTime(now.Add(-1 * time.Hour)),
	}

	actions := appregistry.EvaluateRetentionPrune(index, retention, "g-issues", "v-current", map[string]struct{}{"v-old": {}}, policy, now)
	if len(actions) < 2 {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestApplyRetentionPruneActionLeavesDecodableIndex(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	action := appregistry.RetentionPruneAction{Version: "v-only", Kind: appregistry.RetentionPruneDeleteUnused}

	prunedToEmpty := appregistry.NewEmptyIndex()
	prunedToEmpty.Apps["g-issues"] = appregistry.AppVersions{
		Versions: map[string]appregistry.IndexVersion{
			"v-only": {PublishedAt: now},
		},
	}
	retention := appregistry.NewEmptyRetentionIndex()
	appregistry.UpsertPublishedRetention(retention, "v-only", now, appregistry.RetentionPolicy{})

	if !appregistry.ApplyRetentionPruneAction(prunedToEmpty, retention, "g-issues", action, now) {
		t.Fatal("expected the prune action to apply")
	}
	if _, ok := prunedToEmpty.Apps["g-issues"]; ok {
		t.Fatal("expected the pruned-to-empty app entry to be removed from the index")
	}
	data, err := json.Marshal(prunedToEmpty)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if _, err := appregistry.DecodeIndex(data); err != nil {
		t.Fatalf("pruned index no longer decodes: %v", err)
	}

	keptVersion := appregistry.NewEmptyIndex()
	keptVersion.Apps["g-issues"] = appregistry.AppVersions{
		Versions: map[string]appregistry.IndexVersion{
			"v-only": {PublishedAt: now},
			"v-kept": {PublishedAt: now},
		},
	}
	if !appregistry.ApplyRetentionPruneAction(keptVersion, appregistry.NewEmptyRetentionIndex(), "g-issues", action, now) {
		t.Fatal("expected the prune action to apply")
	}
	appVersions, ok := keptVersion.Apps["g-issues"]
	if !ok {
		t.Fatal("expected the app entry with a remaining version to stay in the index")
	}
	if _, ok := appVersions.Versions["v-kept"]; !ok {
		t.Fatal("expected the unpruned version to remain")
	}
}

func TestShouldApplyRetentionPruneActionRace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	action := appregistry.RetentionPruneAction{Version: "v1", Kind: appregistry.RetentionPruneDeleteArtifact}
	expired := appregistry.RetentionVersion{
		EverDeployed: true,
		ExpiresAt:    ptrTime(now.Add(-1 * time.Hour)),
	}
	if !appregistry.ShouldApplyRetentionPruneAction(action, expired, now) {
		t.Fatal("expected expired historical version to remain eligible")
	}
	redeployed := appregistry.RetentionVersion{
		EverDeployed: true,
		ExpiresAt:    nil,
	}
	if appregistry.ShouldApplyRetentionPruneAction(action, redeployed, now) {
		t.Fatal("expected cleared expiresAt to skip prune")
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
