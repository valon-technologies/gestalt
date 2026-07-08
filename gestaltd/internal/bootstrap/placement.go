package bootstrap

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// RemoteProviderKind identifies providers eligible for gestaltd-to-gestaltd remote delegation.
type RemoteProviderKind string

const (
	RemoteProviderKindApp       RemoteProviderKind = "app"
	RemoteProviderKindAgent     RemoteProviderKind = "agent"
	RemoteProviderKindWorkflow  RemoteProviderKind = "workflow"
	RemoteProviderKindIndexedDB RemoteProviderKind = "indexeddb"
)

// ProviderPlacement describes whether a configured provider is served locally, remotely, or undeclared.
type ProviderPlacement int

const (
	PlacementUndeclared ProviderPlacement = iota
	PlacementLocal
	PlacementRemote
)

// PlacementPlan centralizes local-vs-remote provider placement decisions for plan-6.
type PlacementPlan struct {
	cfg               *config.Config
	selectedIndexedDB string
}

// NewPlacementPlan builds a placement plan from the resolved runtime config.
func NewPlacementPlan(cfg *config.Config) (*PlacementPlan, error) {
	if cfg == nil {
		return &PlacementPlan{}, nil
	}
	selectedIndexedDB, _, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}
	return &PlacementPlan{
		cfg:               cfg,
		selectedIndexedDB: strings.TrimSpace(selectedIndexedDB),
	}, nil
}

// Placement returns whether the named provider is local, remote, or undeclared.
func (p *PlacementPlan) Placement(kind RemoteProviderKind, name string) ProviderPlacement {
	if p == nil || p.cfg == nil {
		return PlacementUndeclared
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return PlacementUndeclared
	}
	entry, declared := p.declaredEntry(kind, name)
	if !declared || entry == nil {
		return PlacementUndeclared
	}
	if p.isLocalOverride(kind, name, entry) {
		return PlacementLocal
	}
	if p.cfg.Server.HasRemote() {
		return PlacementRemote
	}
	return PlacementLocal
}

// ShouldRouteRemote reports whether invocations for the provider should delegate to server.remote.
func (p *PlacementPlan) ShouldRouteRemote(kind RemoteProviderKind, name string) bool {
	return p.Placement(kind, name) == PlacementRemote
}

// IsDeclared reports whether the provider exists in the configured V1 remote scope.
func (p *PlacementPlan) IsDeclared(kind RemoteProviderKind, name string) bool {
	_, declared := p.declaredEntry(kind, name)
	return declared
}

func (p *PlacementPlan) isLocalOverride(kind RemoteProviderKind, name string, entry *config.ProviderEntry) bool {
	switch kind {
	case RemoteProviderKindApp:
		return entry.DevActive
	case RemoteProviderKindIndexedDB:
		return name == p.selectedIndexedDB
	default:
		return false
	}
}

func (p *PlacementPlan) declaredEntry(kind RemoteProviderKind, name string) (*config.ProviderEntry, bool) {
	switch kind {
	case RemoteProviderKindApp:
		entry, ok := p.cfg.Apps[name]
		return entry, ok && entry != nil
	case RemoteProviderKindAgent:
		entry, ok := p.cfg.Providers.Agent[name]
		return entry, ok && entry != nil
	case RemoteProviderKindWorkflow:
		entry, ok := p.cfg.Providers.Workflow[name]
		return entry, ok && entry != nil
	case RemoteProviderKindIndexedDB:
		entry, ok := p.cfg.Providers.IndexedDB[name]
		return entry, ok && entry != nil
	default:
		return nil, false
	}
}
