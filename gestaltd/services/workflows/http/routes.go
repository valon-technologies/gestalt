package workflowshttp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	ListSchedules      http.HandlerFunc
	CreateSchedule     http.HandlerFunc
	GetSchedule        http.HandlerFunc
	UpdateSchedule     http.HandlerFunc
	DeleteSchedule     http.HandlerFunc
	PauseSchedule      http.HandlerFunc
	ResumeSchedule     http.HandlerFunc
	ListEventTriggers  http.HandlerFunc
	CreateEventTrigger http.HandlerFunc
	GetEventTrigger    http.HandlerFunc
	UpdateEventTrigger http.HandlerFunc
	DeleteEventTrigger http.HandlerFunc
	PauseEventTrigger  http.HandlerFunc
	ResumeEventTrigger http.HandlerFunc
	PublishEvent       http.HandlerFunc
	ListRuns           http.HandlerFunc
	GetRun             http.HandlerFunc
	CancelRun          http.HandlerFunc
}

func Mount(r chi.Router, h Handlers) {
	r.Get("/workflow/schedules", h.ListSchedules)
	r.Post("/workflow/schedules", h.CreateSchedule)
	r.Route("/workflow/schedules", func(r chi.Router) {
		r.Get("/", h.ListSchedules)
		r.Post("/", h.CreateSchedule)
		r.Get("/{scheduleID}", h.GetSchedule)
		r.Put("/{scheduleID}", h.UpdateSchedule)
		r.Delete("/{scheduleID}", h.DeleteSchedule)
		r.Post("/{scheduleID}/pause", h.PauseSchedule)
		r.Post("/{scheduleID}/resume", h.ResumeSchedule)
	})
	r.Get("/workflow/event-triggers", h.ListEventTriggers)
	r.Post("/workflow/event-triggers", h.CreateEventTrigger)
	r.Route("/workflow/event-triggers", func(r chi.Router) {
		r.Get("/", h.ListEventTriggers)
		r.Post("/", h.CreateEventTrigger)
		r.Get("/{triggerID}", h.GetEventTrigger)
		r.Put("/{triggerID}", h.UpdateEventTrigger)
		r.Delete("/{triggerID}", h.DeleteEventTrigger)
		r.Post("/{triggerID}/pause", h.PauseEventTrigger)
		r.Post("/{triggerID}/resume", h.ResumeEventTrigger)
	})
	r.Post("/workflow/events", h.PublishEvent)
	r.Get("/workflow/runs", h.ListRuns)
	r.Route("/workflow/runs", func(r chi.Router) {
		r.Get("/", h.ListRuns)
		r.Get("/{runID}", h.GetRun)
		r.Post("/{runID}/cancel", h.CancelRun)
	})
}
