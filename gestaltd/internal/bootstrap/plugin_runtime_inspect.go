package bootstrap

import (
	"context"
	"fmt"
	"slices"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

type RuntimeInspector interface {
	SnapshotPluginRuntimes(ctx context.Context) ([]RuntimeProviderSnapshot, error)
}

type RuntimeProviderSnapshot struct {
	Name          string
	Driver        config.RuntimeProviderDriver
	Default       bool
	Loaded        bool
	SupportLoaded bool
	Advertised    RuntimeBehavior
	Effective     RuntimeBehavior
	Sessions      []pluginruntime.Session
	Error         string
}

func (r *pluginRuntimeRegistry) SnapshotPluginRuntimes(ctx context.Context) ([]RuntimeProviderSnapshot, error) {
	if r == nil || r.cfg == nil {
		return nil, nil
	}

	type item struct {
		name     string
		entry    *config.RuntimeProviderEntry
		provider pluginruntime.Provider
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("plugin runtime registry is closed")
	}
	items := make([]item, 0, len(r.cfg.Runtime.Providers))
	for name, entry := range r.cfg.Runtime.Providers {
		items = append(items, item{
			name:     name,
			entry:    entry,
			provider: r.providers[name],
		})
	}
	r.mu.Unlock()

	slices.SortFunc(items, func(a, b item) int {
		switch {
		case a.name < b.name:
			return -1
		case a.name > b.name:
			return 1
		default:
			return 0
		}
	})

	out := make([]RuntimeProviderSnapshot, 0, len(items))
	for _, item := range items {
		snapshot := RuntimeProviderSnapshot{
			Name: item.name,
		}
		if item.entry != nil {
			snapshot.Driver = item.entry.Driver
			snapshot.Default = item.entry.Default
		}
		if item.provider != nil {
			snapshot.Loaded = true
			support, err := item.provider.Support(ctx)
			if err != nil {
				snapshot.Error = fmt.Sprintf("support: %v", err)
				out = append(out, snapshot)
				continue
			}
			snapshot.SupportLoaded = true
			snapshot.Advertised = runtimeAdvertisedBehavior(support)
			snapshot.Effective = runtimeResolvedBehavior(snapshot.Advertised, r.deps)
			sessions, err := item.provider.ListSessions(ctx)
			if err != nil {
				snapshot.Error = fmt.Sprintf("list sessions: %v", err)
				out = append(out, snapshot)
				continue
			}
			snapshot.Sessions = sessions
		}
		out = append(out, snapshot)
	}
	return out, nil
}
