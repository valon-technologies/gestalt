package metricutil

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "gestaltd"

var (
	attrProvider       = AttrProvider
	attrAction         = attribute.Key("gestalt.action")
	attrType           = attribute.Key("gestalt.type")
	attrConnectionMode = AttrConnectionMode
)

type counterMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

func newCounterMetrics(meter metric.Meter, prefix, desc string) counterMetrics {
	return counterMetrics{
		count: NewInt64Counter(
			meter,
			prefix+".count",
			"Counts "+desc+".",
		),
		errorCount: NewInt64Counter(
			meter,
			prefix+".error_count",
			"Counts failed "+desc+".",
		),
		duration: NewFloat64Histogram(
			meter,
			prefix+".duration",
			"Measures "+desc+" duration.",
			"s",
		),
	}
}

var (
	authMetricsCache           MeterCache[counterMetrics]
	connectionAuthMetricsCache MeterCache[counterMetrics]
	discoveryMetricsCache      MeterCache[counterMetrics]
	dbClientMetricsCache       MeterCache[metric.Float64Histogram]
)

var dbClientOperationDurationBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10}

func RecordAuthMetrics(ctx context.Context, startedAt time.Time, provider string, action string, failed bool) {
	metrics := authMetricsCache.Load(ctx, meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.auth", "gestaltd auth actions")
	})
	recordCounterMetrics(ctx, metrics, startedAt, failed,
		attrProvider.String(AttrValue(provider)),
		attrAction.String(AttrValue(action)),
	)
}

func RecordConnectionAuthMetrics(ctx context.Context, startedAt time.Time, provider string, authType string, action string, connectionMode string, failed bool) {
	metrics := connectionAuthMetricsCache.Load(ctx, meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.connection.auth", "gestaltd connection auth actions")
	})
	recordCounterMetrics(ctx, metrics, startedAt, failed,
		attrProvider.String(AttrValue(provider)),
		attrType.String(AttrValue(authType)),
		attrAction.String(AttrValue(action)),
		attrConnectionMode.String(AttrValue(connectionMode)),
	)
}

func RecordDiscoveryMetrics(ctx context.Context, startedAt time.Time, provider string, action string, connectionMode string, failed bool) {
	metrics := discoveryMetricsCache.Load(ctx, meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.discovery", "gestaltd credentialed discovery actions")
	})
	recordCounterMetrics(ctx, metrics, startedAt, failed,
		attrProvider.String(AttrValue(provider)),
		attrAction.String(AttrValue(action)),
		attrConnectionMode.String(AttrValue(connectionMode)),
	)
}

type IndexedDBMetricLabels struct {
	SystemName   string
	DB           string
	ProviderName string
	ObjectStore  string
	IndexName    string
}

func RecordDBClientOperation(ctx context.Context, startedAt time.Time, dims DBMetricDims) {
	if ctx == nil {
		ctx = context.Background()
	}
	duration := dbClientMetricsCache.Load(ctx, meterName, func(meter metric.Meter) metric.Float64Histogram {
		return NewFloat64Histogram(
			meter,
			"db.client.operation.duration",
			"Measures database client operation duration.",
			"s",
			metric.WithExplicitBucketBoundaries(dbClientOperationDurationBuckets...),
		)
	})
	duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(DBClientAttrs(dims)...))
}

func RecordIndexedDBOperation(ctx context.Context, startedAt time.Time, labels IndexedDBMetricLabels, method string, err error) {
	systemName := strings.TrimSpace(labels.SystemName)
	if systemName == "" {
		systemName = DBSystemNameGestaltdIndexedDB
	}
	RecordDBClientOperation(ctx, startedAt, DBMetricDims{
		SystemName:     systemName,
		Namespace:      labels.DB,
		CollectionName: labels.ObjectStore,
		OperationName:  normalizeIndexedDBOperationName(method),
		ProviderName:   labels.ProviderName,
		IndexName:      labels.IndexName,
		ErrorType:      indexedDBErrorType(err),
	})
}

func normalizeIndexedDBOperationName(operation string) string {
	switch strings.TrimSpace(operation) {
	case "Get":
		return "get"
	case "GetKey":
		return "get_key"
	case "Add":
		return "add"
	case "Put":
		return "put"
	case "Delete":
		return "delete"
	case "Clear":
		return "clear"
	case "GetAll":
		return "get_all"
	case "GetAllKeys":
		return "get_all_keys"
	case "Count":
		return "count"
	case "DeleteRange":
		return "delete_range"
	case "OpenCursor":
		return "open_cursor"
	case "OpenKeyCursor":
		return "open_key_cursor"
	case "Index.Get":
		return "index_get"
	case "Index.GetKey":
		return "index_get_key"
	case "Index.GetAll":
		return "index_get_all"
	case "Index.GetAllKeys":
		return "index_get_all_keys"
	case "Index.Count":
		return "index_count"
	case "Index.Delete":
		return "index_delete"
	case "Index.DeleteRange":
		return "index_delete_range"
	case "Index.OpenCursor":
		return "index_open_cursor"
	case "Index.OpenKeyCursor":
		return "index_open_key_cursor"
	default:
		return strings.TrimSpace(operation)
	}
}

func indexedDBErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, indexeddb.ErrNotFound):
		return "not_found"
	case errors.Is(err, indexeddb.ErrAlreadyExists):
		return "already_exists"
	case errors.Is(err, indexeddb.ErrKeysOnly):
		return "keys_only"
	default:
		return "internal"
	}
}

func recordCounterMetrics(ctx context.Context, metrics counterMetrics, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
