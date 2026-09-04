package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	AppAdminUISurfaceMembers           = "members"
	AppAdminUISurfaceAllowedOperations = "allowed_operations"

	AppAdminUIActionList        = "list"
	AppAdminUIActionSave        = "save"
	AppAdminUIActionGrantAdd    = "grant_add"
	AppAdminUIActionGrantRemove = "grant_remove"

	AppAdminUIOutcomeSuccess = "success"
	AppAdminUIOutcomeFailure = "failure"

	AppAdminUIFailureAuth       = "auth_failure"
	AppAdminUIFailureValidation = "validation"
	AppAdminUIFailureServer     = "server"
	AppAdminUIFailureOther      = "other"
)

var (
	AttrAppAdminUIApp             = attribute.Key("gestaltd.app_admin.ui.app")
	AttrAppAdminUISurface         = attribute.Key("gestaltd.app_admin.ui.surface")
	AttrAppAdminUIAction          = attribute.Key("gestaltd.app_admin.ui.action")
	AttrAppAdminUIOutcome         = attribute.Key("gestaltd.app_admin.ui.outcome")
	AttrAppAdminUIFailureCategory = attribute.Key("gestaltd.app_admin.ui.failure_category")
)

type AppAdminUIInteraction struct {
	App             string
	Surface         string
	Action          string
	Failed          bool
	FailureCategory string
	StatusCode      int
	Err             error
	RequestID       string
}

func RecordAppAdminUI(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &appAdminUIMetrics, "gestaltd.app_admin.ui", "gestaltd app admin UI interactions", startedAt, failed, attrs...)
}

func RecordAppAdminUIInteraction(ctx context.Context, startedAt time.Time, interaction AppAdminUIInteraction) {
	interaction.App = strings.TrimSpace(interaction.App)
	interaction.Surface = strings.TrimSpace(interaction.Surface)
	interaction.Action = strings.TrimSpace(interaction.Action)
	if interaction.App == "" || interaction.Surface == "" || interaction.Action == "" {
		return
	}
	outcome := AppAdminUIOutcomeSuccess
	if interaction.Failed {
		outcome = AppAdminUIOutcomeFailure
	}
	attrs := []attribute.KeyValue{
		AttrAppAdminUIApp.String(interaction.App),
		AttrAppAdminUISurface.String(interaction.Surface),
		AttrAppAdminUIAction.String(interaction.Action),
		AttrAppAdminUIOutcome.String(outcome),
	}
	if interaction.Failed && interaction.FailureCategory != "" {
		attrs = append(attrs, AttrAppAdminUIFailureCategory.String(interaction.FailureCategory))
	}
	RecordAppAdminUI(ctx, startedAt, interaction.Failed, attrs...)
	if interaction.Failed {
		LogAppAdminUIFailure(ctx, interaction)
	}
}

func LogAppAdminUIFailure(ctx context.Context, interaction AppAdminUIInteraction) {
	attrs := []any{
		slog.String("event", "app_admin.ui"),
		slog.String("app", interaction.App),
		slog.String("surface", interaction.Surface),
		slog.String("action", interaction.Action),
		slog.String("failure_category", interaction.FailureCategory),
	}
	if interaction.Err != nil {
		attrs = append(attrs, slog.String("error", interaction.Err.Error()))
	} else if interaction.StatusCode > 0 {
		attrs = append(attrs, slog.String("error", fmt.Sprintf("HTTP %d", interaction.StatusCode)))
	}
	if requestID := strings.TrimSpace(interaction.RequestID); requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
	}
	slog.WarnContext(ctx, "app admin ui interaction failed", attrs...)
}

func AppAdminUIFailureCategoryHTTP(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return AppAdminUIFailureAuth
	case status == http.StatusBadRequest:
		return AppAdminUIFailureValidation
	case status >= http.StatusInternalServerError:
		return AppAdminUIFailureServer
	default:
		return AppAdminUIFailureOther
	}
}

func AppAdminUIFailureCategoryGRPC(err error) string {
	if err == nil {
		return ""
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return AppAdminUIFailureAuth
	case codes.InvalidArgument:
		return AppAdminUIFailureValidation
	case codes.Internal, codes.Unavailable:
		return AppAdminUIFailureServer
	default:
		return AppAdminUIFailureOther
	}
}
