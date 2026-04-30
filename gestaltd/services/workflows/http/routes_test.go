package workflowshttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountRoutes(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	Mount(r, Handlers{
		ListSchedules:      testHandler("list-schedules"),
		CreateSchedule:     testHandler("create-schedule"),
		GetSchedule:        testHandler("get-schedule"),
		UpdateSchedule:     testHandler("update-schedule"),
		DeleteSchedule:     testHandler("delete-schedule"),
		PauseSchedule:      testHandler("pause-schedule"),
		ResumeSchedule:     testHandler("resume-schedule"),
		ListEventTriggers:  testHandler("list-event-triggers"),
		CreateEventTrigger: testHandler("create-event-trigger"),
		GetEventTrigger:    testHandler("get-event-trigger"),
		UpdateEventTrigger: testHandler("update-event-trigger"),
		DeleteEventTrigger: testHandler("delete-event-trigger"),
		PauseEventTrigger:  testHandler("pause-event-trigger"),
		ResumeEventTrigger: testHandler("resume-event-trigger"),
		PublishEvent:       testHandler("publish-event"),
		ListRuns:           testHandler("list-runs"),
		GetRun:             testHandler("get-run"),
		CancelRun:          testHandler("cancel-run"),
	})

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/workflow/schedules", "list-schedules"},
		{http.MethodPost, "/workflow/schedules", "create-schedule"},
		{http.MethodGet, "/workflow/schedules/", "list-schedules"},
		{http.MethodPost, "/workflow/schedules/", "create-schedule"},
		{http.MethodGet, "/workflow/schedules/schedule-1", "get-schedule"},
		{http.MethodPut, "/workflow/schedules/schedule-1", "update-schedule"},
		{http.MethodDelete, "/workflow/schedules/schedule-1", "delete-schedule"},
		{http.MethodPost, "/workflow/schedules/schedule-1/pause", "pause-schedule"},
		{http.MethodPost, "/workflow/schedules/schedule-1/resume", "resume-schedule"},
		{http.MethodGet, "/workflow/event-triggers", "list-event-triggers"},
		{http.MethodPost, "/workflow/event-triggers", "create-event-trigger"},
		{http.MethodGet, "/workflow/event-triggers/", "list-event-triggers"},
		{http.MethodPost, "/workflow/event-triggers/", "create-event-trigger"},
		{http.MethodGet, "/workflow/event-triggers/trigger-1", "get-event-trigger"},
		{http.MethodPut, "/workflow/event-triggers/trigger-1", "update-event-trigger"},
		{http.MethodDelete, "/workflow/event-triggers/trigger-1", "delete-event-trigger"},
		{http.MethodPost, "/workflow/event-triggers/trigger-1/pause", "pause-event-trigger"},
		{http.MethodPost, "/workflow/event-triggers/trigger-1/resume", "resume-event-trigger"},
		{http.MethodPost, "/workflow/events", "publish-event"},
		{http.MethodGet, "/workflow/runs", "list-runs"},
		{http.MethodGet, "/workflow/runs/", "list-runs"},
		{http.MethodGet, "/workflow/runs/run-1", "get-run"},
		{http.MethodPost, "/workflow/runs/run-1/cancel", "cancel-run"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("X-Test-Handler"); got != tc.want {
			t.Fatalf("%s %s handler = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func testHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test-Handler", name)
		w.WriteHeader(http.StatusNoContent)
	}
}
