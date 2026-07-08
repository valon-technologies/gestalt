package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
)

func dialRemoteClients(ctx context.Context, cfg *config.Config) (*remote.ClientSet, error) {
	if cfg == nil || strings.TrimSpace(cfg.Server.Remote) == "" {
		return nil, nil
	}
	return remote.NewClientSet(ctx, remote.Config{
		URL:   cfg.Server.Remote,
		Token: cfg.Server.RemoteToken,
	})
}

func registerRemoteApps(
	providers *registry.ProviderMap[core.Provider],
	cfg *config.Config,
	clients *remote.ClientSet,
) error {
	if providers == nil || cfg == nil || clients == nil || clients.App == nil {
		return nil
	}
	for name, entry := range cfg.Apps {
		name = strings.TrimSpace(name)
		if name == "" || entry == nil || providerBuildsLocal(cfg, entry) {
			continue
		}
		spec, err := remoteAppSpec(name, entry)
		if err != nil {
			return fmt.Errorf("remote app %q: %w", name, err)
		}
		provider := appservice.NewGestaltRemoteProvider(clients.App, spec)
		if provider == nil {
			return fmt.Errorf("remote app %q: provider client is required", name)
		}
		if err := providers.Register(name, provider); err != nil {
			return fmt.Errorf("remote app %q: %w", name, err)
		}
		slog.Info("registered remote app provider", "provider", name)
	}
	return nil
}

func remoteAppSpec(name string, entry *config.ProviderEntry) (appservice.StaticProviderSpec, error) {
	if spec, _, err := buildStartupProviderSpec(name, entry); err == nil {
		return spec, nil
	}
	displayName := strings.TrimSpace(entry.DisplayName)
	if displayName == "" {
		displayName = name
	}
	return appservice.StaticProviderSpec{
		Name:        name,
		DisplayName: displayName,
		Description: strings.TrimSpace(entry.Description),
	}, nil
}
