package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
)

func resolvesLocal(cfg *config.Config, entry *config.ProviderEntry, forHost bool) bool {
	if entry == nil {
		return false
	}
	if entry.DevActive || entry.Local {
		return true
	}
	if forHost {
		return cfg == nil || cfg.DefaultRemoteName() == ""
	}
	if entry.Remote != "" {
		return false
	}
	return cfg == nil || cfg.DefaultRemoteName() == ""
}

func appRemoteName(cfg *config.Config, entry *config.ProviderEntry) string {
	if entry == nil {
		return ""
	}
	if name := strings.TrimSpace(entry.Remote); name != "" {
		return name
	}
	if cfg == nil {
		return ""
	}
	return cfg.DefaultRemoteName()
}

func requireDefaultClientSet(deps Deps) (*remote.ClientSet, error) {
	name := strings.TrimSpace(deps.DefaultRemoteName)
	if name == "" {
		return nil, fmt.Errorf("bootstrap: default remote client is required")
	}
	clients := deps.RemoteClients[name]
	if clients == nil {
		return nil, fmt.Errorf("bootstrap: default remote %q client is required", name)
	}
	return clients, nil
}

func closeRemoteClients(clients map[string]*remote.ClientSet) error {
	if len(clients) == 0 {
		return nil
	}
	var errs []error
	for name, set := range clients {
		if set == nil {
			continue
		}
		if err := set.Close(); err != nil {
			errs = append(errs, fmt.Errorf("remote %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func dialRemoteClients(ctx context.Context, cfg *config.Config) (map[string]*remote.ClientSet, string, string, error) {
	if cfg == nil || len(cfg.Server.Remotes) == 0 {
		return nil, "", "", nil
	}

	clients := make(map[string]*remote.ClientSet, len(cfg.Server.Remotes))
	var defaultName string
	var defaultToken string
	for name, remoteCfg := range cfg.Server.Remotes {
		if remoteCfg == nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, "", "", errors.Join(
				fmt.Errorf("bootstrap: remote name is required"),
				closeRemoteClients(clients),
			)
		}
		clientSet, err := remote.NewClientSet(ctx, remote.Config{
			URL:   remoteCfg.URL,
			Token: remoteCfg.Token,
		})
		if err != nil {
			return nil, "", "", errors.Join(
				fmt.Errorf("bootstrap: remote %q: %w", name, err),
				closeRemoteClients(clients),
			)
		}
		clients[name] = clientSet
		if remoteCfg.Default {
			defaultName = name
			defaultToken = remoteCfg.Token
		}
	}
	return clients, defaultName, defaultToken, nil
}
