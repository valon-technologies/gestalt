package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

func TestAppProviderRestarterStartAppMountsRegistryInstalledVersion(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()
	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}
	if _, err := materializer.Ensure(context.Background(), &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	oldCommand := filepath.Join(t.TempDir(), "old-binary")
	entry := &config.ProviderEntry{
		Command: oldCommand,
	}
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": entry,
		},
	}
	reg := registry.New()
	restarter := bootstrap.NewAppProviderRestarter(bootstrap.AppProviderRestarterConfig{
		Config:    cfg,
		Providers: &reg.Providers,
		RegistryMounter: &appregistry.MountService{
			ArtifactsDir: artifactsDir,
		},
	})

	if err := restarter.StartApp(context.Background(), "g-issues", fixture.Version); err == nil {
		t.Fatal("StartApp: expected build failure after registry mount")
	}

	wantCommand := filepath.Join(
		appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version),
		filepath.FromSlash(packageio.InstalledExecutablePath("g-issues", runtime.GOOS)),
	)
	if entry.Command != wantCommand {
		t.Fatalf("Command = %q, want %q", entry.Command, wantCommand)
	}
	if _, err := os.Stat(entry.Command); err != nil {
		t.Fatalf("stat mounted executable: %v", err)
	}
}
