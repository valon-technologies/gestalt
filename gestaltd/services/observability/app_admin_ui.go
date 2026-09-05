package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
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
	AttrAppAdminApp               = attribute.Key("gestaltd.app_admin.app")
	AttrAppAdminOperation         = attribute.Key("gestaltd.app_admin.operation")
	AttrAppAdminOutcome           = attribute.Key("gestaltd.app_admin.outcome")
	AttrAppAdminFailureCategory   = attribute.Key("gestaltd.app_admin.failure_category")
	AttrAppAdminTargetSubjectKind = attribute.Key("gestaltd.app_admin.target_subject.kind")
	AttrAppAdminUITargetSubjectID = attribute.Key("gestaltd.app_admin.ui.target_subject.id")
)

type AppAdminUIInteraction struct {
	App                  string
	Surface              string
	Action               string
	ClientKind           string
	Failed               bool
	FailureCategory      string
	StatusCode           int
	Err                  error
	RequestID            string
	PrincipalSubjectKind string
	PrincipalSubjectID   string
	TargetSubjectKind    string
	TargetSubjectID      string
}

func AppAdminOperation(surface, action string) string {
	surface = strings.TrimSpace(surface)
	action = strings.TrimSpace(action)
	if surface == "" || action == "" {
		return ""
	}
	return surface + "_" + action
}

func RecordAppAdminUI(ctx context.Context, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	record(ctx, &appAdminUIMetrics, "gestaltd.app_admin", "gestaltd app admin interactions", startedAt, failed, attrs...)
}

func RecordAppAdminUIInteraction(ctx context.Context, startedAt time.Time, interaction AppAdminUIInteraction) {
	interaction.App = strings.TrimSpace(interaction.App)
	interaction.Surface = strings.TrimSpace(interaction.Surface)
	interaction.Action = strings.TrimSpace(interaction.Action)
	operation := AppAdminOperation(interaction.Surface, interaction.Action)
	if interaction.App == "" || operation == "" {
		return
	}
	outcome := AppAdminUIOutcomeSuccess
	if interaction.Failed {
		outcome = AppAdminUIOutcomeFailure
	}
	attrs := []attribute.KeyValue{
		AttrAppAdminApp.String(interaction.App),
		AttrAppAdminOperation.String(operation),
		AttrAppAdminOutcome.String(outcome),
	}
	if interaction.Failed && interaction.FailureCategory != "" {
		attrs = append(attrs, AttrAppAdminFailureCategory.String(interaction.FailureCategory))
	}
	if kind := strings.TrimSpace(interaction.PrincipalSubjectKind); kind != "" {
		attrs = append(attrs, AttrSubjectKind.String(kind))
	}
	if kind := strings.TrimSpace(interaction.TargetSubjectKind); kind != "" {
		attrs = append(attrs, AttrAppAdminTargetSubjectKind.String(kind))
	}
	if clientKindAttr, ok := appAdminClientKindAttr(ctx, interaction.ClientKind); ok {
		attrs = append(attrs, clientKindAttr)
	}
	RecordAppAdminUI(ctx, startedAt, interaction.Failed, attrs...)
	LogAppAdminUI(ctx, interaction)
}

func appAdminClientKindAttr(ctx context.Context, explicit string) (attribute.KeyValue, bool) {
	kind := strings.TrimSpace(explicit)
	if kind == "" {
		kind = metricutil.ClientKindFromContext(ctx)
	}
	if kind != metricutil.ClientKindWeb && kind != metricutil.ClientKindCLI {
		return attribute.KeyValue{}, false
	}
	return attribute.String("gestaltd.client.kind", kind), true
}

func appendAppAdminUILogSubject(
	attrs []any,
	kindKey, idKey, subjectKind, subjectID string,
) []any {
	subjectKind = strings.TrimSpace(subjectKind)
	if subjectKind == "" {
		return attrs
	}
	return append(attrs,
		slog.String(kindKey, subjectKind),
		slog.String(idKey, strings.TrimSpace(subjectID)),
	)
}

func LogAppAdminUI(ctx context.Context, interaction AppAdminUIInteraction) {
	outcome := AppAdminUIOutcomeSuccess
	if interaction.Failed {
		outcome = AppAdminUIOutcomeFailure
	}
	attrs := []any{
		slog.String("event", "app_admin.ui"),
		slog.String("app", interaction.App),
		slog.String("surface", interaction.Surface),
		slog.String("action", interaction.Action),
		slog.String("outcome", outcome),
	}
	attrs = appendAppAdminUILogSubject(
		attrs,
		"principal_subject_kind",
		"principal_subject_id",
		interaction.PrincipalSubjectKind,
		interaction.PrincipalSubjectID,
	)
	attrs = appendAppAdminUILogSubject(
		attrs,
		"target_subject_kind",
		"target_subject_id",
		interaction.TargetSubjectKind,
		interaction.TargetSubjectID,
	)
	if interaction.Failed {
		if failureCategory := strings.TrimSpace(interaction.FailureCategory); failureCategory != "" {
			attrs = append(attrs, slog.String("failure_category", failureCategory))
		}
		if interaction.Err != nil {
			attrs = append(attrs, slog.String("error", interaction.Err.Error()))
		} else if interaction.StatusCode > 0 {
			attrs = append(attrs, slog.String("error", fmt.Sprintf("HTTP %d", interaction.StatusCode)))
		}
	}
	if requestID := strings.TrimSpace(interaction.RequestID); requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
	}
	if interaction.Failed {
		slog.WarnContext(ctx, "app admin ui interaction failed", attrs...)
		return
	}
	slog.InfoContext(ctx, "app admin ui interaction", attrs...)
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
