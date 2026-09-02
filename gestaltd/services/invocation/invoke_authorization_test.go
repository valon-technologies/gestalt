package invocation

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestInvokeAuthorizationSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "explicit http binding",
			ctx:  WithInvocationSurface(context.Background(), InvocationSurfaceHTTPBinding),
			want: "http_binding",
		},
		{
			name: "workflow caller",
			ctx: WithCallerProvider(
				WithEntry(context.Background(), EntryGRPC),
				ProviderKindWorkflow,
				"temporal",
			),
			want: "workflow",
		},
		{
			name: "cross-app caller",
			ctx: WithCallerProvider(
				WithEntry(context.Background(), EntryGRPC),
				ProviderKindApp,
				"ci-cd",
			),
			want: "cross_app",
		},
		{
			name: "http entry without explicit surface",
			ctx:  WithEntry(context.Background(), EntryHTTP),
			want: "http",
		},
		{
			name: "unknown internal entry",
			ctx:  context.Background(),
			want: metricutil.UnknownAttrValue,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := invokeAuthorizationSurface(tc.ctx); got != tc.want {
				t.Fatalf("invokeAuthorizationSurface() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInvokeAuthorizationSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		p        *principal.Principal
		wantKind string
		wantID   string
	}{
		{
			name: "user email",
			p: &principal.Principal{
				SubjectID: "user:u-1",
				Kind:      principal.KindUser,
				Identity:  &core.UserIdentity{Email: "User@Example.com"},
			},
			wantKind: "user",
			wantID:   "user@example.com",
		},
		{
			name: "service account",
			p: &principal.Principal{
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotKind, gotID := invokeAuthorizationSubject(tc.p)
			if gotKind != tc.wantKind || gotID != tc.wantID {
				t.Fatalf("invokeAuthorizationSubject() = (%q, %q), want (%q, %q)", gotKind, gotID, tc.wantKind, tc.wantID)
			}
		})
	}
}

func TestAuthorizeOperationRecordsAllowAndDenyMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	authz := &recordingAuthorizationProvider{
		allowed:          true,
		matchedRelations: []string{"admin"},
	}
	broker := NewBroker(
		nil,
		nil,
		nil,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"traffic-cop": ProviderKindApp}),
	)

	allowAttrs := map[string]string{
		"gestalt.provider":                      "traffic-cop",
		"gestalt.operation":                     "sync.workqueue",
		"gestaltd.invocation.surface":           metricutil.UnknownAttrValue,
		"gestaltd.invoke.authorization.decision": "allow",
		"gestaltd.subject.kind":                 "user",
		"gestaltd.subject.id":                   "user@example.com",
	}
	_, err := broker.authorizeOperation(
		ctx,
		&principal.Principal{
			SubjectID: "user:u-123",
			UserID:    "u-123",
			Kind:      principal.KindUser,
			Identity:  &core.UserIdentity{Email: "user@example.com"},
		},
		"traffic-cop",
		catalog.CatalogOperation{ID: "sync.workqueue", AllowedRoles: []string{"admin"}},
	)
	if err != nil {
		t.Fatalf("authorizeOperation allow: %v", err)
	}
	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.count", 1, allowAttrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.invoke.authorization.error_count", allowAttrs)

	authz.matchedRelations = []string{"viewer"}
	denyAttrs := map[string]string{
		"gestalt.provider":                        "traffic-cop",
		"gestalt.operation":                       "sync.workqueue",
		"gestaltd.invocation.surface":             metricutil.UnknownAttrValue,
		"gestaltd.invoke.authorization.decision":  "deny",
		"gestaltd.invoke.authorization.deny_reason": "role_denied",
		"gestaltd.subject.kind":                   "user",
		"gestaltd.subject.id":                     "user@example.com",
	}
	_, err = broker.authorizeOperation(
		ctx,
		&principal.Principal{
			SubjectID: "user:u-123",
			UserID:    "u-123",
			Kind:      principal.KindUser,
			Identity:  &core.UserIdentity{Email: "user@example.com"},
		},
		"traffic-cop",
		catalog.CatalogOperation{ID: "sync.workqueue", AllowedRoles: []string{"admin"}},
	)
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("authorizeOperation deny error = %v, want ErrAuthorizationDenied", err)
	}
	rm = metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.count", 1, denyAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.error_count", 1, denyAttrs)
}

func TestCheckAuthorizationAccessRecordsRelationDeniedMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(
		WithInvocationSurface(context.Background(), InvocationSurfaceHTTP),
		metrics.Provider,
	)
	authz := &recordingAuthorizationProvider{allowed: false}
	broker := NewBroker(nil, nil, nil, WithAuthorizationProvider(authz))

	denyAttrs := map[string]string{
		"gestalt.provider":                        "slack",
		"gestalt.operation":                       "chat.postMessage",
		"gestaltd.invocation.surface":             "http",
		"gestaltd.invoke.authorization.decision":  "deny",
		"gestaltd.invoke.authorization.deny_reason": "relation_denied",
		"gestaltd.subject.kind":                   "service_account",
		"gestaltd.subject.id":                     "workflow-roadmap",
	}
	err := broker.checkAuthorizationAccess(
		ctx,
		&principal.Principal{SubjectID: "service_account:workflow-roadmap"},
		"slack",
		"chat.postMessage",
	)
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("checkAuthorizationAccess error = %v, want ErrAuthorizationDenied", err)
	}
	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.count", 1, denyAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.error_count", 1, denyAttrs)
}
