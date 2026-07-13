package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppVersionChangeRequestService(t *testing.T) {
	t.Parallel()

	t.Run("append_and_list_requests_by_app", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		first, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: "",
			ToVersion:   "0.0.1",
			Actor:       "user:alice",
			Timestamp:   base,
			Metadata: map[string]any{
				"registry": "toolshed",
			},
		})
		if err != nil {
			t.Fatalf("AppendRequest first: %v", err)
		}
		if first.ID == "" || first.Timestamp.IsZero() {
			t.Fatalf("first = %#v", first)
		}

		second, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: "0.0.1",
			ToVersion:   "0.0.2",
			Actor:       "user:bob",
			Timestamp:   base.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("AppendRequest second: %v", err)
		}
		if second.Timestamp.Before(first.Timestamp) {
			t.Fatalf("timestamps out of order: first=%v second=%v", first.Timestamp, second.Timestamp)
		}

		requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListRequestsByApp: %v", err)
		}
		if len(requests) != 2 {
			t.Fatalf("requests = %#v", requests)
		}
		if requests[0].ToVersion != "0.0.1" || requests[1].ToVersion != "0.0.2" {
			t.Fatalf("requests order = %#v", requests)
		}
	})

	t.Run("has_known_version", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		svc := testutil.NewStubServices(t)

		known, err := svc.AppVersionChangeRequests.HasKnownVersion(ctx, "g-issues", "0.0.1")
		if err != nil {
			t.Fatalf("HasKnownVersion before append: %v", err)
		}
		if known {
			t.Fatal("expected version to be unknown")
		}

		if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:       "g-issues",
			ToVersion: "0.0.1",
		}); err != nil {
			t.Fatalf("AppendRequest: %v", err)
		}

		known, err = svc.AppVersionChangeRequests.HasKnownVersion(ctx, "g-issues", "0.0.1")
		if err != nil {
			t.Fatalf("HasKnownVersion after append: %v", err)
		}
		if !known {
			t.Fatal("expected version to be known")
		}
	})
}

func TestAppVersionChangeRequestProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("ListKnownVersionsByApp_dedupes_to_version", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:       "g-issues",
			ToVersion: "0.0.1",
			Timestamp: base,
			Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
				AppName:   "g-issues",
				Version:   "0.0.1",
				SourceRef: "abc",
				Registry:  "toolshed",
			}, "/tmp/v1"),
		}); err != nil {
			t.Fatalf("AppendRequest v1: %v", err)
		}
		if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: "0.0.1",
			ToVersion:   "0.0.2",
			Timestamp:   base.Add(time.Minute),
			Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
				AppName:   "g-issues",
				Version:   "0.0.2",
				SourceRef: "def",
				Registry:  "toolshed",
			}, "/tmp/v2"),
		}); err != nil {
			t.Fatalf("AppendRequest v2: %v", err)
		}

		versions, err := svc.AppVersionChangeRequests.ListKnownVersionsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListKnownVersionsByApp: %v", err)
		}
		if len(versions) != 2 {
			t.Fatalf("versions = %#v", versions)
		}
		if versions[0].Version != "0.0.1" || versions[1].Version != "0.0.2" {
			t.Fatalf("versions order = %#v", versions)
		}
		if versions[0].SourceRef != "abc" || versions[1].SourceRef != "def" {
			t.Fatalf("versions metadata = %#v", versions)
		}
	})

	t.Run("ListAllKnownVersions_returns_latest_per_app_version", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		for _, app := range []string{"g-issues", "g-docs"} {
			if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
				App:       app,
				ToVersion: "0.0.1",
				Timestamp: base,
			}); err != nil {
				t.Fatalf("AppendRequest %s: %v", app, err)
			}
		}
		if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: "0.0.1",
			ToVersion:   "0.0.2",
			Timestamp:   base.Add(time.Minute),
		}); err != nil {
			t.Fatalf("AppendRequest g-issues v2: %v", err)
		}

		versions, err := svc.AppVersionChangeRequests.ListAllKnownVersions(ctx)
		if err != nil {
			t.Fatalf("ListAllKnownVersions: %v", err)
		}
		if len(versions) != 3 {
			t.Fatalf("versions = %#v", versions)
		}
	})
}
