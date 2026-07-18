package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

type countingRegistryResolver struct {
	calls int
}

func (r *countingRegistryResolver) ResolveInstalledApp(_ string, entry *config.ProviderEntry, _ string) (*config.ProviderEntry, error) {
	r.calls++
	return entry, nil
}

func TestAppProviderRestarterStartAppDoesNotResolveWhenProviderRunning(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Apps: map[string]*config.ProviderEntry{"g-issues": {}}}
	reg := registry.New()
	if err := reg.Providers.Register("g-issues", &coretesting.StubIntegration{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	resolver := &countingRegistryResolver{}
	restarter := bootstrap.NewAppProviderRestarter(bootstrap.AppProviderRestarterConfig{
		Config:           cfg,
		Providers:        &reg.Providers,
		RegistryResolver: resolver,
	})

	if err := restarter.StartApp(context.Background(), "g-issues", "1.0.0"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
}

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
		RegistryResolver: &appregistry.MountService{
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
	if entry.Command != oldCommand {
		t.Fatalf("deploy-time Command = %q, want unchanged %q", entry.Command, oldCommand)
	}
	if _, err := os.Stat(wantCommand); err != nil {
		t.Fatalf("stat mounted executable: %v", err)
	}
}
