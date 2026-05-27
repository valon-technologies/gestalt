package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
)

type RuntimeInspector interface {
	SnapshotRuntimes(ctx context.Context) ([]RuntimeProviderSnapshot, error)
	ListRuntimeSessions(ctx context.Context, providerName string, req runtimeprovider.ListSessionsRequest) (*runtimeprovider.ListSessionsResponse, error)
	ListRuntimeSessionLogs(ctx context.Context, providerName, sessionID string, afterSeq int64, limit int) ([]runtimelogs.Record, error)
}

var (
	ErrRuntimeProviderNotFound    = errors.New("runtime provider not found")
	ErrRuntimeProviderUnavailable = errors.New("runtime provider unavailable")
)

type RuntimeProviderSnapshot struct {
	Name          string
	Driver        config.RuntimeProviderDriver
	Default       bool
	Loaded        bool
	SupportLoaded bool
	Advertised    RuntimeBehavior
	Effective     RuntimeBehavior
	Error         string
}

func (r *runtimeRegistry) SnapshotRuntimes(ctx context.Context) ([]RuntimeProviderSnapshot, error) {
	if r == nil || r.cfg == nil {
		return nil, nil
	}

	type item struct {
		name     string
		entry    *config.RuntimeProviderEntry
		provider runtimeprovider.Provider
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("runtime registry is closed")
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
			snapshot.Effective = runtimeResolvedBehavior(snapshot.Advertised, runtimeHostServiceAccess(support, r.deps), r.deps)
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (r *runtimeRegistry) ListRuntimeSessions(ctx context.Context, providerName string, req runtimeprovider.ListSessionsRequest) (*runtimeprovider.ListSessionsResponse, error) {
	providerName = strings.TrimSpace(providerName)
	if r == nil || r.cfg == nil || providerName == "" {
		return nil, ErrRuntimeProviderNotFound
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: registry is closed", ErrRuntimeProviderUnavailable)
	}
	_, configured := r.cfg.Runtime.Providers[providerName]
	provider := r.providers[providerName]
	r.mu.Unlock()

	if !configured {
		return nil, ErrRuntimeProviderNotFound
	}
	if provider == nil {
		return nil, fmt.Errorf("%w: provider is not loaded", ErrRuntimeProviderUnavailable)
	}
	return provider.ListSessions(ctx, req)
}

func (r *runtimeRegistry) ListRuntimeSessionLogs(ctx context.Context, providerName, sessionID string, afterSeq int64, limit int) ([]runtimelogs.Record, error) {
	if r == nil || r.deps.Services == nil || r.deps.Services.RuntimeSessionLogs == nil {
		return nil, nil
	}
	return r.deps.Services.RuntimeSessionLogs.ListSessionLogs(ctx, providerName, sessionID, afterSeq, limit)
}
