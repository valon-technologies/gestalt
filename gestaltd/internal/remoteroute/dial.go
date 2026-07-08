package remoteroute

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
)

// Dial creates an authenticated remote client set from server config.
func Dial(ctx context.Context, cfg *config.Config) (*remote.ClientSet, error) {
	if cfg == nil {
		return nil, nil
	}
	cfg.Server.NormalizeRemote()
	if strings.TrimSpace(cfg.Server.Remote) == "" {
		return nil, nil
	}
	return remote.NewClientSet(ctx, remote.Config{
		URL:   cfg.Server.Remote,
		Token: cfg.Server.RemoteToken,
	})
}
