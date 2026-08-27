package invocation

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/apiexec"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestClassifyOperationOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     *core.OperationResult
		err        error
		dispatched bool
		want       metricutil.TerminalOutcome
	}{
		{
			name:   "success",
			result: &core.OperationResult{Status: http.StatusOK},
			want:   successfulOperationOutcome,
		},
		{
			name: "missing result",
			want: failedOperation(metricutil.CauseGestalt, operationReasonInvalidResult),
		},
		{
			name: "authorization rejection",
			err:  fmt.Errorf("%w: slack.chat.postMessage", ErrAuthorizationDenied),
			want: rejectedOperation(metricutil.CauseCaller, operationReasonAuthorizationDenied),
		},
		{
			name: "missing connection credential",
			err:  fmt.Errorf("%w: slack", ErrNoCredential),
			want: rejectedOperation(metricutil.CauseConnection, operationReasonCredentialMissing),
		},
		{
			name: "gestaltd internal failure",
			err:  fmt.Errorf("%w: lookup failed", ErrInternal),
			want: failedOperation(metricutil.CauseGestalt, operationReasonInternal),
		},
		{
			name: "unclassified pre-dispatch failure",
			err:  errors.New("catalog failed"),
			want: failedOperation(metricutil.CauseGestalt, operationReasonInternal),
		},
		{
			name:       "unclassified provider execution failure",
			err:        errors.New("execute failed"),
			dispatched: true,
			want:       failedOperation(metricutil.CauseProvider, operationReasonExecutionError),
		},
		{
			name:       "upstream timeout",
			err:        fmt.Errorf("%w: deadline exceeded", apiexec.ErrUpstreamTimedOut),
			dispatched: true,
			want:       failedOperation(metricutil.CauseUpstream, operationReasonUpstreamTimeout),
		},
		{
			name:       "upstream HTTP error",
			err:        &apiexec.UpstreamHTTPError{Status: http.StatusTooManyRequests},
			dispatched: true,
			want:       failedOperation(metricutil.CauseUpstream, operationReasonUpstreamHTTPError),
		},
		{
			name:   "unclassified 4xx result",
			result: &core.OperationResult{Status: http.StatusBadRequest},
			want:   failedOperation(metricutil.CauseUnknown, operationReasonResultError),
		},
		{
			name:   "unclassified 5xx result",
			result: &core.OperationResult{Status: http.StatusBadGateway},
			want:   failedOperation(metricutil.CauseUnknown, operationReasonResultError),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyOperationOutcome(test.result, test.err, test.dispatched); got != test.want {
				t.Fatalf("classifyOperationOutcome() = %+v, want %+v", got, test.want)
			}
		})
	}
}
