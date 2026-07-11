package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

func TestAppInstallationService(t *testing.T) {
	t.Parallel()

	t.Run("PutGet_round_trip", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		activeSince := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		got, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:            "g-issues",
			VersionConstraint:  "0.0.0-snapshot.gabc123",
			ResolvedVersion:    "0.0.0-snapshot.gabc123",
			SourceRef:          "abc123def456abc123def456abc123def456abcd",
			Registry:           "toolshed",
			ProviderReleaseURL: "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry/apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
			ArtifactChecksums: map[string]string{
				"linux/amd64": "sha256:deadbeef",
			},
			RolloutStatus:           core.AppInstallationRolloutStatusPromoted,
			ActiveSince:             &activeSince,
			PreviousResolvedVersion: "",
			InstalledBy:             "user:alice",
		})
		if err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}
		if got.AppName != "g-issues" {
			t.Fatalf("AppName = %q, want g-issues", got.AppName)
		}
		if got.ResolvedVersion != "0.0.0-snapshot.gabc123" {
			t.Fatalf("ResolvedVersion = %q", got.ResolvedVersion)
		}
		if got.ArtifactChecksums["linux/amd64"] != "sha256:deadbeef" {
			t.Fatalf("ArtifactChecksums = %#v", got.ArtifactChecksums)
		}
		if got.InstalledAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Fatalf("InstalledAt/UpdatedAt should be set: %+v", got)
		}

		stored, err := svc.AppInstallations.GetInstallation(ctx, "g-issues")
		if err != nil {
			t.Fatalf("GetInstallation: %v", err)
		}
		if stored.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
			t.Fatalf("RolloutStatus = %q, want promoted", stored.RolloutStatus)
		}
		if stored.ActiveSince == nil || !stored.ActiveSince.Equal(activeSince) {
			t.Fatalf("ActiveSince = %v, want %v", stored.ActiveSince, activeSince)
		}
	})

	t.Run("GetInstallation_not_found", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		_, err = svc.AppInstallations.GetInstallation(context.Background(), "missing")
		if err != core.ErrNotFound {
			t.Fatalf("GetInstallation = %v, want ErrNotFound", err)
		}
	})

	t.Run("PutInstallation_rejects_unknown_rollout_status", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		_, err = svc.AppInstallations.PutInstallation(context.Background(), &core.AppInstallation{
			AppName:       "g-issues",
			RolloutStatus: "installing",
		})
		if err == nil {
			t.Fatal("expected unsupported rollout_status error")
		}
	})

	t.Run("ListInstallations_filters_by_rollout_status", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		for _, installation := range []*core.AppInstallation{
			{AppName: "g-issues", RolloutStatus: core.AppInstallationRolloutStatusPromoted, ResolvedVersion: "v1"},
			{AppName: "dealHub", RolloutStatus: core.AppInstallationRolloutStatusPending, ResolvedVersion: "v2"},
		} {
			if _, err := svc.AppInstallations.PutInstallation(ctx, installation); err != nil {
				t.Fatalf("PutInstallation(%s): %v", installation.AppName, err)
			}
		}

		promoted, err := svc.AppInstallations.ListInstallations(ctx, core.AppInstallationRolloutStatusPromoted)
		if err != nil {
			t.Fatalf("ListInstallations(promoted): %v", err)
		}
		if len(promoted) != 1 || promoted[0].AppName != "g-issues" {
			t.Fatalf("promoted installations = %#v, want g-issues only", promoted)
		}

		all, err := svc.AppInstallations.ListInstallations(ctx, "")
		if err != nil {
			t.Fatalf("ListInstallations(all): %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("all installations = %d, want 2", len(all))
		}
	})

	t.Run("UpdateInstallation", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		if _, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:         "g-issues",
			RolloutStatus:   core.AppInstallationRolloutStatusPending,
			ResolvedVersion: "v1",
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		updated, err := svc.AppInstallations.UpdateInstallation(ctx, "g-issues", func(installation *core.AppInstallation) error {
			installation.PreviousResolvedVersion = installation.ResolvedVersion
			installation.ResolvedVersion = "v2"
			installation.RolloutStatus = core.AppInstallationRolloutStatusPromoted
			now := time.Now().UTC().Truncate(time.Millisecond)
			installation.ActiveSince = &now
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateInstallation: %v", err)
		}
		if updated.ResolvedVersion != "v2" {
			t.Fatalf("ResolvedVersion = %q, want v2", updated.ResolvedVersion)
		}
		if updated.PreviousResolvedVersion != "v1" {
			t.Fatalf("PreviousResolvedVersion = %q, want v1", updated.PreviousResolvedVersion)
		}
		if updated.RolloutStatus != core.AppInstallationRolloutStatusPromoted {
			t.Fatalf("RolloutStatus = %q, want promoted", updated.RolloutStatus)
		}
	})

	t.Run("PutInstallation_preserves_installed_at_on_reupsert", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		installedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
		if _, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:       "g-issues",
			RolloutStatus: core.AppInstallationRolloutStatusPending,
			InstalledAt:   installedAt,
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		got, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:         "g-issues",
			RolloutStatus:   core.AppInstallationRolloutStatusPromoted,
			ResolvedVersion: "v2",
		})
		if err != nil {
			t.Fatalf("PutInstallation reupsert: %v", err)
		}
		if !got.InstalledAt.Equal(installedAt) {
			t.Fatalf("InstalledAt = %v, want %v", got.InstalledAt, installedAt)
		}
	})

	t.Run("PutInstallation_preserves_fields_on_partial_reupsert", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		if _, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:           "g-issues",
			RolloutStatus:     core.AppInstallationRolloutStatusPending,
			Registry:          "toolshed",
			ResolvedVersion:   "v1",
			ArtifactChecksums: map[string]string{"linux/amd64": "sha256:deadbeef"},
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		got, err := svc.AppInstallations.PutInstallation(ctx, &core.AppInstallation{
			AppName:         "g-issues",
			RolloutStatus:   core.AppInstallationRolloutStatusPromoted,
			ResolvedVersion: "v2",
		})
		if err != nil {
			t.Fatalf("PutInstallation reupsert: %v", err)
		}
		if got.Registry != "toolshed" {
			t.Fatalf("Registry = %q, want toolshed", got.Registry)
		}
		if got.ArtifactChecksums["linux/amd64"] != "sha256:deadbeef" {
			t.Fatalf("ArtifactChecksums = %#v", got.ArtifactChecksums)
		}
	})

	t.Run("ListInstallations_rejects_unknown_rollout_status", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		_, err = svc.AppInstallations.ListInstallations(context.Background(), "installing")
		if err == nil {
			t.Fatal("expected unsupported rollout_status error")
		}
	})
}

