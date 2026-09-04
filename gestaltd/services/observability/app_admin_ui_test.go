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
)

func TestRecordAppAdminUI(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	startedAt := time.Now()

	RecordAppAdminUI(ctx, startedAt, false,
		AttrAppAdminUIApp.String("g-issues"),
		AttrAppAdminUISurface.String("members"),
		AttrAppAdminUIAction.String("list"),
		AttrAppAdminUIOutcome.String("success"),
	)

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.app_admin.ui.app":     "g-issues",
		"gestaltd.app_admin.ui.surface": "members",
		"gestaltd.app_admin.ui.action":  "list",
		"gestaltd.app_admin.ui.outcome": "success",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.app_admin.ui.duration", attrs)

	RecordAppAdminUI(ctx, startedAt, true,
		AttrAppAdminUIApp.String("g-issues"),
		AttrAppAdminUISurface.String("members"),
		AttrAppAdminUIAction.String("grant_add"),
		AttrAppAdminUIOutcome.String("failure"),
		AttrAppAdminUIFailureCategory.String("auth_failure"),
	)

	rm = metrictest.CollectMetrics(t, metrics.Reader)
	failAttrs := map[string]string{
		"gestaltd.app_admin.ui.app":              "g-issues",
		"gestaltd.app_admin.ui.surface":          "members",
		"gestaltd.app_admin.ui.action":           "grant_add",
		"gestaltd.app_admin.ui.outcome":          "failure",
		"gestaltd.app_admin.ui.failure_category": "auth_failure",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.count", 1, failAttrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.app_admin.ui.error_count", 1, failAttrs)
}

func TestLogAppAdminUIFailureIncludesPrincipalAndTargetSubjects(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	LogAppAdminUI(context.Background(), AppAdminUIInteraction{
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
	}, AppAdminUIOutcomeFailure)

	var record map[string]string
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("unmarshal log record: %v", err)
	}
	for key, want := range map[string]string{
		"event":                  "app_admin.ui",
		"outcome":                AppAdminUIOutcomeFailure,
		"level":                  "WARN",
		"principal_subject_kind": "user",
		"principal_subject_id":   "user:abc",
		"target_subject_kind":    "user",
		"target_subject_id":      "user:def",
		"failure_category":       AppAdminUIFailureAuth,
	} {
		if got := strings.TrimSpace(record[key]); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLogAppAdminUISuccessIncludesOutcome(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	LogAppAdminUI(context.Background(), AppAdminUIInteraction{
		App:                  "g-issues",
		Surface:              AppAdminUISurfaceMembers,
		Action:               AppAdminUIActionList,
		PrincipalSubjectKind: "user",
		PrincipalSubjectID:   "user:abc",
	}, AppAdminUIOutcomeSuccess)

	var record map[string]string
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("unmarshal log record: %v", err)
	}
	for key, want := range map[string]string{
		"event":                  "app_admin.ui",
		"outcome":                AppAdminUIOutcomeSuccess,
		"level":                  "INFO",
		"principal_subject_kind": "user",
		"principal_subject_id":   "user:abc",
	} {
		if got := strings.TrimSpace(record[key]); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := record["failure_category"]; ok {
		t.Fatalf("failure_category should be omitted on success logs")
	}
}
