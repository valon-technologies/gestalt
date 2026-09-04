package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/observability"
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
		interaction := observability.AppAdminUIInteraction{
			App:       appName,
			Surface:   surface,
			Action:    action,
			Failed:    failed,
			StatusCode: recorder.status,
			RequestID: appAdminUIRequestID(r),
		}
		if failed {
			interaction.FailureCategory = observability.AppAdminUIFailureCategoryHTTP(recorder.status)
		}
		observability.RecordAppAdminUIInteraction(r.Context(), startedAt, interaction)
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
			return observability.AppAdminUISurfaceMembers, observability.AppAdminUIActionList, true
		case strings.HasSuffix(path, "/admin/allowed-operations"):
			return observability.AppAdminUISurfaceAllowedOperations, observability.AppAdminUIActionList, true
		}
	case http.MethodPut:
		if strings.HasSuffix(path, "/admin/allowed-operations") {
			return observability.AppAdminUISurfaceAllowedOperations, observability.AppAdminUIActionSave, true
		}
	}
	return "", "", false
}

func recordAppAdminUIAuthFailure(ctx context.Context, r *http.Request, appName, surface, action string, err error) {
	if strings.TrimSpace(appName) == "" || surface == "" || action == "" {
		return
	}
	observability.RecordAppAdminUIInteraction(ctx, time.Now(), observability.AppAdminUIInteraction{
		App:             appName,
		Surface:         surface,
		Action:          action,
		Failed:          true,
		FailureCategory: observability.AppAdminUIFailureAuth,
		StatusCode:      http.StatusForbidden,
		Err:             err,
		RequestID:       appAdminUIRequestID(r),
	})
}

func appAdminUIRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	if meta := invocation.MetaFromContext(r.Context()); meta != nil {
		return strings.TrimSpace(meta.RequestID)
	}
	return ""
}
