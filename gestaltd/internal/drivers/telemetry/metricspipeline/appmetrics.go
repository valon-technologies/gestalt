package metricspipeline

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
	"github.com/valon-technologies/gestalt/server/core"
)

const (
	latencyLowestMicros  = int64(1)
	latencyHighestMicros = int64((10 * time.Minute) / time.Microsecond)
	latencySigFigs       = 2
)

type operationStore struct {
	mu             sync.RWMutex
	bucketInterval time.Duration
	window         time.Duration
	latest         time.Time
	buckets        map[time.Time]*operationBucket
}

type operationBucket struct {
	total      operationAggregate
	providers  map[string]*operationAggregate
	operations map[string]*operationAggregate
}

type operationAggregate struct {
	Provider  string
	Operation string
	Requests  int64
	Errors    int64
	Latency   latencySummary
}

type latencySummary struct {
	Count     int64
	Sum       time.Duration
	Histogram *hdrhistogram.Histogram
}

func newOperationStore() *operationStore {
	return &operationStore{
		bucketInterval: defaultDashboardInterval,
		window:         defaultDashboardWindow,
		buckets:        map[time.Time]*operationBucket{},
	}
}

func (s *operationStore) RecordOperationMetric(record core.OperationMetricRecord) {
	record = normalizeRecord(record)
	start := record.RecordedAt.Truncate(s.bucketInterval)

	s.mu.Lock()
	defer s.mu.Unlock()

	bucket := bucketForTime(s.buckets, start)
	bucket.observe(record)
	if record.RecordedAt.After(s.latest) {
		s.latest = record.RecordedAt
	}
	s.pruneLocked(s.latest)
}

func (s *operationStore) Overview(_ context.Context) (*core.OperationMetricsOverview, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)

	overview := &core.OperationMetricsOverview{
		Enabled:       true,
		GeneratedAt:   now,
		WindowSeconds: int64(s.window / time.Second),
		BucketSeconds: int64(s.bucketInterval / time.Second),
	}
	if len(s.buckets) == 0 {
		return overview, nil
	}

	starts := slices.Collect(maps.Keys(s.buckets))
	slices.SortFunc(starts, func(a, b time.Time) int { return a.Compare(b) })
	coveredWindow := coveredWindow(starts, s.bucketInterval)

	total := &operationAggregate{}
	providers := map[string]*operationAggregate{}
	operations := map[string]*operationAggregate{}
	series := make([]core.OperationMetricsSeriesItem, 0, len(starts))

	for _, start := range starts {
		bucket := s.buckets[start]
		total.merge(&bucket.total)
		mergeBreakdowns(providers, bucket.providers)
		mergeBreakdowns(operations, bucket.operations)
		series = append(series, core.OperationMetricsSeriesItem{
			Start:         start,
			Requests:      bucket.total.Requests,
			Errors:        bucket.total.Errors,
			ErrorRate:     rate(bucket.total.Errors, bucket.total.Requests),
			P95LatencyMs:  bucket.total.Latency.quantileMilliseconds(0.95),
			ThroughputRPS: perSecond(bucket.total.Requests, s.bucketInterval),
		})
	}

	overview.Summary = core.OperationMetricsSummary{
		Requests:      total.Requests,
		Errors:        total.Errors,
		ErrorRate:     rate(total.Errors, total.Requests),
		AvgLatencyMs:  total.Latency.averageMilliseconds(),
		P95LatencyMs:  total.Latency.quantileMilliseconds(0.95),
		ThroughputRPS: perSecond(total.Requests, coveredWindow),
	}
	overview.Series = series
	overview.Providers = summarizeBreakdowns(providers, defaultDashboardTopN, coveredWindow)
	overview.Operations = summarizeBreakdowns(operations, defaultDashboardTopN, coveredWindow)
	return overview, nil
}

func (s *operationStore) pruneLocked(reference time.Time) {
	if reference.IsZero() {
		return
	}
	cutoff := reference.Truncate(s.bucketInterval).Add(s.bucketInterval - s.window)
	for start := range s.buckets {
		if start.Before(cutoff) {
			delete(s.buckets, start)
		}
	}
}

func (b *operationBucket) observe(record core.OperationMetricRecord) {
	b.total.observe(record)
	breakdownFor(b.providers, record.Provider, "").observe(record)
	breakdownFor(b.operations, record.Provider, record.Operation).observe(record)
}

func (a *operationAggregate) observe(record core.OperationMetricRecord) {
	a.Requests++
	if record.Failed {
		a.Errors++
	}
	a.Latency.observe(record.Duration)
}