func TestAppInstallationEventService(t *testing.T) {
	t.Parallel()

	t.Run("AppendListEventsByApp", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()

		first, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			FromVersion:    "",
			ToVersion:      "v1",
			Type:           core.AppInstallationEventTypeInstallRequested,
			Actor:          "user:alice",
			Metadata:       map[string]any{"registry": "toolshed"},
		})
		if err != nil {
			t.Fatalf("AppendEvent(first): %v", err)
		}
		if first.ID == "" {
			t.Fatal("ID should be generated")
		}

		second, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			FromVersion:    "v1",
			ToVersion:      "v2",
			Type:           core.AppInstallationEventTypePromoted,
			Actor:          "user:alice",
		})
		if err != nil {
			t.Fatalf("AppendEvent(second): %v", err)
		}
		if second.ID == first.ID {
			t.Fatal("expected distinct event IDs")
		}

		events, err := svc.AppInstallationEvents.ListEventsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListEventsByApp: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("events = %d, want 2", len(events))
		}
		var foundMetadata bool
		for _, event := range events {
			if event.Metadata != nil && event.Metadata["registry"] == "toolshed" {
				foundMetadata = true
				break
			}
		}
		if !foundMetadata {
			t.Fatalf("expected install_requested metadata on one event, got %#v", events)
		}
	})

	t.Run("ListEventsByApp_returns_timestamp_order", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		firstAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		secondAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			Type:           core.AppInstallationEventTypeInstallRequested,
			Timestamp:      secondAt,
		}); err != nil {
			t.Fatalf("AppendEvent(first): %v", err)
		}
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			Type:           core.AppInstallationEventTypePromoted,
			Timestamp:      firstAt,
		}); err != nil {
			t.Fatalf("AppendEvent(second): %v", err)
		}

		events, err := svc.AppInstallationEvents.ListEventsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListEventsByApp: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("events = %d, want 2", len(events))
		}
		if !events[0].Timestamp.Equal(firstAt) || events[1].Timestamp.Equal(secondAt) == false {
			t.Fatalf("event order = [%v, %v], want [%v, %v]", events[0].Timestamp, events[1].Timestamp, firstAt, secondAt)
		}
		if events[0].Type != core.AppInstallationEventTypePromoted {
			t.Fatalf("first event type = %q, want promoted", events[0].Type)
		}
	})

	t.Run("AppendEvent_rejects_unknown_type", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		_, err = svc.AppInstallationEvents.AppendEvent(context.Background(), &core.AppInstallationEvent{
			InstallationID: "g-issues",
			Type:           "activated",
		})
		if err == nil {
			t.Fatal("expected unsupported type error")
		}
	})
}

