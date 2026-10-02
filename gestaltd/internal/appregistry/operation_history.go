package appregistry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	operationHistoryCacheLimit = 1024
	changeRequestsTTL          = time.Minute
	historyNegativeTTL         = 10 * time.Second
)

// EntryFetcher is the part of RegistryReader the operation history needs.
type EntryFetcher interface {
	FetchEntry(ctx context.Context, publicRoot, appName, version string) (*Entry, error)
}

// VersionChangeLister lists an app's version changes, oldest first.
type VersionChangeLister interface {
	ListRequestsByApp(ctx context.Context, appName string) ([]*core.AppVersionChangeRequest, error)
}

type changesCacheEntry struct {
	requests []*core.AppVersionChangeRequest
	err      error
	expires  time.Time
}

type entryFailure struct {
	err     error
	expires time.Time
}

type operationHistoryKey struct {
	registryApp string
	publicRoot  string
	version     string
}

// OperationHistory answers core.OperationHistory for registry-sourced apps by
// finding the version that was live at a time and reading that version's
// published interface. Entries are immutable, so they are cached per version.
type OperationHistory struct {
	fetcher    EntryFetcher
	registries map[string]config.AppRegistryConfig
	apps       map[string]*config.ProviderEntry
	changes    VersionChangeLister

	now func() time.Time

	mu       sync.Mutex
	cache    map[operationHistoryKey][]string
	failures map[operationHistoryKey]entryFailure
	requests map[string]changesCacheEntry
}

var _ core.OperationHistory = (*OperationHistory)(nil)

func NewOperationHistory(fetcher EntryFetcher, registries map[string]config.AppRegistryConfig, apps map[string]*config.ProviderEntry, changes VersionChangeLister) *OperationHistory {
	return &OperationHistory{
		fetcher:    fetcher,
		registries: registries,
		apps:       apps,
		changes:    changes,
		now:        time.Now,
		cache:      make(map[operationHistoryKey][]string),
		failures:   make(map[operationHistoryKey]entryFailure),
		requests:   make(map[string]changesCacheEntry),
	}
}

func (h *OperationHistory) OperationsAt(ctx context.Context, app string, at time.Time) ([]string, bool, error) {
	if h == nil || h.fetcher == nil || h.changes == nil {
		return nil, false, nil
	}
	entry := h.apps[app]
	if entry == nil || !entry.Source.IsRegistry() {
		return nil, false, nil
	}
	registry, ok := h.registries[strings.TrimSpace(entry.Source.Registry)]
	if !ok {
		return nil, false, nil
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return nil, false, nil
	}

	requests, err := h.changeRequests(ctx, app)
	if err != nil {
		return nil, false, fmt.Errorf("list version changes for %q: %w", app, err)
	}
	if at.IsZero() {
		return nil, false, nil
	}
	version := versionLiveAt(requests, at)
	if version == "" {
		return nil, false, nil
	}

	key := operationHistoryKey{registryApp: entry.Source.RegistryAppName(app), publicRoot: publicRoot, version: version}
	if ids, ok := h.cached(key); ok {
		return ids, len(ids) > 0, nil
	}
	if err := h.failed(key); err != nil {
		return nil, false, err
	}
	fetched, err := h.fetcher.FetchEntry(ctx, key.publicRoot, key.registryApp, key.version)
	if err != nil {
		err = fmt.Errorf("fetch %q version %q: %w", key.registryApp, key.version, err)
		h.storeFailure(key, err)
		return nil, false, err
	}
	var ids []string
	if fetched != nil {
		for id := range fetched.Interface.Operations {
			ids = append(ids, id)
		}
		slices.Sort(ids)
	}
	h.store(key, ids)
	return ids, len(ids) > 0, nil
}

// versionLiveAt returns the version live at the given time. A positive time
// before the first recorded change pre-dates recorded history, so the version
// the first change replaced is used. An unknown time yields no version.
// Matching on request time errs toward a larger existed set, which only adds
// opt-outs.
func versionLiveAt(requests []*core.AppVersionChangeRequest, at time.Time) string {
	var earliest *core.AppVersionChangeRequest
	for _, req := range requests {
		if req != nil && (earliest == nil || req.Timestamp.Before(earliest.Timestamp)) {
			earliest = req
		}
	}
	if earliest == nil {
		return ""
	}
	if at.Before(earliest.Timestamp) {
		return strings.TrimSpace(earliest.FromVersion)
	}
	var (
		version string
		latest  time.Time
	)
	for _, req := range requests {
		if req == nil || req.Timestamp.After(at) || req.Timestamp.Before(latest) {
			continue
		}
		latest = req.Timestamp
		version = strings.TrimSpace(req.ToVersion)
	}
	return version
}

func (h *OperationHistory) changeRequests(ctx context.Context, app string) ([]*core.AppVersionChangeRequest, error) {
	h.mu.Lock()
	cached, ok := h.requests[app]
	h.mu.Unlock()
	if ok && h.now().Before(cached.expires) {
		return cached.requests, cached.err
	}
	requests, err := h.changes.ListRequestsByApp(ctx, app)
	ttl := changeRequestsTTL
	if err != nil {
		ttl = historyNegativeTTL
	}
	h.mu.Lock()
	h.requests[app] = changesCacheEntry{requests: requests, err: err, expires: h.now().Add(ttl)}
	h.mu.Unlock()
	return requests, err
}

func (h *OperationHistory) failed(key operationHistoryKey) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	failure, ok := h.failures[key]
	if !ok {
		return nil
	}
	if !h.now().Before(failure.expires) {
		delete(h.failures, key)
		return nil
	}
	return failure.err
}

func (h *OperationHistory) storeFailure(key operationHistoryKey, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.failures) >= operationHistoryCacheLimit {
		clear(h.failures)
	}
	h.failures[key] = entryFailure{err: err, expires: h.now().Add(historyNegativeTTL)}
}

func (h *OperationHistory) cached(key operationHistoryKey) ([]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids, ok := h.cache[key]
	return ids, ok
}

func (h *OperationHistory) store(key operationHistoryKey, ids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.cache) >= operationHistoryCacheLimit {
		clear(h.cache)
	}
	h.cache[key] = ids
}
