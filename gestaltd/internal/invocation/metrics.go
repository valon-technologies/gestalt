package invocation

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type operationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

func newOperationMetrics(meter metric.Meter) operationMetrics {
	return operationMetrics{
		count: metricutil.NewInt64Counter(
			meter,
			"gestaltd.operation.count",
			"Counts gestaltd operation invocations.",
		),
		errorCount: metricutil.NewInt64Counter(
			meter,
			"gestaltd.operation.error_count",
			"Counts gestaltd operation invocations that fail.",
		),
		duration: metricutil.NewFloat64Histogram(
			meter,
			"gestaltd.operation.duration",
			"Measures gestaltd operation invocation duration.",
			"s",
		),
	}
}

var operationMetricsCache metricutil.MeterCache[operationMetrics]

func recordOperationMetrics(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	resultStatus int,
	failed bool,
) {
	metrics := operationMetricsCache.Load(ctx, tracerName, newOperationMetrics)
	resultStatusValue, resultStatusClass := resultStatusAttributes(resultStatus)
	attrs := []attribute.KeyValue{
		attrProvider.String(metricutil.AttrValue(provider)),
		attrOperation.String(metricutil.AttrValue(operation)),
		attrTransport.String(metricutil.AttrValue(transport)),
		attrConnectionMode.String(metricutil.AttrValue(connectionMode)),
		metricutil.AttrResultStatus.String(resultStatusValue),
		metricutil.AttrResultStatusClass.String(resultStatusClass),
	}
	if surface := InvocationSurfaceFromContext(ctx); surface != "" {
		attrs = append(attrs, metricutil.AttrInvocationSurface.String(metricutil.AttrValue(string(surface))))
	}
	if binding := HTTPBindingFromContext(ctx); binding != "" {
		attrs = append(attrs, metricutil.AttrHTTPBinding.String(metricutil.AttrValue(binding)))
	}
	metricutil.AddHTTPAttributes(ctx, attrs...)

	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	duration := time.Since(startedAt)
	metrics.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func operationResultStatus(result *core.OperationResult, err error) int {
	if result != nil && validHTTPStatus(result.Status) {
		return result.Status
	}
	if err == nil {
		return 0
	}

	var upstreamErr *apiexec.UpstreamHTTPError
	switch {
	case errors.Is(err, ErrProviderNotFound), errors.Is(err, ErrOperationNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrNotAuthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, ErrAuthorizationDenied), errors.Is(err, ErrScopeDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrNoCredential), errors.Is(err, ErrReconnectRequired):
		return http.StatusPreconditionFailed
	case errors.Is(err, ErrAmbiguousInstance):
		return http.StatusConflict
	case errors.Is(err, ErrUserResolution), errors.Is(err, ErrInternal):
		return http.StatusInternalServerError
	case errors.Is(err, ErrInvalidInvocation), errors.Is(err, core.ErrMCPOnly), errors.Is(err, apiexec.ErrMissingPathParam):
		return http.StatusBadRequest
	case errors.As(err, &upstreamErr) && validHTTPStatus(upstreamErr.Status):
		return upstreamErr.Status
	default:
		return http.StatusBadGateway
	}
}

func operationResultFailed(status int, err error) bool {
	return err != nil || status >= http.StatusBadRequest && status <= 599
}

func resultStatusAttributes(status int) (string, string) {
	if !validHTTPStatus(status) {
		return metricutil.UnknownAttrValue, metricutil.UnknownAttrValue
	}
	return strconv.Itoa(status), strconv.Itoa(status/100) + "xx"
}

func validHTTPStatus(status int) bool {
	return status >= 100 && status <= 599
}
