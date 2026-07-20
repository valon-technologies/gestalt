package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type startupRegistryMaterializer struct {
	installations []*core.AppInstallation
}

func (m *startupRegistryMaterializer) Ensure(_ context.Context, installation *core.AppInstallation) (*core.AppMaterializationResult, error) {
	m.installations = append(m.installations, installation)
	return &core.AppMaterializationResult{}, nil
}

type startupRegistryStarter struct {
	app     string
	version string
}

func (s *startupRegistryStarter) ValidateInstallation(*core.AppInstallation) error {
	return nil
}

func (s *startupRegistryStarter) StartApp(_ context.Context, app, version string) error {
	s.app = app
	s.version = version
	return nil
}

func TestStartRegistryOnlyAppProvidersStartsLatestKnownVersion(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	for index, version := range []string{"v1", "v2"} {
		installedAt := time.Date(2026, 7, 20, 12, index, 0, 0, time.UTC)
		installation := &core.AppInstallation{
			AppName:     "g-issues",
			Version:     version,
			Registry:    "toolshed",
			InstalledAt: installedAt,
			UpdatedAt:   installedAt,
		}
		if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: "previous",
			ToVersion:   version,
			Timestamp:   installedAt,
			Metadata:    coredata.ChangeRequestMetadata(installation),
		}); err != nil {
			t.Fatalf("AppendRequest: %v", err)
		}
	}

	materializer := &startupRegistryMaterializer{}
	starter := &startupRegistryStarter{}
	err := startRegistryOnlyAppProviders(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
	}, services.AppVersionChangeRequests, materializer, starter)
	if err != nil {
		t.Fatalf("startRegistryOnlyAppProviders: %v", err)
	}
	if len(materializer.installations) != 1 || materializer.installations[0].Version != "v2" {
		t.Fatalf("materialized installations = %#v", materializer.installations)
	}
	if starter.app != "g-issues" || starter.version != "v2" {
		t.Fatalf("started = %s@%s", starter.app, starter.version)
	}
}

func TestStartRegistryOnlyAppProvidersSkipsEmptyCatalog(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	materializer := &startupRegistryMaterializer{}
	starter := &startupRegistryStarter{}
	err := startRegistryOnlyAppProviders(context.Background(), &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
	}, services.AppVersionChangeRequests, materializer, starter)
	if err != nil {
		t.Fatalf("startRegistryOnlyAppProviders: %v", err)
	}
	if len(materializer.installations) != 0 || starter.app != "" {
		t.Fatalf("unexpected startup: materialized=%#v started=%s@%s", materializer.installations, starter.app, starter.version)
	}
}
