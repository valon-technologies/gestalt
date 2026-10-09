package observability

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// AppInvocationBucketWidth is the time resolution of persisted per-app
	// request statistics.
	AppInvocationBucketWidth = 5 * time.Minute
	// DefaultAppInvocationWindow is the default span a statistics query covers.
	DefaultAppInvocationWindow = 24 * time.Hour
	// DefaultAppInvocationRetention is how long persisted buckets are kept.
	DefaultAppInvocationRetention = 72 * time.Hour
	// DefaultAppInvocationFlushInterval is how often a process publishes its
	// unflushed statistics to the shared store.
	DefaultAppInvocationFlushInterval = 15 * time.Second
	// DefaultAppInvocationRecentLimit caps the recent-request list per app.
	DefaultAppInvocationRecentLimit = 32
)

// AppInvocationWriter identifies the process publishing statistics. Every
// stored row has exactly one writer, so a writer can overwrite its own rows
// with cumulative values and no two processes ever update the same row.
// BootID is unique per process start, so a restarted instance cannot overwrite
// the rows its previous process wrote.
type AppInvocationWriter struct {
	InstanceID string
	BootID     string
}

// AppInvocationBucket is one writer's request tally for one app operation in
// one time bucket.
type AppInvocationBucket struct {
	Provider    string
	Operation   string
	Start       time.Time
	Requests    int64
	Errors      int64
	DurationSum time.Duration
}

// AppInvocationOperationSummary is the fleet-wide tally of one operation.
type AppInvocationOperationSummary struct {
	Operation   string
	Requests    int64
	Errors      int64
	DurationSum time.Duration
}

// AppInvocationSummary is the fleet-wide view of one app over a window.
type AppInvocationSummary struct {
	Requests    int64
	Errors      int64
	DurationSum time.Duration
	Operations  []AppInvocationOperationSummary
	// Recent is merged across servers, newest first.
	Recent []InvocationRecord
	// Instances is how many distinct servers recorded requests in the window.
	Instances int
}

// AppInvocationStatsSink persists statistics published by one process.
type AppInvocationStatsSink interface {
	WriteBuckets(ctx context.Context, writer AppInvocationWriter, buckets []AppInvocationBucket) error
	WriteRecent(ctx context.Context, writer AppInvocationWriter, provider string, records []InvocationRecord) error
	PruneBefore(ctx context.Context, cutoff time.Time) error
}

// AppInvocationStatsSource reads statistics published by every process.
type AppInvocationStatsSource interface {
	AppInvocationStats(ctx context.Context, provider string, since time.Time, recentLimit int) (AppInvocationSummary, error)
}

type appInvocationBucketKey struct {
	provider  string
	operation string
	start     int64
}

type accumulatedBucket struct {
	bucket  AppInvocationBucket
	version uint64
	flushed uint64
}

// AppInvocationAccumulator tallies this process's completed invocations into
// time buckets by completion time, so a long-running invocation never reopens
// a bucket that was already flushed and evicted. It is the in-memory half of fleet statistics: a flusher
// publishes the buckets that changed to the shared store.
type AppInvocationAccumulator struct {
	mu          sync.Mutex
	width       time.Duration
	buckets     map[appInvocationBucketKey]*accumulatedBucket
	recentDirty map[string]uint64
	recentSeq   uint64
	now         func() time.Time
}

func NewAppInvocationAccumulator() *AppInvocationAccumulator {
	return NewAppInvocationAccumulatorWithClock(time.Now)
}

// NewAppInvocationAccumulatorWithClock builds an accumulator that reads the
// current time from now.
func NewAppInvocationAccumulatorWithClock(now func() time.Time) *AppInvocationAccumulator {
	return &AppInvocationAccumulator{
		width:       AppInvocationBucketWidth,
		buckets:     make(map[appInvocationBucketKey]*accumulatedBucket),
		recentDirty: make(map[string]uint64),
		now:         now,
	}
}

func (a *AppInvocationAccumulator) RecordInvocation(record InvocationRecord) {
	if a == nil {
		return
	}
	provider := strings.TrimSpace(record.Provider)
	operation := strings.TrimSpace(record.Operation)
	start := a.now().UTC().Truncate(a.width)
	key := appInvocationBucketKey{provider: provider, operation: operation, start: start.Unix()}

	a.mu.Lock()
	defer a.mu.Unlock()
	entry := a.buckets[key]
	if entry == nil {
		entry = &accumulatedBucket{bucket: AppInvocationBucket{
			Provider:  provider,
			Operation: operation,
			Start:     start,
		}}
		a.buckets[key] = entry
	}
	entry.bucket.Requests++
	if record.Outcome == InvocationFailed {
		entry.bucket.Errors++
	}
	entry.bucket.DurationSum += record.Duration
	entry.version++
	a.recentSeq++
	a.recentDirty[provider] = a.recentSeq
}

// AppInvocationPending is a snapshot of what changed since the last
// successful flush. Committing it clears only the changes it contains, so
// invocations recorded while the flush was in flight stay pending.
type AppInvocationPending struct {
	Buckets         []AppInvocationBucket
	RecentProviders []string

	versions        map[appInvocationBucketKey]uint64
	recentSequences map[string]uint64
}

func (a *AppInvocationAccumulator) Pending() AppInvocationPending {
	pending := AppInvocationPending{
		versions:        make(map[appInvocationBucketKey]uint64),
		recentSequences: make(map[string]uint64),
	}
	if a == nil {
		return pending
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, entry := range a.buckets {
		if entry.version == entry.flushed {
			continue
		}
		pending.Buckets = append(pending.Buckets, entry.bucket)
		pending.versions[key] = entry.version
	}
	for provider, seq := range a.recentDirty {
		pending.RecentProviders = append(pending.RecentProviders, provider)
		pending.recentSequences[provider] = seq
	}
	sort.Strings(pending.RecentProviders)
	sort.Slice(pending.Buckets, func(i, j int) bool {
		x, y := pending.Buckets[i], pending.Buckets[j]
		if !x.Start.Equal(y.Start) {
			return x.Start.Before(y.Start)
		}
		if x.Provider != y.Provider {
			return x.Provider < y.Provider
		}
		return x.Operation < y.Operation
	})
	return pending
}

// Commit marks pending as published and drops buckets that are fully flushed
// and old enough that no new invocation can land in them.
func (a *AppInvocationAccumulator) Commit(pending AppInvocationPending) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, version := range pending.versions {
		if entry := a.buckets[key]; entry != nil && entry.flushed < version {
			entry.flushed = version
		}
	}
	for provider, seq := range pending.recentSequences {
		if a.recentDirty[provider] == seq {
			delete(a.recentDirty, provider)
		}
	}
	evictBefore := a.now().UTC().Add(-2 * a.width).Unix()
	for key, entry := range a.buckets {
		if entry.version == entry.flushed && key.start < evictBefore {
			delete(a.buckets, key)
		}
	}
}

// multiInvocationRecorder fans one completed invocation out to several recorders.
type multiInvocationRecorder []InvocationRecordRecorder

func NewMultiInvocationRecorder(recorders ...InvocationRecordRecorder) InvocationRecordRecorder {
	filtered := make(multiInvocationRecorder, 0, len(recorders))
	for _, recorder := range recorders {
		if recorder != nil {
			filtered = append(filtered, recorder)
		}
	}
	return filtered
}

func (m multiInvocationRecorder) RecordInvocation(record InvocationRecord) {
	for _, recorder := range m {
		recorder.RecordInvocation(record)
	}
}

var _ InvocationRecordRecorder = (*AppInvocationAccumulator)(nil)
