package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
)

func TestAppAdminOperation(t *testing.T) {
	t.Parallel()

	if got := AppAdminOperation(AppAdminUISurfaceMembers, AppAdminUIActionList); got != "members_list" {
		t.Fatalf("AppAdminOperation() = %q, want members_list", got)
	}
	if got := AppAdminOperation(" ", AppAdminUIActionList); got != "" {
		t.Fatalf("AppAdminOperation() = %q, want empty", got)
	}
}

func TestRecordAppAdminUI(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	startedAt := time.Now()

	RecordAppAdminUI(ctx, startedAt, false,
		AttrAppAdminApp.String("g-issues"),
		AttrAppAdminOperation.String("members_list"),
		AttrAppAdminOutcome.String("success"),
	)

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":       "g-issues",
		"gestaltd.app_admin.operation": "members_list",
		"gestaltd.app_admin.outcome":   "success",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.duration", attrs)

	RecordAppAdminUI(ctx, startedAt, true,
		AttrAppAdminApp.String("g-issues"),
		AttrAppAdminOperation.String("members_grant_add"),
		AttrAppAdminOutcome.String("failure"),
		AttrAppAdminFailureCategory.String("auth_failure"),
	)

	rm = metrictest.CollectMetrics(t, metrics.Reader)
	failAttrs := map[string]string{
		"gestaltd.app_admin.app":              "g-issues",
		"gestaltd.app_admin.operation":        "members_grant_add",
		"gestaltd.app_admin.outcome":          "failure",
		"gestaltd.app_admin.failure_category": "auth_failure",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, failAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.error_count", 1, failAttrs)
}

func TestRecordAppAdminUIInteractionOmitsSubjectIDsFromMetrics(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)

	RecordAppAdminUIInteraction(ctx, time.Now(), AppAdminUIInteraction{
		App:                  "g-issues",
		Surface:              AppAdminUISurfaceMembers,
		Action:               AppAdminUIActionGrantAdd,
		ClientKind:           metricutil.ClientKindCLI,
		PrincipalSubjectKind: "user",
		PrincipalSubjectID:   "user:abc",
		TargetSubjectKind:    "user",
		TargetSubjectID:      "user:def",
	})

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":                 "g-issues",
		"gestaltd.app_admin.operation":           "members_grant_add",
		"gestaltd.app_admin.outcome":             "success",
		"gestaltd.client.kind":                   metricutil.ClientKindCLI,
		"gestaltd.subject.kind":                  "user",
		"gestaltd.app_admin.target_subject.kind": "user",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.count", map[string]string{
		"gestaltd.app_admin.app":                 "g-issues",
		"gestaltd.app_admin.operation":           "members_grant_add",
		"gestaltd.app_admin.outcome":             "success",
		"gestaltd.subject.kind":                  "user",
		"gestaltd.subject.id":                    "user:abc",
		"gestaltd.app_admin.target_subject.kind": "user",
	})
}

func TestRecordAppAdminUIInteractionClientKindFromContext(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	labeler := &otelhttp.Labeler{}
	labeler.Add(attribute.String("gestaltd.client.kind", metricutil.ClientKindWeb))
	ctx := otelhttp.ContextWithLabeler(context.Background(), labeler)
	ctx = metricutil.WithMeterProvider(ctx, metrics.Provider)

	RecordAppAdminUIInteraction(ctx, time.Now(), AppAdminUIInteraction{
		App:     "g-issues",
		Surface: AppAdminUISurfaceMembers,
		Action:  AppAdminUIActionList,
	})

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.app":       "g-issues",
		"gestaltd.app_admin.operation": "members_list",
		"gestaltd.app_admin.outcome":   "success",
		"gestaltd.client.kind":         metricutil.ClientKindWeb,
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.count", 1, attrs)
}

func TestLogAppAdminUI(t *testing.T) { //nolint:paralleltest // mutates slog.Default()
	tests := []struct {
		name        string
		interaction AppAdminUIInteraction
		wantLevel   string
		want        map[string]string
		omit        []string
	}{
		{
			name: "failure includes principal target and category",
			interaction: AppAdminUIInteraction{
				App:                  "g-issues",
				Surface:              AppAdminUISurfaceMembers,
				Action:               AppAdminUIActionGrantAdd,
				Failed:               true,
				FailureCategory:      AppAdminUIFailureAuth,
				StatusCode:           http.StatusForbidden,
				PrincipalSubjectKind: "user",
				PrincipalSubjectID:   "user:abc",
				TargetSubjectKind:    "user",
				TargetSubjectID:      "user:def",
			},
			wantLevel: "WARN",
			want: map[string]string{
				"event":                  "app_admin.ui",
				"outcome":                AppAdminUIOutcomeFailure,
				"principal_subject_kind": "user",
				"principal_subject_id":   "user:abc",
				"target_subject_kind":    "user",
				"target_subject_id":      "user:def",
				"failure_category":       AppAdminUIFailureAuth,
			},
		},
		{
			name: "success omits failure category",
			interaction: AppAdminUIInteraction{
				App:                  "g-issues",
				Surface:              AppAdminUISurfaceMembers,
				Action:               AppAdminUIActionList,
				PrincipalSubjectKind: "user",
				PrincipalSubjectID:   "user:abc",
			},
			wantLevel: "INFO",
			want: map[string]string{
				"event":                  "app_admin.ui",
				"outcome":                AppAdminUIOutcomeSuccess,
				"principal_subject_kind": "user",
				"principal_subject_id":   "user:abc",
			},
			omit: []string{"failure_category"},
		},
	}

	for _, tt := range tests { //nolint:paralleltest // mutates slog.Default()
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
			t.Cleanup(func() { slog.SetDefault(previous) })

			LogAppAdminUI(context.Background(), tt.interaction)

			var record map[string]string
			if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
				t.Fatalf("unmarshal log record: %v", err)
			}
			if got := strings.TrimSpace(record["level"]); got != tt.wantLevel {
				t.Fatalf("level = %q, want %q", got, tt.wantLevel)
			}
			for key, want := range tt.want {
				if got := strings.TrimSpace(record[key]); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
			for _, key := range tt.omit {
				if _, ok := record[key]; ok {
					t.Fatalf("%s should be omitted", key)
				}
			}
		})
	}
}
