package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

var (
	AttrAppAdminUIApp             = attribute.Key("gestaltd.app_admin.ui.app")
	AttrAppAdminUISurface         = attribute.Key("gestaltd.app_admin.ui.surface")
	AttrAppAdminUIAction          = attribute.Key("gestaltd.app_admin.ui.action")
	AttrAppAdminUIOutcome         = attribute.Key("gestaltd.app_admin.ui.outcome")
	AttrAppAdminUIFailureCategory = attribute.Key("gestaltd.app_admin.ui.failure_category")
)

func RecordAppAdminUI(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &appAdminUIMetrics, "gestaltd.app_admin.ui", "gestaltd app admin UI interactions", startedAt, failed, attrs...)
}
