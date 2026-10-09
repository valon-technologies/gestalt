package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/observability"
)

// rowKeySeparator cannot appear in provider or operation names, so composite
// row ids stay unambiguous.
const rowKeySeparator = "\x1f"

// AppInvocationStatsService stores per-process request statistics in the
// shared datastore and answers fleet-wide queries over them.
type AppInvocationStatsService struct {
	db      indexeddb.IndexedDB
	buckets idb.ObjectStore
	recents idb.ObjectStore
}

func NewAppInvocationStatsService(ds indexeddb.IndexedDB) *AppInvocationStatsService {
	return &AppInvocationStatsService{
		db:      ds,
		buckets: ds.ObjectStore(StoreAppInvocationBuckets),
		recents: ds.ObjectStore(StoreAppRecentInvocations),
	}
}

var (
	_ observability.AppInvocationStatsSink   = (*AppInvocationStatsService)(nil)
	_ observability.AppInvocationStatsSource = (*AppInvocationStatsService)(nil)
)

func validWriter(writer observability.AppInvocationWriter) error {
	if strings.TrimSpace(writer.InstanceID) == "" || strings.TrimSpace(writer.BootID) == "" {
		return fmt.Errorf("instance id and boot id are required")
	}
	return nil
}

// WriteBuckets replaces the writer's rows for the given buckets. Values are
// cumulative for the writer, so a repeated write is idempotent.
func (s *AppInvocationStatsService) WriteBuckets(
	ctx context.Context,
	writer observability.AppInvocationWriter,
	buckets []observability.AppInvocationBucket,
) error {
	if s == nil {
		return fmt.Errorf("write app invocation buckets: service is not configured")
	}
	if err := validWriter(writer); err != nil {
		return fmt.Errorf("write app invocation buckets: %w", err)
	}
	if len(buckets) == 0 {
		return nil
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreAppInvocationBuckets},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return fmt.Errorf("write app invocation buckets: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreAppInvocationBuckets)
	for _, bucket := range buckets {
		provider := strings.TrimSpace(bucket.Provider)
		if provider == "" {
			continue
		}
		start := bucket.Start.UTC().Truncate(observability.AppInvocationBucketWidth)
		rec := idb.Record{
			"id": strings.Join([]string{
				writer.InstanceID, writer.BootID, provider, bucket.Operation, fmt.Sprint(start.Unix()),
			}, rowKeySeparator),
			"instance_id":  writer.InstanceID,
			"boot_id":      writer.BootID,
			"provider":     provider,
			"operation":    bucket.Operation,
			"bucket_start": start,
			"requests":     bucket.Requests,
			"errors":       bucket.Errors,
			"duration_ns":  int64(bucket.DurationSum),
			"updated_at":   now,
		}
		if err := store.Put(ctx, rec); err != nil {
			return fmt.Errorf("write app invocation buckets: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("write app invocation buckets: commit: %w", err)
	}
	committed = true
	return nil
}

type recentInvocationJSON struct {
	ID        uint64    `json:"id"`
	Operation string    `json:"operation"`
	Outcome   string    `json:"outcome"`
	Status    int       `json:"status"`
	DurationN int64     `json:"duration_ns"`
	Timestamp time.Time `json:"timestamp"`
}

// WriteRecent replaces the writer's recent-invocation list for one app.
func (s *AppInvocationStatsService) WriteRecent(
	ctx context.Context,
	writer observability.AppInvocationWriter,
	provider string,
	records []observability.InvocationRecord,
) error {
	if s == nil {
		return fmt.Errorf("write recent invocations: service is not configured")
	}
	if err := validWriter(writer); err != nil {
		return fmt.Errorf("write recent invocations: %w", err)
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("write recent invocations: provider is required")
	}
	encoded := make([]recentInvocationJSON, 0, len(records))
	for _, record := range records {
		encoded = append(encoded, recentInvocationJSON{
			ID:        record.ID,
			Operation: record.Operation,
			Outcome:   string(record.Outcome),
			Status:    record.Status,
			DurationN: int64(record.Duration),
			Timestamp: record.Timestamp.UTC(),
		})
	}
	rec := idb.Record{
		"id":          strings.Join([]string{writer.InstanceID, writer.BootID, provider}, rowKeySeparator),
		"instance_id": writer.InstanceID,
		"boot_id":     writer.BootID,
		"provider":    provider,
		"records":     jsonColumnValue(encoded),
		"updated_at":  time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.recents.Put(ctx, rec); err != nil {
		return fmt.Errorf("write recent invocations: %w", err)
	}
	return nil
}

// AppInvocationStats sums every writer's buckets for the app since the given
// time and merges their recent invocations.
func (s *AppInvocationStatsService) AppInvocationStats(
	ctx context.Context,
	provider string,
	since time.Time,
	recentLimit int,
) (observability.AppInvocationSummary, error) {
	var summary observability.AppInvocationSummary
	if s == nil {
		return summary, fmt.Errorf("app invocation stats: service is not configured")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return summary, fmt.Errorf("app invocation stats: provider is required")
	}
	since = since.UTC().Truncate(observability.AppInvocationBucketWidth)

	query := idb.Bound(
		[]any{provider, since},
		[]any{provider, indexedDBMaxTime},
		false,
		false,
	)
	recs, err := s.buckets.Index("by_provider_bucket_start").GetAll(ctx, query)
	if err != nil {
		return summary, fmt.Errorf("app invocation stats: list buckets: %w", err)
	}
	instances := map[string]struct{}{}
	byOperation := map[string]*observability.AppInvocationOperationSummary{}
	for _, rec := range recs {
		operation := recString(rec, "operation")
		row := byOperation[operation]
		if row == nil {
			row = &observability.AppInvocationOperationSummary{Operation: operation}
			byOperation[operation] = row
		}
		requests := int64(recordInt(rec, "requests"))
		errs := int64(recordInt(rec, "errors"))
		duration := time.Duration(recordInt64(rec, "duration_ns"))
		row.Requests += requests
		row.Errors += errs
		row.DurationSum += duration
		summary.Requests += requests
		summary.Errors += errs
		summary.DurationSum += duration
		instances[recString(rec, "instance_id")] = struct{}{}
	}
	summary.Instances = len(instances)
	names := make([]string, 0, len(byOperation))
	for name := range byOperation {
		names = append(names, name)
	}
	sort.Strings(names)
	summary.Operations = make([]observability.AppInvocationOperationSummary, 0, len(names))
	for _, name := range names {
		summary.Operations = append(summary.Operations, *byOperation[name])
	}

	if recentLimit > 0 {
		recentRecs, err := s.recents.Index("by_provider").GetAll(ctx, provider)
		if err != nil {
			return summary, fmt.Errorf("app invocation stats: list recent invocations: %w", err)
		}
		summary.Recent = mergeRecentInvocations(recentRecs, since, recentLimit)
	}
	return summary, nil
}

// mergeRecentInvocations merges every writer's list, newest first. Writers
// number their records independently, so ids are reassigned to stay unique.
func mergeRecentInvocations(recs []idb.Record, since time.Time, limit int) []observability.InvocationRecord {
	var merged []observability.InvocationRecord
	for _, rec := range recs {
		var items []recentInvocationJSON
		if raw := recJSON(rec, "records"); len(raw) > 0 {
			_ = json.Unmarshal(raw, &items)
		}
		provider := recString(rec, "provider")
		for _, item := range items {
			if item.Timestamp.Before(since) {
				continue
			}
			merged = append(merged, observability.InvocationRecord{
				Provider:  provider,
				Operation: item.Operation,
				Outcome:   observability.InvocationOutcome(item.Outcome),
				Status:    item.Status,
				Duration:  time.Duration(item.DurationN),
				Timestamp: item.Timestamp,
			})
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Timestamp.After(merged[j].Timestamp) })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	for i := range merged {
		merged[i].ID = uint64(i + 1)
	}
	return merged
}

// PruneBefore deletes buckets and recent lists last written before the cutoff.
func (s *AppInvocationStatsService) PruneBefore(ctx context.Context, cutoff time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("prune app invocation stats: service is not configured")
	}
	cutoff = cutoff.UTC()
	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreAppInvocationBuckets, StoreAppRecentInvocations},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return fmt.Errorf("prune app invocation stats: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	if err := pruneStore(ctx, tx.ObjectStore(StoreAppInvocationBuckets), "by_bucket_start", cutoff); err != nil {
		return fmt.Errorf("prune app invocation stats: buckets: %w", err)
	}
	if err := pruneStore(ctx, tx.ObjectStore(StoreAppRecentInvocations), "by_updated_at", cutoff); err != nil {
		return fmt.Errorf("prune app invocation stats: recent invocations: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("prune app invocation stats: commit: %w", err)
	}
	committed = true
	return nil
}

func pruneStore(ctx context.Context, store idb.TransactionObjectStore, index string, cutoff time.Time) error {
	recs, err := store.Index(index).GetAll(ctx, idb.UpperBound(cutoff, true))
	if err != nil {
		return err
	}
	for _, rec := range recs {
		id := recString(rec, "id")
		if id == "" {
			continue
		}
		if err := store.Delete(ctx, id); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return err
		}
	}
	return nil
}

func recordInt64(rec idb.Record, key string) int64 {
	switch value := rec[key].(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

// jsonColumnValue turns a Go value into the generic JSON form a TypeJSON
// column stores.
func jsonColumnValue(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []any{}
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return []any{}
	}
	return decoded
}
