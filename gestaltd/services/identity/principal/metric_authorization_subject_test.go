package principal

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestMetricAuthorizationSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		p        *Principal
		wantKind string
		wantID   string
	}{
		{
			name: "user email",
			p: &Principal{
				SubjectID: "user:u-1",
				Kind:      KindUser,
				Identity:  &core.UserIdentity{Email: "User@Example.com"},
			},
			wantKind: "user",
			wantID:   "user@example.com",
		},
		{
			name: "service account",
			p: &Principal{
				SubjectID: "service_account:ingress-verify-probe",
			},
			wantKind: "service_account",
			wantID:   "ingress-verify-probe",
		},
		{
			name:     "nil principal",
			p:        nil,
			wantKind: metricutil.UnknownAttrValue,
			wantID:   metricutil.UnknownAttrValue,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotKind, gotID := MetricAuthorizationSubject(tc.p)
			if gotKind != tc.wantKind || gotID != tc.wantID {
				t.Fatalf("MetricAuthorizationSubject() = (%q, %q), want (%q, %q)", gotKind, gotID, tc.wantKind, tc.wantID)
			}
		})
	}
}
