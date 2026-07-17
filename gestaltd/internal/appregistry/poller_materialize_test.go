package appregistry_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type recordingAppRestarter struct {
	stopCalls  []string
	startCalls []string
}

func (r *recordingAppRestarter) Restartable(app string) (bool, error) {
	return true, nil
}

func (r *recordingAppRestarter) StopApp(_ context.Context, app string) error {
	r.stopCalls = append(r.stopCalls, app)
	return nil
}

func (r *recordingAppRestarter) StartApp(_ context.Context, app string) error {
	r.startCalls = append(r.startCalls, app)
	return nil
}

func (r *recordingAppRestarter) AbortRestarts() {}

func TestCatalogPollerMaterializesBeforeStop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	restartReady := make(chan struct{})

	if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   fixture.Version,
		Timestamp:   now,
		Metadata: map[string]any{
			"registry": "toolshed",
		},
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{}
	poller := appregistry.NewCatalogPoller(appregistry.CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppMaterializer: &appregistry.Materializer{
			Registries: map[string]config.AppRegistryConfig{
				"toolshed": fixture.Registry,
			},
			Reader:       fixture.Reader,
			ArtifactsDir: artifactsDir,
		},
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		RestartReady:        restartReady,
		Now:                 func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce before providers ready: %v", err)
	}
	if got := len(restarter.stopCalls); got != 0 {
		t.Fatalf("stopCalls before providers ready = %d, want 0", got)
	}

	materialization, err := services.AppInstanceMaterializations.Get(ctx, "replica-a", "g-issues", fixture.Version)
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.MaterializedAt != now {
		t.Fatalf("MaterializedAt = %v, want %v", materialization.MaterializedAt, now)
	}
	wantPath := appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)
	if materialization.MaterializedPath != wantPath {
		t.Fatalf("MaterializedPath = %q, want %q", materialization.MaterializedPath, wantPath)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "manifest.yaml")); err != nil {
		t.Fatalf("stat materialized manifest: %v", err)
	}

	close(restartReady)
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce after providers ready: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 || restarter.stopCalls[0] != "g-issues" {
		t.Fatalf("stopCalls after providers ready = %#v, want [g-issues]", restarter.stopCalls)
	}
}
