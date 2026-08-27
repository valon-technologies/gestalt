package invocation

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type operationObservation struct {
	startedAt      time.Time
	requestedApp   string
	requestedOp    string
	provider       string
	operation      string
	transport      string
	connectionMode string
	dispatched     bool
}

func newOperationObservation(provider, operation string) *operationObservation {
	return &operationObservation{
		startedAt:      time.Now(),
		requestedApp:   strings.TrimSpace(provider),
		requestedOp:    strings.TrimSpace(operation),
		provider:       metricutil.UnknownAttrValue,
		operation:      metricutil.UnknownAttrValue,
		transport:      metricutil.UnknownAttrValue,
		connectionMode: metricutil.UnknownAttrValue,
	}
}

// observeOperation is the single completion point for unary invocations and
// dispatch failures. Metrics, logs, and traces all use the same classification.
func (b *Broker) observeOperation(
	ctx context.Context,
	span trace.Span,
	p *principal.Principal,
	observation *operationObservation,
	result *core.OperationResult,
	err error,
) {
	if observation == nil {
		return
	}
	resultStatus := operationResultStatus(result, err)
	outcome := classifyOperationOutcome(result, err, observation.dispatched)
	recordOperationMetrics(
		ctx,
		observation.startedAt,
		observation.provider,
		observation.operation,
		observation.transport,
		observation.connectionMode,
		resultStatus,
		outcome,
	)

	status, statusClass := resultStatusAttributes(resultStatus)
	span.SetAttributes(
		metricutil.AttrOutcome.String(outcome.Status),
		metricutil.AttrFailureCause.String(outcome.Cause),
		metricutil.AttrFailureReason.String(outcome.Reason),
		metricutil.AttrResultStatus.String(status),
		metricutil.AttrResultStatusClass.String(statusClass),
	)
	if outcome.Status == metricutil.OutcomeFailed {
		if err != nil {
			span.RecordError(err)
		}
		span.SetStatus(codes.Error, outcome.Reason)
	}

	if !outcome.Unsuccessful() {
		return
	}
	// Rejections are already captured by the audit sink. Emit a separate
	// diagnostic only for admitted failures, avoiding duplicate high-volume
	// logs for expected caller and connection rejections.
	if outcome.Status == metricutil.OutcomeRejected {
		return
	}

	attrs := []any{
		"provider", observedProviderName(observation),
		"operation", observedOperationName(observation),
		"transport", observation.transport,
		"connection_mode", observation.connectionMode,
		"outcome", outcome.Status,
		"failure_cause", outcome.Cause,
		"failure_reason", outcome.Reason,
		"result_status", resultStatus,
		"result_status_class", statusClass,
	}
	if meta := MetaFromContext(ctx); meta != nil {
		attrs = append(attrs, "request_id", strings.TrimSpace(meta.RequestID), "invocation_depth", meta.Depth)
	}
	if caller := CallerProviderFromContext(ctx); caller.Name != "" {
		attrs = append(attrs, "caller_kind", string(caller.Kind), "caller_name", caller.Name)
	}
	if surface := InvocationSurfaceFromContext(ctx); surface != "" {
		attrs = append(attrs, "surface", string(surface))
	}
	if binding := HTTPBindingFromContext(ctx); binding != "" {
		attrs = append(attrs, "http_binding", binding)
	}
	if subjectID := resultSubjectID(p); subjectID != "" {
		attrs = append(attrs, "subject_id", subjectID)
	}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
	// Preserve the existing diagnostic body for executable-app 5xx results.
	// Other bodies are intentionally omitted because they may contain user or
	// upstream data and are not needed to classify the failure.
	if result != nil && observation.transport == catalog.TransportApp && result.Status >= http.StatusInternalServerError {
		attrs = append(attrs, "result_body", truncateResultBodyForLog(result.Body))
	}

	b.log().WarnContext(ctx, "gestaltd operation completed", attrs...)
}

func observedProviderName(observation *operationObservation) string {
	if observation.provider != metricutil.UnknownAttrValue {
		return observation.provider
	}
	return observation.requestedApp
}

func observedOperationName(observation *operationObservation) string {
	if observation.operation != metricutil.UnknownAttrValue {
		return observation.operation
	}
	return observation.requestedOp
}

func truncateResultBodyForLog(body []byte) string {
	if len(body) <= resultBodyLogLimit {
		return string(body)
	}
	return string(body[:resultBodyLogLimit])
}

func resultSubjectID(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.SubjectID)
}
