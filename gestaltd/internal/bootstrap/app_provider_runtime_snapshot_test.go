package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

func TestAppProviderRestarterSnapshotRegistryAppsRequiresRuntimeAgreement(t *testing.T) {
	t.Parallel()
	artifactsDir := t.TempDir()
	providerRegistry := registry.New()
	if err := providerRegistry.Providers.Register("running", &coretesting.StubIntegration{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := providerRegistry.Providers.Register("mismatch", &coretesting.StubIntegration{}); err != nil {
		t.Fatalf("Register mismatch: %v", err)
	}
	writeActiveVersionMarker(t, artifactsDir, "running", "v2")
	writeActiveVersionMarker(t, artifactsDir, "mismatch", "v1")
	restarter := NewAppProviderRestarter(AppProviderRestarterConfig{
		Config: &config.Config{Apps: map[string]*config.ProviderEntry{
			"running":  {Source: config.ProviderSource{Registry: "toolshed"}},
			"mismatch": {Source: config.ProviderSource{Registry: "toolshed"}},
			"legacy":   {},
		}},
		Providers:    &providerRegistry.Providers,
		ArtifactsDir: artifactsDir,
	})
	restarter.runningVersions["running"] = "v2"
	restarter.runningVersions["mismatch"] = "v2"

	got := restarter.SnapshotRegistryApps()
	if len(got) != 2 {
		t.Fatalf("snapshot = %#v, want registry apps only", got)
	}
	if got["running"].State != core.GestaltdInstanceAppStateRunning || got["running"].RunningVersion != "v2" {
		t.Fatalf("running = %#v", got["running"])
	}
	if got["mismatch"].State != core.GestaltdInstanceAppStateError {
		t.Fatalf("mismatch = %#v, want error", got["mismatch"])
	}
}

func TestAppProviderRestarterSnapshotSerializesWithLifecycleMutation(t *testing.T) {
	t.Parallel()
	restarter := NewAppProviderRestarter(AppProviderRestarterConfig{
		Config: &config.Config{Apps: map[string]*config.ProviderEntry{
			"app": {Source: config.ProviderSource{Registry: "toolshed"}},
		}},
	})
	restarter.lifecycleMu.Lock()
	done := make(chan struct{})
	go func() {
		_ = restarter.SnapshotRegistryApps()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("snapshot completed while lifecycle write lock was held")
	case <-time.After(20 * time.Millisecond):
	}
	restarter.lifecycleMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("snapshot did not resume after lifecycle write lock release")
	}
}

type snapshotRemoteResolver struct {
	calls atomic.Int32
}

func (r *snapshotRemoteResolver) ResolveProvider(context.Context, string) (core.Provider, error) {
	r.calls.Add(1)
	return &coretesting.StubIntegration{}, nil
}

func TestAppProviderRestarterSnapshotUsesLocalProviderMembershipOnly(t *testing.T) {
	t.Parallel()
	providerRegistry := registry.New()
	resolver := &snapshotRemoteResolver{}
	providerRegistry.Providers.SetRemoteResolver(resolver)
	restarter := NewAppProviderRestarter(AppProviderRestarterConfig{
		Config: &config.Config{Apps: map[string]*config.ProviderEntry{
			"app": {Source: config.ProviderSource{Registry: "toolshed"}},
		}},
		Providers: &providerRegistry.Providers,
	})

	got := restarter.SnapshotRegistryApps()
	if resolver.calls.Load() != 0 {
		t.Fatalf("remote resolver calls = %d, want 0", resolver.calls.Load())
	}
	if got["app"].State != core.GestaltdInstanceAppStateNotRunning {
		t.Fatalf("app = %#v, want local not_running", got["app"])
	}
}

func writeActiveVersionMarker(t *testing.T, artifactsDir, app, version string) {
	t.Helper()
	dir := filepath.Join(artifactsDir, "registry-installed", app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active-version"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
