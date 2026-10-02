package appregistry

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

const (
	historyApp      = "tasks"
	historyRegistry = "main"
	historyBucket   = "history-bucket"
)

type fakeChanges struct {
	requests []*core.AppVersionChangeRequest
	err      error
}

func (f fakeChanges) ListRequestsByApp(context.Context, string) ([]*core.AppVersionChangeRequest, error) {
	return f.requests, f.err
}

type fakeFetcher struct {
	entries map[string]*Entry
	err     error
	calls   atomic.Int32
}

func (f *fakeFetcher) FetchEntry(_ context.Context, publicRoot, appName, version string) (*Entry, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if !strings.HasSuffix(publicRoot, "/"+historyBucket) || appName != historyApp {
		return nil, errors.New("unexpected registry coordinates")
	}
	return f.entries[version], nil
}

func entryWithOps(ops ...string) *Entry {
	e := &Entry{}
	e.Interface.Operations = map[string]OperationContract{}
	for _, op := range ops {
		e.Interface.Operations[op] = OperationContract{}
	}
	return e
}

func changeTo(version string, at time.Time) *core.AppVersionChangeRequest {
	return &core.AppVersionChangeRequest{App: historyApp, ToVersion: version, Timestamp: at}
}

func newHistory(t *testing.T, fetcher EntryFetcher, changes VersionChangeLister, apps map[string]*config.ProviderEntry) *OperationHistory {
	t.Helper()
	registry, err := config.NewGCSAppRegistry(historyBucket)
	if err != nil {
		t.Fatal(err)
	}
	return NewOperationHistory(fetcher, map[string]config.AppRegistryConfig{historyRegistry: registry}, apps, changes)
}

func registryApps() map[string]*config.ProviderEntry {
	return map[string]*config.ProviderEntry{historyApp: {Source: config.ProviderSource{Registry: historyRegistry}}}
}

func TestOperationHistoryResolvesVersionLiveAtTime(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	changes := fakeChanges{requests: []*core.AppVersionChangeRequest{
		changeTo("1.0.0", base),
		changeTo("2.0.0", base.Add(24*time.Hour)),
		changeTo("1.0.0", base.Add(48*time.Hour)),
	}}
	fetcher := &fakeFetcher{entries: map[string]*Entry{
		"1.0.0": entryWithOps("read"),
		"2.0.0": entryWithOps("read", "write"),
	}}
	h := newHistory(t, fetcher, changes, registryApps())

	cases := []struct {
		name string
		at   time.Time
		want []string
	}{
		{"first version", base.Add(time.Hour), []string{"read"}},
		{"second version", base.Add(30 * time.Hour), []string{"read", "write"}},
		{"after rollback", base.Add(72 * time.Hour), []string{"read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, known, err := h.OperationsAt(context.Background(), historyApp, tc.at)
			if err != nil || !known || !slices.Equal(ids, tc.want) {
				t.Fatalf("got %v known=%v err=%v, want %v", ids, known, err, tc.want)
			}
		})
	}
}

func TestOperationHistoryUnknownCases(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	live := fakeChanges{requests: []*core.AppVersionChangeRequest{changeTo("1.0.0", base)}}
	cases := []struct {
		name    string
		apps    map[string]*config.ProviderEntry
		changes fakeChanges
		entry   *Entry
		at      time.Time
	}{
		{"no history", registryApps(), fakeChanges{}, entryWithOps("read"), base.Add(time.Hour)},
		{"older than oldest request", registryApps(), live, entryWithOps("read"), base.Add(-time.Hour)},
		{"non-registry app", map[string]*config.ProviderEntry{historyApp: {}}, live, entryWithOps("read"), base.Add(time.Hour)},
		{"unconfigured app", map[string]*config.ProviderEntry{}, live, entryWithOps("read"), base.Add(time.Hour)},
		{"empty interface", registryApps(), live, entryWithOps(), base.Add(time.Hour)},
		{"missing entry", registryApps(), live, nil, base.Add(time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &fakeFetcher{entries: map[string]*Entry{"1.0.0": tc.entry}}
			h := newHistory(t, fetcher, tc.changes, tc.apps)
			ids, known, err := h.OperationsAt(context.Background(), historyApp, tc.at)
			if err != nil || known || len(ids) != 0 {
				t.Fatalf("got %v known=%v err=%v, want unknown", ids, known, err)
			}
		})
	}
}

func TestOperationHistoryReturnsErrors(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	live := fakeChanges{requests: []*core.AppVersionChangeRequest{changeTo("1.0.0", base)}}
	boom := errors.New("boom")

	t.Run("fetch", func(t *testing.T) {
		h := newHistory(t, &fakeFetcher{err: boom}, live, registryApps())
		if _, known, err := h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour)); !errors.Is(err, boom) || known {
			t.Fatalf("known=%v err=%v, want fetch error", known, err)
		}
	})
	t.Run("list", func(t *testing.T) {
		h := newHistory(t, &fakeFetcher{}, fakeChanges{err: boom}, registryApps())
		if _, known, err := h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour)); !errors.Is(err, boom) || known {
			t.Fatalf("known=%v err=%v, want list error", known, err)
		}
	})
}

