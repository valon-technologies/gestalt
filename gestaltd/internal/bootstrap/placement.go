package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// PlacementPlan decides whether configured providers are built locally or routed remote.
type PlacementPlan struct {
	remote bool
}

func NewPlacementPlan(cfg *config.Config) *PlacementPlan {
	remote := false
	if cfg != nil {
		remote = strings.TrimSpace(cfg.Server.Remote) != ""
	}
	return &PlacementPlan{remote: remote}
}

func (p *PlacementPlan) ShouldBuildLocal(entry *config.ProviderEntry) bool {
	if entry == nil {
		return false
	}
	if entry.DevActive {
		return true
	}
	return p == nil || !p.remote
}

func (p *PlacementPlan) ShouldRouteRemote(entry *config.ProviderEntry) bool {
	return entry != nil && entry.DevActive == false && p != nil && p.remote
}
