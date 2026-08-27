package invocation

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type operationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

const (
	operationReasonProviderNotFound        = "provider_not_found"
	operationReasonOperationNotFound       = "operation_not_found"
	operationReasonNotAuthenticated        = "not_authenticated"
	operationReasonAuthorizationDenied     = "authorization_denied"
	operationReasonCredentialMissing       = "credential_missing"
	operationReasonReconnectRequired       = "reconnect_required"
	operationReasonAmbiguousInstance       = "ambiguous_instance"
	operationReasonUserResolution          = "user_resolution"
	operationReasonInternal                = "internal"
	operationReasonInvalidInvocation       = "invalid_invocation"
	operationReasonStreamingUnsupported    = "streaming_unsupported"
	operationReasonMaxDepth                = "max_depth"
	operationReasonRecursiveInvocation     = "recursive_invocation"
	operationReasonRateLimited             = "rate_limited"
	operationReasonProviderScope           = "provider_scope"
	operationReasonCanceled                = "canceled"
	operationReasonUpstreamUnavailable     = "upstream_unavailable"
	operationReasonUpstreamTimeout         = "upstream_timeout"
	operationReasonUpstreamResponseRead    = "upstream_response_read"
	operationReasonUpstreamInvalidResponse = "upstream_invalid_response"
	operationReasonUpstreamHTTPError       = "upstream_http_error"
	operationReasonUpstreamOperationError  = "upstream_operation_error"
	operationReasonExecutionError          = "execution_error"
	operationReasonResultError             = "operation_result_error"
	operationReasonInvalidResult           = "invalid_result"
)

