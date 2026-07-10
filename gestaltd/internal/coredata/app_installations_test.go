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
			AppName:                  "g-issues",
			DesiredVersionConstraint: "0.0.0-snapshot.gabc123",
			ResolvedVersion:          "0.0.0-snapshot.gabc123",
			SourceRef:                "abc123def456abc123def456abc123def456abcd",
			Registry:                 "toolshed",
			ProviderReleaseURL:         "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry/apps/g-issues/versions/0.0.0-snapshot.gabc123.json",
			ArtifactChecksums: map[string]string{
				"linux/amd64": "sha256:deadbeef",
			},
			DesiredState:            core.AppInstallationDesiredStateActive,
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
		if stored.DesiredState != core.AppInstallationDesiredStateActive {
			t.Fatalf("DesiredState = %q, want active", stored.DesiredState)
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

	t.Run("PutInstallation_rejects_unknown_desired_state", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		_, err = svc.AppInstallations.PutInstallation(context.Background(), &core.AppInstallation{
			AppName:      "g-issues",
			DesiredState: "installing",
		})
		if err == nil {
			t.Fatal("expected unsupported desired_state error")
		}
	})

	t.Run("ListInstallations_filters_by_desired_state", func(t *testing.T) {
		t.Parallel()
		svc, err := coredata.New(&coretesting.StubIndexedDB{})
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}
		ctx := context.Background()
		for _, installation := range []*core.AppInstallation{
			{AppName: "g-issues", DesiredState: core.AppInstallationDesiredStateActive, ResolvedVersion: "v1"},
			{AppName: "dealHub", DesiredState: core.AppInstallationDesiredStatePending, ResolvedVersion: "v2"},
		} {
			if _, err := svc.AppInstallations.PutInstallation(ctx, installation); err != nil {
				t.Fatalf("PutInstallation(%s): %v", installation.AppName, err)
			}
		}

		active, err := svc.AppInstallations.ListInstallations(ctx, core.AppInstallationDesiredStateActive)
		if err != nil {
			t.Fatalf("ListInstallations(active): %v", err)
		}
		if len(active) != 1 || active[0].AppName != "g-issues" {
			t.Fatalf("active installations = %#v, want g-issues only", active)
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
			DesiredState:    core.AppInstallationDesiredStatePending,
			ResolvedVersion: "v1",
		}); err != nil {
			t.Fatalf("PutInstallation: %v", err)
		}

		updated, err := svc.AppInstallations.UpdateInstallation(ctx, "g-issues", func(installation *core.AppInstallation) error {
			installation.PreviousResolvedVersion = installation.ResolvedVersion
			installation.ResolvedVersion = "v2"
			installation.DesiredState = core.AppInstallationDesiredStateActive
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
		if updated.DesiredState != core.AppInstallationDesiredStateActive {
			t.Fatalf("DesiredState = %q, want active", updated.DesiredState)
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
			AppName:     "g-issues",
			FromVersion: "",
			ToVersion:   "v1",
			EventType:   "install_requested",
			Actor:       "user:alice",
			Metadata:    map[string]any{"registry": "toolshed"},
		})
		if err != nil {
			t.Fatalf("AppendEvent(first): %v", err)
		}
		if first.EventID == "" {
			t.Fatal("EventID should be generated")
		}

		second, err := svc.AppInstallationEvents.AppendEvent(ctx, &core.AppInstallationEvent{
			AppName:     "g-issues",
			FromVersion: "v1",
			ToVersion:   "v2",
			EventType:   "activated",
			Actor:       "user:alice",
		})
		if err != nil {
			t.Fatalf("AppendEvent(second): %v", err)
		}
		if second.EventID == first.EventID {
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
}
