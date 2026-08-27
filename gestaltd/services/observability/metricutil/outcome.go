package metricutil

import "go.opentelemetry.io/otel/attribute"

const (
	OutcomeSuccess  = "success"
	OutcomeRejected = "rejected"
	OutcomeFailed   = "failed"

	CauseNone       = "none"
	CauseCaller     = "caller"
	CauseConnection = "connection"
	CauseGestalt    = "gestalt"
	CauseProvider   = "provider"
	CauseUpstream   = "upstream"
	CauseUnknown    = "unknown"

	ReasonNone    = "none"
	ReasonUnknown = "unknown"
)

// TerminalOutcome is the bounded result shared by gestaltd metrics, traces,
// and audit logs. Product-specific classifiers own the reason vocabulary.
type TerminalOutcome struct {
	Status string
	Cause  string
	Reason string
}

func SuccessOutcome() TerminalOutcome {
	return TerminalOutcome{Status: OutcomeSuccess, Cause: CauseNone, Reason: ReasonNone}
}

func RejectedOutcome(cause, reason string) TerminalOutcome {
	return TerminalOutcome{Status: OutcomeRejected, Cause: cause, Reason: reason}
}

func FailedOutcome(cause, reason string) TerminalOutcome {
	return TerminalOutcome{Status: OutcomeFailed, Cause: cause, Reason: reason}
}

func (o TerminalOutcome) Unsuccessful() bool {
	return o.Status != OutcomeSuccess
}

func (o TerminalOutcome) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrOutcome.String(o.Status),
		AttrFailureCause.String(o.Cause),
		AttrFailureReason.String(o.Reason),
	}
}