var successfulOperationOutcome = metricutil.SuccessOutcome()

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
			"Counts unsuccessful gestaltd operation invocations for compatibility.",
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
	outcome metricutil.TerminalOutcome,
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
		metricutil.AttrOutcome.String(outcome.Status),
		metricutil.AttrFailureCause.String(outcome.Cause),
		metricutil.AttrFailureReason.String(outcome.Reason),
	}
	surface := InvocationSurfaceFromContext(ctx)
	binding := HTTPBindingFromContext(ctx)
	if surface != "" {
		attrs = append(attrs, metricutil.AttrInvocationSurface.String(metricutil.AttrValue(string(surface))))
	}
	if binding != "" {
		attrs = append(attrs, metricutil.AttrHTTPBinding.String(metricutil.AttrValue(binding)))
	}
	httpDims := metricutil.HTTPMetricDims{
		ProviderName:   provider,
		OperationName:  operation,
		Transport:      transport,
		ConnectionMode: connectionMode,
	}
	if surface != "" {
		httpDims.Surface = string(surface)
	}
	if binding != "" {
		httpDims.HTTPBindingName = binding
		httpDims.Surface = metricutil.InvocationSurfaceHTTPBinding
	}
	metricutil.AddHTTPServerMetricDims(ctx, httpDims)

	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	duration := time.Since(startedAt)
	metrics.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	// Retain the old counter while dashboards migrate to filtering the canonical
	// operation count by outcome. It is deliberately derived from the same
	// classification so the two views cannot disagree.
	if outcome.Unsuccessful() {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func classifyOperationOutcome(result *core.OperationResult, err error, dispatched bool) metricutil.TerminalOutcome {
	if err == nil {
		if result == nil || !validHTTPStatus(result.Status) {
			return failedOperation(metricutil.CauseGestalt, operationReasonInvalidResult)
		}
		if result.Status < http.StatusBadRequest {
			return successfulOperationOutcome
		}
		return metricutil.TerminalOutcome{
			Status: metricutil.OutcomeFailed,
			Cause:  metricutil.CauseUnknown,
			Reason: operationReasonResultError,
		}
	}

	var upstreamHTTPError *apiexec.UpstreamHTTPError
	var upstreamOperationError *apiexec.UpstreamOperationError
	var maxDepthError *MaxDepthError
	var recursionError *RecursionError
	var rateLimitError *RateLimitError
	var scopeError *providerScopeError
	switch {
	case errors.Is(err, ErrProviderNotFound):
		return rejectedOperation(metricutil.CauseCaller, operationReasonProviderNotFound)
	case errors.Is(err, ErrOperationNotFound):
		return rejectedOperation(metricutil.CauseCaller, operationReasonOperationNotFound)
	case errors.Is(err, ErrNotAuthenticated):
		return rejectedOperation(metricutil.CauseCaller, operationReasonNotAuthenticated)
	case errors.Is(err, ErrAuthorizationDenied), errors.Is(err, ErrScopeDenied):
		return rejectedOperation(metricutil.CauseCaller, operationReasonAuthorizationDenied)
	case errors.Is(err, ErrNoCredential):
		return rejectedOperation(metricutil.CauseConnection, operationReasonCredentialMissing)
	case errors.Is(err, ErrReconnectRequired), errors.Is(err, core.ErrReconnectRequired):
		return rejectedOperation(metricutil.CauseConnection, operationReasonReconnectRequired)
	case errors.Is(err, ErrAmbiguousInstance), errors.Is(err, core.ErrAmbiguousCredential):
		return rejectedOperation(metricutil.CauseConnection, operationReasonAmbiguousInstance)
	case errors.Is(err, ErrInvalidInvocation), errors.Is(err, core.ErrMCPOnly), errors.Is(err, apiexec.ErrMissingPathParam):
		return rejectedOperation(metricutil.CauseCaller, operationReasonInvalidInvocation)
	case errors.Is(err, ErrStreamingUnsupported):
		return rejectedOperation(metricutil.CauseGestalt, operationReasonStreamingUnsupported)
	case errors.As(err, &maxDepthError):
		return rejectedOperation(metricutil.CauseCaller, operationReasonMaxDepth)
	case errors.As(err, &recursionError):
		return rejectedOperation(metricutil.CauseCaller, operationReasonRecursiveInvocation)
	case errors.As(err, &rateLimitError):
		return rejectedOperation(metricutil.CauseCaller, operationReasonRateLimited)
	case errors.As(err, &scopeError):
		return rejectedOperation(metricutil.CauseCaller, operationReasonProviderScope)
	case errors.Is(err, context.Canceled):
		return rejectedOperation(metricutil.CauseCaller, operationReasonCanceled)
	case errors.Is(err, ErrUserResolution):
		return failedOperation(metricutil.CauseGestalt, operationReasonUserResolution)
	case errors.Is(err, ErrInternal):
		return failedOperation(metricutil.CauseGestalt, operationReasonInternal)
	case errors.Is(err, apiexec.ErrUpstreamTimedOut):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamTimeout)
	case errors.Is(err, apiexec.ErrUpstreamUnavailable):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamUnavailable)
	case errors.Is(err, apiexec.ErrUpstreamResponseRead):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamResponseRead)
	case errors.Is(err, apiexec.ErrUpstreamInvalidResponse):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamInvalidResponse)
	case errors.As(err, &upstreamHTTPError):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamHTTPError)
	case errors.As(err, &upstreamOperationError):
		return failedOperation(metricutil.CauseUpstream, operationReasonUpstreamOperationError)
	case dispatched:
		return failedOperation(metricutil.CauseProvider, operationReasonExecutionError)
	default:
		return failedOperation(metricutil.CauseGestalt, operationReasonInternal)
	}
}

func rejectedOperation(cause, reason string) metricutil.TerminalOutcome {
	return metricutil.RejectedOutcome(cause, reason)
}

func failedOperation(cause, reason string) metricutil.TerminalOutcome {
	return metricutil.FailedOutcome(cause, reason)
}

func operationResultStatus(result *core.OperationResult, err error) int {
	if result != nil && validHTTPStatus(result.Status) {
		return result.Status
	}
	return OperationErrorHTTPStatus(err)
}

// OperationErrorHTTPStatus maps invocation sentinel errors to their HTTP-facing status.
func OperationErrorHTTPStatus(err error) int {
	var upstreamErr *apiexec.UpstreamHTTPError
	switch {
	case err == nil:
		return 0
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

// OperationErrorResultStatus reports invocation errors that should be returned to
// app SDK callers as operation results instead of transport errors.
func OperationErrorResultStatus(err error) (int, bool) {
	switch {
	case errors.Is(err, ErrNoCredential), errors.Is(err, ErrReconnectRequired):
		return OperationErrorHTTPStatus(err), true
	default:
		return 0, false
	}
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
