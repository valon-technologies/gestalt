package observability

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "gestaltd"

var (
	AttrAgentOperation         = attribute.Key("gestalt.agent.operation")
	AttrAgentProvider          = attribute.Key("gestalt.agent.provider")
	AttrAgentRuntimePhase      = attribute.Key("gestalt.agent.runtime.phase")
	AttrAgentRuntimeReason     = attribute.Key("gestalt.agent.runtime.reason")
	AttrAgentToolSource        = attribute.Key("gestalt.agent.tool.source")
	AttrErrorType              = attribute.Key("error.type")
	AttrGenAIOperationName     = attribute.Key("gen_ai.operation.name")
	AttrGenAIProviderName      = attribute.Key("gen_ai.provider.name")
	AttrGenAIAgentName         = attribute.Key("gen_ai.agent.name")
	AttrGenAIConversationID    = attribute.Key("gen_ai.conversation.id")
	AttrGenAIToolName          = attribute.Key("gen_ai.tool.name")
	AttrGenAIToolCallID        = attribute.Key("gen_ai.tool.call.id")
	AttrGenAIToolType          = attribute.Key("gen_ai.tool.type")
	AttrAuthorizationOperation = attribute.Key("gestalt.authorization.operation")
	AttrAuthorizationProvider  = attribute.Key("gestalt.authorization.provider")
	AttrAuthorizationScope     = attribute.Key("gestalt.authorization.scope")
	AttrCredentialProvider     = attribute.Key("gestalt.credential.provider")
	AttrCredentialOperation    = attribute.Key("gestalt.credential.operation")
	AttrCatalogSource          = attribute.Key("gestalt.catalog.source")
	AttrMCPCatalogCacheResult  = attribute.Key("gestalt.mcp.catalog.cache.result")
)

type metricSet struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

type agentRuntimeInstanceMetricSet struct {
	ready    metric.Int64Gauge
	starting metric.Int64Gauge
	draining metric.Int64Gauge
}

type countMetricSet struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
}

var (
	agentOperationMetrics                metricutil.MeterCache[metricSet]
	agentProviderOperationMetrics        metricutil.MeterCache[metricSet]
	agentRuntimeInstanceMetrics          metricutil.MeterCache[agentRuntimeInstanceMetricSet]
	agentRuntimeStartMetrics             metricutil.MeterCache[metricSet]
	agentRuntimeHealthCheckMetrics       metricutil.MeterCache[metricSet]
	agentRuntimeReplacementMetrics       metricutil.MeterCache[countMetricSet]
	agentToolResolveMetrics              metricutil.MeterCache[metricSet]
	genAIClientOperationDurationMetrics  metricutil.MeterCache[metric.Float64Histogram]
	authorizationProviderOperationMetric metricutil.MeterCache[metricSet]
	authorizationProviderEvaluateMetrics metricutil.MeterCache[metricSet]
	catalogOperationResolveMetrics       metricutil.MeterCache[metricSet]
	credentialProviderOperationMetrics   metricutil.MeterCache[metricSet]
	mcpCatalogCacheHitMetrics            metricutil.MeterCache[countMetricSet]
	mcpCatalogCacheMissMetrics           metricutil.MeterCache[countMetricSet]
	mcpCatalogDiscoverMetrics            metricutil.MeterCache[metricSet]
)

var genAIClientOperationDurationBuckets = []float64{0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return otel.Tracer(tracerName).Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

func EndSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func SetSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	if len(attrs) == 0 {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

func RecordAgentOperation(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentOperationMetrics, "gestaltd.agent.operation", "gestaltd agent operations", startedAt, failed, attrs...)
}

func RecordAgentProviderOperation(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentProviderOperationMetrics, "gestaltd.agent.provider.operation", "gestaltd agent provider operations", startedAt, failed, attrs...)
}

func RecordAgentRuntimeInstances(ctx context.Context, ready, starting, draining int64, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := agentRuntimeInstanceMetrics.Load(ctx, tracerName, func(meter metric.Meter) agentRuntimeInstanceMetricSet {
		return agentRuntimeInstanceMetricSet{
			ready: metricutil.NewInt64Gauge(
				meter,
				"gestaltd.agent.runtime.ready_instances",
				"Records ready gestaltd agent runtime instances.",
			),
			starting: metricutil.NewInt64Gauge(
				meter,
				"gestaltd.agent.runtime.starting_instances",
				"Records starting gestaltd agent runtime instances.",
			),
			draining: metricutil.NewInt64Gauge(
				meter,
				"gestaltd.agent.runtime.draining_instances",
				"Records draining gestaltd agent runtime instances.",
			),
		}
	})
	opts := metric.WithAttributes(attrs...)
	metrics.ready.Record(ctx, ready, opts)
	metrics.starting.Record(ctx, starting, opts)
	metrics.draining.Record(ctx, draining, opts)
}

