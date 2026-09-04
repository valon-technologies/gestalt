package observability

import (
	"context"
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