func (a *operationAggregate) merge(other *operationAggregate) {
	a.Requests += other.Requests
	a.Errors += other.Errors
	a.Latency.merge(other.Latency)
	if a.Provider == "" {
		a.Provider = other.Provider
	}
	if a.Operation == "" {
		a.Operation = other.Operation
	}
}

func (s *latencySummary) observe(duration time.Duration) {
	if duration < 0 {
		duration = 0
	}
	s.Count++
	s.Sum += duration
	if s.Histogram == nil {
		s.Histogram = hdrhistogram.New(latencyLowestMicros, latencyHighestMicros, latencySigFigs)
	}
	micros := int64(duration / time.Microsecond)
	if micros < latencyLowestMicros {
		micros = latencyLowestMicros
	}
	if micros > latencyHighestMicros {
		micros = latencyHighestMicros
	}
	_ = s.Histogram.RecordValue(micros)
}

func (s *latencySummary) merge(other latencySummary) {
	s.Count += other.Count
	s.Sum += other.Sum
	if other.Histogram == nil {
		return
	}
	if s.Histogram == nil {
		s.Histogram = hdrhistogram.New(latencyLowestMicros, latencyHighestMicros, latencySigFigs)
	}
	s.Histogram.Merge(other.Histogram)
}

func (s latencySummary) averageMilliseconds() float64 {
	if s.Count == 0 {
		return 0
	}
	return float64(s.Sum) / float64(time.Millisecond) / float64(s.Count)
}

func (s latencySummary) quantileMilliseconds(q float64) float64 {
	if s.Count == 0 || s.Histogram == nil {
		return 0
	}
	if q <= 0 {
		return 0
	}
	if q > 1 {
		q = 1
	}
	return float64(s.Histogram.ValueAtPercentile(q*100)) / 1000
}

func bucketForTime(items map[time.Time]*operationBucket, start time.Time) *operationBucket {
	item := items[start]
	if item == nil {
		item = &operationBucket{
			providers:  map[string]*operationAggregate{},
			operations: map[string]*operationAggregate{},
		}
		items[start] = item
	}
	return item
}

func breakdownFor(items map[string]*operationAggregate, provider, operation string) *operationAggregate {
	key := provider
	if operation != "" {
		key += "\x00" + operation
	}
	item := items[key]
	if item == nil {
		item = &operationAggregate{Provider: provider, Operation: operation}
		items[key] = item
	}
	return item
}

func mergeBreakdowns(dst, src map[string]*operationAggregate) {
	for _, item := range src {
		breakdownFor(dst, item.Provider, item.Operation).merge(item)
	}
}

func summarizeBreakdowns(items map[string]*operationAggregate, topN int, window time.Duration) []core.OperationMetricsBreakdown {
	rows := make([]core.OperationMetricsBreakdown, 0, len(items))
	for _, item := range items {
		rows = append(rows, core.OperationMetricsBreakdown{
			Provider:      item.Provider,
			Operation:     item.Operation,
			Requests:      item.Requests,
			Errors:        item.Errors,
			ErrorRate:     rate(item.Errors, item.Requests),
			AvgLatencyMs:  item.Latency.averageMilliseconds(),
			P95LatencyMs:  item.Latency.quantileMilliseconds(0.95),
			ThroughputRPS: perSecond(item.Requests, window),
		})
	}

	slices.SortFunc(rows, func(a, b core.OperationMetricsBreakdown) int {
		return cmp.Or(
			cmp.Compare(b.Requests, a.Requests),
			cmp.Compare(b.Errors, a.Errors),
			cmp.Compare(a.Provider, b.Provider),
			cmp.Compare(a.Operation, b.Operation),
		)
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

func coveredWindow(starts []time.Time, bucketInterval time.Duration) time.Duration {
	if len(starts) == 0 || bucketInterval <= 0 {
		return 0
	}
	return starts[len(starts)-1].Sub(starts[0]) + bucketInterval
}

func normalizeRecord(record core.OperationMetricRecord) core.OperationMetricRecord {
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	record.RecordedAt = record.RecordedAt.UTC()
	record.Provider = metricLabel(record.Provider)
	record.Operation = metricLabel(record.Operation)
	if record.Duration < 0 {
		record.Duration = 0
	}
	return record
}

func metricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func rate(errors, requests int64) float64 {
	if requests == 0 {
		return 0
	}
	return float64(errors) / float64(requests)
}

func perSecond(count int64, window time.Duration) float64 {
	if count == 0 || window <= 0 {
		return 0
	}
	return float64(count) / window.Seconds()
}
