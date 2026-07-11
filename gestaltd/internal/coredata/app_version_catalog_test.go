package coredata_test

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppVersionCatalogService(t *testing.T) {
	t.Parallel()

	t.Run("append_and_list_records_by_app", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		first, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:       "g-issues",
			Version:   "0.0.1",
			Type:      core.AppVersionCatalogRecordTypeVersionAdded,
			Actor:     "user:alice",
			Timestamp: base,
			Metadata: map[string]any{
				"registry": "toolshed",
			},
		})
		if err != nil {
			t.Fatalf("AppendRecord first: %v", err)
		}
		if first.ID == "" || first.Timestamp.IsZero() {
			t.Fatalf("first = %#v", first)
		}

		second, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:       "g-issues",
			Version:   "0.0.2",
			Type:      core.AppVersionCatalogRecordTypeVersionAdded,
			Actor:     "user:bob",
			Timestamp: base.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("AppendRecord second: %v", err)
		}
		if second.Timestamp.Before(first.Timestamp) {
			t.Fatalf("timestamps out of order: first=%v second=%v", first.Timestamp, second.Timestamp)
		}

		records, err := svc.AppVersionCatalog.ListRecordsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListRecordsByApp: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("records = %#v", records)
		}
		if records[0].Version != "0.0.1" || records[1].Version != "0.0.2" {
			t.Fatalf("records order = %#v", records)
		}
	})

	t.Run("rejects_unsupported_type", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		svc := testutil.NewStubServices(t)

		_, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:     "g-issues",
			Version: "0.0.1",
			Type:    "promoted",
		})
		if err == nil {
			t.Fatal("expected error for unsupported type")
		}
	})

	t.Run("has_known_version", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		svc := testutil.NewStubServices(t)

		known, err := svc.AppVersionCatalog.HasKnownVersion(ctx, "g-issues", "0.0.1")
		if err != nil {
			t.Fatalf("HasKnownVersion before append: %v", err)
		}
		if known {
			t.Fatal("expected version to be unknown")
		}

		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:     "g-issues",
			Version: "0.0.1",
			Type:    core.AppVersionCatalogRecordTypeVersionAdded,
		}); err != nil {
			t.Fatalf("AppendRecord: %v", err)
		}

		known, err = svc.AppVersionCatalog.HasKnownVersion(ctx, "g-issues", "0.0.1")
		if err != nil {
			t.Fatalf("HasKnownVersion after append: %v", err)
		}
		if !known {
			t.Fatal("expected version to be known")
		}
	})
}

func TestAppVersionCatalogProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("ListKnownVersionsByApp_dedupes_version_added", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:       "g-issues",
			Version:   "0.0.1",
			Type:      core.AppVersionCatalogRecordTypeVersionAdded,
			Timestamp: base,
			Metadata: coredata.VersionAddedMetadata(&core.AppInstallation{
				AppName:   "g-issues",
				Version:   "0.0.1",
				SourceRef: "abc",
				Registry:  "toolshed",
			}, "/tmp/v1"),
		}); err != nil {
			t.Fatalf("AppendRecord v1: %v", err)
		}
		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:       "g-issues",
			Version:   "0.0.2",
			Type:      core.AppVersionCatalogRecordTypeVersionAdded,
			Timestamp: base.Add(time.Minute),
			Metadata: coredata.VersionAddedMetadata(&core.AppInstallation{
				AppName:   "g-issues",
				Version:   "0.0.2",
				SourceRef: "def",
				Registry:  "toolshed",
			}, "/tmp/v2"),
		}); err != nil {
			t.Fatalf("AppendRecord v2: %v", err)
		}

		versions, err := svc.AppVersionCatalog.ListKnownVersionsByApp(ctx, "g-issues")
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

	t.Run("ListKnownVersionsByApp_skips_install_failed", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)

		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:     "g-issues",
			Version: "0.0.1",
			Type:    core.AppVersionCatalogRecordTypeInstallFailed,
		}); err != nil {
			t.Fatalf("AppendRecord failed: %v", err)
		}
		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:     "g-issues",
			Version: "0.0.2",
			Type:    core.AppVersionCatalogRecordTypeVersionAdded,
		}); err != nil {
			t.Fatalf("AppendRecord added: %v", err)
		}

		versions, err := svc.AppVersionCatalog.ListKnownVersionsByApp(ctx, "g-issues")
		if err != nil {
			t.Fatalf("ListKnownVersionsByApp: %v", err)
		}
		if len(versions) != 1 || versions[0].Version != "0.0.2" {
			t.Fatalf("versions = %#v", versions)
		}
	})

	t.Run("ListAllKnownVersions_returns_latest_per_app_version", func(t *testing.T) {
		t.Parallel()

		svc := testutil.NewStubServices(t)
		base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

		for _, app := range []string{"g-issues", "g-docs"} {
			if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
				App:       app,
				Version:   "0.0.1",
				Type:      core.AppVersionCatalogRecordTypeVersionAdded,
				Timestamp: base,
			}); err != nil {
				t.Fatalf("AppendRecord %s: %v", app, err)
			}
		}
		if _, err := svc.AppVersionCatalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
			App:       "g-issues",
			Version:   "0.0.2",
			Type:      core.AppVersionCatalogRecordTypeVersionAdded,
			Timestamp: base.Add(time.Minute),
		}); err != nil {
			t.Fatalf("AppendRecord g-issues v2: %v", err)
		}

		versions, err := svc.AppVersionCatalog.ListAllKnownVersions(ctx)
		if err != nil {
			t.Fatalf("ListAllKnownVersions: %v", err)
		}
		if len(versions) != 3 {
			t.Fatalf("versions = %#v", versions)
		}
	})
}