func TestOperationHistoryCachesPerVersion(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	changes := fakeChanges{requests: []*core.AppVersionChangeRequest{
		changeTo("1.0.0", base),
		changeTo("2.0.0", base.Add(24*time.Hour)),
	}}
	fetcher := &fakeFetcher{entries: map[string]*Entry{"1.0.0": entryWithOps("read"), "2.0.0": entryWithOps("write")}}
	h := newHistory(t, fetcher, changes, registryApps())

	for range 3 {
		if _, _, err := h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if got := fetcher.calls.Load(); got != 1 {
		t.Fatalf("fetches after repeated reads of one version = %d, want 1", got)
	}
	if _, _, err := h.OperationsAt(context.Background(), historyApp, base.Add(30*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := fetcher.calls.Load(); got != 2 {
		t.Fatalf("fetches after a second version = %d, want 2", got)
	}
}

type countingChanges struct {
	requests []*core.AppVersionChangeRequest
	err      error
	calls    int
}

func (c *countingChanges) ListRequestsByApp(context.Context, string) ([]*core.AppVersionChangeRequest, error) {
	c.calls++
	return c.requests, c.err
}

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

func TestOperationHistoryUsesEarliestFromVersionBeforeRecordedHistory(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := changeTo("2.0.0", base)
	first.FromVersion = "1.0.0"
	fetcher := &fakeFetcher{entries: map[string]*Entry{"1.0.0": entryWithOps("read"), "2.0.0": entryWithOps("read", "write")}}
	h := newHistory(t, fetcher, fakeChanges{requests: []*core.AppVersionChangeRequest{first}}, registryApps())

	ids, known, err := h.OperationsAt(context.Background(), historyApp, base.Add(-24*time.Hour))
	if err != nil || !known || !slices.Equal(ids, []string{"read"}) {
		t.Fatalf("got %v known=%v err=%v, want the original version's operations", ids, known, err)
	}
}

func TestOperationHistoryUnknownSaveTimeIsUnknown(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := changeTo("2.0.0", base)
	first.FromVersion = "1.0.0"
	fetcher := &fakeFetcher{entries: map[string]*Entry{"1.0.0": entryWithOps("read"), "2.0.0": entryWithOps("read", "write")}}
	h := newHistory(t, fetcher, fakeChanges{requests: []*core.AppVersionChangeRequest{first}}, registryApps())

	ids, known, err := h.OperationsAt(context.Background(), historyApp, time.Time{})
	if err != nil || known || len(ids) != 0 {
		t.Fatalf("got %v known=%v err=%v, want unknown", ids, known, err)
	}
}

func TestOperationHistoryUsesVersionRequestedByTheSaveTime(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next := base.Add(24 * time.Hour)
	changes := fakeChanges{requests: []*core.AppVersionChangeRequest{changeTo("1.0.0", base), changeTo("2.0.0", next)}}
	fetcher := &fakeFetcher{entries: map[string]*Entry{"1.0.0": entryWithOps("read"), "2.0.0": entryWithOps("read", "write")}}
	h := newHistory(t, fetcher, changes, registryApps())

	cases := []struct {
		name string
		at   time.Time
		want []string
	}{
		{"just before the next request", next.Add(-time.Minute), []string{"read"}},
		{"at the next request", next, []string{"read", "write"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, known, err := h.OperationsAt(context.Background(), historyApp, tc.at)
			if err != nil || !known || !slices.Equal(ids, tc.want) {
				t.Fatalf("got %v known=%v err=%v, want %v", ids, known, err, tc.want)
			}
		})
	}
}

func TestOperationHistoryCachesChangeRequestsWithTTL(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	changes := &countingChanges{requests: []*core.AppVersionChangeRequest{changeTo("1.0.0", base)}}
	h := newHistory(t, &fakeFetcher{entries: map[string]*Entry{"1.0.0": entryWithOps("read")}}, changes, registryApps())
	clock := &testClock{now: base}
	h.now = clock.Now

	query := func() {
		t.Helper()
		if _, _, err := h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	query()
	query()
	if changes.calls != 1 {
		t.Fatalf("list calls within TTL = %d, want 1", changes.calls)
	}
	clock.now = base.Add(changeRequestsTTL + time.Second)
	query()
	if changes.calls != 2 {
		t.Fatalf("list calls after TTL = %d, want 2", changes.calls)
	}
}

func TestOperationHistoryNegativeCachesFailures(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	boom := errors.New("boom")

	t.Run("list error", func(t *testing.T) {
		changes := &countingChanges{err: boom}
		h := newHistory(t, &fakeFetcher{}, changes, registryApps())
		clock := &testClock{now: base}
		h.now = clock.Now
		for range 3 {
			if _, _, err := h.OperationsAt(context.Background(), historyApp, base); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want list error", err)
			}
		}
		if changes.calls != 1 {
			t.Fatalf("list calls while negative-cached = %d, want 1", changes.calls)
		}
		clock.now = base.Add(historyNegativeTTL + time.Second)
		_, _, _ = h.OperationsAt(context.Background(), historyApp, base)
		if changes.calls != 2 {
			t.Fatalf("list calls after negative TTL = %d, want 2", changes.calls)
		}
	})

	t.Run("fetch error", func(t *testing.T) {
		fetcher := &fakeFetcher{err: boom}
		live := fakeChanges{requests: []*core.AppVersionChangeRequest{changeTo("1.0.0", base)}}
		h := newHistory(t, fetcher, live, registryApps())
		clock := &testClock{now: base}
		h.now = clock.Now
		for range 3 {
			if _, _, err := h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour)); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want fetch error", err)
			}
		}
		if got := fetcher.calls.Load(); got != 1 {
			t.Fatalf("fetches while negative-cached = %d, want 1", got)
		}
		clock.now = base.Add(historyNegativeTTL + time.Second)
		_, _, _ = h.OperationsAt(context.Background(), historyApp, base.Add(time.Hour))
		if got := fetcher.calls.Load(); got != 2 {
			t.Fatalf("fetches after negative TTL = %d, want 2", got)
		}
	})
}
