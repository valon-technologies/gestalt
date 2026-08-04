package coredata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestConnectionInstancePreferenceService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t).ConnectionInstancePreferences

	t.Run("missing preference", func(t *testing.T) {
		t.Parallel()
		if _, err := svc.Get(ctx, "user:alice", "manual-multi:default"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get missing error = %v, want %v", err, core.ErrNotFound)
		}
	})

	t.Run("set get delete round trip", func(t *testing.T) {
		t.Parallel()
		got, err := svc.Set(ctx, "user:alice", "manual-multi:default", "team-a")
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
		if got.SubjectID != "user:alice" || got.ConnectionID != "manual-multi:default" || got.Instance != "team-a" || got.UpdatedAt.IsZero() {
			t.Fatalf("Set returned %#v", got)
		}
		stored, err := svc.Get(ctx, "user:alice", "manual-multi:default")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if stored.Instance != "team-a" {
			t.Fatalf("stored instance = %q, want team-a", stored.Instance)
		}
		instance, err := svc.PreferredInstance(ctx, "user:alice", "manual-multi:default")
		if err != nil || instance != "team-a" {
			t.Fatalf("PreferredInstance = (%q, %v), want (team-a, nil)", instance, err)
		}
		if err := svc.Delete(ctx, "user:alice", "manual-multi:default"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := svc.Get(ctx, "user:alice", "manual-multi:default"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("set replaces existing", func(t *testing.T) {
		t.Parallel()
		if _, err := svc.Set(ctx, "user:bob", "svc:default", "one"); err != nil {
			t.Fatalf("first Set: %v", err)
		}
		updated, err := svc.Set(ctx, "user:bob", "svc:default", "two")
		if err != nil {
			t.Fatalf("second Set: %v", err)
		}
		if !updated.UpdatedAt.After(time.Time{}) {
			t.Fatalf("updated timestamp missing: %#v", updated)
		}
		got, err := svc.Get(ctx, "user:bob", "svc:default")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Instance != "two" {
			t.Fatalf("instance = %q, want two", got.Instance)
		}
	})
}