func RecordAgentRuntimeStart(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentRuntimeStartMetrics, "gestaltd.agent.runtime.start", "gestaltd agent runtime starts", startedAt, failed, attrs...)
}

func RecordAgentRuntimeHealthCheck(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentRuntimeHealthCheckMetrics, "gestaltd.agent.runtime.health_check", "gestaltd agent runtime health checks", startedAt, failed, attrs...)
}

func RecordAgentRuntimeReplacement(ctx context.Context, failed bool, attrs ...attribute.KeyValue) {
	recordCount(ctx, &agentRuntimeReplacementMetrics, "gestaltd.agent.runtime.replacement", "gestaltd agent runtime replacements", failed, attrs...)
}

func RecordAgentToolResolve(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentToolResolveMetrics, "gestaltd.agent.tool.resolve", "gestaltd agent tool resolution", startedAt, failed, attrs...)
}

func RecordGenAIClientOperationDuration(ctx context.Context, startedAt time.Time, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	duration := genAIClientOperationDurationMetrics.Load(ctx, tracerName, func(meter metric.Meter) metric.Float64Histogram {
		return metricutil.NewFloat64Histogram(
			meter,
			"gen_ai.client.operation.duration",
			"GenAI operation duration.",
			"s",
			metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
		)
	})
	duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
}

func RecordAuthorizationProviderOperation(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &authorizationProviderOperationMetric, "gestaltd.authorization.provider.operation", "gestaltd authorization provider operations", startedAt, failed, attrs...)
}

func RecordAuthorizationProviderEvaluate(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &authorizationProviderEvaluateMetrics, "gestaltd.authorization.provider.evaluate", "gestaltd provider-backed authorization evaluations", startedAt, failed, attrs...)
}

func RecordCatalogOperationResolve(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &catalogOperationResolveMetrics, "gestaltd.catalog.operation.resolve", "gestaltd catalog operation resolution", startedAt, failed, attrs...)
}

func RecordCredentialProviderOperation(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &credentialProviderOperationMetrics, "gestaltd.credential.provider.operation", "gestaltd credential provider operations", startedAt, failed, attrs...)
}

func RecordMCPCatalogCacheHit(ctx context.Context, attrs ...attribute.KeyValue) {
	recordCount(ctx, &mcpCatalogCacheHitMetrics, "gestaltd.mcp.catalog.cache.hit", "gestaltd MCP catalog cache hits", false, attrs...)
}

func RecordMCPCatalogCacheMiss(ctx context.Context, attrs ...attribute.KeyValue) {
	recordCount(ctx, &mcpCatalogCacheMissMetrics, "gestaltd.mcp.catalog.cache.miss", "gestaltd MCP catalog cache misses", false, attrs...)
}

func RecordMCPCatalogDiscover(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &mcpCatalogDiscoverMetrics, "gestaltd.mcp.catalog.discover", "gestaltd MCP catalog discoveries", startedAt, failed, attrs...)
}

func record(ctx context.Context, cache *metricutil.MeterCache[metricSet], prefix, desc string, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := cache.Load(ctx, tracerName, func(meter metric.Meter) metricSet {
		return metricSet{
			count: metricutil.NewInt64Counter(
				meter,
				prefix+".count",
				"Counts "+desc+".",
			),
			errorCount: metricutil.NewInt64Counter(
				meter,
				prefix+".error_count",
				"Counts failed "+desc+".",
			),
			duration: metricutil.NewFloat64Histogram(
				meter,
				prefix+".duration",
				"Measures "+desc+" duration.",
				"s",
			),
		}
	})
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func recordCount(ctx context.Context, cache *metricutil.MeterCache[countMetricSet], prefix, desc string, failed bool, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics := cache.Load(ctx, tracerName, func(meter metric.Meter) countMetricSet {
		return countMetricSet{
			count: metricutil.NewInt64Counter(
				meter,
				prefix+".count",
				"Counts "+desc+".",
			),
			errorCount: metricutil.NewInt64Counter(
				meter,
				prefix+".error_count",
				"Counts failed "+desc+".",
			),
		}
	})
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
