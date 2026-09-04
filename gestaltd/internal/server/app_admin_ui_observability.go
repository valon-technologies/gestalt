package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	appAdminUISurfaceMembers           = "members"
	appAdminUISurfaceAllowedOperations = "allowed_operations"

	appAdminUIActionList       = "list"
	appAdminUIActionSave       = "save"
	appAdminUIActionGrantAdd   = "grant_add"
	appAdminUIActionGrantRemove = "grant_remove"

	appAdminUIOutcomeSuccess = "success"
	appAdminUIOutcomeFailure = "failure"

	appAdminUIFailureAuth       = "auth_failure"
	appAdminUIFailureValidation = "validation"
	appAdminUIFailureServer     = "server"
	appAdminUIFailureOther      = "other"
)

type appAdminUIResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *appAdminUIResponseRecorder) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *appAdminUIResponseRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (s *Server) appAdminUIObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		surface, action, ok := appAdminUIRouteSpecForRequest(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		appName := strings.TrimSpace(chi.URLParam(r, "app"))
		startedAt := time.Now()
		recorder := &appAdminUIResponseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		failed := recorder.status >= http.StatusBadRequest
		failureCategory := ""
		if failed {
			failureCategory = appAdminUIFailureCategoryFromStatus(recorder.status)
		}
		recordAppAdminUIInteraction(
			r.Context(),
			r,
			startedAt,
			appName,
			surface,
			action,
			failed,
			failureCategory,
			recorder.status,
			nil,
		)
	})
}

func appAdminUIRouteSpecForRequest(r *http.Request) (surface, action string, ok bool) {
	if r == nil {
		return "", "", false
	}
	path := strings.TrimRight(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasSuffix(path, "/admin/members"):
			return appAdminUISurfaceMembers, appAdminUIActionList, true
		case strings.HasSuffix(path, "/admin/allowed-operations"):
			return appAdminUISurfaceAllowedOperations, appAdminUIActionList, true
		}
	case http.MethodPut:
		if strings.HasSuffix(path, "/admin/allowed-operations") {
			return appAdminUISurfaceAllowedOperations, appAdminUIActionSave, true
		}
	}
	return "", "", false
}

func appAdminUIFailureCategoryFromStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return appAdminUIFailureAuth
	case status == http.StatusBadRequest:
		return appAdminUIFailureValidation
	case status >= http.StatusInternalServerError:
		return appAdminUIFailureServer
	default:
		return appAdminUIFailureOther
	}
}

func recordAppAdminUIAuthFailure(ctx context.Context, r *http.Request, appName, surface, action string, err error) {
	if strings.TrimSpace(appName) == "" || surface == "" || action == "" {
		return
	}
	recordAppAdminUIInteraction(
		ctx,
		r,
		time.Now(),
		appName,
		surface,
		action,
		true,
		appAdminUIFailureAuth,
		http.StatusForbidden,
		err,
	)
}

func recordAppAdminUIInteraction(
	ctx context.Context,
	r *http.Request,
	startedAt time.Time,
	appName, surface, action string,
	failed bool,
	failureCategory string,
	statusCode int,
	err error,
) {
	appName = strings.TrimSpace(appName)
	if appName == "" || surface == "" || action == "" {
		return
	}
	outcome := appAdminUIOutcomeSuccess
	if failed {
		outcome = appAdminUIOutcomeFailure
	}
	attrs := []attribute.KeyValue{
		observability.AttrAppAdminUIApp.String(appName),
		observability.AttrAppAdminUISurface.String(surface),
		observability.AttrAppAdminUIAction.String(action),
		observability.AttrAppAdminUIOutcome.String(outcome),
	}
	if failed && failureCategory != "" {
		attrs = append(attrs, observability.AttrAppAdminUIFailureCategory.String(failureCategory))
	}
	observability.RecordAppAdminUI(ctx, startedAt, failed, attrs...)
	if failed {
		logAppAdminUIFailure(ctx, r, appName, surface, action, failureCategory, statusCode, err)
	}
}

func logAppAdminUIFailure(
	ctx context.Context,
	r *http.Request,
	app, surface, action, failureCategory string,
	statusCode int,
	err error,
) {
	attrs := []any{
		slog.String("event", "app_admin.ui"),
		slog.String("app", app),
		slog.String("surface", surface),
		slog.String("action", action),
		slog.String("failure_category", failureCategory),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	} else if statusCode > 0 {
		attrs = append(attrs, slog.String("error", fmt.Sprintf("HTTP %d", statusCode)))
	}
	if r != nil {
		if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
			attrs = append(attrs, slog.String("request_id", requestID))
		}
	}
	if meta := invocation.MetaFromContext(ctx); meta != nil {
		if requestID := strings.TrimSpace(meta.RequestID); requestID != "" {
			attrs = append(attrs, slog.String("request_id", requestID))
		}
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs, slog.String("trace_id", spanCtx.TraceID().String()))
	}
	slog.WarnContext(ctx, "app admin ui interaction failed", attrs...)
}

func appAdminUIAuthFailureError(message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("app access denied")
	}
	return errors.New(message)
}
