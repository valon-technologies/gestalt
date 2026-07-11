package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

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
