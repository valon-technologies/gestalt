package coredata_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAppAccessProfileService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).AppAccessProfiles
	const subject = "user:person@example.com"
	const app = "slack"

	if _, err := svc.GetAppAccessProfile(ctx, subject, app); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing profile error = %v, want %v", err, core.ErrNotFound)
	}

	created, err := svc.EnsureAppAccessDefaults(ctx, subject, app, []string{"users.list", "conversations.list", "users.list"})
	if err != nil {
		t.Fatalf("EnsureAppAccessDefaults: %v", err)
	}
	if !created.DefaultsInitialized || !slices.Equal(created.EnabledOperations, []string{"conversations.list", "users.list"}) {
		t.Fatalf("created profile = %#v", created)
	}

	unchanged, err := svc.EnsureAppAccessDefaults(ctx, subject, app, []string{"chat.postMessage"})
	if err != nil {
		t.Fatalf("EnsureAppAccessDefaults existing: %v", err)
	}
	if !slices.Equal(unchanged.EnabledOperations, created.EnabledOperations) {
		t.Fatalf("reconnect changed profile from %#v to %#v", created.EnabledOperations, unchanged.EnabledOperations)
	}

	updated, err := svc.SetAppAccessOperations(ctx, subject, app, []string{"chat.postMessage"})
	if err != nil {
		t.Fatalf("SetAppAccessOperations: %v", err)
	}
	if !slices.Equal(updated.EnabledOperations, []string{"chat.postMessage"}) {
		t.Fatalf("updated profile = %#v", updated)
	}

	cleared, err := svc.SetAppAccessOperations(ctx, subject, app, nil)
	if err != nil {
		t.Fatalf("SetAppAccessOperations empty: %v", err)
	}
	if len(cleared.EnabledOperations) != 0 {
		t.Fatalf("cleared profile = %#v", cleared)
	}
}
