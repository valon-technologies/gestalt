package server

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

const (
	connectionReasonInvalidRequest          = "invalid_request"
	connectionReasonUnsupportedAuth         = "unsupported_auth"
	connectionReasonMissingCredential       = "missing_credential"
	connectionReasonInvalidOAuthState       = "invalid_oauth_state"
	connectionReasonConfiguration           = "configuration"
	connectionReasonStateEncoding           = "state_encoding"
	connectionReasonOAuthURL                = "oauth_url"
	connectionReasonTokenExchange           = "token_exchange"
	connectionReasonInvalidUpstreamResponse = "invalid_upstream_response"
	connectionReasonSetup                   = "connection_setup"
)

func classifyConnectionOutcome(err error) metricutil.TerminalOutcome {
	if err == nil {
		return metricutil.SuccessOutcome()
	}

	switch strings.TrimSpace(err.Error()) {
	case "invalid JSON body", "integration is required", "integration not found", "invalid connection", "invalid instance", "invalid connection parameters", "missing code or state parameter":
		return metricutil.RejectedOutcome(metricutil.CauseCaller, connectionReasonInvalidRequest)
	case "integration does not support manual auth":
		return metricutil.RejectedOutcome(metricutil.CauseCaller, connectionReasonUnsupportedAuth)
	case "credential is required":
		return metricutil.RejectedOutcome(metricutil.CauseConnection, connectionReasonMissingCredential)
	case "invalid or expired oauth state":
		return metricutil.RejectedOutcome(metricutil.CauseCaller, connectionReasonInvalidOAuthState)
	case "connection is not configured", "oauth is not configured", "oauth state encryption is not configured":
		return metricutil.FailedOutcome(metricutil.CauseGestalt, connectionReasonConfiguration)
	case "failed to encode oauth state", "failed to prepare pending connection":
		return metricutil.FailedOutcome(metricutil.CauseGestalt, connectionReasonStateEncoding)
	case "failed to prepare oauth URL":
		return metricutil.FailedOutcome(metricutil.CauseGestalt, connectionReasonOAuthURL)
	case "token exchange failed":
		return metricutil.FailedOutcome(metricutil.CauseUpstream, connectionReasonTokenExchange)
	case "failed to extract connection metadata from token response":
		return metricutil.FailedOutcome(metricutil.CauseUpstream, connectionReasonInvalidUpstreamResponse)
	case "connection setup failed":
		return metricutil.FailedOutcome(metricutil.CauseProvider, connectionReasonSetup)
	default:
		return metricutil.FailedOutcome(metricutil.CauseUnknown, metricutil.ReasonUnknown)
	}
}
