package observability_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/services/observability"
)

type recordingSink struct {
	mu          sync.Mutex
	buckets     map[string]observability.AppInvocationBucket
	recent      map[string][]observability.InvocationRecord
	pruned      []time.Time
	failBuckets error
}

func newRecordingSink() *recordingSink {
	return &recordingSink{
		buckets: map[string]observability.AppInvocationBucket{},
		recent:  map[string][]observability.InvocationRecord{},
	}
}

func (s *recordingSink) WriteBuckets(_ context.Context, _ observability.AppInvocationWriter, buckets []observability.AppInvocationBucket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failBuckets != nil {
		return s.failBuckets
	}
	for _, bucket := range buckets {
		s.buckets[bucket.Provider+"/"+bucket.Operation+"/"+bucket.Start.String()] = bucket
	}
	return nil
}

func (s *recordingSink) WriteRecent(_ context.Context, _ observability.AppInvocationWriter, provider string, records []observability.InvocationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent[provider] = records
	return nil
}

func (s *recordingSink) PruneBefore(_ context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruned = append(s.pruned, cutoff)
	return nil
}

func (s *recordingSink) total() (requests, errs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, bucket := range s.buckets {
		requests += bucket.Requests
		errs += bucket.Errors
	}
	return requests, errs
}

func record(provider, operation string, outcome observability.InvocationOutcome, at time.Time) observability.InvocationRecord {
	return observability.InvocationRecord{
		Provider:  provider,
		Operation: operation,
		Outcome:   outcome,
		Status:    200,
		Duration:  10 * time.Millisecond,
		Timestamp: at,
	}
}

func newFlusher(acc *observability.AppInvocationAccumulator, recent observability.InvocationRecordReader, sink *recordingSink) *observability.AppInvocationFlusher {
	return &observability.AppInvocationFlusher{
		Accumulator: acc,
		Recent:      recent,
		Sink:        sink,
		Writer:      observability.AppInvocationWriter{InstanceID: "replica-a", BootID: "boot-1"},
	}
}

func TestFlusherPublishesCumulativeBucketsAndRecentList(t *testing.T) {
	t.Parallel()

	acc := observability.NewAppInvocationAccumulator()
	store := observability.NewInvocationRecordStore(0)
	recorder := observability.NewMultiInvocationRecorder(store, acc)
	sink := newRecordingSink()
	flusher := newFlusher(acc, store, sink)
	now := time.Now().UTC()

	recorder.RecordInvocation(record("billing", "list", observability.InvocationPassed, now))
	recorder.RecordInvocation(record("billing", "list", observability.InvocationFailed, now))
	if err := flusher.FlushOnce(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if requests, errs := sink.total(); requests != 2 || errs != 1 {
		t.Fatalf("after first flush requests=%d errors=%d, want 2 and 1", requests, errs)
	}
	if got := len(sink.recent["billing"]); got != 2 {
		t.Fatalf("recent = %d records, want 2", got)
	}

	recorder.RecordInvocation(record("billing", "list", observability.InvocationPassed, now))
	if err := flusher.FlushOnce(context.Background()); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	// The row holds the cumulative tally, not a delta, so a rewrite is
	// idempotent and never double counts.
	if requests, _ := sink.total(); requests != 3 {
		t.Fatalf("after second flush requests=%d, want 3", requests)
	}
}

func TestFlusherFailedFlushStaysPendingAndRepairsOnNextPass(t *testing.T) {
	t.Parallel()

	acc := observability.NewAppInvocationAccumulator()
	sink := newRecordingSink()
	flusher := newFlusher(acc, nil, sink)
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, time.Now()))

	sink.failBuckets = errors.New("datastore down")
	if err := flusher.FlushOnce(context.Background()); err == nil {
		t.Fatal("expected the failed write to surface")
	}
	if requests, _ := sink.total(); requests != 0 {
		t.Fatalf("requests = %d after failed flush, want 0", requests)
	}

	sink.failBuckets = nil
	if err := flusher.FlushOnce(context.Background()); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if requests, _ := sink.total(); requests != 1 {
		t.Fatalf("requests = %d after retry, want 1", requests)
	}
}

func TestAccumulatorKeepsInvocationsRecordedDuringAFlush(t *testing.T) {
	t.Parallel()

	acc := observability.NewAppInvocationAccumulator()
	now := time.Now()
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, now))
	pending := acc.Pending()
	// Arrives while the flush is in flight.
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, now))
	acc.Commit(pending)

	again := acc.Pending()
	if len(again.Buckets) != 1 || again.Buckets[0].Requests != 2 {
		t.Fatalf("pending after commit = %+v, want the bucket with 2 requests still pending", again.Buckets)
	}
	acc.Commit(again)
	if left := acc.Pending(); len(left.Buckets) != 0 || len(left.RecentProviders) != 0 {
		t.Fatalf("pending after second commit = %+v, want empty", left)
	}
}

func TestAccumulatorSeparatesBucketsAndAppsAndOperations(t *testing.T) {
	t.Parallel()

	clock := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	acc := observability.NewAppInvocationAccumulatorWithClock(func() time.Time { return clock })
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, clock))
	acc.RecordInvocation(record("billing", "get", observability.InvocationPassed, clock))
	acc.RecordInvocation(record("other", "list", observability.InvocationPassed, clock))
	clock = clock.Add(observability.AppInvocationBucketWidth)
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, clock))

	if got := len(acc.Pending().Buckets); got != 4 {
		t.Fatalf("pending buckets = %d, want one per app, operation and time bucket", got)
	}
}

func TestAccumulatorCountsALongRunningInvocationWhereItCompletes(t *testing.T) {
	t.Parallel()

	clock := time.Now().UTC().Truncate(observability.AppInvocationBucketWidth)
	acc := observability.NewAppInvocationAccumulatorWithClock(func() time.Time { return clock })
	started := clock
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, started))
	acc.Commit(acc.Pending())

	// Much later, an invocation that started in the old bucket completes. It
	// must not reopen that bucket and overwrite the flushed tally with 1.
	clock = clock.Add(time.Hour)
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, started))
	pending := acc.Pending()
	if len(pending.Buckets) != 1 || !pending.Buckets[0].Start.Equal(clock) {
		t.Fatalf("pending = %+v, want one bucket at the completion time", pending.Buckets)
	}
}

func TestFlusherStopPublishesWhatIsStillPending(t *testing.T) {
	t.Parallel()

	acc := observability.NewAppInvocationAccumulator()
	sink := newRecordingSink()
	flusher := newFlusher(acc, nil, sink)
	flusher.Interval = time.Hour
	flusher.Start(context.Background())
	acc.RecordInvocation(record("billing", "list", observability.InvocationPassed, time.Now()))

	flusher.Stop()
	if requests, _ := sink.total(); requests != 1 {
		t.Fatalf("requests = %d after Stop, want the final flush to publish 1", requests)
	}
}
