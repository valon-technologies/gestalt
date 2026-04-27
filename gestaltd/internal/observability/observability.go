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
	AttrAgentToolSource        = attribute.Key("gestalt.agent.tool.source")
	AttrAuthorizationOperation = attribute.Key("gestalt.authorization.operation")
	AttrAuthorizationProvider  = attribute.Key("gestalt.authorization.provider")
	AttrAuthorizationScope     = attribute.Key("gestalt.authorization.scope")
	AttrCredentialProvider     = attribute.Key("gestalt.credential.provider")
	AttrCredentialOperation    = attribute.Key("gestalt.credential.operation")
	AttrCatalogSource          = attribute.Key("gestalt.catalog.source")
)

type metricSet struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

var (
	agentOperationMetrics                metricutil.MeterCache[metricSet]
	agentProviderOperationMetrics        metricutil.MeterCache[metricSet]
	agentToolResolveMetrics              metricutil.MeterCache[metricSet]
	agentRunMetadataWriteMetrics         metricutil.MeterCache[metricSet]
	authorizationProviderOperationMetric metricutil.MeterCache[metricSet]
	authorizationProviderEvaluateMetrics metricutil.MeterCache[metricSet]
	catalogOperationResolveMetrics       metricutil.MeterCache[metricSet]
	credentialProviderOperationMetrics   metricutil.MeterCache[metricSet]
	credentialBindingResolveMetrics      metricutil.MeterCache[metricSet]
	credentialTokenResolveMetrics        metricutil.MeterCache[metricSet]
)

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

func RecordAgentToolResolve(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentToolResolveMetrics, "gestaltd.agent.tool.resolve", "gestaltd agent tool resolution", startedAt, failed, attrs...)
}

func RecordAgentRunMetadataWrite(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &agentRunMetadataWriteMetrics, "gestaltd.agent.run_metadata.write", "gestaltd agent run metadata writes", startedAt, failed, attrs...)
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

func RecordCredentialBindingResolve(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &credentialBindingResolveMetrics, "gestaltd.credential.binding.resolve", "gestaltd credential binding resolution", startedAt, failed, attrs...)
}

func RecordCredentialTokenResolve(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &credentialTokenResolveMetrics, "gestaltd.credential.token.resolve", "gestaltd credential token resolution", startedAt, failed, attrs...)
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
