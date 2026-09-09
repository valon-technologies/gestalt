package coredata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestGroupServiceDisplayNameMapping(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).Groups
	ctx := context.Background()
	groupID := "servicemacusa-employees"

	name, err := svc.DisplayName(ctx, groupID)
	if err != nil {
		t.Fatalf("DisplayName missing: %v", err)
	}
	if name != "" {
		t.Fatalf("DisplayName missing = %q", name)
	}
	if _, err := svc.Get(ctx, groupID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Get missing = %v", err)
	}

	created, err := svc.UpsertDisplayName(ctx, groupID, "ServiceMac employees")
	if err != nil {
		t.Fatalf("UpsertDisplayName: %v", err)
	}
	if created.ID != groupID || created.DisplayName != "ServiceMac employees" {
		t.Fatalf("created = %#v", created)
	}

	name, err = svc.DisplayName(ctx, groupID)
	if err != nil {
		t.Fatalf("DisplayName: %v", err)
	}
	if name != "ServiceMac employees" {
		t.Fatalf("DisplayName = %q", name)
	}

	updated, err := svc.UpsertDisplayName(ctx, groupID, "ServiceMac USA employees")
	if err != nil {
		t.Fatalf("UpsertDisplayName update: %v", err)
	}
	if updated.DisplayName != "ServiceMac USA employees" {
		t.Fatalf("updated = %#v", updated)
	}
	if updated.CreatedAt.IsZero() || updated.UpdatedAt.Before(created.CreatedAt) {
		t.Fatalf("timestamps created=%v updated=%v", updated.CreatedAt, updated.UpdatedAt)
	}
}

func TestGroupServiceRejectsEmptyDisplayName(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t).Groups
	if _, err := svc.UpsertDisplayName(context.Background(), "carrington", "  "); err == nil {
		t.Fatal("expected empty display name to fail")
	}
}
