package invocation

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestAuthorizationSurface(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := authorizationSurface(tc.ctx); got != tc.want {
				t.Fatalf("authorizationSurface() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEvaluateInvokeAuthorizationRecordsAllowAndDenyMetrics(t *testing.T) {
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
	p := &principal.Principal{
		SubjectID: "user:u-123",
		UserID:    "u-123",
		Kind:      principal.KindUser,
		Identity:  &core.UserIdentity{Email: "user@example.com"},
	}

	allowAttrs := map[string]string{
		"gestalt.provider":                       "traffic-cop",
		"gestalt.operation":                      "sync.workqueue",
		"gestaltd.invocation.surface":            metricutil.UnknownAttrValue,
		"gestaltd.invoke.authorization.decision": "allow",
		"gestaltd.subject.kind":                  "user",
		"gestaltd.subject.id":                    "user@example.com",
	}
	_, err := broker.evaluateInvokeAuthorization(ctx, p, "traffic-cop", "sync.workqueue", []string{"admin"})
	if err != nil {
		t.Fatalf("evaluateInvokeAuthorization allow: %v", err)
	}
	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.invoke.authorization.count", 1, allowAttrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.invoke.authorization.error_count", allowAttrs)

	authz.matchedRelations = []string{"viewer"}
	denyAttrs := map[string]string{
		"gestalt.provider":                          "traffic-cop",
		"gestalt.operation":                         "sync.workqueue",
		"gestaltd.invocation.surface":               metricutil.UnknownAttrValue,
		"gestaltd.invoke.authorization.decision":    "deny",
		"gestaltd.invoke.authorization.deny_reason": "role_denied",
		"gestaltd.subject.kind":                     "user",
		"gestaltd.subject.id":                       "user@example.com",
	}
	_, err = broker.evaluateInvokeAuthorization(ctx, p, "traffic-cop", "sync.workqueue", []string{"admin"})
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("evaluateInvokeAuthorization deny error = %v, want ErrAuthorizationDenied", err)
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
		"gestalt.provider":                          "slack",
		"gestalt.operation":                         "chat.postMessage",
		"gestaltd.invocation.surface":               "http",
		"gestaltd.invoke.authorization.decision":    "deny",
		"gestaltd.invoke.authorization.deny_reason": "relation_denied",
		"gestaltd.subject.kind":                     "service_account",
		"gestaltd.subject.id":                       "workflow-roadmap",
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

func TestCheckProviderAccessDoesNotRecordInvokeAuthorizationMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	authz := &recordingAuthorizationProvider{allowed: true}
	broker := NewBroker(
		nil,
		nil,
		nil,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"github": ProviderKindApp}),
	)

	err := broker.CheckProviderAccess(
		ctx,
		&principal.Principal{SubjectID: "service_account:workflow-roadmap"},
		"github",
	)
	if err != nil {
		t.Fatalf("CheckProviderAccess: %v", err)
	}
	if authz.checkAccessCalls != 1 {
		t.Fatalf("CheckAccess calls = %d, want 1", authz.checkAccessCalls)
	}
	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireNoMetric(t, rm, "gestaltd.invoke.authorization.count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.invoke.authorization.error_count")
	metrictest.RequireNoMetric(t, rm, "gestaltd.invoke.authorization.duration")
}
