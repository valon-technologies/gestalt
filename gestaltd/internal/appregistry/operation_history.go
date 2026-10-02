package appregistry

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

type operationHistoryKey struct {
	registryApp string
	publicRoot  string
	version     string
}

type changeList struct {
	requests []*core.AppVersionChangeRequest
	err      error
}

type versionOperations struct {
	ids   []string
	known bool
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

	requests *ttlCache[string, changeList]
	entries  *ttlCache[operationHistoryKey, versionOperations]
	failures *ttlCache[operationHistoryKey, error]
}

var _ core.OperationHistory = (*OperationHistory)(nil)

func NewOperationHistory(fetcher EntryFetcher, registries map[string]config.AppRegistryConfig, apps map[string]*config.ProviderEntry, changes VersionChangeLister) *OperationHistory {
	h := &OperationHistory{
		fetcher:    fetcher,
		registries: registries,
		apps:       apps,
		changes:    changes,
		now:        time.Now,
	}
	clock := func() time.Time { return h.now() }
	h.requests = newTTLCache[string, changeList](operationHistoryCacheLimit, clock)
	h.entries = newTTLCache[operationHistoryKey, versionOperations](operationHistoryCacheLimit, clock)
	h.failures = newTTLCache[operationHistoryKey, error](operationHistoryCacheLimit, clock)
	return h
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
	publicRoot, ok := registryPublicRoot(registry)
	if !ok {
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
	return h.operationsOf(ctx, key)
}

// operationsOf returns the operations a version published. An empty
// interface means "no static catalog, history unavailable", so known is false.
func (h *OperationHistory) operationsOf(ctx context.Context, key operationHistoryKey) ([]string, bool, error) {
	if cached, ok := h.entries.get(key); ok {
		return cached.ids, cached.known, nil
	}
	if err, ok := h.failures.get(key); ok {
		return nil, false, err
	}
	fetched, err := h.fetcher.FetchEntry(ctx, key.publicRoot, key.registryApp, key.version)
	if err != nil {
		err = fmt.Errorf("fetch %q version %q: %w", key.registryApp, key.version, err)
		h.failures.put(key, err, historyNegativeTTL)
		return nil, false, err
	}
	var ids []string
	if fetched != nil {
		for id := range fetched.Interface.Operations {
			ids = append(ids, id)
		}
		slices.Sort(ids)
	}
	known := len(ids) > 0
	h.entries.put(key, versionOperations{ids: ids, known: known}, noExpiry)
	return ids, known, nil
}

// versionLiveAt returns the version live at the given time. A positive time
// before the first recorded change pre-dates recorded history, so the version
// the first change replaced is used. An unknown time yields no version.
// Matching on request time errs toward a larger existed set, which only adds
// opt-outs.
func versionLiveAt(requests []*core.AppVersionChangeRequest, at time.Time) string {
	ordered := make([]*core.AppVersionChangeRequest, 0, len(requests))
	for _, req := range requests {
		if req != nil {
			ordered = append(ordered, req)
		}
	}
	if len(ordered) == 0 {
		return ""
	}
	slices.SortStableFunc(ordered, func(a, b *core.AppVersionChangeRequest) int {
		return a.Timestamp.Compare(b.Timestamp)
	})
	if at.Before(ordered[0].Timestamp) {
		return strings.TrimSpace(ordered[0].FromVersion)
	}
	version := strings.TrimSpace(ordered[0].ToVersion)
	for _, req := range ordered {
		if req.Timestamp.After(at) {
			break
		}
		version = strings.TrimSpace(req.ToVersion)
	}
	return version
}

func (h *OperationHistory) changeRequests(ctx context.Context, app string) ([]*core.AppVersionChangeRequest, error) {
	if cached, ok := h.requests.get(app); ok {
		return cached.requests, cached.err
	}
	requests, err := h.changes.ListRequestsByApp(ctx, app)
	ttl := changeRequestsTTL
	if err != nil {
		ttl = historyNegativeTTL
	}
	h.requests.put(app, changeList{requests: requests, err: err}, ttl)
	return requests, err
}

// registryPublicRoot reports false for a registry without a usable public URL,
// which leaves its history unknown rather than failing the caller.
func registryPublicRoot(registry config.AppRegistryConfig) (string, bool) {
	root, err := registry.PublicURL()
	return root, err == nil
}
