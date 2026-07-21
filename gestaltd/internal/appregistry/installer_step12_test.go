package appregistry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestInstallerAddUsesFirstInstallSentinel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	installer := newRegistryOnlyInstaller(svc, fixture)

	if _, err := installer.Add(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp: %v", err)
	}
	if len(requests) != 1 || requests[0].FromVersion != "registry:first-install" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestInstallerAddAndUpgradeRequireCorrectCatalogState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)
	installer := newRegistryOnlyInstaller(svc, fixture)

	if _, err := installer.Upgrade(ctx, appregistry.InstallInput{
		Registry: "toolshed", App: "g-issues", Version: fixture.Version,
	}); !errors.Is(err, appregistry.ErrAppNotAdded) {
		t.Fatalf("Upgrade empty catalog error = %v", err)
	}

	now := time.Now().UTC()
	installer.Now = func() time.Time { return now.Add(time.Second) }
	if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		App: "g-issues", FromVersion: "registry:first-install", ToVersion: "0.9.0", Timestamp: now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	if _, err := installer.Add(ctx, appregistry.InstallInput{
		Registry: "toolshed", App: "g-issues", Version: fixture.Version,
	}); !errors.Is(err, appregistry.ErrAppAlreadyAdded) {
		t.Fatalf("Add populated catalog error = %v", err)
	}
	if _, err := installer.Upgrade(ctx, appregistry.InstallInput{
		Registry: "toolshed", App: "g-issues", Version: fixture.Version,
	}); err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp: %v", err)
	}
	if len(requests) != 2 || requests[1].FromVersion != "0.9.0" {
		t.Fatalf("requests = %#v", requests)
	}
}

func newRegistryOnlyInstaller(svc *testutil.Services, fixture registrytest.InstallFixture) *appregistry.Installer {
	return &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Reader:         fixture.Reader,
		ChangeRequests: svc.AppVersionChangeRequests,
		Locks:          svc.AppVersionInstallLocks,
		Rollouts:       svc.AppRollouts,
	}
}
