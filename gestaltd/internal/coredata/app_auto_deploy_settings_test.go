package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAutoDeploySettingsService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("missing settings", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Get(ctx, "g-issues"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get missing error = %v, want %v", err, core.ErrNotFound)
		}
	})

	t.Run("update initializes and round trips", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		got, err := svc.Update(ctx, " g-issues ", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			settings.PendingVersion = " v2 "
			settings.LastSeenVersion = " v2 "
			settings.LastError = " validation failed "
			return nil
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.App != "g-issues" || !got.Enabled || got.PendingVersion != "v2" ||
			got.LastSeenVersion != "v2" || got.LastError != "validation failed" {
			t.Fatalf("updated settings = %#v", got)
		}
		stored, err := svc.Get(ctx, "g-issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if *stored != *got {
			t.Fatalf("stored settings = %#v, want %#v", stored, got)
		}
	})

	t.Run("update clears fields and preserves app", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			settings.PendingVersion = "v2"
			settings.LastError = "failed"
			return nil
		}); err != nil {
			t.Fatalf("seed settings: %v", err)
		}
		got, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.App = "different-app"
			settings.Enabled = false
			settings.PendingVersion = ""
			settings.LastError = ""
			return nil
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.App != "g-issues" || got.Enabled || got.PendingVersion != "" || got.LastError != "" {
			t.Fatalf("updated settings = %#v", got)
		}
	})

	t.Run("list enabled", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		for app, enabled := range map[string]bool{
			"g-issues": true,
			"g-slack":  false,
			"g-tasks":  true,
		} {
			if _, err := svc.Update(ctx, app, func(settings *core.AppAutoDeploySettings) error {
				settings.Enabled = enabled
				return nil
			}); err != nil {
				t.Fatalf("Update %s: %v", app, err)
			}
		}
		got, err := svc.ListEnabled(ctx)
		if err != nil {
			t.Fatalf("ListEnabled: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("enabled settings = %#v, want two", got)
		}
		for _, settings := range got {
			if !settings.Enabled || settings.App == "g-slack" {
				t.Fatalf("enabled settings includes %#v", settings)
			}
		}
	})

	t.Run("validation and failed update", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t).AutoDeploySettings
		if _, err := svc.Get(ctx, " "); err == nil {
			t.Fatal("Get with empty app succeeded")
		}
		if _, err := svc.Update(ctx, "", func(*core.AppAutoDeploySettings) error { return nil }); err == nil {
			t.Fatal("Update with empty app succeeded")
		}
		if _, err := svc.Update(ctx, "g-issues", nil); err == nil {
			t.Fatal("Update with nil function succeeded")
		}
		wantErr := errors.New("stop")
		if _, err := svc.Update(ctx, "g-issues", func(settings *core.AppAutoDeploySettings) error {
			settings.Enabled = true
			return fmt.Errorf("wrapped: %w", wantErr)
		}); !errors.Is(err, wantErr) {
			t.Fatalf("failed update error = %v, want %v", err, wantErr)
		}
		if _, err := svc.Get(ctx, "g-issues"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("Get after failed update error = %v, want %v", err, core.ErrNotFound)
		}
	})
}