func TestAppInstallationEventProjection(t *testing.T) {
	t.Parallel()

	t.Run("HeadInstallation_from_promoted_events", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		firstAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		secondAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)

		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			FromVersion:    "",
			ToVersion:      "v1",
			Type:           core.AppInstallationEventTypePromoted,
			Actor:          "user:alice",
			Timestamp:      firstAt,
			Metadata: coredata.PromotedInstallationMetadata(&core.AppInstallation{
				AppName:           "g-issues",
				VersionConstraint: "v1",
				ResolvedVersion:   "v1",
				Registry:          "toolshed",
				SourceRef:         "abc123",
				InstalledAt:       firstAt,
			}, "/tmp/g-issues/v1"),
		}); err != nil {
			t.Fatalf("AppendEvent(v1): %v", err)
		}
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			FromVersion:    "v1",
			ToVersion:      "v2",
			Type:           core.AppInstallationEventTypePromoted,
			Actor:          "user:bob",
			Timestamp:      secondAt,
			Metadata: coredata.PromotedInstallationMetadata(&core.AppInstallation{
				AppName:           "g-issues",
				VersionConstraint: "v2",
				ResolvedVersion:   "v2",
				Registry:          "toolshed",
				SourceRef:         "def456",
				InstalledAt:       firstAt,
			}, "/tmp/g-issues/v2"),
		}); err != nil {
			t.Fatalf("AppendEvent(v2): %v", err)
		}

		head, err := svc.AppInstallationEvents.HeadInstallation(ctx, "g-issues")
		if err != nil {
			t.Fatalf("HeadInstallation: %v", err)
		}
		if head.ResolvedVersion != "v2" {
			t.Fatalf("ResolvedVersion = %q, want v2", head.ResolvedVersion)
		}
		if head.PreviousResolvedVersion != "v1" {
			t.Fatalf("PreviousResolvedVersion = %q, want v1", head.PreviousResolvedVersion)
		}
		if head.SourceRef != "def456" {
			t.Fatalf("SourceRef = %q", head.SourceRef)
		}
		if !head.InstalledAt.Equal(firstAt) {
			t.Fatalf("InstalledAt = %v, want %v", head.InstalledAt, firstAt)
		}
	})

	t.Run("ListPromotionHistory_returns_promoted_records", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		firstAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		secondAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			ToVersion:      "v1",
			Type:           core.AppInstallationEventTypePromoted,
			Timestamp:      firstAt,
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			ToVersion:      "v2",
			Type:           core.AppInstallationEventTypeFailed,
			Timestamp:      time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("AppendEvent failed: %v", err)
		}
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			ToVersion:      "v2",
			Type:           core.AppInstallationEventTypePromoted,
			Timestamp:      secondAt,
		}); err != nil {
			t.Fatalf("AppendEvent v2: %v", err)
		}

		history, err := svc.AppInstallationEvents.ListPromotionHistory(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListPromotionHistory: %v", err)
		}
		if len(history) != 2 || history[0].ResolvedVersion != "v1" || history[1].ResolvedVersion != "v2" {
			t.Fatalf("history = %#v", history)
		}
	})

	t.Run("ListHeadInstallations_returns_latest_per_app", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		for _, spec := range []struct {
			app, version string
			at           time.Time
		}{
			{"g-issues", "v1", time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)},
			{"dealHub", "v3", time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC)},
			{"g-issues", "v2", time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)},
		} {
			if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
				InstallationID: spec.app,
				ToVersion:      spec.version,
				Type:           core.AppInstallationEventTypePromoted,
				Timestamp:      spec.at,
			}); err != nil {
				t.Fatalf("AppendEvent(%s): %v", spec.app, err)
			}
		}

		heads, err := svc.AppInstallationEvents.ListHeadInstallations(ctx)
		if err != nil {
			t.Fatalf("ListHeadInstallations: %v", err)
		}
		if len(heads) != 2 {
			t.Fatalf("heads = %d, want 2", len(heads))
		}
		byApp := make(map[string]string, len(heads))
		for _, head := range heads {
			byApp[head.AppName] = head.ResolvedVersion
		}
		if byApp["g-issues"] != "v2" || byApp["dealHub"] != "v3" {
			t.Fatalf("heads = %#v", byApp)
		}
	})

	t.Run("HeadInstallation_promote_links_supersedes_event_id", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		firstAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		secondAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)

		v1, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			ToVersion:      "v1",
			Type:           core.AppInstallationEventTypePromoted,
			Timestamp:      firstAt,
		})
		if err != nil {
			t.Fatalf("AppendEvent(v1): %v", err)
		}
		v2, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID:    "g-issues",
			FromVersion:       "v1",
			ToVersion:         "v2",
			Type:              core.AppInstallationEventTypePromoted,
			Timestamp:         secondAt,
			SupersedesEventID: v1.ID,
		})
		if err != nil {
			t.Fatalf("AppendEvent(v2): %v", err)
		}
		if v2.SupersedesEventID != v1.ID {
			t.Fatalf("SupersedesEventID = %q, want %q", v2.SupersedesEventID, v1.ID)
		}

		headEvent, err := svc.AppInstallationEvents.HeadEvent(ctx, "g-issues")
		if err != nil {
			t.Fatalf("HeadEvent: %v", err)
		}
		if headEvent.ID != v2.ID {
			t.Fatalf("HeadEvent ID = %q, want %q", headEvent.ID, v2.ID)
		}
	})

	t.Run("HeadInstallation_rollback_restores_linked_promoted_metadata", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		firstAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
		secondAt := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
		rollbackAt := time.Date(2026, 7, 10, 14, 0, 0, 0, time.UTC)

		v1, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID: "g-issues",
			ToVersion:      "v1",
			Type:           core.AppInstallationEventTypePromoted,
			Timestamp:      firstAt,
			Metadata: coredata.PromotedInstallationMetadata(&core.AppInstallation{
				AppName:           "g-issues",
				VersionConstraint: "v1",
				ResolvedVersion:   "v1",
				Registry:          "toolshed",
				SourceRef:         "abc123",
				InstalledAt:       firstAt,
			}, "/tmp/g-issues/v1"),
		})
		if err != nil {
			t.Fatalf("AppendEvent(v1): %v", err)
		}
		v2, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID:    "g-issues",
			FromVersion:       "v1",
			ToVersion:         "v2",
			Type:              core.AppInstallationEventTypePromoted,
			Timestamp:         secondAt,
			SupersedesEventID: v1.ID,
			Metadata: coredata.PromotedInstallationMetadata(&core.AppInstallation{
				AppName:           "g-issues",
				VersionConstraint: "v2",
				ResolvedVersion:   "v2",
				Registry:          "toolshed",
				SourceRef:         "def456",
				InstalledAt:       firstAt,
			}, "/tmp/g-issues/v2"),
		})
		if err != nil {
			t.Fatalf("AppendEvent(v2): %v", err)
		}
		if _, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			InstallationID:    "g-issues",
			FromVersion:       "v1",
			ToVersion:         "v2",
			Type:              core.AppInstallationEventTypeRollback,
			Timestamp:         rollbackAt,
			SupersedesEventID: v2.ID,
		}); err != nil {
			t.Fatalf("AppendEvent(rollback): %v", err)
		}

		head, err := svc.AppInstallationEvents.HeadInstallation(ctx, "g-issues")
		if err != nil {
			t.Fatalf("HeadInstallation: %v", err)
		}
		if head.ResolvedVersion != "v1" {
			t.Fatalf("ResolvedVersion = %q, want v1", head.ResolvedVersion)
		}
		if head.SourceRef != "abc123" {
			t.Fatalf("SourceRef = %q, want abc123", head.SourceRef)
		}
		if head.PreviousResolvedVersion != "v2" {
			t.Fatalf("PreviousResolvedVersion = %q, want v2", head.PreviousResolvedVersion)
		}
		if head.ActiveSince == nil || !head.ActiveSince.Equal(rollbackAt) {
			t.Fatalf("ActiveSince = %v, want %v", head.ActiveSince, rollbackAt)
		}
	})
}
