package coredata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

func TestAppAllowedOperationsService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppAllowedOperations
	const app = "hello-world"

	if _, err := svc.GetOverlay(ctx, app); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing overlay error = %v, want %v", err, core.ErrNotFound)
	}

	if err := svc.SetOverlay(ctx, &coredata.AppAllowedOperationsOverlay{
		App: app,
		Operations: map[string]*operationexposure.OperationOverride{
			"get_item": {AllowedRoles: []string{"viewer"}},
		},
	}); err != nil {
		t.Fatalf("SetOverlay: %v", err)
	}

	overlay, err := svc.GetOverlay(ctx, app)
	if err != nil {
		t.Fatalf("GetOverlay: %v", err)
	}
	if overlay.Operations["get_item"].AllowedRoles[0] != "viewer" {
		t.Fatalf("overlay = %#v", overlay)
	}

	patched := coredata.MergeOverlayPatch(overlay, app, map[string]*operationexposure.OperationOverride{
		"create": {AllowedRoles: []string{"admin"}},
	}, []string{"get_item"})
	if err := svc.SetOverlay(ctx, patched); err != nil {
		t.Fatalf("SetOverlay patch: %v", err)
	}
	overlay, err = svc.GetOverlay(ctx, app)
	if err != nil {
		t.Fatalf("GetOverlay after patch: %v", err)
	}
	if overlay.Operations["create"] == nil || overlay.Operations["get_item"] != nil {
		t.Fatalf("patched overlay = %#v", overlay)
	}
	if len(overlay.Removed) != 1 || overlay.Removed[0] != "get_item" {
		t.Fatalf("patched removed = %#v", overlay.Removed)
	}

	if err := svc.DeleteOverlay(ctx, app); err != nil {
		t.Fatalf("DeleteOverlay: %v", err)
	}
	if _, err := svc.GetOverlay(ctx, app); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleted overlay error = %v, want %v", err, core.ErrNotFound)
	}
}
