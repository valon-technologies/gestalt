package server

import (
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestClassifyConnectionOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want metricutil.TerminalOutcome
	}{
		{name: "success", want: metricutil.SuccessOutcome()},
		{
			name: "invalid caller request",
			err:  errors.New("invalid JSON body"),
			want: metricutil.RejectedOutcome(metricutil.CauseCaller, connectionReasonInvalidRequest),
		},
		{
			name: "missing Gestalt configuration",
			err:  errors.New("oauth is not configured"),
			want: metricutil.FailedOutcome(metricutil.CauseGestalt, connectionReasonConfiguration),
		},
		{
			name: "upstream token exchange",
			err:  errors.New("token exchange failed"),
			want: metricutil.FailedOutcome(metricutil.CauseUpstream, connectionReasonTokenExchange),
		},
		{
			name: "provider setup",
			err:  errors.New("connection setup failed"),
			want: metricutil.FailedOutcome(metricutil.CauseProvider, connectionReasonSetup),
		},
		{
			name: "unknown",
			err:  errors.New("new failure"),
			want: metricutil.FailedOutcome(metricutil.CauseUnknown, metricutil.ReasonUnknown),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyConnectionOutcome(test.err); got != test.want {
				t.Fatalf("classifyConnectionOutcome() = %+v, want %+v", got, test.want)
			}
		})
	}
}
