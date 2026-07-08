package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// RemoteProviderKind identifies providers in plan-6 remote placement scope.
type RemoteProviderKind string

const (
	RemoteProviderKindApp       RemoteProviderKind = "app"
	RemoteProviderKindAgent     RemoteProviderKind = "agent"
	RemoteProviderKindWorkflow  RemoteProviderKind = "workflow"
	RemoteProviderKindIndexedDB RemoteProviderKind = "indexeddb"
)

// ProviderPlacement is where a configured provider is served.
type ProviderPlacement int

const (
	ProviderPlacementAbsent ProviderPlacement = iota
	ProviderPlacementLocal
	ProviderPlacementRemote
)

// PlacementPlan centralizes local-vs-remote provider placement before routing is wired.
type PlacementPlan struct {
	cfg              *config.Config
	remoteConfigured bool
}

// NewPlacementPlan derives placement decisions from the loaded config.
func NewPlacementPlan(cfg *config.Config) *PlacementPlan {
	if cfg == nil {
		return &PlacementPlan{}
	}
	cfg.Server.NormalizeRemote()
	return &PlacementPlan{
		cfg:              cfg,
		remoteConfigured: strings.TrimSpace(cfg.Server.Remote) != "",
	}
}

// RemoteConfigured reports whether server.remote is configured.
func (p *PlacementPlan) RemoteConfigured() bool {
	return p != nil && p.remoteConfigured
}

// Placement returns whether a provider is absent, local, or remote.
func (p *PlacementPlan) Placement(kind RemoteProviderKind, name string) ProviderPlacement {
	if p == nil || p.cfg == nil || strings.TrimSpace(name) == "" {
		return ProviderPlacementAbsent
	}
	if !isDeclaredProvider(p.cfg, kind, name) {
		return ProviderPlacementAbsent
	}
	if isLocalDevActive(p.cfg, kind, name) {
		return ProviderPlacementLocal
	}
	if p.remoteConfigured {
		return ProviderPlacementRemote
	}
	return ProviderPlacementLocal
}

// ShouldBuildLocal reports whether bootstrap should start the provider locally.
func (p *PlacementPlan) ShouldBuildLocal(kind RemoteProviderKind, name string) bool {
	return p.Placement(kind, name) == ProviderPlacementLocal
}

// ShouldRouteRemote reports whether lookups should delegate to the remote gestaltd.
func (p *PlacementPlan) ShouldRouteRemote(kind RemoteProviderKind, name string) bool {
	return p.Placement(kind, name) == ProviderPlacementRemote
}

func isLocalDevActive(cfg *config.Config, kind RemoteProviderKind, name string) bool {
	entry := declaredProviderEntry(cfg, kind, name)
	return entry != nil && entry.DevActive
}

func isDeclaredProvider(cfg *config.Config, kind RemoteProviderKind, name string) bool {
	return declaredProviderEntry(cfg, kind, name) != nil
}

func declaredProviderEntry(cfg *config.Config, kind RemoteProviderKind, name string) *config.ProviderEntry {
	if cfg == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	switch kind {
	case RemoteProviderKindApp:
		return cfg.Apps[name]
	case RemoteProviderKindAgent:
		return cfg.Providers.Agent[name]
	case RemoteProviderKindWorkflow:
		return cfg.Providers.Workflow[name]
	case RemoteProviderKindIndexedDB:
		return cfg.Providers.IndexedDB[name]
	default:
		return nil
	}
}
