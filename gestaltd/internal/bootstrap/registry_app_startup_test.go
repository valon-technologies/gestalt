package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type startupMaterializerStub struct {
	installations []*core.AppInstallation
}

func (s *startupMaterializerStub) MaterializeApp(_ context.Context, installation *core.AppInstallation) error {
	s.installations = append(s.installations, installation)
	return nil
}

type startupAppStarterStub struct {
	app         string
	version     string
	deactivated string
}

func (s *startupAppStarterStub) StartApp(_ context.Context, app, version string) error {
	s.app = app
	s.version = version
	return nil
}

func (s *startupAppStarterStub) DeactivateApp(_ context.Context, app string) error {
	s.deactivated = app
	return nil
}

func registryOnlyStartupConfig() *config.Config {
	return &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
	}
}

func TestStartRegistryOnlyAppProvidersSkipsEmptyCatalog(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	materializer := &startupMaterializerStub{}
	starter := &startupAppStarterStub{}
	if err := startRegistryOnlyAppProviders(t.Context(), registryOnlyStartupConfig(), services.AppVersionChangeRequests, materializer, starter); err != nil {
		t.Fatalf("startRegistryOnlyAppProviders: %v", err)
	}
	if len(materializer.installations) != 0 || starter.app != "" {
		t.Fatalf("empty catalog materialized %#v and started %q", materializer.installations, starter.app)
	}
	if starter.deactivated != "g-issues" {
		t.Fatalf("deactivated app = %q, want g-issues", starter.deactivated)
	}
}

func TestStartRegistryOnlyAppProvidersMaterializesAndStartsLatestKnownVersion(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	older := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	for _, request := range []*core.AppVersionChangeRequest{
		{
			App:         "g-issues",
			FromVersion: "registry:first-install",
			ToVersion:   "1.0.0",
			Timestamp:   older,
			Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
				AppName: "g-issues", Version: "1.0.0", Registry: "toolshed", UpdatedAt: older,
			}),
		},
		{
			App:         "g-issues",
			FromVersion: "1.0.0",
			ToVersion:   "2.0.0",
			Timestamp:   newer,
			Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
				AppName: "g-issues", Version: "2.0.0", Registry: "toolshed", UpdatedAt: newer,
			}),
		},
	} {
		if _, err := services.AppVersionChangeRequests.AppendRequest(t.Context(), request); err != nil {
			t.Fatalf("AppendRequest: %v", err)
		}
	}

	materializer := &startupMaterializerStub{}
	starter := &startupAppStarterStub{}
	if err := startRegistryOnlyAppProviders(t.Context(), registryOnlyStartupConfig(), services.AppVersionChangeRequests, materializer, starter); err != nil {
		t.Fatalf("startRegistryOnlyAppProviders: %v", err)
	}
	if len(materializer.installations) != 1 || materializer.installations[0].Version != "2.0.0" {
		t.Fatalf("materialized installations = %#v", materializer.installations)
	}
	if starter.app != "g-issues" || starter.version != "2.0.0" {
		t.Fatalf("started %q@%q", starter.app, starter.version)
	}
}

func TestStartRegistryOnlyAppProvidersDeactivatesMismatchedRegistry(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if _, err := services.AppVersionChangeRequests.AppendRequest(t.Context(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "registry:first-install",
		ToVersion:   "1.0.0",
		Timestamp:   now,
		Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
			AppName: "g-issues", Version: "1.0.0", Registry: "old-registry", UpdatedAt: now,
		}),
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	materializer := &startupMaterializerStub{}
	starter := &startupAppStarterStub{}
	err := startRegistryOnlyAppProviders(t.Context(), registryOnlyStartupConfig(), services.AppVersionChangeRequests, materializer, starter)
	if err == nil {
		t.Fatal("startRegistryOnlyAppProviders error = nil, want registry mismatch")
	}
	if starter.deactivated != "g-issues" {
		t.Fatalf("deactivated app = %q, want g-issues", starter.deactivated)
	}
	if len(materializer.installations) != 0 || starter.app != "" {
		t.Fatalf("mismatched catalog materialized %#v and started %q", materializer.installations, starter.app)
	}
}

func TestResultStartAppProvidersClosesRegistryReadinessAfterRegistryStartup(t *testing.T) {
	t.Parallel()

	startupReady := make(chan struct{})
	close(startupReady)
	registryReady := make(chan struct{})
	registryStarted := false
	result := &Result{
		StartupProvidersReady:  startupReady,
		RegistryProvidersReady: registryReady,
		startAppProviders:      func() {},
		startRegistryAppProviders: func(context.Context) error {
			registryStarted = true
			return nil
		},
		registryProvidersReady: registryReady,
	}
	if err := result.StartAppProviders(t.Context()); err != nil {
		t.Fatalf("StartAppProviders: %v", err)
	}
	if !registryStarted {
		t.Fatal("registry providers were not started")
	}
	select {
	case <-result.RegistryProvidersReady:
	default:
		t.Fatal("registry provider readiness remained open")
	}
}

func TestResultStartAppProvidersDoesNotFailCoreBootForRegistryAppFailure(t *testing.T) {
	t.Parallel()

	startupReady := make(chan struct{})
	close(startupReady)
	registryReady := make(chan struct{})
	result := &Result{
		StartupProvidersReady:  startupReady,
		RegistryProvidersReady: registryReady,
		startAppProviders:      func() {},
		startRegistryAppProviders: func(context.Context) error {
			return errors.New("registry app failed")
		},
		registryProvidersReady: registryReady,
	}
	if err := result.StartAppProviders(t.Context()); err != nil {
		t.Fatalf("StartAppProviders returned registry app failure: %v", err)
	}
	select {
	case <-result.RegistryProvidersReady:
	default:
		t.Fatal("registry provider readiness remained open after failure")
	}
}
